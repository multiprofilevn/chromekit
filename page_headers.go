package chromekit

import (
	"fmt"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// SetExtraHeaders attaches a static header set to every subsequent request the
// page makes. Pass an empty map to clear. Headers added here override
// page-controlled ones (e.g. User-Agent, Referer) — use sparingly.
//
// The override applies until the next call replaces it; it does NOT survive a
// chrome restart.
func (p *Page) SetExtraHeaders(headers map[string]string) error {
	if err := p.alive(); err != nil {
		return err
	}
	h := network.Headers{}
	for k, v := range headers {
		h[k] = v
	}
	if err := chromedp.Run(p.ctx, network.SetExtraHTTPHeaders(h)); err != nil {
		return fmt.Errorf("%w: set headers: %s", ErrEvaluate, err.Error())
	}
	return nil
}
