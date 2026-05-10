//go:build integration

package chromekit

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/tuwibu/chromekit/input"
)

// resolveChromePath returns a usable chrome.exe path for the integration test.
// Order: env CHROME_PATH > common Windows install dirs > skip.
func resolveChromePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	if runtime.GOOS != "windows" {
		t.Skip("integration test currently expects Windows chrome.exe — set CHROME_PATH to override")
	}
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skip("no chrome.exe found in standard locations; set CHROME_PATH to run this test")
	return ""
}

// dataURLInputPage embeds a single text input pre-filled with a known string.
// Use a value with a leading 'a' so a buggy Ctrl+A path that types "a" first
// (chromedp issue #1384) leaves a detectable artifact.
const dataURLInputPage = `data:text/html,` +
	`<html><body>` +
	`<input id="q" type="text" value="hello world">` +
	`<script>document.getElementById('q').focus();</script>` +
	`</body></html>`

// TestIntegration_KeyboardClearInput_ClearsViaCDP boots a real headless Chrome
// over CDP, focuses an input via selector, calls KeyboardAPI.ClearInput, and
// asserts the field is empty. This is the regression guard for chromedp issue
// #1384 — sending Ctrl+A via DispatchKeyEvent with WithText("a") would type
// "a" into the field instead of triggering select-all. cdpBackend.KeyCombo
// suppresses .text when modifiers are held; this test proves the suppression.
func TestIntegration_KeyboardClearInput_ClearsViaCDP(t *testing.T) {
	chromePath := resolveChromePath(t)

	b, err := Launch(chromePath, WithHeadless(true))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer b.Close()

	p := b.Current()
	if err := p.Navigate(dataURLInputPage, 15*time.Second); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Sanity — field starts populated as designed.
	var initial string
	if err := p.Evaluate(`document.getElementById('q').value`, &initial); err != nil {
		t.Fatalf("read initial value: %v", err)
	}
	if initial != "hello world" {
		t.Fatalf("setup: expected 'hello world' in field, got %q", initial)
	}

	if err := p.Keyboard().ClearInput("#q"); err != nil {
		t.Fatalf("ClearInput: %v", err)
	}

	var after string
	if err := p.Evaluate(`document.getElementById('q').value`, &after); err != nil {
		t.Fatalf("read post-clear value: %v", err)
	}
	if after != "" {
		// Common buggy outcomes: "a" (Ctrl+A typed instead of selecting),
		// "ahello world" (Ctrl+A inserted "a" then Delete deleted forward
		// from cursor leaving suffix), or "hello world" (combo no-op).
		t.Fatalf("ClearInput failed — chromedp #1384 regression. value=%q, want empty", after)
	}
}

// TestIntegration_KeyboardClearInput_NoStrayCharacterFromCtrlA isolates the
// specific failure mode of chromedp #1384: even if the field ends empty,
// confirm Ctrl+A by itself doesn't inject 'a'. Re-fills the input, sends
// only Ctrl+A (no Delete), then asserts value is unchanged. A buggy path
// would mutate the value to include an extra "a".
func TestIntegration_KeyboardClearInput_NoStrayCharacterFromCtrlA(t *testing.T) {
	chromePath := resolveChromePath(t)

	b, err := Launch(chromePath, WithHeadless(true))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer b.Close()

	p := b.Current()
	if err := p.Navigate(dataURLInputPage, 15*time.Second); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Send only Ctrl+A through the page-level Press helper (same dispatch
	// path used inside ClearInput's wrapper before the Delete). If #1384
	// regresses, the input's value would gain an "a" — select-all is supposed
	// to be a no-op on the value itself.
	if err := p.Keyboard().Press("a", input.ModCtrl); err != nil {
		t.Fatalf("Press Ctrl+A: %v", err)
	}

	var v string
	if err := p.Evaluate(`document.getElementById('q').value`, &v); err != nil {
		t.Fatalf("read value: %v", err)
	}
	if v != "hello world" {
		t.Fatalf("Ctrl+A mutated input value — #1384 regression. got=%q, want %q", v, "hello world")
	}
}
