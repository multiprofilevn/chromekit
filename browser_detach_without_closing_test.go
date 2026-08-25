package chromekit

import (
	"context"
	"testing"

	"github.com/chromedp/chromedp"
)

// A live context is the case detachWithoutClosing exists for: clearing Target
// is what stops chromedp from closing a tab the user already had.
func TestDetachWithoutClosingClearsLiveContext(t *testing.T) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	c := chromedp.FromContext(ctx)
	c.Target = &chromedp.Target{}

	detachWithoutClosing(ctx)

	if c.Target != nil {
		t.Fatal("detachWithoutClosing left Target set on a live context")
	}
}

// A context that is already done has chromedp's cleanup goroutine awake and
// reading Target. Writing it there races that reader, and the nil it stores is
// what the reader dereferences a line later — an unrecoverable panic that kills
// the host process. The write must be skipped, not merely made rarer.
func TestDetachWithoutClosingSkipsDoneContext(t *testing.T) {
	ctx, cancel := chromedp.NewContext(context.Background())
	c := chromedp.FromContext(ctx)
	sentinel := &chromedp.Target{}
	c.Target = sentinel
	cancel()

	detachWithoutClosing(ctx)

	if c.Target != sentinel {
		t.Fatal("detachWithoutClosing wrote Target on a context whose cleanup goroutine was already running")
	}
}

func TestDetachWithoutClosingIgnoresNilContext(t *testing.T) {
	detachWithoutClosing(nil) // must not panic
}
