package chromekit

import (
	"context"
	"fmt"

	"github.com/chromedp/chromedp"
)

// EvalSelector queries the first element matching sel and runs fnBody on it.
// fnBody must be a JS arrow function source (e.g. "el => el.innerText"). The
// result is decoded into out via JSON; pass nil to discard.
//
// Throws if the selector matches nothing — wrap with WaitForSelector if the
// element appears asynchronously.
func (p *Page) EvalSelector(sel, fnBody string, out any) error {
	if err := p.alive(); err != nil {
		return err
	}
	selJS, err := jsString(sel)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf(`(() => {
		const el = document.querySelector(%s);
		if (!el) throw new Error('selector not found: ' + %s);
		return (%s)(el);
	})()`, selJS, selJS, fnBody)
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(expr, out)); err != nil {
		return fmt.Errorf("%w: eval %q: %s", ErrEvaluate, sel, err.Error())
	}
	return nil
}

// EvaluateCtx is Evaluate with a caller-supplied context for cancellation —
// firefoxkit parity. Values (the chromedp Target executor) still come from
// the page's own context; only the deadline/cancellation races between ctx
// and the page's base context (via mergeCtx). Used by URLLive/TitleLive and
// any future poll loop that wants a sub-deadline finer than the page's own.
func (p *Page) EvaluateCtx(ctx context.Context, expression string, out any) error {
	if err := p.alive(); err != nil {
		return err
	}
	runCtx, cancel := p.mergeCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(expression, out)); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %s", ErrContextCanceled, ctx.Err())
		}
		return fmt.Errorf("%w: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// mergeCtx derives a context that carries p.ctx's chromedp values (the
// Target executor chromedp.Run requires) but whose cancellation follows
// whichever of p.ctx or ext fires first. Needed because chromedp.Run
// requires "a valid chromedp context" (one created via chromedp.NewContext)
// — an arbitrary caller-supplied ctx carries no Target value and can't be
// passed to it directly. The watcher goroutine always exits: either ext
// fires first (cancel propagates), or the returned CancelFunc's close(stop)
// does, whichever comes first — it never outlives the call.
func (p *Page) mergeCtx(ext context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(p.ctx)
	stop := make(chan struct{})
	go func() {
		select {
		case <-ext.Done():
			cancel()
		case <-stop:
		}
	}()
	return child, func() {
		close(stop)
		cancel()
	}
}

// EvalSelectorAll queries every element matching sel and maps jsFn over them.
// jsFn receives one element at a time. Result is an array decoded into out.
// Empty match returns an empty array (not an error).
func (p *Page) EvalSelectorAll(sel, jsFn string, out any) error {
	if err := p.alive(); err != nil {
		return err
	}
	selJS, err := jsString(sel)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf(`Array.from(document.querySelectorAll(%s)).map(%s)`, selJS, jsFn)
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(expr, out)); err != nil {
		return fmt.Errorf("%w: eval-all %q: %s", ErrEvaluate, sel, err.Error())
	}
	return nil
}
