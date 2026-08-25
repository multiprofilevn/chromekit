package chromekit

import (
	"encoding/json"

	"github.com/chromedp/cdproto/browser"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// downloadEvent carries one stage of a download lifecycle to WaitForDownload.
// Filename is set on the begin event; Done flips true on completion or
// cancellation; Err is set when the download was canceled.
type downloadEvent struct {
	Filename string
	Done     bool
	Err      error
}

// initListeners enables the Page + Runtime domains and wires a single
// chromedp.ListenTarget that dispatches every event chromekit cares about
// (dialog, console, download). Bind to p.ctx → cancellation when the page
// closes also unsubscribes the listener, no goroutine leak.
//
// Idempotent: sync.Once guards the enable + listen registration. Safe to call
// from any feature module (OnDialog, OnConsole, SetDownloadPath).
func (p *Page) initListeners() {
	p.listenersOnce.Do(func() {
		// Attach through the guard first: this used to be the very first Run on
		// a freshly created page context, i.e. the lazy attach itself, which
		// must never race another one (see ensureAttached). Enabling the
		// domains only makes sense once that succeeded anyway.
		if err := p.ensureAttached(); err == nil {
			_ = chromedp.Run(p.ctx,
				cdppage.Enable(),
				runtime.Enable(),
			)
		}
		chromedp.ListenTarget(p.ctx, p.dispatchEvent)
	})
}

func (p *Page) dispatchEvent(ev any) {
	switch e := ev.(type) {
	case *cdppage.EventJavascriptDialogOpening:
		p.handleDialog(e)
	case *runtime.EventConsoleAPICalled:
		p.handleConsole(e)
	case *browser.EventDownloadWillBegin:
		p.handleDownloadBegin(e)
	case *browser.EventDownloadProgress:
		p.handleDownloadProgress(e)
	case *cdppage.EventFrameDetached:
		p.detachFrame(string(e.FrameID))
	case *cdppage.EventFrameAttached, *cdppage.EventFrameNavigated:
		p.markFramesDirty()
	case *runtime.EventExecutionContextCreated:
		p.recordContext(e)
	case *runtime.EventExecutionContextDestroyed:
		p.dropContext(e)
	case *runtime.EventExecutionContextsCleared:
		p.clearContexts()
	}
}

func (p *Page) markFramesDirty() {
	p.frameMu.Lock()
	p.framesDirty = true
	// Bumped on every invalidation so rebuildFrames can tell whether the tree
	// changed while it was fetching, and refuse to mark a stale result clean.
	p.frameGen++
	p.frameMu.Unlock()
}

// detachFrame marks a frame gone so IsAttached reports it, rather than leaving
// every cached frame permanently "attached".
func (p *Page) detachFrame(id string) {
	p.frameMu.Lock()
	for _, f := range p.framesCache {
		if f.id == id {
			f.attached = false
		}
	}
	p.framesDirty = true
	p.frameGen++
	p.frameMu.Unlock()
}

func (p *Page) recordContext(ev *runtime.EventExecutionContextCreated) {
	if ev == nil || ev.Context == nil {
		return
	}
	frameID := frameIDFromAuxData([]byte(ev.Context.AuxData))
	if frameID == "" {
		return
	}
	p.frameMu.Lock()
	if p.frameCtxByID == nil {
		p.frameCtxByID = map[string]int64{}
	}
	p.frameCtxByID[frameID] = int64(ev.Context.ID)
	p.frameMu.Unlock()
}

func (p *Page) dropContext(ev *runtime.EventExecutionContextDestroyed) {
	if ev == nil {
		return
	}
	p.frameMu.Lock()
	for fid, cid := range p.frameCtxByID {
		if cid == int64(ev.ExecutionContextID) {
			delete(p.frameCtxByID, fid)
		}
	}
	p.frameMu.Unlock()
}

func (p *Page) clearContexts() {
	p.frameMu.Lock()
	p.frameCtxByID = nil
	p.frameMu.Unlock()
}

// frameIDFromAuxData reads the frameId out of a context's auxData JSON
// payload. CDP shape: {"frameId": "...", "isDefault": true, "type": "default"}.
func frameIDFromAuxData(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var aux struct {
		FrameID string `json:"frameId"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return ""
	}
	return aux.FrameID
}

func (p *Page) handleDialog(ev *cdppage.EventJavascriptDialogOpening) {
	p.listenerMu.RLock()
	h := p.dialogHandler
	p.listenerMu.RUnlock()

	e := DialogEvent{
		Type:          DialogType(ev.Type),
		Message:       ev.Message,
		DefaultPrompt: ev.DefaultPrompt,
	}
	// The user handler runs on its own goroutine, NOT on chromedp's event-
	// dispatch goroutine. Calling it inline deadlocks the moment a handler
	// touches the page (Evaluate, GetURL): the response it waits for can only
	// be processed by the very goroutine the handler is blocking. That freeze
	// takes the whole tab with it — every pending CDP reply, including the
	// mouse-move acknowledgements of an in-flight gesture.
	go func() {
		// Skip if the page is already gone — the dialog reply below would fail
		// on a dead context anyway, and there is no point running the handler.
		if p.ctx.Err() != nil {
			return
		}
		action := DialogAction{Accept: false}
		if h != nil {
			action = h(e)
		}
		req := cdppage.HandleJavaScriptDialog(action.Accept)
		if action.PromptText != "" {
			req = req.WithPromptText(action.PromptText)
		}
		_ = chromedp.Run(p.ctx, req)
	}()
}

// maxConsoleTextLen caps a single console message. Page scripts choose both
// how many messages to emit and how big each one is; without a cap, one
// console.log of a huge string is an unbounded allocation triggered from the
// page.
const maxConsoleTextLen = 4096

func (p *Page) handleConsole(ev *runtime.EventConsoleAPICalled) {
	p.listenerMu.RLock()
	h := p.consoleHandler
	sem := p.consoleSem
	p.listenerMu.RUnlock()
	if h == nil {
		return
	}
	parts := make([]string, 0, len(ev.Args))
	for _, a := range ev.Args {
		parts = append(parts, formatRemoteObject(a))
	}
	text := truncateConsoleText(joinSpace(parts))
	e := ConsoleEvent{Level: mapConsoleLevel(string(ev.Type)), Text: text}

	// Don't spawn a handler for a page that's already tearing down.
	if p.ctx.Err() != nil {
		return
	}
	// Bounded fan-out. A page in a console.log loop would otherwise spawn a
	// goroutine per message; drop instead, the way DevTools does.
	select {
	case sem <- struct{}{}:
		go func() {
			defer func() { <-sem }()
			h(e)
		}()
	default:
	}
}

func truncateConsoleText(s string) string {
	if len(s) <= maxConsoleTextLen {
		return s
	}
	return s[:maxConsoleTextLen] + "…(truncated)"
}

// formatRemoteObject renders one runtime.RemoteObject the way DevTools'
// console does — primitives by their value, objects via their JSON
// preview, fallback to the type name.
func formatRemoteObject(o *runtime.RemoteObject) string {
	if o == nil {
		return ""
	}
	if len(o.Value) > 0 {
		var v any
		if err := json.Unmarshal(o.Value, &v); err == nil {
			if s, ok := v.(string); ok {
				return s
			}
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	if o.Description != "" {
		return o.Description
	}
	return string(o.Type)
}

func joinSpace(parts []string) string {
	out := ""
	for i, s := range parts {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

// downloadChan snapshots the download channel under the listener lock so
// dispatch never races SetPath swapping it.
func (p *Page) downloadChan() chan downloadEvent {
	p.listenerMu.RLock()
	defer p.listenerMu.RUnlock()
	return p.downloadCh
}

func (p *Page) handleDownloadBegin(ev *browser.EventDownloadWillBegin) {
	ch := p.downloadChan()
	if ch == nil {
		return
	}
	select {
	case ch <- downloadEvent{Filename: ev.SuggestedFilename}:
	default: // channel full — caller didn't WaitForDownload, drop and continue
	}
}

func (p *Page) handleDownloadProgress(ev *browser.EventDownloadProgress) {
	ch := p.downloadChan()
	if ch == nil {
		return
	}
	switch ev.State {
	case browser.DownloadProgressStateCompleted:
		select {
		case ch <- downloadEvent{Done: true}:
		default:
		}
	case browser.DownloadProgressStateCanceled:
		select {
		case ch <- downloadEvent{Done: true, Err: ErrDownloadCanceled}:
		default:
		}
	}
}

func mapConsoleLevel(t string) ConsoleLevel {
	switch t {
	case "warning":
		return ConsoleWarn
	case "error":
		return ConsoleError
	case "info":
		return ConsoleInfo
	case "debug":
		return ConsoleDebug
	default:
		return ConsoleLog
	}
}
