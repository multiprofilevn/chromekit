package chromekit

import (
	"errors"

	"github.com/multiprofilevn/chromekit/input"
)

// ErrForegroundLost is raised when the browser window lost the OS foreground
// mid-input, so the remaining keystrokes would have landed somewhere else.
// Aliased from the input package: the sentinel has to live there (that is where
// it is returned from), and chromekit → chromekit/input is a one-way import.
var ErrForegroundLost = input.ErrForegroundLost

var (
	ErrContextCanceled        = errors.New("chromekit: context canceled")
	ErrPageNotFound           = errors.New("chromekit: page not found")
	ErrNavigate               = errors.New("chromekit: navigate failed")
	ErrTimeout                = errors.New("chromekit: timeout")
	ErrEvaluate               = errors.New("chromekit: evaluate failed")
	ErrNativeNotSupported     = errors.New("chromekit: native input not supported on this platform")
	ErrInvalidConfig          = errors.New("chromekit: invalid config")
	ErrFrameDetached          = errors.New("chromekit: frame detached")
	ErrDownloadCanceled       = errors.New("chromekit: download canceled")
	ErrTypeSelectorMismatch   = errors.New("chromekit: type selector value mismatch")
	ErrTabActivateTimeout     = errors.New("chromekit: tab activation not confirmed (timeout)")
	ErrNoLaunchTarget         = errors.New("chromekit: no page target after launch")
	ErrCannotCloseLastPage    = errors.New("chromekit: cannot close the only page (use Close)")
	ErrCannotClosePrimaryPage = errors.New("chromekit: cannot close the primary page (use Close)")
)
