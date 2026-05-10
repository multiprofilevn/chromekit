package chromekit

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

const downloadChBuf = 16

// DownloadsAPI groups download lifecycle operations.
type DownloadsAPI struct{ p *Page }

// SetPath redirects file downloads triggered on this page to dir. Creates
// dir if missing. Subsequent downloads emit lifecycle events the caller can
// wait on via Wait.
//
// Per-page resolution: Browser.setDownloadBehavior is browser-scope in CDP,
// so calling this on one page affects every page in the same browser
// context. Acceptable trade-off — the v1 API is intentionally minimal.
func (d *DownloadsAPI) SetPath(dir string) error {
	if err := d.p.alive(); err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("%w: abs %q: %s", ErrInvalidConfig, dir, err.Error())
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("%w: mkdir %q: %s", ErrInvalidConfig, abs, err.Error())
	}
	action := browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
		WithDownloadPath(abs).
		WithEventsEnabled(true)
	if err := chromedp.Run(d.p.ctx, action); err != nil {
		return fmt.Errorf("%w: set download: %s", ErrEvaluate, err.Error())
	}
	d.p.listenerMu.Lock()
	if d.p.downloadCh == nil {
		d.p.downloadCh = make(chan downloadEvent, downloadChBuf)
	}
	d.p.listenerMu.Unlock()
	d.p.initListeners()
	return nil
}

// Wait blocks until a download started on this page completes (or is
// canceled) or timeout fires. Returns the suggested filename Chrome
// reported when the download began, relative to the directory passed to
// SetPath.
//
// Subsequent downloads queue in a 16-slot channel — call Wait promptly
// after the trigger to avoid dropping events.
func (d *DownloadsAPI) Wait(timeout time.Duration) (string, error) {
	if d.p.downloadCh == nil {
		return "", fmt.Errorf("%w: SetPath not called", ErrInvalidConfig)
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	var filename string
	for {
		select {
		case ev := <-d.p.downloadCh:
			if ev.Filename != "" {
				filename = ev.Filename
			}
			if ev.Done {
				if ev.Err != nil {
					return filename, ev.Err
				}
				return filename, nil
			}
		case <-deadline.C:
			return filename, fmt.Errorf("%w: wait download", ErrTimeout)
		case <-d.p.ctx.Done():
			return filename, ErrContextCanceled
		}
	}
}
