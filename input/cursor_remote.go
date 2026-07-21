package input

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"
)

var (
	remoteTransportOnce sync.Once
	remoteTransport     *http.Transport
)

// sharedRemoteTransport returns a process-wide pooled HTTP transport for remote
// mouse-path fetches. One shared transport across every Cursor reuses TCP
// connections; the old per-Cursor http.Client meant hundreds of separate
// connection pools at fleet scale, none of them reusing sockets.
func sharedRemoteTransport() *http.Transport {
	remoteTransportOnce.Do(func() {
		remoteTransport = &http.Transport{
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 32,
			IdleConnTimeout:     90 * time.Second,
		}
	})
	return remoteTransport
}

// resampleRemotePoints thins a dense server path down to at most max points,
// sampled evenly and always keeping the exact final point so the click lands on
// target. Each kept point keeps its own Delay; the delays of dropped points are
// discarded, which intentionally shortens the gesture — the whole point of the
// cap is to stop waiting on a round-trip per pixel. pts of length <= max (or
// max <= 0) is returned unchanged.
func resampleRemotePoints(pts []remotePoint, max int) []remotePoint {
	if max <= 0 || len(pts) <= max {
		return pts
	}
	last := len(pts) - 1
	out := make([]remotePoint, 0, max)
	for i := 0; i < max-1; i++ {
		idx := int(float64(i) * float64(last) / float64(max-1))
		out = append(out, pts[idx])
	}
	return append(out, pts[last])
}

// remotePoint is one step of a server-generated cursor path: a coordinate plus
// the delay (ms) to wait before emitting the next move.
type remotePoint struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Delay float64 `json:"delay"`
}

// mouseReq is the POST body for the mouse-positions endpoint.
type mouseReq struct {
	From xy `json:"from"`
	To   xy `json:"to"`
}

type xy struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// mouseResp mirrors the {success, data} envelope the endpoint returns.
type mouseResp struct {
	Success bool          `json:"success"`
	Data    []remotePoint `json:"data"`
}

// fetchRemotePath POSTs {from,to} to url and returns the server's path. Any
// transport/decode error is returned so the caller can fall back to the local
// bezier engine — remote pathing must never be a hard dependency.
func fetchRemotePath(ctx context.Context, hc *http.Client, url string, fromX, fromY, toX, toY float64) ([]remotePoint, error) {
	if hc == nil {
		return nil, fmt.Errorf("cursor: nil http client")
	}
	body, err := json.Marshal(mouseReq{From: xy{fromX, fromY}, To: xy{toX, toY}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cursor: mouse-positions status %d", resp.StatusCode)
	}
	var out mouseResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.Success || len(out.Data) == 0 {
		return nil, fmt.Errorf("cursor: mouse-positions empty/failed")
	}
	return out.Data, nil
}

// validRemotePath guards that the server path actually terminates at the target
// (within 1px). A path that drifts off-target would land clicks in the wrong
// place — reject it and fall back to local bezier.
func validRemotePath(pts []remotePoint, toX, toY float64) bool {
	if len(pts) == 0 {
		return false
	}
	// Reject any non-finite point (a malformed server, or JSON null decoded to
	// 0, would otherwise walk the pointer through a bogus coord like (0,0)).
	for _, p := range pts {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return false
		}
	}
	last := pts[len(pts)-1]
	return math.Abs(last.X-toX) <= 1 && math.Abs(last.Y-toY) <= 1
}
