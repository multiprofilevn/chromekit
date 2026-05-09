package chromekit

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/tuwibu/chromekit/input"
)

// Page is a Chrome tab. Hides chromedp's context plumbing behind methods.
type Page struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	log    Logger
	cfg    *config

	mu    sync.RWMutex
	index int

	inputOnce sync.Once
	backend   input.Backend
	cursor    *input.Cursor
	keyboard  *input.Keyboard
	touch     *input.TouchScreen
}

// ID returns the chromedp target ID for the page.
func (p *Page) ID() string { return p.id }

// Index returns the page's order within the browser.
func (p *Page) Index() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.index
}

// Close terminates the page context. Idempotent.
func (p *Page) Close() {
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
	return chromedp.Run(p.ctx, target.ActivateTarget(target.ID(p.id)))
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

// alive returns ErrContextCanceled if the page context is done.
func (p *Page) alive() error {
	select {
	case <-p.ctx.Done():
		return ErrContextCanceled
	default:
		return nil
	}
}
