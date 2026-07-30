//go:build !windows

package chromekit

// foregroundGuard is Windows-only — there is no foreground-window primitive to
// check against elsewhere, so the native backend runs unguarded (it is a stub
// on those platforms anyway).
func (b *Browser) foregroundGuard() func() error { return nil }
