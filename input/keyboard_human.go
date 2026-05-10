package input

import (
	"context"
	"math/rand"
	"time"
)

// HumanTypeConfig tunes TypeHuman pacing and typo simulation. Zero values
// fall back to the defaults applied by withDefaults.
type HumanTypeConfig struct {
	MinDelay        time.Duration // per-char min pause (default 80ms)
	MaxDelay        time.Duration // per-char max pause (default 220ms)
	MistakeRate     float64       // 0..1 chance per char to mistype + correct (default 0.05)
	CorrectionPause time.Duration // pause before backspacing a typo (default 250ms)
	Rand            *rand.Rand    // RNG; nil uses math/rand global
}

func (c HumanTypeConfig) withDefaults() HumanTypeConfig {
	if c.MinDelay <= 0 {
		c.MinDelay = 80 * time.Millisecond
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = 220 * time.Millisecond
	}
	if c.MaxDelay < c.MinDelay {
		c.MaxDelay = c.MinDelay
	}
	if c.MistakeRate < 0 {
		c.MistakeRate = 0
	}
	if c.MistakeRate > 1 {
		c.MistakeRate = 1
	}
	if c.CorrectionPause <= 0 {
		c.CorrectionPause = 250 * time.Millisecond
	}
	return c
}

// TypeHuman types text with a slower, jittered cadence than Type and an
// occasional typo that gets backspaced and rewritten. Pass at most one
// HumanTypeConfig — extras are ignored. Honors ctx cancellation between
// keystrokes and during pauses.
func (k *Keyboard) TypeHuman(ctx context.Context, text string, cfg ...HumanTypeConfig) error {
	c := HumanTypeConfig{}
	if len(cfg) > 0 {
		c = cfg[0]
	}
	c = c.withDefaults()

	randFloat := func() float64 {
		if c.Rand != nil {
			return c.Rand.Float64()
		}
		return rand.Float64()
	}
	randInt := func(n int) int {
		if n <= 0 {
			return 0
		}
		if c.Rand != nil {
			return c.Rand.Intn(n)
		}
		return rand.Intn(n)
	}

	for _, ch := range text {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.MistakeRate > 0 && randFloat() < c.MistakeRate {
			if wrong, ok := pickTypo(ch, randInt); ok {
				if err := k.backend.KeyChar(ctx, wrong); err != nil {
					return err
				}
				if err := sleepCtx(ctx, c.CorrectionPause); err != nil {
					return err
				}
				if err := k.backend.KeyCombo(ctx, VKBackspace, 0); err != nil {
					return err
				}
				if err := sleepCtx(ctx, humanDelay(c.MinDelay, c.MaxDelay, randInt)); err != nil {
					return err
				}
			}
		}
		if err := k.backend.KeyChar(ctx, ch); err != nil {
			return err
		}
		if err := sleepCtx(ctx, humanDelay(c.MinDelay, c.MaxDelay, randInt)); err != nil {
			return err
		}
	}
	return nil
}

func humanDelay(min, max time.Duration, randInt func(int) int) time.Duration {
	if max <= min {
		return min
	}
	span := int((max - min) / time.Millisecond)
	if span <= 0 {
		return min
	}
	return min + time.Duration(randInt(span))*time.Millisecond
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// qwertyNeighbors lists physically adjacent keys on a US QWERTY layout.
// pickTypo case-folds before lookup; runes outside this map (digits, symbols,
// non-Latin) do not produce a typo and are typed straight through.
var qwertyNeighbors = map[rune]string{
	'q': "wa", 'w': "qeas", 'e': "wrds", 'r': "etfd", 't': "rygf",
	'y': "tuhg", 'u': "yijh", 'i': "uokj", 'o': "iplk", 'p': "ol",
	'a': "qwsz", 's': "awedxz", 'd': "serfcx", 'f': "drtgvc",
	'g': "ftyhbv", 'h': "gyujnb", 'j': "huiknm", 'k': "jiolm",
	'l': "kop", 'z': "asx", 'x': "zsdc", 'c': "xdfv", 'v': "cfgb",
	'b': "vghn", 'n': "bhjm", 'm': "njk",
}

func pickTypo(ch rune, randInt func(int) int) (rune, bool) {
	lower := ch
	upper := false
	if ch >= 'A' && ch <= 'Z' {
		lower = ch + ('a' - 'A')
		upper = true
	}
	neighbors, ok := qwertyNeighbors[lower]
	if !ok || len(neighbors) == 0 {
		return 0, false
	}
	pick := rune(neighbors[randInt(len(neighbors))])
	if upper {
		pick = pick - ('a' - 'A')
	}
	return pick, true
}
