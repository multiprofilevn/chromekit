package chromekit

import "errors"

var (
	ErrContextCanceled    = errors.New("chromekit: context canceled")
	ErrPageNotFound       = errors.New("chromekit: page not found")
	ErrNavigate           = errors.New("chromekit: navigate failed")
	ErrTimeout            = errors.New("chromekit: timeout")
	ErrEvaluate           = errors.New("chromekit: evaluate failed")
	ErrNativeNotSupported = errors.New("chromekit: native input not supported on this platform")
	ErrInvalidConfig      = errors.New("chromekit: invalid config")
)
