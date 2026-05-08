//go:build !windows

package chromekit

import "context"

// SetHWND is a no-op on non-Windows platforms.
func (b *Browser) SetHWND(_ uintptr) {}

// HWND is unsupported outside Windows.
func (b *Browser) HWND() (uintptr, error) { return 0, ErrNativeNotSupported }

// Focus is unsupported outside Windows; native input requires platform-specific
// foreground-window primitives that chromekit only ships for Windows today.
func (b *Browser) Focus(_ context.Context) error { return ErrNativeNotSupported }
