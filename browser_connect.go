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
// chromekit will not start or kill it. Existing tabs become Pages; one is
// picked as Current. If there are none, a blank tab is opened.
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

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(cfg.logger.Debugf))
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return nil, fmt.Errorf("%w: connect: %s", ErrInvalidConfig, err.Error())
	}

	b := &Browser{
		ctx:         ctx,
		cancel:      cancel,
		allocCancel: allocCancel,
		cfg:         cfg,
		log:         cfg.logger,
	}
	if err := b.attachExistingPages(); err != nil {
		b.Close()
		return nil, err
	}
	if b.Current() == nil {
		if _, err := b.NewPage(); err != nil {
			b.Close()
			return nil, err
		}
	}
	return b, nil
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

// attachExistingPages enumerates Chrome targets and wraps every "page" target
// as a chromekit Page. The most recently visited one becomes current.
func (b *Browser) attachExistingPages() error {
	targets, err := chromedp.Targets(b.ctx)
	if err != nil {
		return fmt.Errorf("%w: list targets: %s", ErrInvalidConfig, err.Error())
	}

	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		p := newAttachedPage(b, t)
		b.pages = append(b.pages, p)
		b.pageCount++
		b.current = p
	}
	return nil
}

func newAttachedPage(b *Browser, t *target.Info) *Page {
	pageCtx, pageCancel := chromedp.NewContext(b.ctx, chromedp.WithTargetID(t.TargetID))
	return &Page{
		id:     string(t.TargetID),
		ctx:    pageCtx,
		cancel: pageCancel,
		log:    b.log,
		cfg:    b.cfg,
		index:  b.pageCount,
	}
}
