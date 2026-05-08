package input

import (
	"math"
	"math/rand"
)

// Point is one step on a cursor path.
type Point struct{ X, Y float64 }

// stepCount picks a step count proportional to distance, clamped to [3, 25].
// Short hops still get >1 step so the move feels less robotic.
func stepCount(x1, y1, x2, y2 float64) int {
	dist := math.Hypot(x2-x1, y2-y1)
	steps := int(dist / 25)
	if steps < 3 {
		steps = 3
	}
	if steps > 25 {
		steps = 25
	}
	return steps
}

// interpolate returns `steps` points from (x1,y1) to (x2,y2), with random
// per-axis jitter so the path is not a perfect line. Last point is exactly
// (x2, y2) so the cursor lands where requested.
func interpolate(x1, y1, x2, y2 float64, steps int) []Point {
	if steps < 1 {
		steps = 1
	}
	out := make([]Point, 0, steps)
	jitter := 2.0
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		px := x1 + (x2-x1)*t
		py := y1 + (y2-y1)*t
		if i < steps {
			px += (rand.Float64()*2 - 1) * jitter
			py += (rand.Float64()*2 - 1) * jitter
		}
		out = append(out, Point{X: px, Y: py})
	}
	return out
}
