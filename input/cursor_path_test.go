package input

import (
	"math"
	"testing"
)

func TestEaseInOutCubic_Endpoints(t *testing.T) {
	if got := easeInOutCubic(0); got != 0 {
		t.Errorf("easeInOutCubic(0) = %v, want 0", got)
	}
	if got := easeInOutCubic(1); got != 1 {
		t.Errorf("easeInOutCubic(1) = %v, want 1", got)
	}
	if got := easeInOutCubic(0.5); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("easeInOutCubic(0.5) = %v, want 0.5", got)
	}
}

func TestEaseInOutCubic_Monotonic(t *testing.T) {
	prev := easeInOutCubic(0)
	for i := 1; i <= 100; i++ {
		t1 := float64(i) / 100
		cur := easeInOutCubic(t1)
		if cur < prev {
			t.Fatalf("easeInOutCubic not monotonic at t=%v: prev=%v cur=%v", t1, prev, cur)
		}
		prev = cur
	}
}

func TestHumanStepCount_FloorAndCap(t *testing.T) {
	if got := humanStepCount(0, 0, 50, 0); got != 8 {
		t.Errorf("short distance min step, got %d want 8", got)
	}
	if got := humanStepCount(0, 0, 100, 0); got != 8 {
		t.Errorf("100px = floor 8 step, got %d want 8", got)
	}
	if got := humanStepCount(0, 0, 5000, 0); got != maxGesturePoints {
		t.Errorf("very long distance cap, got %d want %d", got, maxGesturePoints)
	}
	// 500px / 12 ≈ 41, now clamped to the gesture cap.
	if got := humanStepCount(0, 0, 500, 0); got != maxGesturePoints {
		t.Errorf("500px should clamp to cap %d, got %d", maxGesturePoints, got)
	}
}

func TestResamplePoints_CapAndEndpoint(t *testing.T) {
	// Build a dense 300-point path 0..299 on X.
	dense := make([]Point, 300)
	for i := range dense {
		dense[i] = Point{X: float64(i), Y: 0}
	}
	got := resamplePoints(dense, maxGesturePoints)
	if len(got) > maxGesturePoints {
		t.Fatalf("resample len %d > cap %d", len(got), maxGesturePoints)
	}
	if last := got[len(got)-1]; last.X != 299 {
		t.Errorf("last point must be preserved exactly, got X=%v want 299", last.X)
	}
	if got[0].X != 0 {
		t.Errorf("first sample should start at path head, got X=%v", got[0].X)
	}
}

func TestResamplePoints_ShortUnchanged(t *testing.T) {
	in := []Point{{X: 1}, {X: 2}, {X: 3}}
	got := resamplePoints(in, maxGesturePoints)
	if len(got) != 3 {
		t.Fatalf("short path should pass through unchanged, got len %d", len(got))
	}
}

func TestResamplePoints_CapOne(t *testing.T) {
	in := []Point{{X: 1}, {X: 2}, {X: 3}, {X: 9}}
	got := resamplePoints(in, 1)
	if len(got) != 1 || got[0].X != 9 {
		t.Fatalf("cap 1 should keep only the endpoint, got %+v", got)
	}
}

func TestBezierPath_StepsAndLanding(t *testing.T) {
	pts := bezierPath(0, 0, 1000, 500, 40)
	if len(pts) != 40 {
		t.Fatalf("want 40 points, got %d", len(pts))
	}
	last := pts[len(pts)-1]
	if math.Abs(last.X-1000) > 1e-6 || math.Abs(last.Y-500) > 1e-6 {
		t.Errorf("last point should equal end, got (%v,%v) want (1000,500)", last.X, last.Y)
	}
}

func TestBezierPath_NotStraightLine(t *testing.T) {
	// A bezier path with random control points should not coincide with the
	// straight-line interpolation. Sample mid-path and check deviation > 0.
	pts := bezierPath(0, 0, 1000, 0, 40)
	// Straight-line y would be 0 along (0,0)→(1000,0). Bezier should deviate
	// because control points lie perpendicular to the axis.
	maxDev := 0.0
	for _, p := range pts {
		// linear y at this x along (0,0)→(1000,0) is 0
		if d := math.Abs(p.Y); d > maxDev {
			maxDev = d
		}
	}
	if maxDev < 5 {
		t.Errorf("bezier path too close to straight line; max y deviation %v < 5px", maxDev)
	}
}
