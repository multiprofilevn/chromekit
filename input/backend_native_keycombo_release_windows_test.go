//go:build windows

package input

import (
	"errors"
	"testing"
)

// A modifier that goes down and never comes back up is stuck for the whole
// OS, not just this browser: Windows keeps applying it to every keystroke in
// every application until a human presses that key again.

type vkEvent struct {
	vk uint16
	up bool
}

// withStubbedSendVK swaps the SendInput call for a recorder, optionally
// failing on the nth call (1-based; 0 never fails).
func withStubbedSendVK(t *testing.T, failOn int) *[]vkEvent {
	t.Helper()
	var events []vkEvent
	calls := 0
	orig := sendVKFn
	sendVKFn = func(vk uint16, flags uint32) error {
		calls++
		if failOn != 0 && calls == failOn {
			return errors.New("SendInput refused")
		}
		events = append(events, vkEvent{vk: vk, up: flags&keyEventfKeyUp != 0})
		return nil
	}
	t.Cleanup(func() { sendVKFn = orig })
	return &events
}

// heldAfter reports the keys still down once the recorded sequence finishes.
func heldAfter(events []vkEvent) []uint16 {
	var held []uint16
	for _, e := range events {
		if e.up {
			for i, k := range held {
				if k == e.vk {
					held = append(held[:i], held[i+1:]...)
					break
				}
			}
			continue
		}
		held = append(held, e.vk)
	}
	return held
}

func TestKeyCombo_ReleasesEverythingOnSuccess(t *testing.T) {
	events := withStubbedSendVK(t, 0)
	b := &nativeBackend{}

	if err := b.KeyCombo(t.Context(), VKA, ModCtrl|ModShift); err != nil {
		t.Fatalf("KeyCombo: %v", err)
	}
	if held := heldAfter(*events); len(held) != 0 {
		t.Fatalf("keys still down after a clean combo: %v", held)
	}
}

func TestKeyCombo_ReleasesModifiersWhenTheKeyFails(t *testing.T) {
	// Ctrl down, Shift down, then the main key fails: pre-fix this returned
	// with both modifiers held.
	events := withStubbedSendVK(t, 3)
	b := &nativeBackend{}

	if err := b.KeyCombo(t.Context(), VKA, ModCtrl|ModShift); err == nil {
		t.Fatal("KeyCombo returned nil despite a SendInput failure")
	}
	if held := heldAfter(*events); len(held) != 0 {
		t.Fatalf("modifiers stuck down after a mid-combo failure: %v", held)
	}
}

func TestKeyCombo_ReleasesFirstModifierWhenSecondFails(t *testing.T) {
	events := withStubbedSendVK(t, 2)
	b := &nativeBackend{}

	if err := b.KeyCombo(t.Context(), VKA, ModCtrl|ModShift); err == nil {
		t.Fatal("KeyCombo returned nil despite a SendInput failure")
	}
	if held := heldAfter(*events); len(held) != 0 {
		t.Fatalf("first modifier stuck down after the second failed: %v", held)
	}
}

func TestKeyCombo_ReleasesInReverseOrder(t *testing.T) {
	events := withStubbedSendVK(t, 0)
	b := &nativeBackend{}

	if err := b.KeyCombo(t.Context(), VKA, ModCtrl|ModAlt|ModShift); err != nil {
		t.Fatalf("KeyCombo: %v", err)
	}

	var downs, ups []uint16
	for _, e := range *events {
		if e.up {
			ups = append(ups, e.vk)
		} else {
			downs = append(downs, e.vk)
		}
	}
	if len(downs) != len(ups) {
		t.Fatalf("%d downs vs %d ups", len(downs), len(ups))
	}
	for i := range downs {
		if downs[i] != ups[len(ups)-1-i] {
			t.Fatalf("release order %v is not the reverse of press order %v", ups, downs)
		}
	}
}
