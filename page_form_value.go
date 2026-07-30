package chromekit

import "fmt"

// SetValue programmatically sets el.value and dispatches input + change
// events — useful for forms bound to JS frameworks (React/Vue) that ignore
// native typing but watch for these synthetic events. Uses the prototype
// value setter (not plain assignment) so React's dirty-check still observes
// the change — same trick firefoxkit uses. Returns ErrPageNotFound when sel
// matches nothing (mirrors firefoxkit exactly).
func (p *Page) SetValue(sel, value string) error {
	if err := p.alive(); err != nil {
		return err
	}
	valJS, err := jsString(value)
	if err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		const proto = Object.getPrototypeOf(el);
		const setter = Object.getOwnPropertyDescriptor(proto, 'value');
		if (setter && setter.set) setter.set.call(el, %s);
		else el.value = %s;
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return true;
	})()`, sel, valJS, valJS)
	var ok bool
	if err := p.Evaluate(js, &ok); err != nil {
		return err
	}
	if !ok {
		return ErrPageNotFound
	}
	return nil
}

// CheckBox sets el.checked = checked on the matched input and dispatches
// input + change events. Returns ErrPageNotFound when sel matches nothing
// (mirrors firefoxkit exactly).
func (p *Page) CheckBox(sel string, checked bool) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		el.checked = %v;
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return true;
	})()`, sel, checked)
	var ok bool
	if err := p.Evaluate(js, &ok); err != nil {
		return err
	}
	if !ok {
		return ErrPageNotFound
	}
	return nil
}
