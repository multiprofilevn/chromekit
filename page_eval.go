package chromekit

import (
	"fmt"

	"github.com/chromedp/chromedp"
)

// EvalSelector queries the first element matching sel and runs jsFn on it.
// jsFn must be a JS arrow function source (e.g. "el => el.innerText"). The
// result is decoded into out via JSON; pass nil to discard.
//
// Throws if the selector matches nothing — wrap with WaitForSelector if the
// element appears asynchronously.
func (p *Page) EvalSelector(sel, jsFn string, out any) error {
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
	})()`, selJS, selJS, jsFn)
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(expr, out)); err != nil {
		return fmt.Errorf("%w: eval %q: %s", ErrEvaluate, sel, err.Error())
	}
	return nil
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
