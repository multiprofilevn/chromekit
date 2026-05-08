package chromekit

import (
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Navigate loads url and waits for document.readyState == "complete".
// timeout=0 → 30 seconds.
func (p *Page) Navigate(url string, timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("navigate: %s", url)
	if err := chromedp.Run(p.ctx, chromedp.Navigate(url)); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.WaitForReady(timeout)
}

// Reload reloads the current page.
func (p *Page) Reload(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("reload")
	if err := chromedp.Run(p.ctx, chromedp.Reload()); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.WaitForReady(timeout)
}

// GoBack navigates back in history.
func (p *Page) GoBack(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("go back")
	if err := chromedp.Run(p.ctx, chromedp.NavigateBack()); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.WaitForReady(timeout)
}

// GoForward navigates forward in history.
func (p *Page) GoForward(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("go forward")
	if err := chromedp.Run(p.ctx, chromedp.NavigateForward()); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.WaitForReady(timeout)
}
