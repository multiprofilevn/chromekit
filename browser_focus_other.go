//go:build !windows

package chromekit

import (
	"context"

	"github.com/tuwibu/chromekit/input"
)

// SetHWND is a no-op on non-Windows platforms.
func (b *Browser) SetHWND(_ uintptr) {}

// HWND is unsupported outside Windows.
func (b *Browser) HWND() (uintptr, error) { return 0, ErrNativeNotSupported }

// Focus is unsupported outside Windows; native input requires platform-specific
// foreground-window primitives that chromekit only ships for Windows today.
func (b *Browser) Focus(_ context.Context) error { return ErrNativeNotSupported }

// ContentOffset is unsupported outside Windows.
func (b *Browser) ContentOffset() (int, int, error) { return 0, 0, ErrNativeNotSupported }

// WindowScale is unsupported outside Windows.
func (b *Browser) WindowScale() (float64, error) { return 0, ErrNativeNotSupported }

// WindowRect is unsupported outside Windows.
func (b *Browser) WindowRect() (input.Rect, error) { return input.Rect{}, ErrNativeNotSupported }

// InvalidateNativeWindow is unsupported outside Windows — there is no native
// coordinate frame to re-measure.
func (b *Browser) InvalidateNativeWindow() error { return ErrNativeNotSupported }
