package chromekit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/tuwibu/chromekit/input"
)

// Page is a Chrome tab. Hides chromedp's context plumbing behind methods.
type Page struct {
	id      string
	ctx     context.Context
	cancel  context.CancelFunc
	log     Logger
	cfg     *config
	browser *Browser // back-ref for ContentOffset lookup; may be nil pre-Connect

	// primary marks the page whose ctx/cancel ARE the Browser's own — the
	// target chromedp bound when the connection opened. Cancelling it tears
	// down every other tab and, on the Launch path, the Chrome process, so
	// close paths must refuse it. See canClosePage.
	primary bool

	// owned is true when chromekit opened this tab (NewPage, or a popup
	// WaitForNewPage attached). False for tabs that already existed when
	// Connect ran — those belong to the user and must survive Close().
	owned bool

	mu    sync.RWMutex
	index int

	inputOnce sync.Once
	backend   input.Backend
	cursor    *input.Cursor
	keyboard  *input.Keyboard
	touch     *input.TouchScreen

	listenersOnce  sync.Once
	listenerMu     sync.RWMutex
	dialogHandler  func(DialogEvent) DialogAction
	consoleHandler func(ConsoleEvent)
	consoleSem     chan struct{} // bounds in-flight console handlers
	downloadCh     chan downloadEvent

	frameMu      sync.RWMutex
	frameCtxByID map[string]int64 // frameID → execution context ID (latest)
	framesCache  []*Frame
	framesDirty  bool
	frameGen     uint64 // bumped on every frame-tree invalidation

	cookiesAPI   *CookiesAPI
	mouseAPI     *MouseAPI
	keyboardAPI  *KeyboardAPI
	downloadsAPI *DownloadsAPI
}

// Cookies returns the cookie operations namespace for this page.
func (p *Page) Cookies() *CookiesAPI { return p.cookiesAPI }

// Mouse returns the low-level mouse operations namespace for this page.
// High-level selector-based shortcuts (Click, ClickElement) stay on Page.
func (p *Page) Mouse() *MouseAPI { return p.mouseAPI }

// Keyboard returns the keyboard operations namespace for this page.
// The selector-based shortcut p.Type(sel, text) stays on Page.
func (p *Page) Keyboard() *KeyboardAPI { return p.keyboardAPI }

// Downloads returns the download operations namespace for this page.
func (p *Page) Downloads() *DownloadsAPI { return p.downloadsAPI }

// ID returns the chromedp target ID for the page.
func (p *Page) ID() string { return p.id }

// Ctx returns the underlying chromedp tab context. Use for raw cdproto /
// chromedp.Run calls when a public method doesn't cover the operation
// (e.g. page.AddScriptToEvaluateOnNewDocument, webauthn.Enable). Caller MUST
// NOT cancel the returned context — it is owned by the Page lifecycle.
func (p *Page) Ctx() context.Context { return p.ctx }

// Index returns the page's order within the browser.
func (p *Page) Index() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.index
}

