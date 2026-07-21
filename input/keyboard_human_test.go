package input

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// eventBackend records every KeyChar/KeyCombo invocation in order so tests
// can assert the exact keystroke sequence TypeHuman emits.
type eventBackend struct {
	events      []event
	failKeyChar rune   // if non-zero, KeyChar(this rune) returns failErr
	failComboVK uint16 // if non-zero, KeyCombo(this vk) returns failErr
	failErr     error
}

type event struct {
	kind string // "char" or "combo"
	ch   rune
	vk   uint16
	mods Modifier
}

func (b *eventBackend) MouseMove(_ context.Context, _, _ float64) error { return nil }
func (b *eventBackend) MouseDown(_ context.Context, _, _ float64, _ MouseButton) error {
	return nil
}
func (b *eventBackend) MouseUp(_ context.Context, _, _ float64, _ MouseButton) error { return nil }
func (b *eventBackend) MouseScroll(_ context.Context, _, _ float64) error            { return nil }
func (b *eventBackend) InsertText(_ context.Context, _ string) error                 { return nil }

func (b *eventBackend) KeyChar(_ context.Context, ch rune) error {
	b.events = append(b.events, event{kind: "char", ch: ch})
	if b.failKeyChar != 0 && ch == b.failKeyChar {
		return b.failErr
	}
	return nil
}

func (b *eventBackend) KeyCombo(_ context.Context, vk uint16, mods Modifier) error {
	b.events = append(b.events, event{kind: "combo", vk: vk, mods: mods})
	if b.failComboVK != 0 && vk == b.failComboVK {
		return b.failErr
	}
	return nil
}

func newFastKeyboard() *Keyboard {
	return &Keyboard{backend: &eventBackend{}}
}

// zeroDelayCfg keeps tests fast — no real sleep between keystrokes.
func zeroDelayCfg(seed int64, mistakeRate float64) HumanTypeConfig {
	return HumanTypeConfig{
		MinDelay:        1 * time.Nanosecond,
		MaxDelay:        1 * time.Nanosecond,
		MistakeRate:     mistakeRate,
		CorrectionPause: 1 * time.Nanosecond,
		Rand:            rand.New(rand.NewSource(seed)),
	}
}

