package chromekit

import (
	"testing"
	"time"
)

// BringToFront polls Chrome for up to three seconds. Holding pagesMu across it
// froze every Pages/Current/NewPage/ClosePage caller in the process for that
// long — the "everything stalls when a tab is switched" symptom.

func TestSetCurrentByIndex_ReleasesLockBeforeActivation(t *testing.T) {
	b, _ := newTestBrowser(t)
	b.addTestPage(t, "a", true)
	b.addTestPage(t, "b", false)

	p, err := b.setCurrentByIndex(1)
	if err != nil {
		t.Fatalf("setCurrentByIndex(1) err = %v", err)
	}
	if p == nil || p.id != "b" {
		t.Fatalf("setCurrentByIndex(1) = %v, want page b", p)
	}
	if b.Current().id != "b" {
		t.Errorf("current = %v, want b", b.Current().id)
	}

	// The lock must be free by the time the page is handed back — activation
	// happens after this point and must not block other callers.
	done := make(chan struct{})
	go func() {
		_ = b.Pages()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pagesMu still held after setCurrentByIndex returned — activation would convoy every caller")
	}
}

func TestSetCurrentByIndex_OutOfRange(t *testing.T) {
	b, _ := newTestBrowser(t)
	b.addTestPage(t, "a", true)

	for _, idx := range []int{-1, 1, 99} {
		if _, err := b.setCurrentByIndex(idx); err == nil {
			t.Errorf("setCurrentByIndex(%d) err = nil, want ErrPageNotFound", idx)
		}
	}
	if b.Current().id != "a" {
		t.Errorf("current changed on an out-of-range switch: %v", b.Current().id)
	}
}

func TestSwitchByID_RefusesRemovedPage(t *testing.T) {
	// SwitchPageByURL/Origin pick their target from a Pages() snapshot, so the
	// page can be closed before the promotion lands. Promoting it would point
	// current at a cancelled context.
	b, _ := newTestBrowser(t)
	b.addTestPage(t, "a", true)
	b.addTestPage(t, "b", false)

	b.pagesMu.Lock()
	b.removePageLocked("b")
	b.pagesMu.Unlock()

	if got := b.setCurrentIfTracked("b"); got != nil {
		t.Fatalf("promoted a page that was already closed: %v", got)
	}
	if b.Current().id != "a" {
		t.Errorf("current = %v, want the surviving page a", b.Current().id)
	}
}
