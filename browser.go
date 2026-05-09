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
func Launch(opts ...Option) (*Browser, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.headless && cfg.inputBackend == BackendNative {
		return nil, fmt.Errorf("%w: native input requires a visible window (headless=false)", ErrInvalidConfig)
	}

	allocOpts := buildAllocatorOptions(cfg)
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
	if _, err := b.NewPage(); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

func buildAllocatorOptions(cfg *config) []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
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
	return opts
}

// Close cancels every page, then the browser, then the allocator.
// Order matters — reversing it leaks chromedp goroutines.
//
// CAVEAT: when the Browser was built via Connect, calling Close exits the
// chrome process. chromedp's RemoteAllocator hardcodes a cancel-handler that
// sends Browser.close on ctx.Done(), and there is no public API to opt out.
// Working around it would require a custom allocator (~100 LOC). Until that
// lands, dev workflows that want to keep chrome alive across Connect cycles
// must launch chrome with two tabs (so closing one doesn't exit the process)
// or accept the re-launch cost.
func (b *Browser) Close() {
	b.pagesMu.Lock()
	for i := len(b.pages) - 1; i >= 0; i-- {
		if b.pages[i].cancel != nil {
			b.pages[i].cancel()
		}
	}
	b.pages = nil
	b.current = nil
	b.pagesMu.Unlock()

	if b.cancel != nil {
		b.cancel()
	}
	if b.allocCancel != nil {
		b.allocCancel()
	}
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
