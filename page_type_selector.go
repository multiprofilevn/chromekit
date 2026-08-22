package chromekit

import (
	"fmt"
	"strings"

	"github.com/multiprofilevn/chromekit/input"
)

// TypeSelectorConfig tunes TypeSelector retry + strictness + per-key cadence.
// Zero values fall back to defaults applied by withDefaults.
type TypeSelectorConfig struct {
	// Strict: when true, returning without a matching value after MaxAttempts
	// surfaces ErrTypeSelectorMismatch. When false, a final mismatch is
	// swallowed and TypeSelector returns nil — useful for fields that
	// asynchronously reformat (phone masks, currency) where the displayed
	// value will never round-trip exactly.
	Strict bool

	// MaxAttempts caps total typing attempts (initial + retries). Default 5,
	// minimum 1. After every failed verify, the field is wiped via Ctrl+A +
	// Delete and retyped from scratch.
	MaxAttempts int

	// Human controls TypeHuman pacing + typo simulation per attempt. Zero
	// value applies TypeHuman's own defaults.
	Human input.HumanTypeConfig
}

func (c TypeSelectorConfig) withDefaults() TypeSelectorConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	return c
}

// readableValueJS extracts a comparable string from any of three field shapes:
//   - <input>/<textarea>: el.value
//   - contenteditable host (rich text editors, JS editors): el.innerText
//   - plain element: el.textContent
//
// innerText is preferred over textContent for contenteditable because rich
// editors render visible whitespace through CSS; textContent collapses or
// duplicates separators. .value comes first because input/textarea also expose
// textContent that lags behind keystrokes.
const readableValueJS = `el => {
	if (el == null) return '';
	if ('value' in el && typeof el.value === 'string') return el.value;
	if (el.isContentEditable === true) return (el.innerText != null ? el.innerText : '');
	return (el.textContent != null ? el.textContent : '');
}`

// TypeSelector focuses selector, types text via TypeHuman, then verifies the
// element's value matches text. On mismatch it wipes the field (Ctrl+A +
// Delete) and retries up to cfg.MaxAttempts. Both sides are TrimSpace'd before
// compare, so fields that strip surrounding whitespace (HTML inputs, rich
// editors that wrap in <p>) still round-trip cleanly.
//
// Strict=true → returns ErrTypeSelectorMismatch on final failure.
// Strict=false → swallows the mismatch and returns nil.
//
// Selector must be present in the DOM at call time — wrap with WaitForSelector
// if it appears asynchronously.
//
// Deliberately NOT bounded by DefaultActionTimeout: human-paced typing runs
// 80-220ms per character and may retry MaxAttempts times, so a long field can
// legitimately exceed any fixed budget. Hangs are prevented one level down,
// where each individual CDP dispatch carries its own timeout. Wrap the call
// yourself if your flow needs a hard ceiling.
func (k *KeyboardAPI) TypeSelector(selector, text string, cfg ...TypeSelectorConfig) error {
	if err := k.p.alive(); err != nil {
		return err
	}
	c := TypeSelectorConfig{}
	if len(cfg) > 0 {
		c = cfg[0]
	}
	c = c.withDefaults()
	k.p.initInput()

	wantTrim := strings.TrimSpace(text)

	var lastGot string
	for attempt := 1; attempt <= c.MaxAttempts; attempt++ {
		// Prefer MouseAPI.FocusElement — real human-like click triggers proper
		// focus events on inputs, textareas, and contenteditable hosts (caret
		// placement). It already has chromedp.Focus fallback. Last-resort JS
		// el.focus() handles edge cases where the click can't land (overlay,
		// pointer-events:none on a wrapper, etc).
		if err := k.p.Mouse().FocusElement(selector); err != nil {
			if jsErr := k.p.EvalSelector(selector, "el => { el.focus(); }", nil); jsErr != nil {
				return fmt.Errorf("%w: focus %q: %s (js fallback: %s)",
					ErrEvaluate, selector, err.Error(), jsErr.Error())
			}
		}
		// On retries the field already has prior text — wipe before retyping
		// so we don't append. First attempt may also have pre-existing value
		// (e.g. browser autofill); cheaper to always clear than to peek first.
		if err := k.p.keyboard.ClearInput(k.p.ctx); err != nil {
			return fmt.Errorf("%w: clear %q: %s", ErrEvaluate, selector, err.Error())
		}
		if err := k.p.keyboard.TypeHuman(k.p.ctx, text, c.Human); err != nil {
			return fmt.Errorf("%w: type %q: %s", ErrEvaluate, selector, err.Error())
		}
		got, err := readSelectorValue(k.p, selector)
		if err != nil {
			return err
		}
		lastGot = got
		if strings.TrimSpace(got) == wantTrim {
			return nil
		}
	}

	if c.Strict {
		return fmt.Errorf("%w: %q after %d attempts: got %q, want %q",
			ErrTypeSelectorMismatch, selector, c.MaxAttempts, lastGot, text)
	}
	return nil
}

func readSelectorValue(p *Page, selector string) (string, error) {
	var got string
	if err := p.EvalSelector(selector, readableValueJS, &got); err != nil {
		return "", err
	}
	return got, nil
}
