package chromekit

import (
	"fmt"

	"github.com/chromedp/chromedp"
)

// Click dispatches a CDP click on the first element matching selector.
// For a human-like click (cursor path + delay), use ClickElement.
func (p *Page) Click(selector string) error {
	if err := p.alive(); err != nil {
		return err
	}
	if err := chromedp.Run(p.ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
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
	if err := chromedp.Run(p.ctx, chromedp.SendKeys(selector, text, chromedp.ByQuery)); err != nil {
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
