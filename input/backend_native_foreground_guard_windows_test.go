//go:build windows

package input

import (
	"errors"
	"testing"
)

// SendInput is not addressed to a window: keystrokes go to whatever owns the
// foreground at the instant they are emitted. Focus() checks once, at the start,
// and a 20-character TypeHuman run lasts seconds — long enough for another
// browser to launch and take the foreground mid-string. These tests pin the
// guard at the emitters (sendKey / sendVK / sendMouse) rather than at Keyboard,
// because ClearInput and the omnibox combo never touch Keyboard.Type and would
// otherwise walk straight past it. Stray keys there are destructive, not a leak.

// guardRecorder stubs the three raw emitters and drives the guard from the
// number of characters that actually reached the OS, so a test can say "the
// foreground goes away after character 7" without timing anything.
type guardRecorder struct {
	keyDowns  int // character key-downs emitted (KeyChar down half)
	vkEvents  int // virtual-key events emitted (KeyCombo, both halves)
	guardCall int // times the guard was consulted

	// loseAfter makes the guard report ErrForegroundLost once this many
	// character key-downs have been emitted. 0 means "lost from the start";
	// negative means "never lost".
	loseAfter int
}

// guard is the func() error handed to nativeBackend.
func (g *guardRecorder) guard() error {
	g.guardCall++
	if g.loseAfter < 0 {
		return nil
	}
	if g.keyDowns >= g.loseAfter {
		return ErrForegroundLost
	}
	return nil
}

// install swaps the raw emitters for recorders and returns a backend wired to
// the recorder's guard. useGuard=false leaves nativeBackend.guard nil.
func (g *guardRecorder) install(t *testing.T, useGuard bool) *nativeBackend {
	t.Helper()

	origKey, origVK, origMouse := sendKeyFn, sendVKFn, sendMouseFn
	sendKeyFn = func(_ uint16, flags uint32) error {
		if flags&keyEventfKeyUp == 0 {
			g.keyDowns++
		}
		return nil
	}
	sendVKFn = func(_ uint16, _ uint32) error {
		g.vkEvents++
		return nil
	}
	sendMouseFn = func(_, _ uint32) error { return nil }
	t.Cleanup(func() { sendKeyFn, sendVKFn, sendMouseFn = origKey, origVK, origMouse })

	b := &nativeBackend{}
	if useGuard {
		b.guard = g.guard
	}
	return b
}

// fastKeyboard removes the human cadence so the assertions are about emission
// counts, never about elapsed time.
func fastKeyboard(b Backend) *Keyboard {
	k := NewKeyboard(b)
	k.MinDelay, k.MaxDelay = 0, 0
	return k
}

const guardTestText = "abcdefghijklmnopqrst" // exactly 20 characters

func TestSendKey_GuardAbortsMidType(t *testing.T) {
	if len(guardTestText) != 20 {
		t.Fatalf("guardTestText must be 20 chars, got %d", len(guardTestText))
	}
	g := &guardRecorder{loseAfter: 7}
	b := g.install(t, true)

	err := fastKeyboard(b).Type(t.Context(), guardTestText)
	if !errors.Is(err, ErrForegroundLost) {
		t.Fatalf("Type returned %v, want ErrForegroundLost — the guard never aborted the run", err)
	}
	// The guard runs once per GuardChunkSize characters, so at most one chunk
	// can overshoot the moment the foreground was lost: 7 rounded up to the
	// next chunk boundary, plus that chunk.
	const maxEmitted = 12
	if g.keyDowns > maxEmitted {
		t.Fatalf("emitted %d characters after losing the foreground at 7, want <= %d", g.keyDowns, maxEmitted)
	}
	if g.keyDowns == 20 {
		t.Fatalf("the whole string reached the OS — the guard is not wired into sendKey")
	}
}

func TestClearInput_AbortsOnForegroundLoss(t *testing.T) {
	// Ctrl+A + Delete on a field that is no longer focused wipes whatever the
	// foreground window considers selected. This path goes through KeyCombo,
	// not Keyboard.Type, which is exactly why the guard cannot live in
	// keyboard.go.
	g := &guardRecorder{loseAfter: 0}
	b := g.install(t, true)

	err := fastKeyboard(b).ClearInput(t.Context())
	if !errors.Is(err, ErrForegroundLost) {
		t.Fatalf("ClearInput returned %v, want ErrForegroundLost", err)
	}
	if g.vkEvents != 0 {
		t.Fatalf("emitted %d virtual-key events with the foreground already lost, want 0", g.vkEvents)
	}
}

func TestCombo_AbortsOnForegroundLoss(t *testing.T) {
	// Ctrl+L focuses the omnibox. Sent to a foreign window it is at best a
	// no-op and at worst a hotkey in someone else's application.
	g := &guardRecorder{loseAfter: 0}
	b := g.install(t, true)

	err := fastKeyboard(b).Combo(t.Context(), VKL, ModCtrl)
	if !errors.Is(err, ErrForegroundLost) {
		t.Fatalf("Combo returned %v, want ErrForegroundLost", err)
	}
	if g.vkEvents != 0 {
		t.Fatalf("emitted %d virtual-key events with the foreground already lost, want 0", g.vkEvents)
	}
}

func TestGuard_CalledPerChunk(t *testing.T) {
	// Pins the cost model. Per-keystroke checking would be 20 calls here; the
	// point of chunking is that it is not.
	g := &guardRecorder{loseAfter: -1}
	b := g.install(t, true)

	if err := fastKeyboard(b).Type(t.Context(), guardTestText); err != nil {
		t.Fatalf("Type: %v", err)
	}
	want := len(guardTestText) / GuardChunkSize // 4
	if g.guardCall < want-1 || g.guardCall > want+1 {
		t.Fatalf("guard called %d times for %d characters, want %d (+/-1) at chunk size %d",
			g.guardCall, len(guardTestText), want, GuardChunkSize)
	}
}

func TestGuard_NilIsNoOp(t *testing.T) {
	// No guard configured must be indistinguishable from the pre-guard
	// behaviour — not "fails closed", not "fails open with a warning".
	g := &guardRecorder{loseAfter: 0} // would fail if it were ever consulted
	b := g.install(t, false)

	if err := fastKeyboard(b).Type(t.Context(), guardTestText); err != nil {
		t.Fatalf("Type with a nil guard returned %v, want nil", err)
	}
	if g.keyDowns != len(guardTestText) {
		t.Fatalf("emitted %d characters, want all %d", g.keyDowns, len(guardTestText))
	}
	if g.guardCall != 0 {
		t.Fatalf("guard consulted %d times despite being nil", g.guardCall)
	}
}
