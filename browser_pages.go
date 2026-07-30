package chromekit

import (
	"context"
	"fmt"
	"time"

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
	// Re-check under the lock: Close() may have raced in between the top-of-func
	// check and here. Without this, the freshly created target is appended to a
	// b.pages that Close() already nil'd (reviving it), pageCancel is never
	// called, and on the Connect path the tab is a real orphan left in the
	// user's Chrome (Close() doesn't cancel b.ctx there, so CreateTarget above
	// succeeded against a live connection).
	if b.ctx.Err() != nil || b.isClosedLocked() {
		b.pagesMu.Unlock()
		pageCancel()
		b.closeOrphanTarget(tid)
		return nil, ErrContextCanceled
	}
	p := &Page{
		id:      string(tid),
		ctx:     pageCtx,
		cancel:  pageCancel,
		log:     b.log,
		cfg:     b.cfg,
		browser: b,
		index:   b.pageCount,
		owned:   true,
	}
	b.pages = append(b.pages, p)
	b.pageCount++
	b.current = p
	attachPageNamespaces(p)
	b.pagesMu.Unlock()

	// Enabling the CDP domains talks to Chrome — never under pagesMu.
	p.initListeners()
	return p, nil
}

// closeOrphanTarget best-effort closes a target created moments before Close()
// raced in, so an aborted NewPage never strands a tab in a Connect user's
// Chrome. Errors are ignored: b.ctx may already be dead (Launch path), where
// Chrome reaps the tab with the process anyway.
func (b *Browser) closeOrphanTarget(tid target.ID) {
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
	defer cancel()
	_ = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return target.CloseTarget(tid).Do(ctx)
	}))
}

// attachToTarget wraps a known Chrome target ID into a fully wired *Page and
// registers it on the browser. Shared by Launch (primary=true, the target
// chromedp bound on Run) and Connect's attachKnownTargets.
//
// Lock contract: the CALLER must hold b.pagesMu. attachToTarget does NOT lock
// — it mutates b.pages/b.current/b.pageCount under the caller's lock.
//
// primary=true reuses the browser-anchored ctx/cancel (the already-attached
// target) and makes the page current. Non-primary targets get their own
// sub-context and only become current if none is set yet (preserves the
// original attachKnownTargets selection rule).
//
// Sharing the browser's cancel is why the primary page is not closeable on its
// own: ClosePage and Page.Close refuse it, because running that cancel takes
// every sibling tab down and kills the Chrome process on the Launch path.
// Browser.Close() bypasses the guard by cancelling the contexts directly.
func (b *Browser) attachToTarget(tid string, primary, owned bool) (*Page, error) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
	if primary {
		ctx, cancel = b.ctx, b.cancel
	} else {
		ctx, cancel = chromedp.NewContext(b.ctx, chromedp.WithTargetID(target.ID(tid)))
	}
	p := &Page{
		id:      tid,
		ctx:     ctx,
		cancel:  cancel,
		log:     b.log,
		cfg:     b.cfg,
		browser: b,
		index:   b.pageCount,
		primary: primary,
		owned:   owned,
	}
	b.pages = append(b.pages, p)
	b.pageCount++
	if primary || b.current == nil {
		b.current = p
	}
	attachPageNamespaces(p)
	return p, nil
}

// attachPageNamespaces wires the API holder pointers — pure assignment, no
// chromedp side-effects, so it is safe to run under pagesMu. Every attach path
// MUST run it; without it page.Mouse(), .Keyboard(), .Cookies(), .Downloads()
// return nil and panic on first call.
//
// The matching CDP work (p.initListeners) is deliberately NOT here: it runs
// chromedp.Run, which must happen after the caller releases pagesMu.
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
	target := b.pageByIDLocked(id)
	if target == nil {
		b.pagesMu.Unlock()
		return fmt.Errorf("%w: id=%s", ErrPageNotFound, id)
	}
	if err := canClosePage(target.primary, b.attached, len(b.pages)); err != nil {
		b.pagesMu.Unlock()
		return fmt.Errorf("%w: id=%s", err, id)
	}
	closed := b.removePageLocked(id)
	b.pagesMu.Unlock()

	// Cancel outside pagesMu: chromedp's cancel round-trips CDP (detach +
	// close, up to a second on a wedged tab) and holding the lock across that
	// stalls every Pages/Current/NewPage caller in the process.
	closed.cancel()
	return nil
}

