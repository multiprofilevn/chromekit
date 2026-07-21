package chromekit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestFetchPageTargets_FiltersToPageType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"type": "page", "id": "t1", "title": "Tab One", "url": "https://a/", "webSocketDebuggerUrl": "ws://x/t1"},
			{"type": "service_worker", "id": "sw1", "title": "", "url": "https://a/sw.js"},
			{"type": "page", "id": "t2", "title": "Tab Two", "url": "https://b/", "webSocketDebuggerUrl": "ws://x/t2"},
			{"type": "background_page", "id": "bg1"},
		})
	}))
	defer srv.Close()

	port := mustPort(t, srv.URL)
	got, err := fetchPageTargets(context.Background(), port)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 page targets, got %d (%+v)", len(got), got)
	}
	if got[0].ID != "t1" || got[1].ID != "t2" {
		t.Errorf("ids preserved in order, got %s,%s", got[0].ID, got[1].ID)
	}
	if got[0].Title != "Tab One" {
		t.Errorf("title parsed, got %q", got[0].Title)
	}
}

func TestFetchPageTargets_EmptyListIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	got, err := fetchPageTargets(context.Background(), mustPort(t, srv.URL))
	if err != nil {
		t.Fatalf("empty list should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 entries, got %d", len(got))
	}
}

func TestFetchPageTargets_BadPortReturnsError(t *testing.T) {
	// Port 1 is privileged + nothing listens — connection refused.
	_, err := fetchPageTargets(context.Background(), 1)
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestFetchPageTargets_MalformedJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := fetchPageTargets(context.Background(), mustPort(t, srv.URL))
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "json") {
		t.Errorf("error should mention decoding, got %v", err)
	}
}

func mustPort(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