func TestTypeHuman_EmptyText_NoBackendCalls(t *testing.T) {
	rb := &eventBackend{}
	k := &Keyboard{backend: rb}
	if err := k.TypeHuman(context.Background(), "", zeroDelayCfg(1, 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rb.events) != 0 {
		t.Errorf("expected 0 events for empty text, got %d", len(rb.events))
	}
}

func TestTypeHuman_NoMistakes_TypesEachCharOnce(t *testing.T) {
	rb := &eventBackend{}
	k := &Keyboard{backend: rb}
	if err := k.TypeHuman(context.Background(), "hello", zeroDelayCfg(1, 0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []event{
		{kind: "char", ch: 'h'},
		{kind: "char", ch: 'e'},
		{kind: "char", ch: 'l'},
		{kind: "char", ch: 'l'},
		{kind: "char", ch: 'o'},
	}
	if !equalEvents(rb.events, want) {
		t.Errorf("event sequence = %+v, want %+v", rb.events, want)
	}
}

func TestTypeHuman_FullMistakeRate_TypoBackspaceCorrect(t *testing.T) {
	// MistakeRate=1 with a Latin char that has neighbors → every char emits
	// (typo, backspace combo, correct).
	rb := &eventBackend{}
	k := &Keyboard{backend: rb}
	if err := k.TypeHuman(context.Background(), "ab", zeroDelayCfg(42, 1.0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect exactly 3 events per char: char(typo), combo(backspace), char(correct).
	if len(rb.events) != 6 {
		t.Fatalf("expected 6 events, got %d: %+v", len(rb.events), rb.events)
	}
	// Validate structural pattern; exact typo char depends on RNG, just assert
	// it differs from the intended char and lives in QWERTY neighbor set.
	checkCorrection(t, rb.events[0:3], 'a')
	checkCorrection(t, rb.events[3:6], 'b')
}

func TestTypeHuman_NonLatinChar_NoTypoEvenAtFullMistakeRate(t *testing.T) {
	// '!' has no entry in qwertyNeighbors → pickTypo returns false → straight type.
	rb := &eventBackend{}
	k := &Keyboard{backend: rb}
	if err := k.TypeHuman(context.Background(), "!?", zeroDelayCfg(7, 1.0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []event{
		{kind: "char", ch: '!'},
		{kind: "char", ch: '?'},
	}
	if !equalEvents(rb.events, want) {
		t.Errorf("event sequence = %+v, want %+v", rb.events, want)
	}
}

func TestTypeHuman_UppercaseTypoPreservesCase(t *testing.T) {
	rb := &eventBackend{}
	k := &Keyboard{backend: rb}
	if err := k.TypeHuman(context.Background(), "A", zeroDelayCfg(3, 1.0)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rb.events) != 3 {
		t.Fatalf("expected typo+backspace+correct (3 events), got %d", len(rb.events))
	}
	typo := rb.events[0]
	if typo.kind != "char" || typo.ch < 'A' || typo.ch > 'Z' {
		t.Errorf("typo for 'A' should be uppercase Latin, got %+v", typo)
	}
	if typo.ch == 'A' {
		t.Errorf("typo char should differ from intended 'A'")
	}
}

func TestTypeHuman_ContextCanceledMidWord_ReturnsCtxErr(t *testing.T) {
	rb := &eventBackend{}
	k := &Keyboard{backend: rb}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before any keystroke
	err := k.TypeHuman(ctx, "abc", zeroDelayCfg(1, 0))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if len(rb.events) != 0 {
		t.Errorf("expected no events after pre-canceled context, got %d", len(rb.events))
	}
}

func TestTypeHuman_BackendErrorPropagates(t *testing.T) {
	sentinel := errors.New("backend down")
	rb := &eventBackend{failKeyChar: 'b', failErr: sentinel}
	k := &Keyboard{backend: rb}
	err := k.TypeHuman(context.Background(), "ab", zeroDelayCfg(1, 0))
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	// Should have typed 'a' OK then failed on 'b'.
	if len(rb.events) != 2 || rb.events[1].ch != 'b' {
		t.Errorf("expected to stop after failing 'b', got %+v", rb.events)
	}
}

func TestTypeHuman_BackspaceFailureAborts(t *testing.T) {
	sentinel := errors.New("no backspace")
	rb := &eventBackend{failComboVK: VKBackspace, failErr: sentinel}
	k := &Keyboard{backend: rb}
	err := k.TypeHuman(context.Background(), "a", zeroDelayCfg(42, 1.0))
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	// typo char then failed backspace — no correction char emitted.
	if len(rb.events) != 2 {
		t.Errorf("expected typo + backspace events only, got %+v", rb.events)
	}
	if rb.events[1].kind != "combo" || rb.events[1].vk != VKBackspace {
		t.Errorf("expected backspace combo as 2nd event, got %+v", rb.events[1])
	}
}

func TestHumanTypeConfig_DefaultsApplied(t *testing.T) {
	c := HumanTypeConfig{}.withDefaults()
	if c.MinDelay != 80*time.Millisecond {
		t.Errorf("MinDelay default = %v, want 80ms", c.MinDelay)
	}
	if c.MaxDelay != 220*time.Millisecond {
		t.Errorf("MaxDelay default = %v, want 220ms", c.MaxDelay)
	}
	if c.CorrectionPause != 250*time.Millisecond {
		t.Errorf("CorrectionPause default = %v, want 250ms", c.CorrectionPause)
	}
	if c.MistakeRate != 0 {
		t.Errorf("MistakeRate default = %v, want 0", c.MistakeRate)
	}
}

func TestHumanTypeConfig_ClampsAndOrdering(t *testing.T) {
	// MaxDelay < MinDelay → MaxDelay snaps up to MinDelay.
	c := HumanTypeConfig{MinDelay: 100 * time.Millisecond, MaxDelay: 10 * time.Millisecond}.withDefaults()
	if c.MaxDelay != c.MinDelay {
		t.Errorf("expected MaxDelay clamped to MinDelay, got min=%v max=%v", c.MinDelay, c.MaxDelay)
	}
	// MistakeRate out-of-range gets clamped.
	if got := (HumanTypeConfig{MistakeRate: 2}).withDefaults().MistakeRate; got != 1 {
		t.Errorf("MistakeRate>1 should clamp to 1, got %v", got)
	}
	if got := (HumanTypeConfig{MistakeRate: -5}).withDefaults().MistakeRate; got != 0 {
		t.Errorf("MistakeRate<0 should clamp to 0, got %v", got)
	}
}

func TestPickTypo_LowercaseInNeighborSet(t *testing.T) {
	// Force first index → 'q' for 'a' (qwertyNeighbors['a']="qwsz").
	got, ok := pickTypo('a', func(_ int) int { return 0 })
	if !ok || got != 'q' {
		t.Errorf("pickTypo('a', 0) = (%q, %v), want ('q', true)", got, ok)
	}
}

func TestPickTypo_UnknownRuneReturnsFalse(t *testing.T) {
	if _, ok := pickTypo('!', func(_ int) int { return 0 }); ok {
		t.Errorf("pickTypo('!') should return ok=false")
	}
	if _, ok := pickTypo('β', func(_ int) int { return 0 }); ok {
		t.Errorf("pickTypo('β') should return ok=false")
	}
}

func TestHumanDelay_BoundsRespected(t *testing.T) {
	d := humanDelay(50*time.Millisecond, 50*time.Millisecond, func(int) int { return 0 })
	if d != 50*time.Millisecond {
		t.Errorf("equal min/max → %v, want 50ms", d)
	}
	d = humanDelay(50*time.Millisecond, 150*time.Millisecond, func(int) int { return 99 })
	if d < 50*time.Millisecond || d > 150*time.Millisecond {
		t.Errorf("delay %v out of [50ms, 150ms]", d)
	}
}

// bufferBackend simulates a real text input: KeyChar appends to buf,
// KeyCombo(VKBackspace, 0) pops the last rune. Any other combo is a no-op.
// Lets tests assert the *final visible text* — what actually lands in the
// field after every typo+backspace+retry cycle has played out.
type bufferBackend struct {
	buf []rune
}

func (b *bufferBackend) MouseMove(_ context.Context, _, _ float64) error { return nil }
func (b *bufferBackend) MouseDown(_ context.Context, _, _ float64, _ MouseButton) error {
	return nil
}
func (b *bufferBackend) MouseUp(_ context.Context, _, _ float64, _ MouseButton) error { return nil }
func (b *bufferBackend) MouseScroll(_ context.Context, _, _ float64) error            { return nil }
func (b *bufferBackend) InsertText(_ context.Context, _ string) error                 { return nil }
func (b *bufferBackend) KeyChar(_ context.Context, ch rune) error {
	b.buf = append(b.buf, ch)
	return nil
}
func (b *bufferBackend) KeyCombo(_ context.Context, vk uint16, _ Modifier) error {
	if vk == VKBackspace && len(b.buf) > 0 {
		b.buf = b.buf[:len(b.buf)-1]
	}
	return nil
}

// TestTypeHuman_FinalBufferMatchesInput is the contract test: regardless of
// how many typos fire mid-flight, the field must end up equal to the
// requested text. Sweeps many seeds + the worst-case MistakeRate=1 to ensure
// every code path through pickTypo + backspace + retry restores the intended
// rune. If TypeHuman ever forgets to retype, mistypes the correction, or
// off-by-ones the backspace, this test fails.
func TestTypeHuman_FinalBufferMatchesInput(t *testing.T) {
	inputs := []string{
		"hello",
		"Hello World",
		"abcdefghijklmnopqrstuvwxyz",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"Mix3d c4se w!th symb0ls",
		"a",
		"",
	}
	rates := []float64{0, 0.5, 1.0}
	for _, in := range inputs {
		for _, rate := range rates {
			for seed := int64(0); seed < 25; seed++ {
				bb := &bufferBackend{}
				k := &Keyboard{backend: bb}
				cfg := zeroDelayCfg(seed, rate)
				if err := k.TypeHuman(context.Background(), in, cfg); err != nil {
					t.Fatalf("input=%q rate=%v seed=%d: unexpected error: %v", in, rate, seed, err)
				}
				if got := string(bb.buf); got != in {
					t.Errorf("input=%q rate=%v seed=%d: final buffer = %q, want %q", in, rate, seed, got, in)
				}
			}
		}
	}
}

// --- helpers ---

func equalEvents(a, b []event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func checkCorrection(t *testing.T, events []event, intended rune) {
	t.Helper()
	if len(events) != 3 {
		t.Fatalf("expected 3-event correction block, got %d: %+v", len(events), events)
	}
	if events[0].kind != "char" || events[0].ch == intended {
		t.Errorf("event[0] should be a typo char != %q, got %+v", intended, events[0])
	}
	if events[1].kind != "combo" || events[1].vk != VKBackspace {
		t.Errorf("event[1] should be Backspace combo, got %+v", events[1])
	}
	if events[2].kind != "char" || events[2].ch != intended {
		t.Errorf("event[2] should be correct char %q, got %+v", intended, events[2])
	}
	// And the typo must be a valid QWERTY neighbor of intended.
	neighbors, ok := qwertyNeighbors[intended]
	if !ok {
		t.Fatalf("intended char %q has no neighbor set — test setup wrong", intended)
	}
	found := false
	for _, n := range neighbors {
		if rune(n) == events[0].ch {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("typo %q is not a QWERTY neighbor of %q (allowed: %s)", events[0].ch, intended, neighbors)
	}
}
