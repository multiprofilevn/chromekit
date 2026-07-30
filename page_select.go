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

// SelectByValue picks the option matching value in the <select> at sel.
// Fires both input and change events. Returns ErrPageNotFound when sel
// matches nothing or no option has that value — mirrors firefoxkit exactly.
func (p *Page) SelectByValue(sel, value string) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		const value = %q;
		const opt = Array.from(el.options).find(o => o.value === value);
		if (!opt) return false;
		el.value = value;
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return true;
	})()`, sel, value)
	var ok bool
	if err := p.Evaluate(js, &ok); err != nil {
		return err
	}
	if !ok {
		return ErrPageNotFound
	}
	return nil
}

// SelectByIndex picks options[index]. Same not-found contract as
// SelectByValue.
func (p *Page) SelectByIndex(sel string, index int) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		const i = %d;
		if (i < 0 || i >= el.options.length) return false;
		el.selectedIndex = i;
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return true;
	})()`, sel, index)
	var ok bool
	if err := p.Evaluate(js, &ok); err != nil {
		return err
	}
	if !ok {
		return ErrPageNotFound
	}
	return nil
}

// SelectByText picks the first option whose visible label matches text.
// Same not-found contract as SelectByValue.
func (p *Page) SelectByText(sel, text string) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		const text = %q;
		const opt = Array.from(el.options).find(o => (o.textContent || '').trim() === text);
		if (!opt) return false;
		el.value = opt.value;
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return true;
	})()`, sel, text)
	var ok bool
	if err := p.Evaluate(js, &ok); err != nil {
		return err
	}
	if !ok {
		return ErrPageNotFound
	}
	return nil
}
