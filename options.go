package chromekit

// BackendType selects how chromekit dispatches mouse/keyboard input.
type BackendType int

const (
	// BackendCDP dispatches input events through chromedp / DevTools Protocol.
	// Works on every platform, even when the tab is not focused.
	BackendCDP BackendType = iota
	// BackendNative drives the OS pointer/keyboard directly (winapi on Windows).
	// Only works when the Chrome window is focused and visible.
	BackendNative
)

type nativeWindow struct {
	X, Y  int
	Scale float64
}

type config struct {
	logger        Logger
	userDataDir   string
	headless      bool
	proxy         string
	inputBackend  BackendType
	nativeWindow  nativeWindow
	flags         map[string]any
}

func defaultConfig() *config {
	return &config{
		logger:       noopLogger{},
		headless:     false,
		inputBackend: BackendCDP,
		nativeWindow: nativeWindow{Scale: 1.0},
		flags:        map[string]any{},
	}
}

// Option configures Launch.
type Option func(*config)

func WithLogger(l Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

func WithUserDataDir(dir string) Option {
	return func(c *config) { c.userDataDir = dir }
}

func WithHeadless(h bool) Option {
	return func(c *config) { c.headless = h }
}

func WithProxy(addr string) Option {
	return func(c *config) { c.proxy = addr }
}

func WithInputBackend(t BackendType) Option {
	return func(c *config) { c.inputBackend = t }
}

// WithNativeWindow tells the native backend where the Chrome window sits on
// screen and the OS scale factor, so DOM coordinates can be converted to
// absolute screen coordinates.
func WithNativeWindow(x, y int, scale float64) Option {
	return func(c *config) {
		if scale <= 0 {
			scale = 1.0
		}
		c.nativeWindow = nativeWindow{X: x, Y: y, Scale: scale}
	}
}

// WithFlag passes an arbitrary chromedp ExecAllocatorOption flag.
// Stored as raw key/value; consumed by the launcher.
func WithFlag(key string, value any) Option {
	return func(c *config) { c.flags[key] = value }
}
