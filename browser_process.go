package chromekit

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

const (
	ownedProcessWaitDelay        = 3 * time.Second
	ownedProcessTerminateTimeout = 5 * time.Second
)

type processTerminator func(context.Context, *exec.Cmd, uint64) error

// ownedProcess keeps the exact command chromedp started and memoizes its
// teardown. A Browser created by Connect never gets one.
type ownedProcess struct {
	mu           sync.RWMutex
	cmd          *exec.Cmd
	rootIdentity uint64
	terminate    processTerminator
	attemptMu    sync.Mutex
	attemptDone  chan struct{}
	terminated   bool
}

func newOwnedProcess(terminate processTerminator) *ownedProcess {
	return &ownedProcess{terminate: terminate}
}

func (p *ownedProcess) bind(cmd *exec.Cmd) {
	if p == nil || cmd == nil {
		return
	}
	p.mu.Lock()
	p.cmd = cmd
	p.mu.Unlock()

	cmd.WaitDelay = ownedProcessWaitDelay
	cmd.Cancel = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), ownedProcessTerminateTimeout)
		defer cancel()
		return p.Terminate(ctx)
	}
}

func (p *ownedProcess) command() *exec.Cmd {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cmd
}

func (p *ownedProcess) rememberIdentity() error {
	cmd := p.command()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	identity, err := ownedProcessIdentity(cmd.Process)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.rootIdentity = identity
	p.mu.Unlock()
	return nil
}

func (p *ownedProcess) snapshot() (*exec.Cmd, uint64) {
	if p == nil {
		return nil, 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cmd, p.rootIdentity
}

// Terminate single-flights concurrent callers and caches verified success.
// A timeout/error is deliberately retryable with a fresh deadline: caching a
// failed first attempt would permanently strand the owned process tree.
func (p *ownedProcess) Terminate(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for {
		p.attemptMu.Lock()
		if p.terminated {
			p.attemptMu.Unlock()
			return nil
		}
		if running := p.attemptDone; running != nil {
			p.attemptMu.Unlock()
			select {
			case <-running:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		done := make(chan struct{})
		p.attemptDone = done
		p.attemptMu.Unlock()

		var err error
		if p.terminate != nil {
			cmd, rootIdentity := p.snapshot()
			err = p.terminate(ctx, cmd, rootIdentity)
		}
		p.attemptMu.Lock()
		if err == nil {
			p.terminated = true
		}
		p.attemptDone = nil
		close(done)
		p.attemptMu.Unlock()
		return err
	}
}

func withOwnedProcessCmd(p *ownedProcess, userHook func(*exec.Cmd)) func(*exec.Cmd) {
	return func(cmd *exec.Cmd) {
		if userHook != nil {
			withProcessGroupDefaults(userHook)(cmd)
		} else {
			applyDefaultProcessAttrs(cmd)
		}
		p.bind(cmd)
	}
}
