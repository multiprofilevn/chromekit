package chromekit

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/chromedp"
)

// Browser owns a Chrome process and the pages opened on it.
type Browser struct {
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
	cfg         *config
	log         Logger

	pagesMu   sync.RWMutex
	pages     []*Page
	current   *Page
	pageCount int

	hwndMu     sync.Mutex
	cachedHWND uintptr

	contentMu      sync.Mutex
	contentOffsetX int
	contentOffsetY int
	contentCached  bool

	// attached is true when this Browser was built via Connect (chromekit
	// does NOT own the chrome process). Close() then detaches the targets
	// instead of closing them, leaving the chrome process running.
	attached bool

	// port is the CDP remote-debugging port from Connect. Used by HWND PID
	// lookup to find the chrome main process. 0 for Launch path.
	port int

	// pid is the chrome browser process PID for Launch path (chromekit
	// spawned it). 0 for Connect path — looked up from port at HWND time.
	pid uint32
}

// Launch starts Chrome with the given options and opens the first blank page.
// Caller MUST defer b.Close() to avoid leaking the Chrome process.
//
// chromePath is the absolute path to the Chrome / Chromium binary. Pass an
// empty string to fall back to chromedp's default `exec.LookPath("chrome")`,
// which often fails on Windows where chrome.exe is not on PATH — pass the
// real path explicitly when you can.
func Launch(chromePath string, opts ...Option) (*Browser, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.headless && cfg.inputBackend == BackendNative {
		return nil, fmt.Errorf("%w: native input requires a visible window (headless=false)", ErrInvalidConfig)
	}

	allocOpts := buildAllocatorOptions(chromePath, cfg)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(cfg.logger.Debugf))
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return nil, fmt.Errorf("%w: launch: %s", ErrInvalidConfig, err.Error())
	}

	b := &Browser{
		ctx:         ctx,
		cancel:      cancel,
		allocCancel: allocCancel,
		cfg:         cfg,
		log:         cfg.logger,
	}
	// Capture chrome PID from chromedp's allocator. Required for HWND lookup
	// via PID enum — without it Focus falls back to page-title heuristic which
	// fails on blank pages (the typical state at automation start).
	if c := chromedp.FromContext(ctx); c != nil && c.Browser != nil {
		if p := c.Browser.Process(); p != nil {
			b.pid = uint32(p.Pid)
		}
	}
	if _, err := b.NewPage(); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

func buildAllocatorOptions(chromePath string, cfg *config) []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	if chromePath != "" {
		// chromedp.ExecPath overrides the default exec.LookPath("chrome").
		// Place after defaults so it wins ordering-wise.
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	opts = append(opts, chromedp.Flag("headless", cfg.headless))
	if cfg.userDataDir != "" {
		opts = append(opts, chromedp.UserDataDir(cfg.userDataDir))
	}
	if cfg.proxy != "" {
		opts = append(opts, chromedp.ProxyServer(cfg.proxy))
	}
	for k, v := range cfg.flags {
		opts = append(opts, chromedp.Flag(k, v))
	}
	if cfg.modifyCmd != nil {
		opts = append(opts, chromedp.ModifyCmdFunc(cfg.modifyCmd))
	}
	return opts
}

// Close cancels every page, then the browser, then (for Launch) the
// allocator. Order matters — reversing it leaks chromedp goroutines.
//
// For Connect (b.attached), the allocator cancel is INTENTIONALLY skipped.
// chromedp's RemoteAllocator hardcodes a cancel-handler that sends CDP
// Browser.close on ctx.Done(), which exits the entire chrome process —
// hostile when chromekit doesn't own that process. Skipping allocCancel
// keeps the chrome process alive at the cost of leaking 1 goroutine + 1
// browser-level WebSocket per Connect/Close cycle. Acceptable for typical
// dev workflows; long-lived processes that loop Connect/Close many times
// should reuse a single Browser instead. A real fix needs a custom
// allocator with chromedp's setContext (currently unexported).
func (b *Browser) Close() {
	b.pagesMu.Lock()
	pages := b.pages
	b.pages = nil
	b.current = nil
	b.pagesMu.Unlock()

	if b.attached {
		// Cancelling ANY chromedp context derived from this Browser closes
		// the browser-level WebSocket, which trips chromedp's
		// LostConnection handler -> Cancel(ctx) -> CDP Browser.close ->
		// chrome process exit. To keep chrome alive we drop references and
		// let GC reclaim the goroutines. One goroutine + one WebSocket
		// leak per Connect/Close cycle. Acceptable for typical dev use;
		// production loops should reuse a single Browser.
		_ = pages
		return
	}

	for i := len(pages) - 1; i >= 0; i-- {
		if pages[i].cancel != nil {
			pages[i].cancel()
		}
	}
	if b.cancel != nil {
		b.cancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
}

// Done returns a channel that closes when the browser context is cancelled —
// either via Close() or because the underlying chromedp context died (chrome
// process exited / crashed). Caller can <-Done() to wait for chrome lifetime.
func (b *Browser) Done() <-chan struct{} {
	if b == nil || b.ctx == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return b.ctx.Done()
}

// PID returns the chrome browser process id captured at Launch. 0 for the
// Connect path (chromekit didn't spawn the process — caller may know the pid
// from elsewhere).
func (b *Browser) PID() uint32 {
	if b == nil {
		return 0
	}
	return b.pid
}

// Current returns the active page (last opened or last switched-to).
func (b *Browser) Current() *Page {
	b.pagesMu.RLock()
	defer b.pagesMu.RUnlock()
	return b.current
}

// Pages returns a snapshot copy of all pages owned by this browser.
func (b *Browser) Pages() []*Page {
	b.pagesMu.RLock()
	defer b.pagesMu.RUnlock()
	out := make([]*Page, len(b.pages))
	copy(out, b.pages)
	return out
}

// GoBack navigates the current page back in history.
func (b *Browser) GoBack() error {
	p := b.Current()
	if p == nil {
		return ErrPageNotFound
	}
	return p.GoBack(0)
}
