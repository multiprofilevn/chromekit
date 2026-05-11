//go:build integration

package chromekit

import (
	"errors"
	"strings"
	"testing"
)

const selectPage = `data:text/html,` +
	`<html><body>` +
	`<select id="picker">` +
	`<option value="a">A</option>` +
	`<option value="b">B</option>` +
	`<option value="c">C</option>` +
	`</select>` +
	`<div id="echo"></div>` +
	`<script>` +
	`document.getElementById('picker').addEventListener('change', e => {` +
	`document.getElementById('echo').textContent = e.target.value;` +
	`});` +
	`</script>` +
	`</body></html>`

func TestIntegration_SetSelectValue_AssignsAndFiresChange(t *testing.T) {
	p := bootIntegrationPage(t, selectPage)
	if err := p.SetSelectValue("#picker", "b"); err != nil {
		t.Fatalf("SetSelectValue: %v", err)
	}
	var val string
	if err := p.Evaluate(`document.getElementById('picker').value`, &val); err != nil {
		t.Fatalf("read value: %v", err)
	}
	if val != "b" {
		t.Errorf("select.value = %q, want %q", val, "b")
	}
	var echo string
	if err := p.Evaluate(`document.getElementById('echo').textContent`, &echo); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if echo != "b" {
		t.Errorf("change event echo = %q, want %q (event did not fire)", echo, "b")
	}
}

func TestIntegration_SetSelectValue_NotFound(t *testing.T) {
	p := bootIntegrationPage(t, selectPage)
	err := p.SetSelectValue("#nope", "x")
	if err == nil {
		t.Fatal("expected error for missing selector, got nil")
	}
	if !errors.Is(err, ErrEvaluate) {
		t.Errorf("err = %v, want wraps ErrEvaluate", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err message %q missing 'not found'", err.Error())
	}
}
