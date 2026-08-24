package input

import (
	"context"
	"testing"
	"time"
)

// A line break must reach the backend as a VK_RETURN combo, never as a
// printable character. KeyChar('\n') is wrong on both backends: CDP encodes it
// as a keyDown plus a separate char event, which an editor that consumes Enter
// in its own keydown handler (Draft.js / the X composer) turns into TWO line
// breaks; native injects U+000A as a Unicode scan code, which Chromium drops,
// losing the break. These tests pin the Enter press so neither regression can
// come back unnoticed.

func noDelayKeyboard(b Backend) *Keyboard {
	return &Keyboard{backend: b, MinDelay: time.Nanosecond, MaxDelay: time.Nanosecond}
}

// assertEnterBetweenAB checks the recorded sequence is exactly
// char 'A' → Enter combo → char 'B', with no printable line-break char.
func assertEnterBetweenAB(t *testing.T, events []event) {
	t.Helper()
	want := []event{
		{kind: "char", ch: 'A'},
		{kind: "combo", vk: VKReturn, mods: 0},
		{kind: "char", ch: 'B'},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events %+v, want %d %+v", len(events), events, len(want), want)
	}
	for i, w := range want {
		if events[i] != w {
			t.Fatalf("event %d = %+v, want %+v (full: %+v)", i, events[i], w, events)
		}
	}
}

func TestTypeSendsLineBreakAsEnter(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"LF", "A\nB"},
		{"CRLF collapses to one Enter", "A\r\nB"},
		{"lone CR", "A\rB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &eventBackend{}
			if err := noDelayKeyboard(b).Type(context.Background(), tc.in); err != nil {
				t.Fatalf("Type: %v", err)
			}
			assertEnterBetweenAB(t, b.events)
		})
	}
}

func TestTypeHumanSendsLineBreakAsEnter(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"LF", "A\nB"},
		{"CRLF collapses to one Enter", "A\r\nB"},
		{"lone CR", "A\rB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &eventBackend{}
			k := &Keyboard{backend: b}
			// MistakeRate 0: a typo would add backspace events and this test
			// is about the break, not the typo simulation.
			if err := k.TypeHuman(context.Background(), tc.in, zeroDelayCfg(1, 0)); err != nil {
				t.Fatalf("TypeHuman: %v", err)
			}
			assertEnterBetweenAB(t, b.events)
		})
	}
}

func TestPressSendsLineBreakAsEnter(t *testing.T) {
	for _, ch := range []rune{'\n', '\r'} {
		b := &eventBackend{}
		if err := noDelayKeyboard(b).Press(context.Background(), ch); err != nil {
			t.Fatalf("Press(%q): %v", ch, err)
		}
		want := []event{{kind: "combo", vk: VKReturn, mods: 0}}
		if len(b.events) != 1 || b.events[0] != want[0] {
			t.Fatalf("Press(%q) emitted %+v, want %+v", ch, b.events, want)
		}
	}
}

func TestNormalizeLineBreaks(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"no breaks", "no breaks"},
		{"a\nb", "a\nb"},
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"a\r\n\r\nb", "a\n\nb"},
	} {
		if got := normalizeLineBreaks(tc.in); got != tc.want {
			t.Fatalf("normalizeLineBreaks(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
