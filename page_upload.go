package chromekit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

// SetInputFiles attaches files to the <input type="file"> matched by selector.
// Bypasses the native picker dialog (which automation cannot drive). Paths
// are resolved to absolute and must exist; missing files return ErrInvalidConfig
// before any CDP call is made.
//
// Pass multiple paths to populate a multi-file input.
func (p *Page) SetInputFiles(selector string, paths ...string) error {
	if err := p.alive(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("%w: no file paths given", ErrInvalidConfig)
	}
	abs := make([]string, len(paths))
	for i, raw := range paths {
		a, err := filepath.Abs(raw)
		if err != nil {
			return fmt.Errorf("%w: abs %q: %s", ErrInvalidConfig, raw, err.Error())
		}
		if _, err := os.Stat(a); err != nil {
			return fmt.Errorf("%w: file %q: %s", ErrInvalidConfig, a, err.Error())
		}
		abs[i] = a
	}
	node, err := p.QuerySelector(selector, 0)
	if err != nil {
		return err
	}
	if err := chromedp.Run(p.ctx, dom.SetFileInputFiles(abs).WithNodeID(node.NodeID)); err != nil {
		return fmt.Errorf("%w: set files: %s", ErrEvaluate, err.Error())
	}
	return nil
}
