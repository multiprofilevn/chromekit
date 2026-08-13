package chromekit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/chromedp/chromedp"
)

// Browser owns a Chrome process and the pages opened on it.
type Browser struct {
	ctx         context.Context
	cancel      context.CancelFunc
	allocCancel context.CancelFunc
	cfg         *config
	log         Logger

	// pagesMu guards pages/current/pageCount.
	//
	// INVARIANT: never hold it across a CDP call (chromedp.Run, a page cancel,
	// BringToFront, initListeners). Those round-trip to Chrome and can take
	// seconds on a wedged tab, and every accessor here — Pages, Current,
	// NewPage, ClosePage, the target-destroyed evictor — would queue behind
	// them. Take the lock, mutate the bookkeeping, release, then talk to Chrome.
	pagesMu   sync.RWMutex
	pages     []*Page
	current   *Page
	pageCount int

	hwndMu     sync.Mutex
	cachedHWND uintptr

	contentMu      sync.Mutex
	contentOffsetX int
	contentOffsetY int
	contentCached  bool

	// attached is true when this Browser was built via Connect (chromekit
	// does NOT own the chrome process). Close() then detaches the targets
	// instead of closing them, leaving the chrome process running.
	attached bool

	// port is the CDP remote-debugging port from Connect. Used by HWND PID
	// lookup to find the chrome main process. 0 for Launch path.
	port int

	// pid is the chrome browser process PID for Launch path (chromekit
	// spawned it). 0 for Connect path — looked up from port at HWND time.
	pid uint32

	// closed is shut by Close(). Background workers select on it as well as
	// on b.ctx, because the Connect path deliberately never cancels b.ctx —
	// without this signal those workers would outlive the Browser.
	closeOnce sync.Once
	closed    chan struct{}

	// doneOnce/doneCh memoize the Done() watcher so repeated Done() calls (e.g.
	// an orchestrator health-polling hundreds of browsers) share one goroutine
	// instead of leaking one per call until Close.
	doneOnce sync.Once
	doneCh   chan struct{}

	// ownedProcess is non-nil only for Launch. It terminates the exact command
	// tree before chromedp contexts are cancelled and memoizes the result.
	ownedProcess *ownedProcess
	teardownOnce sync.Once
}

// isClosedLocked reports whether Close() has already signalled shutdown. Caller
// MUST hold b.pagesMu. Uses the closed channel (shut by signalClosed before
// Close nils the page list) so it is reliable even in the window between those
// two steps.
func (b *Browser) isClosedLocked() bool {
	if b.closed == nil {
		return false
	}
	select {
	case <-b.closed:
		return true
	default:
		return false
	}
}

// Launch starts Chrome with the given options and attaches to the blank page
// chromedp bound on Run; it does not spawn an extra tab.
// Caller MUST defer b.Close() to avoid leaking the Chrome process.
//
// chromePath is the absolute path to the Chrome / Chromium binary. Pass an
// empty string to fall back to chromedp's default `exec.LookPath("chrome")`,
// which often fails on Windows where chrome.exe is not on PATH — pass the
// real path explicitly when you can.
func Launch(chromePath string, opts ...Option) (*Browser, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.headless && cfg.inputBackend == BackendNative {
		return nil, fmt.Errorf("%w: native input requires a visible window (headless=false)", ErrInvalidConfig)
	}

	owned := newOwnedProcess(terminateOwnedProcessTree)
	allocOpts := buildAllocatorOptions(chromePath, cfg, owned)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(cfg.logger.Debugf))
	if err := chromedp.Run(ctx); err != nil {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), ownedProcessTerminateTimeout)
		terminateErr := owned.Terminate(terminateCtx)
		terminateCancel()
		cancel()
		allocCancel()
		launchErr := fmt.Errorf("%w: launch: %s", ErrInvalidConfig, err.Error())
		if terminateErr != nil {
			return nil, errors.Join(launchErr, fmt.Errorf("terminate failed launch: %w", terminateErr))
		}
		return nil, launchErr
	}
	if err := owned.rememberIdentity(); err != nil {
		terminateCtx, terminateCancel := context.WithTimeout(context.Background(), ownedProcessTerminateTimeout)
		terminateErr := owned.Terminate(terminateCtx)
		terminateCancel()
		cancel()
		allocCancel()
		identityErr := fmt.Errorf("%w: capture launched process identity: %v", ErrInvalidConfig, err)
		if terminateErr != nil {
			return nil, errors.Join(identityErr, fmt.Errorf("terminate failed launch: %w", terminateErr))
		}
		return nil, identityErr
	}

	b := &Browser{
		ctx:          ctx,
		cancel:       cancel,
		allocCancel:  allocCancel,
		cfg:          cfg,
		log:          cfg.logger,
		ownedProcess: owned,
	}
	go b.cleanupOwnedProcessOnContextDone()
	// Capture chrome PID from chromedp's allocator. Required for HWND lookup
	// via PID enum — without it Focus falls back to page-title heuristic which
	// fails on blank pages (the typical state at automation start).
	if c := chromedp.FromContext(ctx); c != nil && c.Browser != nil {
		if p := c.Browser.Process(); p != nil {
			b.pid = uint32(p.Pid)
		}
	}
	// Attach to the page target chromedp already bound when Run succeeded —
	// deterministic, no enumeration, so an extension welcome tab can never be
	// selected (the historical wrong-tab race). Do NOT spawn a 2nd tab.
	tid, err := resolveLaunchTarget(b.ctx)
	if err != nil {
		b.Close()
		return nil, err
	}
	b.pagesMu.Lock()
	p, aerr := b.attachToTarget(tid, true, true)
	b.pagesMu.Unlock()
	if aerr != nil {
		b.Close()
		return nil, aerr
	}
	// Both talk to Chrome, so they run after pagesMu is released.
	p.initListeners()
	b.initBrowserListeners()
	return b, nil
}

