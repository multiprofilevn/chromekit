package chromekit

import "fmt"

// SetSelectValue assigns a value to a <select> element and dispatches a
// 'change' event so frameworks (React, Vue, Angular) update their bound
// state. Direct CDP click+key on <option> is unreliable across forms; this
// is the deterministic path. Returns ErrEvaluate when the selector matches
// nothing.
func (p *Page) SetSelectValue(selector, value string) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(()=>{
        const el = document.querySelector(%q);
        if (!el) return false;
        el.value = %q;
        el.dispatchEvent(new Event('change', {bubbles:true}));
        return true;
    })()`, selector, value)
	var ok bool
	if err := p.Evaluate(js, &ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: select %q: element not found", ErrEvaluate, selector)
	}
	return nil
}
