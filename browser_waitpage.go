package chromekit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// attachProbeTimeout bounds the round-trip that proves a freshly attached
// target is really alive. Short: the target either answers immediately or is
// already gone.
const attachProbeTimeout = 2 * time.Second

// waitPagePollInterval is how often WaitForNewPage re-lists Chrome targets.
// Kept small because site-opened popups (OAuth consent) may close themselves
// quickly after posting their result — we must attach before that happens.
const waitPagePollInterval = 300 * time.Millisecond

// WaitForNewPage polls Chrome's target list until a page target that is NOT
// already tracked and whose URL contains urlSubstr appears, attaches it, and
// returns the wired *Page. It exists because chromekit does not subscribe to
// Target.targetCreated — a popup the page opens itself (window.open, e.g. a
// Google Identity Services OAuth window) never lands in b.pages on its own.
//
// urlSubstr == "" matches the first brand-new page regardless of URL.
//
// Lock contract: WaitForNewPage acquires b.pagesMu around attachToTarget itself
// — attachToTarget does NOT lock (its doc: "the CALLER must hold b.pagesMu").
// Requires the CDP debug port (Connect path); returns ErrInvalidConfig otherwise.
func (b *Browser) WaitForNewPage(urlSubstr string, timeout time.Duration) (*Page, error) {
	if b == nil || b.port == 0 {
		return nil, fmt.Errorf("%w: WaitForNewPage requires a debug port (Connect)", ErrInvalidConfig)
	}
	known := b.knownTargetIDs()
	// One deadline for the whole wait, shared with each /json/list request so a
	// hung endpoint cannot stretch a 100ms timeout into seconds.
	ctx, cancel := context.WithTimeout(b.ctx, timeout)
	defer cancel()

	for {
		if b.ctx.Err() != nil {
			return nil, ErrContextCanceled
		}
		if targets, err := fetchPageTargets(ctx, b.port); err == nil {
			for _, t := range targets {
				if known[t.ID] {
					continue
				}
				if urlSubstr != "" && !strings.Contains(t.URL, urlSubstr) {
					continue
				}
				if p := b.attachNewTarget(t.ID); p != nil {
					return p, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			if b.ctx.Err() != nil {
				return nil, ErrContextCanceled
			}
			return nil, fmt.Errorf("%w: no new page matching %q within %s", ErrTimeout, urlSubstr, timeout)
		case <-time.After(waitPagePollInterval):
		}
	}
}

// attachNewTarget attaches tid under b.pagesMu, guarding against a concurrent
// WaitForNewPage having already attached the same target. Returns nil if the
// target is already tracked, if attach failed, or if the target turned out to
// be gone (caller keeps polling).
func (b *Browser) attachNewTarget(tid string) *Page {
	b.pagesMu.Lock()
	if b.pageByIDLocked(tid) != nil {
		b.pagesMu.Unlock()
		return nil // already attached by someone else
	}
	p, err := b.attachToTarget(tid, false, true)
	b.pagesMu.Unlock()
	if err != nil {
		return nil
	}

	// attachToTarget only builds the context — the CDP session materializes
	// lazily on first use. Force it now: popups of the kind WaitForNewPage
	// exists for (OAuth consent windows) routinely close themselves in the
	// gap between /json/list and here, and reporting one of those as a live
	// page hands the caller a corpse and leaves it in b.pages forever.
	p.initListeners()
	if err := p.verifyAttached(); err != nil {
		b.log.Debugf("chromekit: target %s vanished before attach completed: %v", tid, err)
		b.removePage(tid)
		return nil
	}
	return p
}

// verifyAttached forces the lazy CDP session to materialize and reports
// whether the target is actually reachable.
func (p *Page) verifyAttached() error {
	ctx, cancel := context.WithTimeout(p.ctx, attachProbeTimeout)
	defer cancel()
	var ready string
	return chromedp.Run(ctx, chromedp.Evaluate(`document.readyState`, &ready))
}

// knownTargetIDs snapshots the currently-tracked page IDs so WaitForNewPage can
// tell a freshly-opened popup from tabs that already existed.
func (b *Browser) knownTargetIDs() map[string]bool {
	b.pagesMu.RLock()
	defer b.pagesMu.RUnlock()
	m := make(map[string]bool, len(b.pages))
	for _, p := range b.pages {
		m[p.id] = true
	}
	return m
}
