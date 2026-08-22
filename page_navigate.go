package chromekit

import (
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/multiprofilevn/chromekit/input"
)

// Navigate loads url and waits for document.readyState == "complete".
// timeout=0 → 30 seconds.
//
// When the page's input backend is Native, navigation goes through Chrome's
// omnibox (Ctrl+L → Ctrl+A → type → End → Enter) instead of CDP. This
// looks like a real user to bot-detection, and the End key step bypasses
// Chrome's inline autocomplete that would otherwise jump to a suggested URL
// from history (e.g. typing "facebook.com" landing on
// "facebook.com/login.php" because that was visited recently).
func (p *Page) Navigate(url string, timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("navigate: %s", url)
	if p.cfg != nil && p.cfg.inputBackend == BackendNative && p.browser != nil {
		return p.navigateOmnibox(url, timeout)
	}
	// The timeout covers the whole operation, not just the wait: chromedp.Run
	// on p.ctx alone blocks indefinitely when the renderer stops answering.
	navCtx, cancel := opCtx(p.ctx, timeout)
	defer cancel()
	if err := chromedp.Run(navCtx, chromedp.Navigate(url)); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.waitReadyWithin(navCtx)
}

// navigateOmnibox simulates a user typing the URL into Chrome's address
// bar. Requires the browser window to be focusable.
func (p *Page) navigateOmnibox(url string, timeout time.Duration) error {
	p.initInput()
	if p.keyboard == nil {
		return fmt.Errorf("%w: keyboard not initialized", ErrNavigate)
	}
	ctx := p.ctx
	if err := p.browser.Focus(ctx); err != nil {
		return fmt.Errorf("%w: focus: %s", ErrNavigate, err.Error())
	}
	// Stamp the current document BEFORE typing the URL so the post-Enter wait can
	// tell the new page has actually committed (readyState alone races — the old
	// doc is still "complete" the instant we poll after Enter).
	p.markNavToken()
	if err := p.keyboard.Combo(ctx, input.VKL, input.ModCtrl); err != nil {
		return fmt.Errorf("%w: focus omnibox: %s", ErrNavigate, err.Error())
	}
	if err := p.keyboard.Combo(ctx, input.VKA, input.ModCtrl); err != nil {
		return fmt.Errorf("%w: select-all: %s", ErrNavigate, err.Error())
	}
	typed := stripScheme(url)
	if err := p.keyboard.Type(ctx, typed); err != nil {
		return fmt.Errorf("%w: type url: %s", ErrNavigate, err.Error())
	}
	// End cancels Chrome's inline autocomplete (suggestion text after the
	// cursor) so Enter navigates to exactly what was typed.
	if err := p.keyboard.Combo(ctx, input.VKEnd, 0); err != nil {
		return fmt.Errorf("%w: end key: %s", ErrNavigate, err.Error())
	}
	if err := p.keyboard.Combo(ctx, input.VKReturn, 0); err != nil {
		return fmt.Errorf("%w: enter: %s", ErrNavigate, err.Error())
	}
	// Wait for the navigation to actually commit a new document + finish loading,
	// not just for the (stale) old document to report ready.
	return p.waitNavCommitted(timeout)
}

// stripScheme removes a leading "http://" or "https://" (case-insensitive).
// Chrome's omnibox auto-prepends https://, so feeding it the raw scheme
// would either duplicate it or get rewritten. Other schemes (ftp, file,
// chrome, etc.) are left intact for callers that genuinely need them.
func stripScheme(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.HasPrefix(lower, "https://"):
		return url[len("https://"):]
	case strings.HasPrefix(lower, "http://"):
		return url[len("http://"):]
	}
	return url
}

// Reload reloads the current page.
func (p *Page) Reload(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("reload")
	ctx, cancel := opCtx(p.ctx, timeout)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Reload()); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.waitReadyWithin(ctx)
}

// GoBack navigates back in history.
func (p *Page) GoBack(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("go back")
	ctx, cancel := opCtx(p.ctx, timeout)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.NavigateBack()); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.waitReadyWithin(ctx)
}

// GoForward navigates forward in history.
func (p *Page) GoForward(timeout time.Duration) error {
	if err := p.alive(); err != nil {
		return err
	}
	p.log.Infof("go forward")
	ctx, cancel := opCtx(p.ctx, timeout)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.NavigateForward()); err != nil {
		return fmt.Errorf("%w: %s", ErrNavigate, err.Error())
	}
	return p.waitReadyWithin(ctx)
}
