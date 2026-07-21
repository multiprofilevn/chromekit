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

// dropFinishedDownloads removes every complete Begin→Done run from a batch of
// buffered events and returns what is left.
//
// A Done event ends a download, so everything up to and including the last one
// belongs to downloads that already finished — stale by definition, since the
// caller was not waiting on them. Anything after it is the download in flight
// now, whose events merely arrived early.
func dropFinishedDownloads(events []downloadEvent) []downloadEvent {
	lastDone := -1
	for i, ev := range events {
		if ev.Done {
			lastDone = i
		}
	}
	if lastDone < 0 {
		return events
	}
	return events[lastDone+1:]
}

// Wait blocks until a download started on this page completes (or is
// canceled) or timeout fires. Returns the suggested filename Chrome
// reported when the download began, relative to the directory passed to
// SetPath.
//
// Subsequent downloads queue in a 16-slot channel — call Wait promptly
// after the trigger to avoid dropping events.
func (d *DownloadsAPI) Wait(timeout time.Duration) (string, error) {
	// Snapshot the channel under the lock and watch that one for the whole
	// wait: SetPath can swap it concurrently, and re-reading the field on
	// every loop iteration is both a data race and a way to start listening
	// to an empty channel halfway through a download.
	d.p.listenerMu.RLock()
	ch := d.p.downloadCh
	d.p.listenerMu.RUnlock()
	if ch == nil {
		return "", fmt.Errorf("%w: SetPath not called", ErrInvalidConfig)
	}
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	// Discard downloads that already finished without anyone waiting on them,
	// or this call resolves with a previous download's filename. Events for
	// the download being waited on now are put back: Chrome reports
	// downloadWillBegin asynchronously, so they routinely arrive before Wait
	// is even entered — draining those would turn a success into a timeout.
	var buffered []downloadEvent
	for {
		select {
		case ev := <-ch:
			buffered = append(buffered, ev)
			continue
		default:
		}
		break
	}
	var filename string
	for _, ev := range dropFinishedDownloads(buffered) {
		if ev.Filename != "" {
			filename = ev.Filename
		}
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case ev := <-ch:
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
