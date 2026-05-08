// Package input contains the InputBackend interface and its implementations
// (CDP via chromedp + Native via winapi). Kept separate so chromekit's root
// package can swap backends at runtime without depending on platform code.
package input

import (
	"context"
	"errors"
)

// ErrNotSupported signals the active backend cannot perform the call on this
// platform (e.g. native backend on macOS/Linux).
var ErrNotSupported = errors.New("input: not supported on this platform")

// MouseButton enumerates the buttons a backend understands.
type MouseButton int

const (
	MouseLeft MouseButton = iota
	MouseRight
	MouseMiddle
)

// Type selects a backend implementation.
type Type int

const (
	TypeCDP Type = iota
	TypeNative
)

// Window describes where the Chrome window sits on screen, so DOM coordinates
// can be converted to absolute screen pixels by the native backend.
type Window struct {
	X, Y  int
	Scale float64
}

// Config bundles knobs the backends need at construction time.
type Config struct {
	Window Window
}

// Backend is the low-level mouse/keyboard dispatcher. Higher-level helpers
// (Cursor, Keyboard, TouchScreen) layer human-like behavior on top.
type Backend interface {
	MouseMove(ctx context.Context, x, y float64) error
	MouseDown(ctx context.Context, x, y float64, button MouseButton) error
	MouseUp(ctx context.Context, x, y float64, button MouseButton) error
	MouseScroll(ctx context.Context, deltaX, deltaY float64) error
	KeyChar(ctx context.Context, ch rune) error
	InsertText(ctx context.Context, text string) error
}

// New returns a backend by type. Native on unsupported platforms returns a
// stub whose methods all error with ErrNativeNotSupported — caller decides
// whether to fall back to CDP.
func New(t Type, cfg Config) Backend {
	switch t {
	case TypeNative:
		return newNative(cfg)
	default:
		return newCDP()
	}
}
