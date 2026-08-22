//go:build stress

package chromekit

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multiprofilevn/chromekit/input"
)

// stressPage keeps its renderer's main thread busy (a rAF loop burning a few ms
// of CPU per frame) so N of these across N processes actually contend for cores
// — the CPU-saturation condition that slows CDP mouse-move acks in the fleet.
const stressPage = `data:text/html,` +
	`<html><body>` +
	`<button id="target" style="width:120px;height:60px">x</button>` +
	`<script>` +
	`function burn(ms){const e=performance.now()+ms;while(performance.now()<e){Math.sqrt(Math.random())}}` +
	`function loop(){burn(4);requestAnimationFrame(loop)}requestAnimationFrame(loop);` +
	`</script>` +
	`</body></html>`

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseWH(s string, dw, dh int) (int, int) {
	parts := strings.Split(s, ",")
	if len(parts) == 2 {
		w, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if e1 == nil && e2 == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	return dw, dh
}

// TestStress_Fleet models the real deployment: STRESS_BROWSERS *separate* Chrome
// processes (not tabs), each with STRESS_PAGES page(s), driven concurrently for
// STRESS_DURATION_SEC. Windows are cascaded so none is fully occluded (occlusion
// is handled in prod by window tiling and is out of scope here — the variable is
// CPU contention). Measures per-op mouse latency, goroutine HWM, heap, CDP
// dispatch volume. STRESS_GESTURE_LIMIT sets the per-process gesture cap (0 = off).
//
// Run: STRESS_BROWSERS=30 STRESS_PAGES=1 STRESS_HEADED=1 STRESS_DURATION_SEC=30 \
//      STRESS_GESTURE_LIMIT=10 go test -tags stress -run TestStress_Fleet -timeout 30m -v .
// Compare limiter on (10) vs off (0). Definitive tuning needs the real ~200-proc
// fleet load; a dev box with fewer cores shows a weaker version of the effect.
func TestStress_Fleet(t *testing.T) {
	chromePath := resolveChromePath(t)

	nBrowsers := envInt("STRESS_BROWSERS", 30)
	nPages := envInt("STRESS_PAGES", 1)
	dur := time.Duration(envInt("STRESS_DURATION_SEC", 30)) * time.Second
	limit := envInt("STRESS_GESTURE_LIMIT", input.DefaultGestureLimit)
	headed := os.Getenv("STRESS_HEADED") != ""
	winSize := envStr("STRESS_WINDOW_SIZE", "400,300")
	scale := envStr("STRESS_SCALE", "1")
	winW, winH := parseWH(winSize, 400, 300)

	SetGestureLimit(limit)
	t.Logf("config: browsers=%d(separate procs) pages=%d duration=%s gestureLimit=%d headless=%v window=%s scale=%s",
		nBrowsers, nPages, dur, limit, !headed, winSize, scale)

	rec := newLatencyRecorder()
	sampler := startResourceSampler(250 * time.Millisecond)
	baseline := stableGoroutineCount()

	var totalMoves, totalClicks int64
	deadline := time.Now().Add(dur)

	var browsers []*Browser
	var bwg sync.WaitGroup
	for bi := 0; bi < nBrowsers; bi++ {
		opts := []Option{WithHeadless(!headed)}
		if headed {
			// Cascade each separate window so none fully covers another (fully
			// occluded windows get backgrounded — a solved-in-prod concern via
			// tiling, kept out of the CPU measurement here).
			x := (bi * 32) % 1400
			y := (bi * 28) % 700
			opts = append(opts,
				WithFlag("no-first-run", true),
				WithFlag("no-default-browser-check", true),
				WithFlag("window-size", winSize),
				WithFlag("window-position", fmt.Sprintf("%d,%d", x, y)),
				WithFlag("force-device-scale-factor", scale),
			)
		}
		b, err := Launch(chromePath, opts...)
		if err != nil {
			t.Fatalf("browser %d launch: %v", bi, err)
		}
		browsers = append(browsers, b)

		for pi := 0; pi < nPages; pi++ {
			p, err := b.NewPage()
			if err != nil {
				t.Fatalf("browser %d page %d: %v", bi, pi, err)
			}
			cb := injectCountingBackend(p, input.ProfileHuman)
			if err := p.Navigate(stressPage, 15*time.Second); err != nil {
				t.Logf("browser %d page %d navigate: %v (continuing)", bi, pi, err)
			}
			_ = p.BringToFront()

			bwg.Add(1)
			go func(p *Page, cb *countingCDPBackend, seed int) {
				defer bwg.Done()
				rng := rand.New(rand.NewSource(int64(seed)))
				for time.Now().Before(deadline) {
					x := 10 + rng.Float64()*float64(winW-40)
					y := 10 + rng.Float64()*float64(winH-60)

					start := time.Now()
					err := p.Mouse().MoveTo(x, y)
					rec.record("move", time.Since(start), err)

					start = time.Now()
					err = p.Mouse().ClickAt(x, y)
					rec.record("click", time.Since(start), err)
				}
				atomic.AddInt64(&totalMoves, atomic.LoadInt64(&cb.moves))
				atomic.AddInt64(&totalClicks, atomic.LoadInt64(&cb.clicks))
			}(p, cb, bi*1000+pi)
		}
	}

	bwg.Wait()
	sampler.Stop()

	for _, b := range browsers {
		b.Close()
	}

	t.Log("=== fleet stress results ===")
	for _, s := range rec.stats() {
		t.Logf("%-6s n=%-7d err=%-5d p50=%-10v p95=%-10v max=%v",
			s.op, s.n, s.errs, s.p50.Round(time.Millisecond),
			s.p95.Round(time.Millisecond), s.max.Round(time.Millisecond))
	}
	t.Logf("cdp dispatch: moves=%d clicks=%d", totalMoves, totalClicks)
	t.Logf("goroutine HWM=%d (baseline=%d)  heap peak=%s",
		sampler.goroutineHWM, baseline, humanBytes(sampler.heapPeak))

	if !awaitGoroutinesAtMost(baseline+8, 10*time.Second) {
		t.Errorf("goroutines did not settle: baseline=%d now=%d", baseline, stableGoroutineCount())
	}
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGT"[exp])
}
