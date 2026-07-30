package chromekit

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

// BoundingBox is an element's rendered geometry in CSS pixels (viewport-relative).
type BoundingBox struct {
	X, Y, Width, Height      float64
	Left, Right, Top, Bottom float64
}

// QuerySelector returns the first node matching selector. Polls until timeout.
// timeout=0 → 30s.
func (p *Page) QuerySelector(selector string, timeout time.Duration) (*cdp.Node, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	var nodes []*cdp.Node
	for {
		_ = chromedp.Run(ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery))
		if len(nodes) > 0 {
			return nodes[0], nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: selector %q", ErrTimeout, selector)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// QuerySelectorAll returns all nodes matching selector (waits until ≥1 found).
func (p *Page) QuerySelectorAll(selector string, timeout time.Duration) ([]*cdp.Node, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	var nodes []*cdp.Node
	for {
		_ = chromedp.Run(ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQueryAll))
		if len(nodes) > 0 {
			return nodes, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: selector %q", ErrTimeout, selector)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// BoundingBox returns the element's box-model margin rectangle.
func (p *Page) BoundingBox(node *cdp.Node) (BoundingBox, error) {
	if err := p.alive(); err != nil {
		return BoundingBox{}, err
	}
	c := chromedp.FromContext(p.ctx)
	box, err := dom.GetBoxModel().WithNodeID(node.NodeID).Do(cdp.WithExecutor(p.ctx, c.Target))
	if err != nil {
		return BoundingBox{}, fmt.Errorf("%w: box model: %s", ErrEvaluate, err.Error())
	}
	m := box.Margin
	return BoundingBox{
		X: m[0], Y: m[1],
		Width:  m[2] - m[0],
		Height: m[5] - m[1],
		Left:   m[0], Right: m[2],
		Top: m[1], Bottom: m[5],
	}, nil
}

// GetText returns the textContent of an element matching selector.
func (p *Page) GetText(selector string) (string, error) {
	if err := p.alive(); err != nil {
		return "", err
	}
	var s string
	js := fmt.Sprintf(`(document.querySelector(%q)?.textContent || '').trim()`, selector)
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(js, &s)); err != nil {
		return "", fmt.Errorf("%w: text: %s", ErrEvaluate, err.Error())
	}
	return s, nil
}

// boxModelByNodeID is BoundingBox keyed by raw NodeID (instead of *cdp.Node).
// Used by frame-level helpers that hold node IDs from a remote query.
func (p *Page) boxModelByNodeID(id cdp.NodeID) (BoundingBox, error) {
	c := chromedp.FromContext(p.ctx)
	box, err := dom.GetBoxModel().WithNodeID(id).Do(cdp.WithExecutor(p.ctx, c.Target))
	if err != nil {
		return BoundingBox{}, fmt.Errorf("%w: box model: %s", ErrEvaluate, err.Error())
	}
	m := box.Margin
	return BoundingBox{
		X: m[0], Y: m[1],
		Width: m[2] - m[0], Height: m[5] - m[1],
		Left: m[0], Right: m[2],
		Top: m[1], Bottom: m[5],
	}, nil
}

// ScrollIntoView scrolls the first element matching selector to the viewport
// center. No-op if the element is already visible.
func (p *Page) ScrollIntoView(selector string) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) throw new Error('selector not found: ' + %q);
		el.scrollIntoView({block: 'center', inline: 'center'});
		return true;
	})()`, selector, selector)
	var ok bool
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(js, &ok)); err != nil {
		return fmt.Errorf("%w: scroll into view %q: %s", ErrEvaluate, selector, err.Error())
	}
	return nil
}

// Focus runs el.focus() on the first matching element. Silently succeeds
// (returns nil) when the selector matches nothing — mirrors firefoxkit's
// Focus exactly: it routes through EvalSelector with out=nil, so a
// null/no-match result never surfaces as an error there either. Callers
// that need the match itself confirmed should combine this with
// QuerySelector or WaitForSelector.
func (p *Page) Focus(selector string) error {
	if err := p.alive(); err != nil {
		return err
	}
	js := fmt.Sprintf(`(() => { const el = document.querySelector(%q); if (el) el.focus(); return true; })()`, selector)
	var ok bool
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(js, &ok)); err != nil {
		return fmt.Errorf("%w: focus %q: %s", ErrEvaluate, selector, err.Error())
	}
	return nil
}

// GetAttribute returns the value of attribute on the first element matching selector.
func (p *Page) GetAttribute(selector, attribute string) (string, error) {
	if err := p.alive(); err != nil {
		return "", err
	}
	var s string
	js := fmt.Sprintf(`document.querySelector(%q)?.getAttribute(%q) || ''`, selector, attribute)
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(js, &s)); err != nil {
		return "", fmt.Errorf("%w: attr: %s", ErrEvaluate, err.Error())
	}
	return s, nil
}
