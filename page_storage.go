package chromekit

import (
	"encoding/json"
	"fmt"

	"github.com/chromedp/chromedp"
)

// LocalStorage returns a handle to the page's window.localStorage. State is
// per-origin — navigating cross-origin invalidates anything you set.
func (p *Page) LocalStorage() *Storage { return &Storage{p: p, kind: "localStorage"} }

// SessionStorage returns a handle to the page's window.sessionStorage.
func (p *Page) SessionStorage() *Storage { return &Storage{p: p, kind: "sessionStorage"} }

// Storage is a thin wrapper over local/sessionStorage. Holds no state of its
// own; every call hits the page via Evaluate.
type Storage struct {
	p    *Page
	kind string // "localStorage" | "sessionStorage"
}

// Get returns the value for key, or "" if missing. Distinguishing missing from
// empty string isn't supported — both round-trip to "".
func (s *Storage) Get(key string) (string, error) {
	if err := s.p.alive(); err != nil {
		return "", err
	}
	k, err := jsString(key)
	if err != nil {
		return "", err
	}
	expr := fmt.Sprintf("(window.%s.getItem(%s) || '')", s.kind, k)
	var out string
	if err := chromedp.Run(s.p.ctx, chromedp.Evaluate(expr, &out)); err != nil {
		return "", fmt.Errorf("%w: storage get: %s", ErrEvaluate, err.Error())
	}
	return out, nil
}

// Set stores key=value.
func (s *Storage) Set(key, value string) error {
	if err := s.p.alive(); err != nil {
		return err
	}
	k, err := jsString(key)
	if err != nil {
		return err
	}
	v, err := jsString(value)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf("window.%s.setItem(%s, %s)", s.kind, k, v)
	if err := chromedp.Run(s.p.ctx, chromedp.Evaluate(expr, nil)); err != nil {
		return fmt.Errorf("%w: storage set: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// Remove deletes the entry for key (no-op if missing).
func (s *Storage) Remove(key string) error {
	if err := s.p.alive(); err != nil {
		return err
	}
	k, err := jsString(key)
	if err != nil {
		return err
	}
	expr := fmt.Sprintf("window.%s.removeItem(%s)", s.kind, k)
	if err := chromedp.Run(s.p.ctx, chromedp.Evaluate(expr, nil)); err != nil {
		return fmt.Errorf("%w: storage remove: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// Clear empties all entries for the current origin.
func (s *Storage) Clear() error {
	if err := s.p.alive(); err != nil {
		return err
	}
	expr := fmt.Sprintf("window.%s.clear()", s.kind)
	if err := chromedp.Run(s.p.ctx, chromedp.Evaluate(expr, nil)); err != nil {
		return fmt.Errorf("%w: storage clear: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// jsString returns a JSON-encoded string literal safe to splice into JS.
// Catches stray quotes, backslashes, unicode without ad-hoc escaping.
func jsString(s string) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("%w: encode: %s", ErrInvalidConfig, err.Error())
	}
	return string(b), nil
}