// Close terminates the page context. Idempotent.
//
// No-op on the primary page: its cancel is the Browser's own, so running it
// would kill every tab (and the Chrome process on the Launch path). Use
// Browser.Close to shut the whole browser down.
func (p *Page) Close() {
	if p.primary {
		if p.log != nil {
			p.log.Debugf("chromekit: Page.Close() ignored on primary page %s — use Browser.Close()", p.id)
		}
		return
	}
	// Go through the browser so the page also leaves b.pages — cancelling
	// alone would leave a dead entry that Pages() keeps handing out.
	if p.browser != nil {
		p.browser.removePage(p.id)
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
}

// BringToFront activates the tab in Chrome's UI so it becomes the visible /
// foreground tab. Required before native input — when the tab is in the
// background, the OS window's title bar reflects another tab and HWND lookup
// by title misses.
func (p *Page) BringToFront() error {
	if err := p.alive(); err != nil {
		return err
	}
	// page.BringToFront (CDP Page.bringToFront) is the reliable primitive — it
	// brings the page to the foreground at the page level. target.ActivateTarget
	// alone often no-ops when session-restore / extension welcome tabs race
	// against the activation. Call both: page-level first (the workhorse), then
	// target-level (keeps tab-strip selection in sync for native-input HWND
	// lookups). Errors on target.ActivateTarget are non-fatal.
	if err := chromedp.Run(p.ctx, page.BringToFront()); err != nil {
		return fmt.Errorf("%w: bring to front: %s", ErrEvaluate, err.Error())
	}
	// page.BringToFront / target.ActivateTarget return on CDP ACK, NOT on the
	// visual tab switch. Block until the tab is actually the visible one
	// (document.visibilityState === 'visible') before returning — otherwise
	// native input fires on whatever tab is still foreground. Re-issue
	// activation each miss; fail loud on timeout (never silently proceed).
	activate := func(c context.Context) error {
		return chromedp.Run(c, target.ActivateTarget(target.ID(p.id)))
	}
	probe := func(c context.Context) (string, error) {
		var s string
		err := chromedp.Run(c, chromedp.Evaluate("document.visibilityState", &s))
		return s, err
	}
	return awaitTabVisible(p.ctx, activate, probe, 3*time.Second, 75*time.Millisecond)
}

// awaitTabVisible blocks until probe reports "visible", re-issuing activate on
// every miss, until deadline. Pure: no globals, no chromedp — all I/O via the
// injected closures, so the determinism guard is testable without a live
// browser. Returns wrapped ErrTabActivateTimeout on deadline, ctx.Err() on
// cancellation. NEVER returns nil unless the tab was confirmed visible.
func awaitTabVisible(
	ctx context.Context,
	activate func(context.Context) error,
	probe func(context.Context) (string, error),
	deadline time.Duration,
	step time.Duration,
) error {
	end := time.Now().Add(deadline)
	for {
		_ = activate(ctx)
		if vis, _ := probe(ctx); vis == "visible" {
			return nil
		}
		if !time.Now().Before(end) {
			return fmt.Errorf("%w: tab not visible after %s", ErrTabActivateTimeout, deadline)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(step):
		}
	}
}

// GetURL returns the current document URL.
func (p *Page) GetURL() (string, error) {
	if err := p.alive(); err != nil {
		return "", err
	}
	var s string
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(`window.location.href`, &s)); err != nil {
		return "", fmt.Errorf("%w: %s", ErrEvaluate, err.Error())
	}
	return s, nil
}

// GetOrigin returns window.location.origin.
func (p *Page) GetOrigin() (string, error) {
	if err := p.alive(); err != nil {
		return "", err
	}
	var s string
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(`window.location.origin`, &s)); err != nil {
		return "", fmt.Errorf("%w: %s", ErrEvaluate, err.Error())
	}
	return s, nil
}

// GetTitle returns document.title.
func (p *Page) GetTitle() (string, error) {
	if err := p.alive(); err != nil {
		return "", err
	}
	var s string
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(`document.title`, &s)); err != nil {
		return "", fmt.Errorf("%w: %s", ErrEvaluate, err.Error())
	}
	return s, nil
}

// DefaultActionTimeout bounds page operations whose signature has no timeout
// parameter (Click, Type, and the action half of Navigate & friends). It is
// deliberately generous: it exists to turn "hangs until the browser dies" into
// "fails after 30 seconds", not to police slow-but-healthy pages.
const DefaultActionTimeout = 30 * time.Second

// opCtx derives a deadline-bounded child of the page context. timeout <= 0
// means DefaultActionTimeout. Caller MUST call the returned cancel.
func opCtx(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultActionTimeout
	}
	return context.WithTimeout(parent, timeout)
}

// alive returns ErrContextCanceled if the page context is done.
func (p *Page) alive() error {
	select {
	case <-p.ctx.Done():
		return ErrContextCanceled
	default:
		return nil
	}
}
