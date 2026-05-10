package chromekit

import (
	"testing"
)

func TestTypeSelectorConfig_Defaults(t *testing.T) {
	c := TypeSelectorConfig{}.withDefaults()
	if c.MaxAttempts != 5 {
		t.Errorf("MaxAttempts default = %d, want 5", c.MaxAttempts)
	}
	if c.Strict {
		t.Errorf("Strict default = true, want false")
	}
}

func TestTypeSelectorConfig_NegativeAttemptsClamp(t *testing.T) {
	for _, in := range []int{-1, 0} {
		c := TypeSelectorConfig{MaxAttempts: in}.withDefaults()
		if c.MaxAttempts != 5 {
			t.Errorf("MaxAttempts(%d) → %d, want 5", in, c.MaxAttempts)
		}
	}
	c := TypeSelectorConfig{MaxAttempts: 3}.withDefaults()
	if c.MaxAttempts != 3 {
		t.Errorf("MaxAttempts(3) preserved → %d", c.MaxAttempts)
	}
}

// Sanity-check the JS expression compiles to expected branches by spot-reading
// it. The runtime behavior is exercised by the integration test; this guards
// against accidental whitespace edits that break the literal.
func TestReadableValueJS_ContainsAllBranches(t *testing.T) {
	must := []string{
		"'value' in el",
		"isContentEditable",
		"innerText",
		"textContent",
	}
	for _, s := range must {
		if !contains(readableValueJS, s) {
			t.Errorf("readableValueJS missing required branch %q", s)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
