package chromekit

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestAttachPageNamespaces_AllFieldsNonNil locks the invariant that every
// freshly constructed Page has the namespace API holders wired. Regression
// for the v0.4.0 panic where Connect-path Pages skipped this init and
// page.Mouse().MoveTo blew up with a nil-pointer deref on the dispatcher's
// critical section.
//
// Pure assignment test — no chromedp, no real Chrome. If this passes, the
// listener-side init is the only remaining variable in wirePage; that is
// exercised by integration paths.
func TestAttachPageNamespaces_AllFieldsNonNil(t *testing.T) {
	p := &Page{}
	attachPageNamespaces(p)

	if p.Mouse() == nil {
		t.Error("Mouse() == nil after attachPageNamespaces")
	}
	if p.Keyboard() == nil {
		t.Error("Keyboard() == nil after attachPageNamespaces")
	}
	if p.Cookies() == nil {
		t.Error("Cookies() == nil after attachPageNamespaces")
	}
	if p.Downloads() == nil {
		t.Error("Downloads() == nil after attachPageNamespaces")
	}

	// Backref check — each holder must point back at p so its methods can
	// reach Page state. Without this the holders are non-nil but unusable.
	if p.mouseAPI.p != p {
		t.Error("MouseAPI.p does not point at the Page")
	}
	if p.keyboardAPI.p != p {
		t.Error("KeyboardAPI.p does not point at the Page")
	}
	if p.cookiesAPI.p != p {
		t.Error("CookiesAPI.p does not point at the Page")
	}
	if p.downloadsAPI.p != p {
		t.Error("DownloadsAPI.p does not point at the Page")
	}
}

// TestAttachKnownTargets_WiresEveryPage locks the call-site invariant — both
// the primary and non-primary branches of attachKnownTargets must run
// wirePage. Without it Connect-path users hit a nil-pointer panic on the
// first page.Mouse() / .Keyboard() call (see chromefleet panic at
// (*MouseAPI).MoveTo with receiver 0x0).
//
// We construct a chromedp parent context but do NOT dial a browser — the
// listener init inside wirePage will issue a chromedp.Run that errors out
// silently (the error is discarded) without blocking. The assertions below
// only depend on the synchronous namespace-pointer assignment, which runs
// before the listener call.
func TestAttachKnownTargets_WiresEveryPage(t *testing.T) {
	parent, parentCancel := chromedp.NewContext(context.Background())
	defer parentCancel()

	b := &Browser{
		ctx:    parent,
		cancel: parentCancel,
		log:    noopLogger{},
		cfg:    defaultConfig(),
	}
	b.attachKnownTargets([]targetEntry{
		{ID: "primary"},
		{ID: "secondary"},
	}, "primary")

	if got := len(b.pages); got != 2 {
		t.Fatalf("want 2 pages registered, got %d", got)
	}
	for i, p := range b.pages {
		if p.Mouse() == nil {
			t.Errorf("page[%d] Mouse() == nil — wirePage missed at call site", i)
		}
		if p.Keyboard() == nil {
			t.Errorf("page[%d] Keyboard() == nil", i)
		}
		if p.Cookies() == nil {
			t.Errorf("page[%d] Cookies() == nil", i)
		}
		if p.Downloads() == nil {
			t.Errorf("page[%d] Downloads() == nil", i)
		}
	}
}
