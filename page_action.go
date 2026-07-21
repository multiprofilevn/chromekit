package chromekit

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Click dispatches a CDP click on the first element matching selector.
// For a human-like click (cursor path + delay), use ClickElement.
func (p *Page) Click(selector string) error {
	if err := p.alive(); err != nil {
		return err
	}
	ctx, cancel := opCtx(p.ctx, 0)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("%w: click %q: %s", ErrEvaluate, selector, err.Error())
	}
	return nil
}

// Type sends text into the first element matching selector via CDP.
// For human-like typing (random per-key delay), use TypeHuman.
func (p *Page) Type(selector, text string) error {
	if err := p.alive(); err != nil {
		return err
	}
	ctx, cancel := opCtx(p.ctx, 0)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.SendKeys(selector, text, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("%w: type %q: %s", ErrEvaluate, selector, err.Error())
	}
	return nil
}

// Evaluate runs a JS expression and decodes the result into out (must be pointer).
// Pass nil for out to discard.
func (p *Page) Evaluate(expr string, out any) error {
	if err := p.alive(); err != nil {
		return err
	}
	if err := chromedp.Run(p.ctx, chromedp.Evaluate(expr, out)); err != nil {
		return fmt.Errorf("%w: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// Screenshot captures the visible viewport as PNG.
func (p *Page) Screenshot() ([]byte, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	var buf []byte
	if err := chromedp.Run(p.ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return nil, fmt.Errorf("%w: screenshot: %s", ErrEvaluate, err.Error())
	}
	return buf, nil
}

// ScreenshotClip captures a viewport region as PNG. Coordinates are CSS pixels
// relative to the viewport top-left.
func (p *Page) ScreenshotClip(x, y, w, h float64) ([]byte, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%w: clip width/height must be > 0", ErrInvalidConfig)
	}
	var buf []byte
	err := chromedp.Run(p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		out, err := page.CaptureScreenshot().
			WithClip(&page.Viewport{X: x, Y: y, Width: w, Height: h, Scale: 1}).
			Do(ctx)
		if err != nil {
			return err
		}
		buf = out
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("%w: screenshot clip: %s", ErrEvaluate, err.Error())
	}
	return buf, nil
}

// ScreenshotElement captures the bounding box of the element matching sel.
// Scrolls the element into view first when off-screen.
func (p *Page) ScreenshotElement(selector string) ([]byte, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	if err := p.WaitForSelector(selector, 0); err != nil {
		return nil, err
	}
	if err := p.ScrollIntoView(selector); err != nil {
		return nil, err
	}
	node, err := p.QuerySelector(selector, 0)
	if err != nil {
		return nil, err
	}
	box, err := p.BoundingBox(node)
	if err != nil {
		return nil, err
	}
	if box.Width <= 0 || box.Height <= 0 {
		return nil, fmt.Errorf("%w: zero-size element %q", ErrEvaluate, selector)
	}
	return p.ScreenshotClip(box.Left, box.Top, box.Width, box.Height)
}

// FullScreenshot captures the full page (scrolling) as PNG with quality 0-100.
func (p *Page) FullScreenshot(quality int) ([]byte, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	if quality <= 0 || quality > 100 {
		quality = 90
	}
	var buf []byte
	if err := chromedp.Run(p.ctx, chromedp.FullScreenshot(&buf, quality)); err != nil {
		return nil, fmt.Errorf("%w: screenshot: %s", ErrEvaluate, err.Error())
	}
	return buf, nil
}
