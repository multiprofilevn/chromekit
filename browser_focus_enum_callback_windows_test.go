//go:build windows

package chromekit

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// syscall.NewCallback allocates a callback slot that is never released, and Go
// only guarantees 1024 of them per process. Building one per HWND lookup makes
// a long-running fleet panic with "too many callback functions". These tests
// lock in the single-allocation design.

func TestEnumCallback_SingleAllocationSite(t *testing.T) {
	src, err := os.ReadFile("browser_focus_windows.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(src), "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") {
			continue
		}
		n += strings.Count(code, "syscall.NewCallback")
	}
	if n != 1 {
		t.Fatalf("browser_focus_windows.go has %d syscall.NewCallback call sites, want exactly 1 "+
			"(package-level, allocated once) — per-call callbacks exhaust the 1024-slot limit", n)
	}
}

func TestEnumCallback_NoLeakOverManyCalls(t *testing.T) {
	// Comfortably past Go's guaranteed 1024-callback ceiling: the pre-fix code
	// allocated one slot per call and panicked in this range.
	for i := 0; i < 1100; i++ {
		if _, err := findChromeWindowsByTitle("chromekit-no-such-window"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}

func TestEnumChromeWindows_ConcurrentDoesNotMixResults(t *testing.T) {
	// Two filters that can never both accept: results must never bleed across
	// concurrent enumerations sharing the package-level accumulator.
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				got, err := enumChromeWindows(func(uintptr) bool { return false })
				if err != nil {
					t.Errorf("goroutine %d: %v", g, err)
					return
				}
				if len(got) != 0 {
					t.Errorf("goroutine %d: reject-all filter returned %d windows — accumulator bled across calls", g, len(got))
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestTitleMatches(t *testing.T) {
	cases := []struct {
		name   string
		title  string
		needle string
		want   bool
	}{
		{"exact", "Example Domain", "example domain", true},
		{"substring", "Example Domain - Google Chrome", "example", true},
		{"case insensitive", "EXAMPLE", "example", true},
		{"empty title never matches", "", "example", false},
		{"empty title, empty needle", "", "", false},
		{"no match", "Some Other Tab", "example", false},
		{"empty needle matches any non-empty title", "Anything", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titleMatches(tc.title, tc.needle); got != tc.want {
				t.Fatalf("titleMatches(%q, %q) = %v, want %v", tc.title, tc.needle, got, tc.want)
			}
		})
	}
}
