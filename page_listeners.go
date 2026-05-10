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
		_ = chromedp.Run(p.ctx,
			cdppage.Enable(),
			runtime.Enable(),
		)
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
	case *cdppage.EventFrameAttached, *cdppage.EventFrameDetached, *cdppage.EventFrameNavigated:
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

	action := DialogAction{Accept: false}
	if h != nil {
		action = h(DialogEvent{
			Type:          DialogType(ev.Type),
			Message:       ev.Message,
			DefaultPrompt: ev.DefaultPrompt,
		})
	}
	// Dispatcher must not block — the JS dialog handler call lives off the
	// event-loop goroutine.
	go func(a DialogAction) {
		req := cdppage.HandleJavaScriptDialog(a.Accept)
		if a.PromptText != "" {
			req = req.WithPromptText(a.PromptText)
		}
		_ = chromedp.Run(p.ctx, req)
	}(action)
}

func (p *Page) handleConsole(ev *runtime.EventConsoleAPICalled) {
	p.listenerMu.RLock()
	h := p.consoleHandler
	p.listenerMu.RUnlock()
	if h == nil {
		return
	}
	parts := make([]string, 0, len(ev.Args))
	for _, a := range ev.Args {
		parts = append(parts, formatRemoteObject(a))
	}
	text := joinSpace(parts)
	level := mapConsoleLevel(string(ev.Type))
	go h(ConsoleEvent{Level: level, Text: text})
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

func (p *Page) handleDownloadBegin(ev *browser.EventDownloadWillBegin) {
	if p.downloadCh == nil {
		return
	}
	select {
	case p.downloadCh <- downloadEvent{Filename: ev.SuggestedFilename}:
	default: // channel full — caller didn't WaitForDownload, drop and continue
	}
}

func (p *Page) handleDownloadProgress(ev *browser.EventDownloadProgress) {
	if p.downloadCh == nil {
		return
	}
	switch ev.State {
	case browser.DownloadProgressStateCompleted:
		select {
		case p.downloadCh <- downloadEvent{Done: true}:
		default:
		}
	case browser.DownloadProgressStateCanceled:
		select {
		case p.downloadCh <- downloadEvent{Done: true, Err: ErrDownloadCanceled}:
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