// resolveLaunchTarget returns the page target ID chromedp bound to ctx when
// chromedp.Run succeeded. Deterministic (no enumeration). Defensive nil-guard:
// if chromedp bound no page target, fail loud with ErrNoLaunchTarget — no
// spawn, no enumerate fallback (both re-introduce the two-tab race).
func resolveLaunchTarget(ctx context.Context) (string, error) {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return "", fmt.Errorf("%w: chromedp bound no page target on Run", ErrNoLaunchTarget)
	}
	return string(c.Target.TargetID), nil
}

func buildAllocatorOptions(chromePath string, cfg *config, owned *ownedProcess) []chromedp.ExecAllocatorOption {
	// chromekit intentionally does NOT prepend chromedp.DefaultExecAllocatorOptions.
	// Callers own the entire flag list — per-engine profiles (gologin / gpmlogin /
	// stock Chrome) need wildly different sets, and silent defaults made it
	// impossible to match upstream-tool flags (e.g. gologin's minimal arg set).
	// If you want the chromedp defaults, append them yourself before Launch.
	opts := []chromedp.ExecAllocatorOption{}
	if chromePath != "" {
		// chromedp.ExecPath overrides the default exec.LookPath("chrome").
		// Place after defaults so it wins ordering-wise.
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	opts = append(opts, chromedp.Flag("headless", cfg.headless))
	if cfg.userDataDir != "" {
		opts = append(opts, chromedp.UserDataDir(cfg.userDataDir))
	}
	if cfg.proxy != "" {
		opts = append(opts, chromedp.ProxyServer(cfg.proxy))
	}
	for k, v := range cfg.flags {
		opts = append(opts, chromedp.Flag(k, v))
	}
	// Always install our command hook: exec.CommandContext otherwise kills only
	// the root process on cancellation, leaving Chrome helpers behind.
	opts = append(opts, chromedp.ModifyCmdFunc(withOwnedProcessCmd(owned, cfg.modifyCmd)))
	if cfg.wsURLReadTimeout > 0 {
		opts = append(opts, chromedp.WSURLReadTimeout(cfg.wsURLReadTimeout))
	}
	return opts
}

// withProcessGroupDefaults wraps a user hook with the platform lifecycle
// attributes required when ModifyCmdFunc replaces chromedp's default hook.
// Windows intentionally adds no implicit creation flags; non-Windows keeps the
// existing process-group behavior for user-modified commands.
func withProcessGroupDefaults(fn func(*exec.Cmd)) func(*exec.Cmd) {
	return func(cmd *exec.Cmd) {
		applyProcessGroupAttrs(cmd)
		fn(cmd)
	}
}

// Close cancels every page, then the browser, then (for Launch) the
// allocator. Order matters — reversing it leaks chromedp goroutines.
//
// Connect (b.attached) tears down just as completely, but detaches the tabs it
// does not own first — see detachWithoutClosing. The distinction that matters
// is per-tab, not per-process: cancelling a chromedp context closes THAT tab,
// so a plain cancel would shut the tabs the user already had (and with them,
// eventually, a Chrome chromekit never started). It is not, as an earlier
// version of this comment claimed, a Browser.close: chromedp routes
// RemoteAllocator contexts around that branch (first=false).
//
// Measured over 30 Connect/Close cycles: goroutines and heap stay flat, the
// user's tabs and Chrome process survive.
//
// Launch teardown first terminates the complete owned process tree, then
// cancels browser/page/allocator contexts. The operation is idempotent and
// serializes concurrent attempts and caches only verified success. A failed
// termination remains retryable on a later Close/Terminate call.
// Connect never invokes a process terminator.
func (b *Browser) Close() error {
	if b == nil {
		return nil
	}
	var terminateErr error
	if !b.attached {
		ctx, cancel := context.WithTimeout(context.Background(), ownedProcessTerminateTimeout)
		terminateErr = b.Terminate(ctx)
		cancel()
	}
	b.teardownOnce.Do(func() {
		b.pagesMu.Lock()
		pages := b.pages
		b.pages = nil
		b.current = nil
		b.pagesMu.Unlock()

		// Signal only after owned-tree termination. Done observers can then rely
		// on a launched browser having no surviving helper processes.
		b.signalClosed()

		if b.attached {
			// Cancelling a page context makes chromedp send Target.closeTarget for
			// that tab. Correct for tabs chromekit opened; destructive for tabs the
			// user already had. Detach the latter first so the cancel tears down
			// their goroutines without touching the tab itself.
			for i := len(pages) - 1; i >= 0; i-- {
				p := pages[i]
				if p.cancel == nil {
					continue
				}
				if !p.owned {
					detachWithoutClosing(p.ctx)
				}
				p.cancel()
			}
			detachWithoutClosing(b.ctx)
		} else {
			for i := len(pages) - 1; i >= 0; i-- {
				if pages[i].cancel != nil {
					pages[i].cancel()
				}
			}
		}
		if b.cancel != nil {
			b.cancel()
		}
		if b.allocCancel != nil {
			b.allocCancel()
		}
	})
	return terminateErr
}

func (b *Browser) cleanupOwnedProcessOnContextDone() {
	<-b.ctx.Done()
	ctx, cancel := context.WithTimeout(context.Background(), ownedProcessTerminateTimeout)
	_ = b.Terminate(ctx)
	cancel()
}

// Terminate kills the complete process tree owned by a Browser created with
// Launch. It is safe to call concurrently and returns the cached first result.
// For a Browser created with Connect it is always a no-op: the caller owns that
// Chrome process.
func (b *Browser) Terminate(ctx context.Context) error {
	if b == nil || b.attached || b.ownedProcess == nil {
		return nil
	}
	return b.ownedProcess.Terminate(ctx)
}

// detachWithoutClosing makes a chromedp context safe to cancel without taking
// its tab down with it.
//
// chromedp's per-context cleanup goroutine sends DetachFromTarget followed by
// Target.closeTarget when the context ends — right for a tab chromedp opened,
// destructive for one that was already there when we attached. That goroutine
// returns early when Context.Target is nil (chromedp.go, "This is a new tab,
// but we didn't create it and attach to it yet"), so clearing the field skips
// the close while every goroutine still exits on cancellation.
//
// Only call this immediately before cancelling: it makes the context unusable
// for further operations.
func detachWithoutClosing(ctx context.Context) {
	if ctx == nil {
		return
	}
	if c := chromedp.FromContext(ctx); c != nil {
		c.Target = nil
	}
}

// signalClosed shuts the close channel exactly once. Lazily allocated so a
// hand-constructed Browser (tests) works without an explicit constructor.
func (b *Browser) signalClosed() {
	b.closeOnce.Do(func() {
		b.pagesMu.Lock()
		if b.closed == nil {
			b.closed = make(chan struct{})
		}
		ch := b.closed
		b.pagesMu.Unlock()
		close(ch)
	})
}

// closeSignal returns the channel that Close shuts, allocating it on first
// use so workers started before any Close still observe the same channel.
func (b *Browser) closeSignal() <-chan struct{} {
	b.pagesMu.Lock()
	defer b.pagesMu.Unlock()
	if b.closed == nil {
		b.closed = make(chan struct{})
	}
	return b.closed
}

// Done returns a channel that closes when this Browser is finished — either
// because Close() was called or because the underlying chromedp context died
// (chrome exited or crashed). Caller can <-Done() to wait for either.
//
// Both signals matter: Close() on the Connect path leaves the caller's Chrome
// running, so waiting on the context alone would block forever.
func (b *Browser) Done() <-chan struct{} {
	if b == nil || b.ctx == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	b.doneOnce.Do(func() {
		b.doneCh = make(chan struct{})
		go func() {
			defer close(b.doneCh)
			select {
			case <-b.ctx.Done():
			case <-b.closeSignal():
			}
		}()
	})
	return b.doneCh
}

// PID returns the chrome browser process id captured at Launch. 0 for the
// Connect path (chromekit didn't spawn the process — caller may know the pid
// from elsewhere).
func (b *Browser) PID() uint32 {
	if b == nil {
		return 0
	}
	return b.pid
}

// InputBackend reports which input backend this Browser was launched with.
// Used by orchestrators (e.g. chromefleet) that want to branch behavior —
// native input requires OS-cursor synchronisation (drift checks, focus),
// CDP input does not.
func (b *Browser) InputBackend() BackendType {
	if b == nil || b.cfg == nil {
		return BackendCDP
	}
	return b.cfg.inputBackend
}

// Current returns the active page (last opened or last switched-to).
func (b *Browser) Current() *Page {
	b.pagesMu.RLock()
	defer b.pagesMu.RUnlock()
	return b.current
}

// Pages returns a snapshot copy of all pages owned by this browser.
func (b *Browser) Pages() []*Page {
	b.pagesMu.RLock()
	defer b.pagesMu.RUnlock()
	out := make([]*Page, len(b.pages))
	copy(out, b.pages)
	return out
}

// GoBack navigates the current page back in history.
func (b *Browser) GoBack() error {
	p := b.Current()
	if p == nil {
		return ErrPageNotFound
	}
	return p.GoBack(0)
}
