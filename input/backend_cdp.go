package input

import (
	"context"

	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

type cdpBackend struct{}

func newCDP() Backend { return &cdpBackend{} }

func (cdpBackend) MouseMove(ctx context.Context, x, y float64) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MouseMoved, x, y).Do(ctx)
	}))
}

func (cdpBackend) MouseDown(ctx context.Context, x, y float64, button MouseButton) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MousePressed, x, y).
			WithButton(toCDPButton(button)).
			WithClickCount(1).
			Do(ctx)
	}))
}

func (cdpBackend) MouseUp(ctx context.Context, x, y float64, button MouseButton) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MouseReleased, x, y).
			WithButton(toCDPButton(button)).
			WithClickCount(1).
			Do(ctx)
	}))
}

func (cdpBackend) MouseScroll(ctx context.Context, deltaX, deltaY float64) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.DispatchMouseEvent(cdpinput.MouseWheel, 0, 0).
			WithDeltaX(deltaX).
			WithDeltaY(deltaY).
			Do(ctx)
	}))
}

func (cdpBackend) KeyChar(ctx context.Context, ch rune) error {
	s := string(ch)
	return chromedp.Run(ctx, chromedp.KeyEvent(s))
}

func (cdpBackend) InsertText(ctx context.Context, text string) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpinput.InsertText(text).Do(ctx)
	}))
}

func toCDPButton(b MouseButton) cdpinput.MouseButton {
	switch b {
	case MouseRight:
		return cdpinput.Right
	case MouseMiddle:
		return cdpinput.Middle
	default:
		return cdpinput.Left
	}
}

var _ Backend = (*cdpBackend)(nil)
