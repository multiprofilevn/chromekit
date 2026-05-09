//go:build linux

package input

import "context"

type nativeBackend struct{}

func newNative(_ Config) Backend { return &nativeBackend{} }

func (nativeBackend) MouseMove(context.Context, float64, float64) error    { return ErrNotSupported }
func (nativeBackend) MouseDown(context.Context, float64, float64, MouseButton) error {
	return ErrNotSupported
}
func (nativeBackend) MouseUp(context.Context, float64, float64, MouseButton) error {
	return ErrNotSupported
}
func (nativeBackend) MouseScroll(context.Context, float64, float64) error { return ErrNotSupported }
func (nativeBackend) KeyChar(context.Context, rune) error                 { return ErrNotSupported }
func (nativeBackend) InsertText(context.Context, string) error            { return ErrNotSupported }
func (nativeBackend) KeyCombo(context.Context, uint16, Modifier) error    { return ErrNotSupported }

var _ Backend = (*nativeBackend)(nil)
