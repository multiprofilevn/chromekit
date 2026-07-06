package chromekit

import "os/exec"

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

// CursorProfile selects the cursor movement style used by Cursor.MoveTo.
type CursorProfile int

const (
	// CursorHuman draws a Bezier path with ease-in-out and per-distance step
	// count. Best for sites with bot detection. Default.
	CursorHuman CursorProfile = iota
	// CursorFast uses a short linear path with minimal sleep — good for tests
	// and CI where wall time matters more than realism.
	CursorFast
	// CursorMachine moves in one shot via SetCursorPos. Fastest, no jitter,
	// looks robotic. Use only for benchmarking.
	CursorMachine
)

type config struct {
	logger        Logger
	userDataDir   string
	headless      bool
	proxy         string
	inputBackend  BackendType
	nativeWindow  nativeWindow
	cursorProfile CursorProfile
	mousePathURL  string
	flags         map[string]any
	modifyCmd     func(*exec.Cmd)
}

func defaultConfig() *config {
	return &config{
		logger:        noopLogger{},
		headless:      false,
		inputBackend:  BackendCDP,
		nativeWindow:  nativeWindow{Scale: 1.0},
		cursorProfile: CursorHuman,
		flags:         map[string]any{},
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

// WithMousePathURL sets the full URL of a mouse-positions endpoint that returns
// a high-entropy {x,y,delay} cursor path. When set, ProfileHuman moves fetch
// from it and replay; empty (default) keeps the pure local bezier engine.
func WithMousePathURL(url string) Option {
	return func(c *config) { c.mousePathURL = url }
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

// WithCursorProfile picks the cursor movement style. Default is CursorHuman
// (Bezier + ease-in-out). Switch to CursorFast for CI speed or CursorMachine
// for one-shot benchmark moves. Only the native backend honors the profile —
// CDP backend dispatches mouse events directly with no intermediate steps.
func WithCursorProfile(p CursorProfile) Option {
	return func(c *config) { c.cursorProfile = p }
}

// WithFlag passes an arbitrary chromedp ExecAllocatorOption flag.
// Stored as raw key/value; consumed by the launcher.
func WithFlag(key string, value any) Option {
	return func(c *config) { c.flags[key] = value }
}

// WithModifyCmd hooks the underlying *exec.Cmd before Chrome starts. Use
// this to inject raw process args that chromedp.Flag can't express — e.g.
// single-dash flags like `-disable-features=` for GPM-Login Chromium forks
// that intentionally bypass the standard `--disable-features=` parser.
//
// The hook fires once per Launch, after chromedp builds the default arg
// list and before exec.Cmd.Start. Mutating Cmd.Args is safe; mutating
// Cmd.Process is not (the process hasn't started yet).
func WithModifyCmd(fn func(*exec.Cmd)) Option {
	return func(c *config) {
		if fn != nil {
			c.modifyCmd = fn
		}
	}
}
