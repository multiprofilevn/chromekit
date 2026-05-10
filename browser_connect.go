package chromekit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Connect attaches to a Chrome instance already running with
// --remote-debugging-port=<port>. The caller owns the Chrome process —
// chromekit will not start or kill it.
//
// Connect attaches to an existing page target via WithTargetID rather than
// letting chromedp spawn a new one. This keeps Chrome's tab count stable and
// ensures the OS window's title bar reflects the tab chromekit drives — which
// is what the Focus()/HWND lookup depends on. If Chrome has zero page-type
// targets (e.g. only DevTools / extensions), fall back to spawning one tab.
func Connect(port int, opts ...Option) (*Browser, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.headless && cfg.inputBackend == BackendNative {
		return nil, fmt.Errorf("%w: native input requires a visible window", ErrInvalidConfig)
	}

	wsURL, err := fetchBrowserWS(port)
	if err != nil {
		return nil, err
	}
	targets, err := fetchPageTargets(port)
	if err != nil {
		return nil, fmt.Errorf("%w: list targets: %s", ErrInvalidConfig, err.Error())
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	b := &Browser{
		cfg:         cfg,
		log:         cfg.logger,
		allocCancel: allocCancel,
		attached:    true,
		port:        port,
	}

	if len(targets) == 0 {
		// Empty Chrome — chromedp will spawn one tab on Run().
		ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(cfg.logger.Debugf))
		if err := chromedp.Run(ctx); err != nil {
			cancel()
			allocCancel()
			return nil, fmt.Errorf("%w: connect (spawn): %s", ErrInvalidConfig, err.Error())
		}
		b.ctx = ctx
		b.cancel = cancel
		// Refresh target list so attachAdditionalPages picks up the spawned tab.
		spawned, _ := fetchPageTargets(port)
		b.attachKnownTargets(spawned, "")
		return b, nil
	}

	// Attach to the most recently created page target. Chrome's /json/list is
	// ordered most-recent-first; pick index 0 so we land on whatever tab was
	// last touched (typically what the user expects "the active tab" to be).
	primary := targets[0]
	primaryCtx, primaryCancel := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(target.ID(primary.ID)),
		chromedp.WithLogf(cfg.logger.Debugf),
	)
	if err := chromedp.Run(primaryCtx); err != nil {
		primaryCancel()
		allocCancel()
		return nil, fmt.Errorf("%w: attach target %s: %s", ErrInvalidConfig, primary.ID, err.Error())
	}
	b.ctx = primaryCtx
	b.cancel = primaryCancel
	b.attachKnownTargets(targets, primary.ID)
	return b, nil
}

// attachKnownTargets wraps page targets into chromekit Pages. The target with
// id == primaryID is wrapped as the b.ctx-anchored Page (already attached);
// every other target gets its own sub-context.
func (b *Browser) attachKnownTargets(targets []targetEntry, primaryID string) {
	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	for _, t := range targets {
		if t.ID == primaryID {
			p := &Page{
				id:      primaryID,
				ctx:     b.ctx,
				cancel:  b.cancel,
				log:     b.log,
				cfg:     b.cfg,
				browser: b,
				index:   b.pageCount,
			}
			b.pages = append(b.pages, p)
			b.pageCount++
			b.current = p
			wirePage(p)
			continue
		}
		ctx, cancel := chromedp.NewContext(b.ctx, chromedp.WithTargetID(target.ID(t.ID)))
		p := &Page{
			id:      t.ID,
			ctx:     ctx,
			cancel:  cancel,
			log:     b.log,
			cfg:     b.cfg,
			browser: b,
			index:   b.pageCount,
		}
		b.pages = append(b.pages, p)
		b.pageCount++
		// b.current already points at primary — leave it.
		if b.current == nil {
			b.current = p
		}
		wirePage(p)
	}
}

// fetchBrowserWS hits http://localhost:<port>/json/version and returns the
// browser-level WebSocket URL chromedp's RemoteAllocator wants.
func fetchBrowserWS(port int) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidConfig, err.Error())
	}
	defer resp.Body.Close()

	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", fmt.Errorf("%w: decode /json/version: %s", ErrInvalidConfig, err.Error())
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%w: empty webSocketDebuggerUrl on port %d", ErrInvalidConfig, port)
	}
	return v.WebSocketDebuggerURL, nil
}

// targetEntry mirrors the page-target shape returned by /json/list.
type targetEntry struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// fetchPageTargets enumerates Chrome targets via raw /json/list and returns
// only entries with type == "page" in source order.
func fetchPageTargets(port int) ([]targetEntry, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/list", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var all []targetEntry
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("decode /json/list: %w", err)
	}
	pages := make([]targetEntry, 0, len(all))
	for _, t := range all {
		if t.Type == "page" {
			pages = append(pages, t)
		}
	}
	return pages, nil
}
