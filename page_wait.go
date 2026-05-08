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
