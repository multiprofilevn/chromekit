package chromekit

import (
	"github.com/chromedp/chromedp"
)

// ensureAttached materialises this page's CDP session, exactly once and never
// from two goroutines at the same time.
//
// chromedp binds a context to its target lazily, on the first chromedp.Run
// against it. That Run stores the session in Context.Target — a field guarded
// by no lock at all — while the cancellation goroutine chromedp.NewContext
// spawns reads the same field when the context dies, re-reading it between
// statements. So two goroutines racing the lazy first Run (a page operation and
// a screenshot, an attach and a teardown) can have the loser store nil after a
// failed attach while the winner's value has already passed the reader's nil
// check. The reader then dereferences nil inside chromedp, which panics on a
// goroutine nobody can recover — the whole process dies.
//
// Serialising the first Run removes the race outright: Run re-attaches only
// while Target is nil, so once a single owner has driven it to a live session
// it never moves back, and the cancellation goroutine cannot observe a value
// turning nil under it.
//
// The attach MUST use p.ctx and never a deadline-bounded child: chromedp hands
// the very context it attached with to the target's event loop, so a timeout
// child would tear the session down when it expired.
func (p *Page) ensureAttached() error {
	p.attachMu.Lock()
	defer p.attachMu.Unlock()
	if p.attached {
		return nil
	}
	if err := chromedp.Run(p.ctx); err != nil {
		return err
	}
	p.attached = true
	return nil
}
