package chromekit

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// NewPage opens a new about:blank tab and makes it current.
func (b *Browser) NewPage() (*Page, error) {
	if b.ctx.Err() != nil {
		return nil, ErrContextCanceled
	}

	var tid target.ID
	err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		tid, err = target.CreateTarget("about:blank").Do(ctx)
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("%w: new page: %s", ErrNavigate, err.Error())
	}

	pageCtx, pageCancel := chromedp.NewContext(b.ctx, chromedp.WithTargetID(tid))

	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	p := &Page{
		id:      string(tid),
		ctx:     pageCtx,
		cancel:  pageCancel,
		log:     b.log,
		cfg:     b.cfg,
		browser: b,
		index:   b.pageCount,
	}
	b.pages = append(b.pages, p)
	b.pageCount++
	b.current = p
	wirePage(p)
	return p, nil
}

// wirePage attaches the namespace API holders and starts the cross-cutting
// listeners on a freshly constructed Page. Both NewPage and Connect's
// attachKnownTargets MUST run this — without it page.Mouse(), .Keyboard(),
// .Cookies(), .Downloads() return nil and panic on first call.
func wirePage(p *Page) {
	attachPageNamespaces(p)
	p.initListeners()
}

// attachPageNamespaces wires only the API holder pointers — pure assignment,
// no chromedp side-effects. Split out from wirePage so tests can assert the
// invariant without needing a live CDP connection.
func attachPageNamespaces(p *Page) {
	p.cookiesAPI = &CookiesAPI{p: p}
	p.mouseAPI = &MouseAPI{p: p}
	p.keyboardAPI = &KeyboardAPI{p: p}
	p.downloadsAPI = &DownloadsAPI{p: p}
}

// ClosePage cancels the page with the given target ID and removes it from the
// browser. If it was current, the next page in the slice (if any) becomes current.
func (b *Browser) ClosePage(id string) error {
	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()

	idx := -1
	for i, p := range b.pages {
		if p.id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: id=%s", ErrPageNotFound, id)
	}

	closed := b.pages[idx]
	closed.cancel()
	b.pages = append(b.pages[:idx], b.pages[idx+1:]...)

	if b.current == closed {
		if len(b.pages) > 0 {
			b.current = b.pages[len(b.pages)-1]
		} else {
			b.current = nil
		}
	}
	return nil
}

// SwitchPageByIndex makes the page at index the current page.
func (b *Browser) SwitchPageByIndex(index int) error {
	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	if index < 0 || index >= len(b.pages) {
		return fmt.Errorf("%w: index=%d", ErrPageNotFound, index)
	}
	b.current = b.pages[index]
	return b.current.BringToFront()
}

// SwitchPageByURL switches to the first page whose current URL matches.
func (b *Browser) SwitchPageByURL(url string) error {
	for _, p := range b.Pages() {
		u, err := p.GetURL()
		if err != nil {
			continue
		}
		if u == url {
			b.pagesMu.Lock()
			b.current = p
			b.pagesMu.Unlock()
			return p.BringToFront()
		}
	}
	return fmt.Errorf("%w: url=%s", ErrPageNotFound, url)
}

// SwitchPageByOrigin switches to a page whose origin matches the current one.
func (b *Browser) SwitchPageByOrigin() error {
	cur := b.Current()
	if cur == nil {
		return ErrPageNotFound
	}
	origin, err := cur.GetOrigin()
	if err != nil {
		return err
	}
	for _, p := range b.Pages() {
		if p == cur {
			continue
		}
		o, err := p.GetOrigin()
		if err != nil {
			continue
		}
		if o == origin {
			b.pagesMu.Lock()
			b.current = p
			b.pagesMu.Unlock()
			return p.BringToFront()
		}
	}
	return fmt.Errorf("%w: origin=%s", ErrPageNotFound, origin)
}

// ClosePagesNotMatching closes every page whose URL is not equal to keep.
func (b *Browser) ClosePagesNotMatching(keep string) error {
	for _, p := range b.Pages() {
		u, err := p.GetURL()
		if err != nil {
			continue
		}
		if u != keep {
			_ = b.ClosePage(p.id)
		}
	}
	return nil
}
