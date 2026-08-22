// browser_native_frame_parity_test.go — compile-time assertions that the
// native-coordinate-frame surface chromekit exposes is byte-for-byte the same
// as firefoxkit's. The mirror of this block lives in firefoxkit's
// parity_compile_test.go; the two must be edited together.
//
// Same signature with a different MEANING is what this phase is cleaning up
// (ContentOffset used to return a relative offset here and an absolute one
// there), and a compiler cannot catch that. What it CAN catch is the shape
// drifting, so pin the shape here and pin the meaning in the doc comments.
//
// No runtime call is ever made — `(*T).Method` is a method-value expression and
// the assignment compiles only when the declared type matches exactly.
package chromekit

import (
	"context"

	"github.com/multiprofilevn/chromekit/input"
)

var (
	_ func(*Browser, context.Context) error = (*Browser).Focus
	_ func(*Browser) (int, int, error)      = (*Browser).ContentOffset
	_ func(*Browser) (float64, error)       = (*Browser).WindowScale
	_ func(*Browser) (input.Rect, error)    = (*Browser).WindowRect
	_ func(*Browser) error                  = (*Browser).InvalidateNativeWindow
	_ func(*Browser) (uintptr, error)       = (*Browser).HWND
	_ func(*Browser, uintptr)               = (*Browser).SetHWND
)
