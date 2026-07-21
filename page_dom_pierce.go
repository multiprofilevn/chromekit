package chromekit

import (
	"fmt"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

// FirstIframeRectPierce finds the first <iframe> anywhere in the DOM — including
// inside OPEN and CLOSED shadow roots — and returns its viewport rectangle.
// found=false when the document contains no iframe.
//
// Unlike a JS `el.shadowRoot` walk (which returns null for closed shadow roots),
// this uses CDP dom.GetDocument().WithPierce(true), which traverses closed shadow
// roots too. Useful for widgets (e.g. Cloudflare Turnstile) whose checkbox iframe
// may be nested in a closed shadow root.
func (p *Page) FirstIframeRectPierce() (BoundingBox, bool, error) {
	if err := p.alive(); err != nil {
		return BoundingBox{}, false, err
	}
	c := chromedp.FromContext(p.ctx)
	exec := cdp.WithExecutor(p.ctx, c.Target)

	doc, err := dom.GetDocument().WithDepth(-1).WithPierce(true).Do(exec)
	if err != nil {
		return BoundingBox{}, false, fmt.Errorf("%w: get document: %s", ErrEvaluate, err.Error())
	}
	node := findIframeInShadow(doc)
	if node == nil {
		return BoundingBox{}, false, nil
	}
	bb, err := p.boxModelByNodeID(node.NodeID)
	if err != nil {
		return BoundingBox{}, false, err
	}
	return bb, true, nil
}

// findIframeInShadow does a depth-first search for the first iframe node,
// preferring shadow-root children over light-DOM children (Turnstile nests its
// iframe in a shadow root). Checks both shadow-root and regular children so a
// plain iframe is found too.
func findIframeInShadow(node *cdp.Node) *cdp.Node {
	if node == nil {
		return nil
	}
	for _, sr := range node.ShadowRoots {
		for _, child := range sr.Children {
			if child.LocalName == "iframe" {
				return child
			}
			if f := findIframeInShadow(child); f != nil {
				return f
			}
		}
	}
	for _, ch := range node.Children {
		if ch.LocalName == "iframe" {
			return ch
		}
		if f := findIframeInShadow(ch); f != nil {
			return f
		}
	}
	return nil
}
