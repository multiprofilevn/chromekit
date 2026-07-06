package input

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidRemotePath_RejectsNonFiniteInterior(t *testing.T) {
	pts := []remotePoint{
		{X: 0, Y: 0},
		{X: math.NaN(), Y: 5}, // corrupted interior point
		{X: 300, Y: 200},      // last point on target
	}
	if validRemotePath(pts, 300, 200) {
		t.Fatal("path with NaN interior point must be rejected")
	}
	inf := []remotePoint{{X: 0, Y: 0}, {X: math.Inf(1), Y: 5}, {X: 300, Y: 200}}
	if validRemotePath(inf, 300, 200) {
		t.Fatal("path with Inf interior point must be rejected")
	}
}

// mouseServer returns an httptest server that echoes a fixed path ending at the
// requested `to`. delay is kept 0 so tests don't sleep.
func mouseServer(t *testing.T, extraLastOffset float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mouseReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := []remotePoint{
			{X: req.From.X, Y: req.From.Y, Delay: 0},
			{X: (req.From.X + req.To.X) / 2, Y: (req.From.Y + req.To.Y) / 2, Delay: 0},
			{X: req.To.X + extraLastOffset, Y: req.To.Y + extraLastOffset, Delay: 0},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mouseResp{Success: true, Data: data})
	}))
}

func TestMoveTo_RemotePath_ReplayedEndsOnTarget(t *testing.T) {
	srv := mouseServer(t, 0)
	defer srv.Close()

	fb := &noSeederBackend{}
	c := NewCursorWithProfileAndPath(fb, ProfileHuman, srv.URL)
	if err := c.MoveTo(context.Background(), 300, 200); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if len(fb.moves) != 3 {
		t.Fatalf("expected 3 replayed moves from server, got %d", len(fb.moves))
	}
	last := fb.moves[len(fb.moves)-1]
	if last.X != 300 || last.Y != 200 {
		t.Fatalf("last move must land on target (300,200), got (%v,%v)", last.X, last.Y)
	}
}

func TestMoveTo_EmptyURL_UsesLocalBezier(t *testing.T) {
	fb := &noSeederBackend{}
	c := NewCursorWithProfileAndPath(fb, ProfileHuman, "")
	if err := c.MoveTo(context.Background(), 300, 200); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	// Local bezier uses humanStepCount (>=8 steps) → distinctly more than the
	// 3-point server path, proving it did not hit the network.
	if len(fb.moves) < 8 {
		t.Fatalf("empty URL should use local bezier (>=8 steps), got %d", len(fb.moves))
	}
	last := fb.moves[len(fb.moves)-1]
	if last.X != 300 || last.Y != 200 {
		t.Fatalf("local bezier last point must be target, got (%v,%v)", last.X, last.Y)
	}
}

func TestMoveTo_ServerError_FallsBackToLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	fb := &noSeederBackend{}
	c := NewCursorWithProfileAndPath(fb, ProfileHuman, srv.URL)
	if err := c.MoveTo(context.Background(), 300, 200); err != nil {
		t.Fatalf("MoveTo must not error on server failure (fallback), got %v", err)
	}
	if len(fb.moves) < 8 {
		t.Fatalf("server 500 should fall back to local bezier, got %d moves", len(fb.moves))
	}
	last := fb.moves[len(fb.moves)-1]
	if last.X != 300 || last.Y != 200 {
		t.Fatalf("fallback last point must be target, got (%v,%v)", last.X, last.Y)
	}
}

func TestMoveTo_OffTargetRemotePath_FallsBackToLocal(t *testing.T) {
	// Server returns a path whose last point misses the target by 50px → the
	// validity guard rejects it and MoveTo falls back to local bezier.
	srv := mouseServer(t, 50)
	defer srv.Close()

	fb := &noSeederBackend{}
	c := NewCursorWithProfileAndPath(fb, ProfileHuman, srv.URL)
	_ = c.MoveTo(context.Background(), 300, 200)
	if len(fb.moves) < 8 {
		t.Fatalf("off-target remote path should fall back to local, got %d moves", len(fb.moves))
	}
	last := fb.moves[len(fb.moves)-1]
	if last.X != 300 || last.Y != 200 {
		t.Fatalf("fallback last point must be exact target, got (%v,%v)", last.X, last.Y)
	}
}
