package chromekit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newWaitTestBrowser builds a Browser whose port points at an httptest server
// serving the given /json/list body. No real Chrome — exercises the poll +
// match + known-skip logic of WaitForNewPage without the attach path.
func newWaitTestBrowser(t *testing.T, listJSON string) *Browser {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, listJSON)
	}))
	t.Cleanup(srv.Close)
	portStr := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port from %q: %v", srv.URL, err)
	}
	return &Browser{ctx: context.Background(), port: port}
}

func TestWaitForNewPage_TimeoutWhenURLDoesNotMatch(t *testing.T) {
	list := `[{"id":"t1","type":"page","url":"https://www.tiktok.com/login"}]`
	b := newWaitTestBrowser(t, list)
	_, err := b.WaitForNewPage("accounts.google.com", 500*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout when no URL matches, got %v", err)
	}
}

func TestWaitForNewPage_SkipsAlreadyTrackedTarget(t *testing.T) {
	// URL matches but the target is already tracked → must be skipped → timeout.
	list := `[{"id":"known","type":"page","url":"https://accounts.google.com/o/oauth2"}]`
	b := newWaitTestBrowser(t, list)
	b.pages = []*Page{{id: "known"}}
	_, err := b.WaitForNewPage("accounts.google.com", 500*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout (known target skipped), got %v", err)
	}
}

func TestWaitForNewPage_RequiresPort(t *testing.T) {
	b := &Browser{ctx: context.Background(), port: 0}
	_, err := b.WaitForNewPage("x", 100*time.Millisecond)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig without a debug port, got %v", err)
	}
}

func TestKnownTargetIDs_Snapshot(t *testing.T) {
	b := &Browser{pages: []*Page{{id: "a"}, {id: "b"}}}
	got := b.knownTargetIDs()
	if !got["a"] || !got["b"] || len(got) != 2 {
		t.Fatalf("knownTargetIDs = %v, want {a,b}", got)
	}
}
