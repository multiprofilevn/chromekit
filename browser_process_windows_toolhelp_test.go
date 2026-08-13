//go:build windows

package chromekit

import "testing"

func TestDescendantProcessIDsRejectsCycles(t *testing.T) {
	children := map[uint32][]uint32{
		1: {2, 1},
		2: {3},
		3: {1, 2},
	}
	got := descendantProcessIDs(1, children)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("cycle traversal = %v, want [1 2 3]", got)
	}
}
