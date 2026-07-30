package chromekit

import "context"

// URL returns the cached URL, populated only by URLLive. chromekit has no
// per-navigation event wiring (unlike firefoxkit's still-pending
// browsingContext.navigationStarted listener), so this is NOT refreshed by
// Navigate — call URLLive for an authoritative value. Empty until the first
// URLLive call.
func (p *Page) URL() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.url
}

// Title returns the cached title, populated only by TitleLive. Same
// cache-vs-live split as URL/URLLive — mirrors firefoxkit's contract
// exactly (its own Title() is likewise refreshed only by TitleLive).
func (p *Page) Title() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.title
}

// URLLive queries window.location.href fresh via CDP and updates the cache
// that URL() reads.
func (p *Page) URLLive(ctx context.Context) (string, error) {
	var s string
	if err := p.EvaluateCtx(ctx, `window.location.href`, &s); err != nil {
		return "", err
	}
	p.mu.Lock()
	p.url = s
	p.mu.Unlock()
	return s, nil
}

// TitleLive queries document.title fresh via CDP and updates the cache that
// Title() reads.
func (p *Page) TitleLive(ctx context.Context) (string, error) {
	var s string
	if err := p.EvaluateCtx(ctx, `document.title`, &s); err != nil {
		return "", err
	}
	p.mu.Lock()
	p.title = s
	p.mu.Unlock()
	return s, nil
}
