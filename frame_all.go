package chromekit

import (
	"context"
	"fmt"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// AllFrames returns every descendant frame, flattened (recursive, main
// frame excluded) — firefoxkit parity. Unlike Frames (cached, includes the
// main frame itself, invalidated by frame events), AllFrames always issues
// a fresh Page.getFrameTree and never touches the cache. ctx bounds the
// round-trip, matching firefoxkit's ctx-taking signature.
func (p *Page) AllFrames(ctx context.Context) ([]*Frame, error) {
	if err := p.alive(); err != nil {
		return nil, err
	}
	runCtx, cancel := p.mergeCtx(ctx)
	defer cancel()
	var tree *cdppage.FrameTree
	err := chromedp.Run(runCtx, chromedp.ActionFunc(func(c context.Context) error {
		t, err := cdppage.GetFrameTree().Do(c)
		if err != nil {
			return err
		}
		tree = t
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("%w: frame tree: %s", ErrEvaluate, err.Error())
	}
	var out []*Frame
	for _, c := range tree.ChildFrames {
		walkFrameTree(c, p, &out)
	}
	return out, nil
}
