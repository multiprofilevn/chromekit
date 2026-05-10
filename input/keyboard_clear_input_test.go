package input

import (
	"context"
	"errors"
	"testing"
)

// recordingBackend captures every KeyCombo invocation in order so tests can
// assert ClearInput dispatches exactly (VKA, ModCtrl) then (VKDelete, 0).
type recordingBackend struct {
	combos    []comboCall
	failOn    int   // index at which to return failErr (0 = first call); -1 = never
	failErr   error
	callCount int
}

type comboCall struct {
	vk   uint16
	mods Modifier
}

func (r *recordingBackend) MouseMove(_ context.Context, _, _ float64) error             { return nil }
func (r *recordingBackend) MouseDown(_ context.Context, _, _ float64, _ MouseButton) error {
	return nil
}
func (r *recordingBackend) MouseUp(_ context.Context, _, _ float64, _ MouseButton) error { return nil }
func (r *recordingBackend) MouseScroll(_ context.Context, _, _ float64) error            { return nil }
func (r *recordingBackend) KeyChar(_ context.Context, _ rune) error                      { return nil }
func (r *recordingBackend) InsertText(_ context.Context, _ string) error                 { return nil }
func (r *recordingBackend) KeyCombo(_ context.Context, vk uint16, mods Modifier) error {
	idx := r.callCount
	r.callCount++
	r.combos = append(r.combos, comboCall{vk: vk, mods: mods})
	if r.failErr != nil && idx == r.failOn {
		return r.failErr
	}
	return nil
}

func TestKeyboard_ClearInput_DispatchesSelectAllThenDelete(t *testing.T) {
	rb := &recordingBackend{failOn: -1}
	k := NewKeyboard(rb)

	if err := k.ClearInput(context.Background()); err != nil {
		t.Fatalf("ClearInput returned unexpected error: %v", err)
	}

	if len(rb.combos) != 2 {
		t.Fatalf("expected 2 KeyCombo calls, got %d: %+v", len(rb.combos), rb.combos)
	}
	if rb.combos[0] != (comboCall{vk: VKA, mods: ModCtrl}) {
		t.Errorf("first combo = %+v, want {VKA, ModCtrl}", rb.combos[0])
	}
	if rb.combos[1] != (comboCall{vk: VKDelete, mods: 0}) {
		t.Errorf("second combo = %+v, want {VKDelete, 0}", rb.combos[1])
	}
}

func TestKeyboard_ClearInput_PropagatesFirstError(t *testing.T) {
	sentinel := errors.New("backend exploded")
	rb := &recordingBackend{failOn: 0, failErr: sentinel}
	k := NewKeyboard(rb)

	err := k.ClearInput(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if len(rb.combos) != 1 {
		t.Errorf("expected ClearInput to stop after first error, got %d calls", len(rb.combos))
	}
}

func TestKeyboard_ClearInput_PropagatesSecondError(t *testing.T) {
	sentinel := errors.New("delete failed")
	rb := &recordingBackend{failOn: 1, failErr: sentinel}
	k := NewKeyboard(rb)

	err := k.ClearInput(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if len(rb.combos) != 2 {
		t.Errorf("expected 2 calls before error surfaced, got %d", len(rb.combos))
	}
}
