//go:build integration

// Behavior tests for phase-06's new Page methods (chromekit superset
// parity): Title/URL cache split, SelectBy*, CheckBox/SetValue. Each test
// pins the same edge case firefoxkit's own tests check for the equivalent
// method — see phase-06-chromekit-superset-parity.md, Test Spec items 4-6.
// Continued in page_parity_behavior2_integration_test.go (items 7-9).
package chromekit

import (
	"context"
	"errors"
	"testing"
	"time"
)

const titleURLPage1 = `data:text/html,<title>Page One</title><body>one</body>`
const titleURLPage2 = `data:text/html,<title>Page Two</title><body>two</body>`

// 4. Title/URL have a cache-vs-live split: Title()/URL() read the cache,
// TitleLive/URLLive re-query and refresh it. chromekit does not eagerly
// refresh the cache on Navigate (documented divergence — see
// page_title_url.go), so Title() legitimately stays stale until the next
// explicit TitleLive call.
func TestIntegration_Page_TitleAndURL(t *testing.T) {
	p := bootIntegrationPage(t, titleURLPage1)

	if got := p.Title(); got != "" {
		t.Errorf("Title() before any TitleLive call = %q, want empty", got)
	}
	title, err := p.TitleLive(context.Background())
	if err != nil {
		t.Fatalf("TitleLive: %v", err)
	}
	if title != "Page One" {
		t.Errorf("TitleLive = %q, want %q", title, "Page One")
	}
	if got := p.Title(); got != "Page One" {
		t.Errorf("Title() after TitleLive = %q, want %q", got, "Page One")
	}

	url, err := p.URLLive(context.Background())
	if err != nil {
		t.Fatalf("URLLive: %v", err)
	}
	if got := p.URL(); got != url {
		t.Errorf("URL() = %q, want cached %q", got, url)
	}

	if err := p.Navigate(titleURLPage2, 10*time.Second); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if got := p.Title(); got != "Page One" {
		t.Errorf("Title() after Navigate = %q, want still-stale %q", got, "Page One")
	}
	live, err := p.TitleLive(context.Background())
	if err != nil {
		t.Fatalf("TitleLive after nav: %v", err)
	}
	if live != "Page Two" {
		t.Errorf("TitleLive after nav = %q, want %q", live, "Page Two")
	}
	if got := p.Title(); got != "Page Two" {
		t.Errorf("Title() after second TitleLive = %q, want %q", got, "Page Two")
	}
}

const selectAllPage = `data:text/html,` +
	`<html><body>` +
	`<select id="picker">` +
	`<option value="a">A</option>` +
	`<option value="b">B</option>` +
	`<option value="c">C</option>` +
	`</select>` +
	`</body></html>`

// 5. SelectByIndex/Text/Value pick the right option and fire input+change;
// a missing selector or no-match option surfaces ErrPageNotFound — same
// error family firefoxkit's own SelectBy* return.
func TestIntegration_Page_SelectByIndexTextValue(t *testing.T) {
	p := bootIntegrationPage(t, selectAllPage)

	readVal := func() string {
		var v string
		if err := p.Evaluate(`document.getElementById('picker').value`, &v); err != nil {
			t.Fatalf("read value: %v", err)
		}
		return v
	}

	if err := p.SelectByIndex("#picker", 1); err != nil {
		t.Fatalf("SelectByIndex: %v", err)
	}
	if got := readVal(); got != "b" {
		t.Errorf("after SelectByIndex(1) value = %q, want %q", got, "b")
	}

	if err := p.SelectByText("#picker", "C"); err != nil {
		t.Fatalf("SelectByText: %v", err)
	}
	if got := readVal(); got != "c" {
		t.Errorf("after SelectByText(C) value = %q, want %q", got, "c")
	}

	if err := p.SelectByValue("#picker", "a"); err != nil {
		t.Fatalf("SelectByValue: %v", err)
	}
	if got := readVal(); got != "a" {
		t.Errorf("after SelectByValue(a) value = %q, want %q", got, "a")
	}

	if err := p.SelectByValue("#picker", "zzz"); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("SelectByValue no-match err = %v, want ErrPageNotFound", err)
	}
	if err := p.SelectByIndex("#nope", 0); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("SelectByIndex missing selector err = %v, want ErrPageNotFound", err)
	}
	if err := p.SelectByText("#nope", "x"); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("SelectByText missing selector err = %v, want ErrPageNotFound", err)
	}
}

const formValuePage = `data:text/html,<body>` +
	`<input id="text1" type="text"><input id="chk1" type="checkbox">` +
	`</body>`

// 6. CheckBox/SetValue set the field and fire input+change; a missing
// selector surfaces ErrPageNotFound — mirrors firefoxkit exactly.
func TestIntegration_Page_CheckBoxSetValue(t *testing.T) {
	p := bootIntegrationPage(t, formValuePage)

	if err := p.SetValue("#text1", "hello"); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	var v string
	if err := p.Evaluate(`document.getElementById('text1').value`, &v); err != nil {
		t.Fatalf("read value: %v", err)
	}
	if v != "hello" {
		t.Errorf("text1 value = %q, want %q", v, "hello")
	}

	if err := p.CheckBox("#chk1", true); err != nil {
		t.Fatalf("CheckBox(true): %v", err)
	}
	var checked bool
	if err := p.Evaluate(`document.getElementById('chk1').checked`, &checked); err != nil {
		t.Fatalf("read checked: %v", err)
	}
	if !checked {
		t.Errorf("chk1.checked = false, want true")
	}

	if err := p.CheckBox("#chk1", false); err != nil {
		t.Fatalf("CheckBox(false): %v", err)
	}
	if err := p.Evaluate(`document.getElementById('chk1').checked`, &checked); err != nil {
		t.Fatalf("read checked: %v", err)
	}
	if checked {
		t.Errorf("chk1.checked = true, want false")
	}

	if err := p.SetValue("#nope", "x"); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("SetValue missing selector err = %v, want ErrPageNotFound", err)
	}
	if err := p.CheckBox("#nope", true); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("CheckBox missing selector err = %v, want ErrPageNotFound", err)
	}
}
