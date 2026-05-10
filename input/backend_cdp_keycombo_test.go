package input

import (
	"strings"
	"testing"

	cdpinput "github.com/chromedp/cdproto/input"
)

// TestVKToCDPKey_CoversDeclaredVKCodes asserts every VK constant exposed by
// the package has an entry in vkToCDPKey. New VKs added to backend.go without
// a matching map entry would silently degrade KeyCombo to "unsupported vk".
func TestVKToCDPKey_CoversDeclaredVKCodes(t *testing.T) {
	required := map[string]uint16{
		"VKReturn": VKReturn,
		"VKEscape": VKEscape,
		"VKEnd":    VKEnd,
		"VKHome":   VKHome,
		"VKDelete": VKDelete,
		"VKA":      VKA,
		"VKL":      VKL,
	}
	for name, vk := range required {
		spec, ok := vkToCDPKey[vk]
		if !ok {
			t.Errorf("vkToCDPKey missing entry for %s (0x%X)", name, vk)
			continue
		}
		if spec.key == "" || spec.code == "" {
			t.Errorf("%s spec has empty key/code: %+v", name, spec)
		}
	}
}

func TestCDPModBits_MatchesCDPOrdering(t *testing.T) {
	cases := []struct {
		name string
		in   Modifier
		want cdpinput.Modifier
	}{
		{"none", 0, 0},
		{"ctrl", ModCtrl, 2},
		{"alt", ModAlt, 1},
		{"shift", ModShift, 8},
		{"meta", ModWin, 4},
		{"ctrl+shift", ModCtrl | ModShift, 10},
		{"all", ModCtrl | ModAlt | ModShift | ModWin, 15},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cdpModBits(c.in)
			if got != c.want {
				t.Errorf("cdpModBits(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestCDPBackend_KeyCombo_UnsupportedVK verifies the new error path: a VK
// not in vkToCDPKey returns a descriptive error mentioning the VK code,
// rather than ErrNotSupported (the old blanket behavior).
func TestCDPBackend_KeyCombo_UnsupportedVK(t *testing.T) {
	b := newCDP()
	err := b.KeyCombo(nil, 0xFFFF, 0)
	if err == nil {
		t.Fatal("expected error for unsupported vk, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported vk") {
		t.Errorf("error message should mention 'unsupported vk', got: %v", err)
	}
}
