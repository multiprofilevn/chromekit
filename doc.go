// Package chromekit wraps chromedp with a Puppeteer-like API.
//
// Goal: page.Click / page.Type without juggling context.Context.
// Engine: github.com/chromedp/chromedp.
// Input backends: CDP (default, all platforms) or Native (winapi, Windows only).
package chromekit
