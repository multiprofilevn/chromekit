package chromekit

import "testing"

func TestStripScheme_HTTPSPrefix(t *testing.T) {
	if got := stripScheme("https://example.com/path"); got != "example.com/path" {
		t.Errorf("got %q, want %q", got, "example.com/path")
	}
}

func TestStripScheme_HTTPPrefixCaseInsensitive(t *testing.T) {
	if got := stripScheme("HTTP://example.com"); got != "example.com" {
		t.Errorf("got %q, want %q", got, "example.com")
	}
	if got := stripScheme("HtTpS://example.com"); got != "example.com" {
		t.Errorf("got %q, want %q", got, "example.com")
	}
}

func TestStripScheme_NoSchemeUnchanged(t *testing.T) {
	if got := stripScheme("example.com/path"); got != "example.com/path" {
		t.Errorf("got %q, want %q", got, "example.com/path")
	}
}

func TestStripScheme_NonHTTPSchemeUnchanged(t *testing.T) {
	if got := stripScheme("ftp://example.com"); got != "ftp://example.com" {
		t.Errorf("got %q, want %q", got, "ftp://example.com")
	}
}

func TestStripScheme_PreservesQueryAndFragment(t *testing.T) {
	if got := stripScheme("https://example.com/page?utm=1#section"); got != "example.com/page?utm=1#section" {
		t.Errorf("got %q, want %q", got, "example.com/page?utm=1#section")
	}
}
