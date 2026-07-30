//go:build windows

package chromekit

import (
	"sync"

	"github.com/tuwibu/chromekit/input"
)

// foregroundGuard builds the closure the native input backend consults before
// letting keystrokes or button events reach the OS. SendInput carries no window
// address: whatever owns the foreground at that instant receives the input, and
// Focus() only proves ownership once, at the start of a run that lasts seconds.
//
// Returns nil (no checking) when the guard is switched off via
// WithForegroundGuard(false) — the caller then owns exclusivity some other way,
// e.g. a fleet-level foreground lease.
//
// An unresolvable HWND degrades to "allow", not "fail". A browser whose window
// handle cannot be found (Connect path before a window exists) would otherwise
// have every job fail for a reason that has nothing to do with the foreground.
// The Warn fires once per backend so a long run does not flood the log.
func (b *Browser) foregroundGuard() func() error {
	if b == nil || b.cfg == nil || !b.cfg.foregroundGuard {
		return nil
	}
	var warnOnce sync.Once
	return func() error {
		hwnd, err := b.HWND()
		if err != nil || hwnd == 0 {
			warnOnce.Do(func() {
				b.log.Warnf("chromekit: foreground guard inactive — HWND unresolved: %v", err)
			})
			return nil
		}
		if foregroundIs(hwnd) {
			return nil
		}
		return input.ErrForegroundLost
	}
}
