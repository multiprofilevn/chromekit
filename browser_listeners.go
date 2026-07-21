package chromekit

import (
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// destroyedQueueSize bounds the target-destroyed backlog. A hostile or merely
// busy page can loop window.open()/close(), so the queue must not grow without
// limit. Dropping is safe: a dropped id just leaves a dead Page in b.pages
// until something else touches it, which is the pre-fix behaviour — never
// worse than not listening at all.
const destroyedQueueSize = 32

// initBrowserListeners subscribes to browser-level target lifecycle events so
// tabs that die outside chromekit's control stop leaking. Without it, a tab
// closed by window.close(), by the user (Connect mode), or by an OAuth popup
// finishing its handshake keeps its *Page, its context node under b.ctx, and
// chromedp's per-target goroutines alive until the whole browser shuts down.
//
// Two things are required, and chromedp only arranges the first on the Launch
// path:
//
//  1. Target discovery enabled on the BROWSER connection. chromedp enables it
//     while bootstrapping a connection it opened itself; Connect() attaches
//     with WithTargetID, which enables discovery on that page's SESSION
//     instead — browser-level targetDestroyed events then never reach
//     ListenBrowser. Enabling it explicitly is idempotent, so it is correct on
//     both paths.
//
//  2. A ListenBrowser subscription. The page-level listener in initListeners
//     cannot observe this event: by the time it fires, the target and its
//     session are already gone.
//
// Best-effort by design: a browser that cannot enable discovery still works,
// it just keeps the old leak. Never fail Launch/Connect over this.
func (b *Browser) initBrowserListeners() {
	if b == nil || b.ctx == nil {
		return
	}
	if err := chromedp.Run(b.ctx, target.SetDiscoverTargets(true)); err != nil {
		b.log.Debugf("chromekit: target discovery unavailable, external tab closes will leak: %v", err)
		return
	}

	destroyed := make(chan string, destroyedQueueSize)
	chromedp.ListenBrowser(b.ctx, func(ev any) {
		e, ok := ev.(*target.EventTargetDestroyed)
		if !ok {
			return
		}
		select {
		case destroyed <- string(e.TargetID):
		default:
			b.log.Debugf("chromekit: target-destroyed queue full, dropped %s", e.TargetID)
		}
	})

	go b.consumeDestroyedTargets(destroyed)
}

// consumeDestroyedTargets evicts destroyed tabs one at a time until the
// browser context ends. Sequential on purpose: a goroutine per event would let
// a page spamming open/close flood the scheduler and pile up contention on
// pagesMu — the very stall this package is trying to remove.
// Stops on Close() as well as on context cancellation: the Connect path never
// cancels b.ctx (that would kill the user's Chrome), so b.ctx.Done alone would
// leave this goroutine running for the life of the process.
func (b *Browser) consumeDestroyedTargets(destroyed <-chan string) {
	closed := b.closeSignal()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-closed:
			return
		case id := <-destroyed:
			b.evictDestroyedTarget(id)
		}
	}
}

// evictDestroyedTarget drops a tab Chrome has already destroyed.
//
// The primary page is evicted from bookkeeping but never cancelled: its cancel
// IS the Browser's cancel, so running it would tear down every sibling tab and,
// in Connect mode, a Chrome process chromekit does not own. A destroyed primary
// does not imply a dying browser — Chrome stays up while other tabs remain — so
// the entry must still go, otherwise current keeps pointing at a dead context.
func (b *Browser) evictDestroyedTarget(id string) {
	b.pagesMu.Lock()
	removed := b.removePageLocked(id)
	b.pagesMu.Unlock()

	if removed == nil {
		return // not a tab we track: an iframe, a worker, another client's tab
	}
	if removed.primary {
		b.log.Debugf("chromekit: primary tab %s destroyed externally; browser context left intact", id)
		return
	}
	// Outside the lock: cancel round-trips CDP.
	if removed.cancel != nil {
		removed.cancel()
	}
}
