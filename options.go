package chromekit

import (
	"os/exec"
	"time"

	"github.com/multiprofilevn/chromekit/input"
)

// SetGestureLimit caps how many mouse gestures run concurrently in THIS process
// (across every Browser and Page). n <= 0 disables the cap. Default is
// input.DefaultGestureLimit (10).
//
// This is the primary throughput control at scale: a mouse gesture is dozens of
// CDP dispatches that each wait on a renderer acknowledgement, so when hundreds
// of tabs render at once the CPU saturates and those acks — and every gesture —
// slow to a crawl. Capping in-flight gestures keeps the active renderer set
// small enough that acks stay fast. It is process-global, not per-Browser: with
// one runner process per OS user, set it once at startup to roughly
// (CPU threads / number of runner processes).
func SetGestureLimit(n int) { input.SetGestureLimit(n) }

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

	// wsURLReadTimeout bounds how long Launch waits for Chrome to print its
	// DevTools websocket URL. 0 keeps chromedp's own default (20s).
	wsURLReadTimeout time.Duration

	foregroundGuard bool
	// contentOffsetTolerance bounds how far the page-measured content origin
	// may fall outside the window manager's rectangle. See
	// WithContentOffsetTolerance.
	contentOffsetTolerance int
}

func defaultConfig() *config {
	return &config{
		logger:        noopLogger{},
		headless:      false,
		inputBackend:  BackendCDP,
		nativeWindow:  nativeWindow{Scale: 1.0},
		cursorProfile: CursorHuman,
		flags:         map[string]any{},
		// On by default: typing into the wrong window is destructive, and the
		// check costs one syscall per five characters.
		foregroundGuard: true,

		contentOffsetTolerance: DefaultContentOffsetTolerance,
	}
}

// DefaultContentOffsetTolerance is the slack, in screen pixels, allowed between
// the content origin measured inside the page and the window rectangle reported
// by the window manager. Borders and DPI rounding account for a couple of
// pixels; anything larger is a measurement that cannot be trusted.
const DefaultContentOffsetTolerance = 4

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

// WithForegroundGuard toggles the native backend's foreground check. When on
// (the default), every keystroke chunk and mouse button event confirms the
// Chrome window still owns the OS foreground and fails with ErrForegroundLost
// if it does not — SendInput has no window address, so input from an
// unfocused browser silently types into whatever took focus.
//
// No-op for the CDP backend, which addresses the tab directly and does not
// need focus at all. Turn it off only when something else already holds a
// foreground lease for the whole critical section.
func WithForegroundGuard(on bool) Option {
	return func(c *config) { c.foregroundGuard = on }
}

// WithMousePathURL sets the full URL of a mouse-positions endpoint that returns
// a high-entropy {x,y,delay} cursor path. When set, ProfileHuman moves fetch
// from it and replay; empty (default) keeps the pure local bezier engine.
func WithMousePathURL(url string) Option {
	return func(c *config) { c.mousePathURL = url }
}

// WithNativeWindow is a MANUAL OVERRIDE for the coordinate frame, used only
// when the live window cannot be measured — no HWND resolves, or ContentOffset
// refuses the page-reported geometry.
//
// The normal path does not need it: the kit measures the content origin and the
// DPI scale from the live window on every frame pin, and those measurements win
// over anything set here. Set it when a browser is driven natively on a host
// where the HWND lookup cannot succeed and the window position is known up
// front instead.
func WithNativeWindow(x, y int, scale float64) Option {
	return func(c *config) {
		if scale <= 0 {
			scale = 1.0
		}
		c.nativeWindow = nativeWindow{X: x, Y: y, Scale: scale}
	}
}

// WithContentOffsetTolerance sets how far, in screen pixels, the content origin
// measured inside the page may sit outside the window rectangle reported by the
// window manager before ContentOffset refuses it.
//
// A few pixels of slack are required: window borders and DPI rounding put the
// content origin marginally outside the rect on real machines, and a check that
// fails those fails every job. Beyond that slack the two numbers cannot be
// reconciled, and the page-reported one is the untrusted half.
//
// px <= 0 is ignored. Default 4.
func WithContentOffsetTolerance(px int) Option {
	return func(c *config) {
		if px <= 0 {
			return
		}
		c.contentOffsetTolerance = px
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

// WithWSURLReadTimeout bounds the wait for Chrome to print its DevTools
// websocket URL on stderr — the step that fails as
// "launch: websocket url timeout reached".
//
// chromedp defaults to 20s, which a cold profile can exceed on its own: a large
// user-data-dir, several unpacked extensions, a slow disk, or a dozen browsers
// launching at once all delay that first line. Raise it on hosts that launch
// profiles in bulk. d <= 0 keeps the chromedp default.
func WithWSURLReadTimeout(d time.Duration) Option {
	return func(c *config) {
		if d <= 0 {
			return
		}
		c.wsURLReadTimeout = d
	}
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
