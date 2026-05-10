package chromekit

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Cookie is a flat representation of an HTTP cookie. Mirrors the subset of
// network.Cookie that callers actually set or read; chromekit hides the
// cdproto types so users don't need that import.
type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	URL      string // when set, CDP infers Domain/Path from this URL
	Expires  time.Time
	HTTPOnly bool
	Secure   bool
	SameSite string // "Strict" | "Lax" | "None" — empty means default
}

// CookiesAPI groups cookie operations under p.Cookies(). Held as a cached
// field on Page so call sites pay zero allocation per access.
type CookiesAPI struct{ p *Page }

// Get returns cookies visible to the current page. Pass urls to scope the
// query (defaults to the current document URL). HTTPOnly cookies are
// included — CDP bypasses document.cookie.
func (c *CookiesAPI) Get(urls ...string) ([]Cookie, error) {
	if err := c.p.alive(); err != nil {
		return nil, err
	}
	action := network.GetCookies()
	if len(urls) > 0 {
		action = action.WithURLs(urls)
	}
	var raw []*network.Cookie
	err := chromedp.Run(c.p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cookies, err := action.Do(ctx)
		if err != nil {
			return err
		}
		raw = cookies
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("%w: get cookies: %s", ErrEvaluate, err.Error())
	}
	out := make([]Cookie, len(raw))
	for i, ck := range raw {
		out[i] = Cookie{
			Name:     ck.Name,
			Value:    ck.Value,
			Domain:   ck.Domain,
			Path:     ck.Path,
			Expires:  time.Unix(int64(ck.Expires), 0),
			HTTPOnly: ck.HTTPOnly,
			Secure:   ck.Secure,
			SameSite: string(ck.SameSite),
		}
	}
	return out, nil
}

// Set installs one or more cookies. Each cookie must have either URL
// (preferred — CDP infers Domain/Path) or Domain set; missing both returns
// ErrInvalidConfig instead of letting CDP silently drop. SameSite=None
// requires Secure=true (Chrome rule).
func (c *CookiesAPI) Set(cookies ...Cookie) error {
	if err := c.p.alive(); err != nil {
		return err
	}
	if len(cookies) == 0 {
		return nil
	}
	params := make([]*network.CookieParam, 0, len(cookies))
	for i, ck := range cookies {
		if err := validateCookie(ck); err != nil {
			return fmt.Errorf("%w: cookie[%d] %q: %s", ErrInvalidConfig, i, ck.Name, err.Error())
		}
		cp := &network.CookieParam{
			Name:     ck.Name,
			Value:    ck.Value,
			Domain:   ck.Domain,
			Path:     ck.Path,
			URL:      ck.URL,
			HTTPOnly: ck.HTTPOnly,
			Secure:   ck.Secure,
		}
		if ck.SameSite != "" {
			cp.SameSite = network.CookieSameSite(ck.SameSite)
		}
		if !ck.Expires.IsZero() {
			t := cdp.TimeSinceEpoch(ck.Expires)
			cp.Expires = &t
		}
		params = append(params, cp)
	}
	if err := chromedp.Run(c.p.ctx, network.SetCookies(params)); err != nil {
		return fmt.Errorf("%w: set cookies: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// Delete removes a single cookie by name. URL scopes the deletion to a
// matching domain/path (recommended); pass empty url to delete by name across
// all matching domains.
func (c *CookiesAPI) Delete(name, cookieURL string) error {
	if err := c.p.alive(); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("%w: empty cookie name", ErrInvalidConfig)
	}
	action := network.DeleteCookies(name)
	if cookieURL != "" {
		action = action.WithURL(cookieURL)
	}
	if err := chromedp.Run(c.p.ctx, action); err != nil {
		return fmt.Errorf("%w: delete cookie: %s", ErrEvaluate, err.Error())
	}
	return nil
}

// Clear wipes all cookies stored by the browser (every origin).
func (c *CookiesAPI) Clear() error {
	if err := c.p.alive(); err != nil {
		return err
	}
	if err := chromedp.Run(c.p.ctx, network.ClearBrowserCookies()); err != nil {
		return fmt.Errorf("%w: clear cookies: %s", ErrEvaluate, err.Error())
	}
	return nil
}

func validateCookie(c Cookie) error {
	if c.Name == "" {
		return fmt.Errorf("name is empty")
	}
	if c.URL == "" && c.Domain == "" {
		return fmt.Errorf("must set URL or Domain")
	}
	if c.SameSite == "None" && !c.Secure {
		return fmt.Errorf("SameSite=None requires Secure=true")
	}
	if c.URL != "" && c.Domain != "" {
		u, err := url.Parse(c.URL)
		if err != nil || u.Host == "" {
			return fmt.Errorf("invalid URL %q", c.URL)
		}
		host := u.Hostname()
		dom := strings.TrimPrefix(c.Domain, ".")
		if host != dom && !strings.HasSuffix(host, "."+dom) {
			return fmt.Errorf("domain %q does not match URL host %q", c.Domain, host)
		}
	}
	return nil
}
