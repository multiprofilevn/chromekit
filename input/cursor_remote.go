package input

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
)

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
