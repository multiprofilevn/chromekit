package chromekit

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

const defaultWaitTimeout = 30 * time.Second

// WaitForReady blocks until document.readyState == "complete" or timeout.
// timeout=0 → defaultWaitTimeout (30s).
func (p *Page) WaitForReady(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	for {
		var ready bool
		err := chromedp.Run(ctx, chromedp.Evaluate(`document.readyState === "complete"`, &ready))
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("%w: wait ready", ErrTimeout)
			}
			return fmt.Errorf("%w: %s", ErrEvaluate, err.Error())
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: wait ready", ErrTimeout)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// WaitForChangeURL blocks until window.location.href differs from what it was
// at call time AND document.readyState=="complete" — a polling-based
// alternative to Puppeteer's event-driven waitForNavigation. Survives
// cross-origin navigation (the page target detaches briefly while the new
// document loads; transient Evaluate failures don't bail).
//
// Trigger the navigation BEFORE calling this — it snapshots the URL on entry
// and waits for it to change.
func (p *Page) WaitForChangeURL(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	initial, _ := p.GetURL() // ignore err — empty initial still works (any non-empty url counts as changed)

	return p.pollUntil(timeout, func(ctx context.Context) (bool, error) {
		var url string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.location.href`, &url)); err != nil {
			// Cross-origin nav causes transient detach — keep polling.
			return false, nil
		}
		if url == "" || url == initial {
			return false, nil
		}
		var ready bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.readyState === "complete"`, &ready)); err != nil {
			return false, nil
		}
		return ready, nil
	})
}

// WaitForFunction polls a JS expression until it returns truthy or timeout.
// expr is evaluated as `!!(expr)`, so any truthy value works.
func (p *Page) WaitForFunction(expr string, timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	wrapped := "!!(" + expr + ")"
	return p.pollUntil(timeout, func(ctx context.Context) (bool, error) {
		var ok bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(wrapped, &ok)); err != nil {
			return false, nil
		}
		return ok, nil
	})
}

// pollUntil ticks every 100ms and returns when check reports true. Errors from
// check abort the poll; a nil error with false keeps polling. Returns
// ErrTimeout if the deadline expires first.
func (p *Page) pollUntil(timeout time.Duration, check func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()
	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: poll", ErrTimeout)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// WaitForSelectorGone blocks until the selector either no longer matches an
// element or the matched element is detached / hidden (offsetParent == null).
// Built on WaitForFunction.
func (p *Page) WaitForSelectorGone(selector string, timeout time.Duration) error {
	expr := fmt.Sprintf(`(()=>{const e=document.querySelector(%q); return !e || e.offsetParent==null;})()`, selector)
	return p.WaitForFunction(expr, timeout)
}

// WaitForSelector polls until the selector matches a visible element or timeout.
func (p *Page) WaitForSelector(selector string, timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		return !!el;
	})()`, selector)

	for {
		var found bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(js, &found))
		if found {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: selector %q", ErrTimeout, selector)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
