//go:build integration

package chromekit

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tuwibu/chromekit/input"
)

// fastHumanCfg keeps the integration test under a couple of seconds while
// still exercising the typo+correction branch (MistakeRate=0 so we don't
// stretch the budget further).
func fastHumanCfg() input.HumanTypeConfig {
	return input.HumanTypeConfig{
		MinDelay:        1 * time.Millisecond,
		MaxDelay:        2 * time.Millisecond,
		MistakeRate:     0,
		CorrectionPause: 1 * time.Millisecond,
	}
}

const typeSelectorPage = `data:text/html,` +
	`<html><body>` +
	`<input id="plain" type="text">` +
	`<textarea id="area"></textarea>` +
	`<div id="rich" contenteditable="true" style="border:1px solid rgb(153,153,153);min-height:40px"></div>` +
	`<input id="readonly-fake" type="text" readonly>` +
	`<input id="strip" type="text" oninput="this.value=this.value.trim()">` +
	`</body></html>`

func TestIntegration_TypeSelector_Input(t *testing.T) {
	p := bootIntegrationPage(t, typeSelectorPage)
	cfg := TypeSelectorConfig{Strict: true, Human: fastHumanCfg()}
	if err := p.Keyboard().TypeSelector("#plain", "hello world", cfg); err != nil {
		t.Fatalf("TypeSelector input: %v", err)
	}
	var got string
	if err := p.Evaluate(`document.getElementById('plain').value`, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "hello world" {
		t.Errorf("input value = %q, want %q", got, "hello world")
	}
}

func TestIntegration_TypeSelector_Textarea(t *testing.T) {
	p := bootIntegrationPage(t, typeSelectorPage)
	cfg := TypeSelectorConfig{Strict: true, Human: fastHumanCfg()}
	if err := p.Keyboard().TypeSelector("#area", "multi line text", cfg); err != nil {
		t.Fatalf("TypeSelector textarea: %v", err)
	}
	var got string
	if err := p.Evaluate(`document.getElementById('area').value`, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "multi line text" {
		t.Errorf("textarea value = %q, want %q", got, "multi line text")
	}
}

func TestIntegration_TypeSelector_ContentEditable(t *testing.T) {
	p := bootIntegrationPage(t, typeSelectorPage)
	cfg := TypeSelectorConfig{Strict: true, Human: fastHumanCfg()}
	if err := p.Keyboard().TypeSelector("#rich", "rich editor body", cfg); err != nil {
		t.Fatalf("TypeSelector contenteditable: %v", err)
	}
	var got string
	if err := p.Evaluate(`document.getElementById('rich').innerText`, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	// innerText may carry trailing newline depending on DOM tree — TrimSpace
	// for the same reason TypeSelector does internally.
	if trimmed := strings.TrimSpace(got); trimmed != "rich editor body" {
		t.Errorf("contenteditable text = %q (trimmed %q), want %q", got, trimmed, "rich editor body")
	}
}

func TestIntegration_TypeSelector_TrimAccepted(t *testing.T) {
	// The "#strip" input runs .value.trim() on every input event, so leading
	// whitespace in the requested text is stripped server-side. TypeSelector
	// must still consider this a match because both sides are TrimSpaced.
	p := bootIntegrationPage(t, typeSelectorPage)
	cfg := TypeSelectorConfig{Strict: true, MaxAttempts: 2, Human: fastHumanCfg()}
	if err := p.Keyboard().TypeSelector("#strip", "  trimmed  ", cfg); err != nil {
		t.Fatalf("TypeSelector should accept trim divergence: %v", err)
	}
	var got string
	if err := p.Evaluate(`document.getElementById('strip').value`, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "trimmed" {
		t.Errorf("strip input value = %q, want %q", got, "trimmed")
	}
}

func TestIntegration_TypeSelector_StrictFailsAfterRetries(t *testing.T) {
	p := bootIntegrationPage(t, typeSelectorPage)
	cfg := TypeSelectorConfig{Strict: true, MaxAttempts: 2, Human: fastHumanCfg()}
	err := p.Keyboard().TypeSelector("#readonly-fake", "anything", cfg)
	if err == nil {
		t.Fatal("expected ErrTypeSelectorMismatch on read-only target")
	}
	if !errors.Is(err, ErrTypeSelectorMismatch) {
		t.Errorf("expected ErrTypeSelectorMismatch, got %v", err)
	}
}

func TestIntegration_TypeSelector_NonStrictSwallowsMismatch(t *testing.T) {
	p := bootIntegrationPage(t, typeSelectorPage)
	cfg := TypeSelectorConfig{Strict: false, MaxAttempts: 2, Human: fastHumanCfg()}
	if err := p.Keyboard().TypeSelector("#readonly-fake", "anything", cfg); err != nil {
		t.Fatalf("non-strict should swallow mismatch, got %v", err)
	}
}

// bootIntegrationPage launches headless Chrome and navigates to the data URL.
// Lives here so each test gets a fresh browser/tab — keeps tests independent
// and avoids state bleed between fields.
func bootIntegrationPage(t *testing.T, url string) *Page {
	t.Helper()
	chromePath := resolveChromePath(t)
	// CursorMachine — TypeSelector uses MouseAPI.FocusElement which moves the
	// cursor; default Bezier path takes ~40s in headless. Machine = teleport.
	b, err := Launch(chromePath, WithHeadless(true), WithCursorProfile(CursorMachine))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	p := b.Current()
	if err := p.Navigate(url, 15*time.Second); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	return p
}