// pageByIDLocked returns the tracked page with the given target id, or nil.
// Caller MUST hold b.pagesMu.
func (b *Browser) pageByIDLocked(id string) *Page {
	for _, p := range b.pages {
		if p.id == id {
			return p
		}
	}
	return nil
}

// removePageLocked drops a page from the browser's bookkeeping and returns it
// (nil when the id is not tracked), reassigning current when needed. Caller
// MUST hold b.pagesMu.
//
// It deliberately does NOT cancel the page: cancelling talks to Chrome, and no
// CDP call may run under pagesMu. Callers cancel after releasing the lock.
func (b *Browser) removePageLocked(id string) *Page {
	idx := -1
	for i, p := range b.pages {
		if p.id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	removed := b.pages[idx]
	b.pages = append(b.pages[:idx], b.pages[idx+1:]...)
	if b.current == removed {
		if len(b.pages) > 0 {
			b.current = b.pages[len(b.pages)-1]
		} else {
			b.current = nil
		}
	}
	return removed
}

// removePage evicts a page and cancels its context. No-op for an id that is no
// longer tracked, so it is safe to call twice.
func (b *Browser) removePage(id string) {
	b.pagesMu.Lock()
	removed := b.removePageLocked(id)
	b.pagesMu.Unlock()

	if removed == nil || removed.primary || removed.cancel == nil {
		return
	}
	removed.cancel()
}

// setCurrentIfTracked promotes the page with id to current, but only while it
// is still tracked. Callers that pick a page from a Pages() snapshot need this:
// the page may have been closed between the snapshot and the promotion, and
// pointing current at a cancelled context breaks every later call.
func (b *Browser) setCurrentIfTracked(id string) *Page {
	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	if p := b.pageByIDLocked(id); p != nil {
		b.current = p
		return p
	}
	return nil
}

// canClosePage reports why a page may not be closed via ClosePage, or nil when
// the close is safe.
//
// Two independent guards, checked in this order so the historical last-page
// error keeps its identity on the Launch path:
//
//   - last page + Launch owns the process: cancelling it silently kills Chrome,
//     so the caller must pick Browser.Close() explicitly (F-NEW-2).
//   - primary page: its cancel IS the Browser's cancel, so closing it takes
//     every sibling tab down with it — and on Connect drops the browser-level
//     WebSocket, killing a Chrome process chromekit does not own.
func canClosePage(primary, attached bool, npages int) error {
	if npages == 1 && !attached {
		return ErrCannotCloseLastPage
	}
	if primary {
		return ErrCannotClosePrimaryPage
	}
	return nil
}

// SwitchPageByIndex makes the page at index the current page.
func (b *Browser) SwitchPageByIndex(i int) error {
	p, err := b.setCurrentByIndex(i)
	if err != nil {
		return err
	}
	// Activation is a CDP round-trip that polls for up to three seconds. It
	// runs after the lock is released; see the pagesMu contract.
	return p.BringToFront()
}

// setCurrentByIndex promotes the page at index to current and returns it.
// Holds pagesMu only for the lookup.
func (b *Browser) setCurrentByIndex(index int) (*Page, error) {
	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	if index < 0 || index >= len(b.pages) {
		return nil, fmt.Errorf("%w: index=%d", ErrPageNotFound, index)
	}
	b.current = b.pages[index]
	return b.current, nil
}

// SwitchPageByURL switches to the first page whose current URL matches.
func (b *Browser) SwitchPageByURL(url string) error {
	for _, p := range b.Pages() {
		u, err := p.GetURL()
		if err != nil {
			continue
		}
		if u == url {
			// Re-check under the lock: the page came from a snapshot and may
			// have been closed while we were querying URLs.
			if b.setCurrentIfTracked(p.id) == nil {
				continue
			}
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
			if b.setCurrentIfTracked(p.id) == nil {
				continue
			}
			return p.BringToFront()
		}
	}
	return fmt.Errorf("%w: origin=%s", ErrPageNotFound, origin)
}

// ClosePagesNotMatching closes every page whose URL is not equal to keep.
//
// The primary page is always kept regardless of its URL — closing it would
// take the whole browser down (see canClosePage). Skipping it up front also
// avoids a pointless GetURL round-trip.
func (b *Browser) ClosePagesNotMatching(keep string) error {
	for _, p := range b.Pages() {
		if p.primary {
			continue
		}
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
