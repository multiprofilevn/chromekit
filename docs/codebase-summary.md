# chromekit — Codebase Summary

## Module layout

```
chromekit/
├── browser.go (196 LOC)
│   Browser struct, lifecycle entry point, Pages slice mgmt
├── browser_connect.go (181 LOC)
│   Connect(port int, ...Option) — attach to running Chrome on debug port
├── browser_pages.go (154 LOC)
│   NewPage / SwitchPage / ClosePage / Pages / Current — tab management
├── browser_focus_windows.go (407 LOC)
│   Windows HWND lookup by PID, fallback to title search
├── browser_focus_other.go
│   macOS/Linux stub (returns ErrNativeNotSupported for focus-aware ops)
├── browser_permissions.go
│   GrantPermissions(origin, perms...) / ResetPermissions()
├── page.go (134 LOC)
│   Page struct, tab lifecycle, getters (Id, URL, Title, etc.)
├── page_navigate.go (116 LOC)
│   Navigate / Reload / GoBack / GoForward
├── page_wait.go (148 LOC)
│   WaitForReady / WaitForSelector / WaitForChangeURL / WaitForFunction
├── page_action.go (124 LOC)
│   Click / ClickElement / Type / Hover — shorthand high-level actions
├── page_dom.go (150 LOC)
│   QuerySelector(All) / BoundingBox / GetText / GetAttribute / ScrollIntoView
├── page_input.go
│   Type / Press routing through Backend (CDP vs native)
├── page_eval.go
│   Evaluate(expr, out) / EvalSelector / EvalSelectorAll — JS execution
├── page_listeners.go (231 LOC)
│   OnDialog(handler) / OnConsole(handler) — event plumbing & dispatch
├── page_dialog.go
│   Dialog event handling
├── page_console.go
│   Console event handling
├── page_cookies_api.go (166 LOC)
│   Cookies() namespace — Get(name) / Set(cookie) / Delete(name) / Clear()
├── page_storage.go (98 LOC)
│   Storage().Local()/Session() — Get(key) / Set(key,val) / Remove(key) / Clear()
├── page_mouse_api.go (154 LOC)
│   Mouse() namespace — MoveTo / ClickAt / Scroll / Tap / Hover / DoubleClick / RightClick / Drag / DragAndDrop
├── page_keyboard_api.go (138 LOC)
│   Keyboard() namespace — Press / Down / Up / Type / TypeFast / TypeHuman
├── page_type_selector.go (123 LOC)
│   Keyboard().TypeSelector(selector, text, cfg) — type via TypeHuman + verify via .value/.innerText/.textContent
├── page_downloads_api.go (87 LOC)
│   Downloads() namespace — SetPath(dir) / Wait()
├── page_headers.go
│   SetExtraHeaders(map[string]string)
├── page_upload.go (44 LOC)
│   SetInputFiles(selector, paths...) — form upload
├── frame.go (289 LOC)
│   Frames / MainFrame / FrameByURL / FrameByName — iframe navigation
├── errors.go
│   Sentinel error vars (ErrContextCanceled, ErrPageNotFound, ErrNavigate, ErrTimeout, ErrEvaluate, ErrNativeNotSupported, ErrInvalidConfig, ErrFrameDetached, ErrDownloadCanceled, ErrTypeSelectorMismatch)
├── options.go (108 LOC)
│   Option func, BackendType enum, CursorProfile enum, Config struct, With* constructors
├── logger.go
│   Logger interface + noopLogger stub
├── doc.go
│   Package godoc
├── examples/
│   ├── basic/main.go — CDP backend, cross-platform
│   └── native/main.go — Windows native input example
├── input/ (subpackage, ~1k LOC)
│   ├── backend.go (102 LOC)
│   │   Backend interface — Click / Type / MoveTo / Scroll / DragTo / KeyDown / KeyUp / Scroll / Tap / KeyChar / KeyCombo
│   ├── keyboard_human.go (100 LOC)
│   │   Keyboard.TypeHuman(ctx, text, cfg) — slow jittered typing with configurable MistakeRate + CorrectionPause
│   ├── keyboard_human_test.go
│   │   13 unit tests covering mistake simulation, typo correction, empty input, uppercase preservation
│   ├── backend_cdp.go
│   │   CDP impl (uses chromedp for all input)
│   ├── backend_native_windows.go (311 LOC)
│   │   winapi SendInput impl (SetCursorPos, mouse button, keyboard scancodes)
│   ├── backend_native_darwin.go
│   │   macOS stub (returns ErrNotSupported)
│   ├── backend_native_linux.go
│   │   Linux stub (returns ErrNotSupported)
│   ├── cursor.go (112 LOC)
│   │   Cursor struct, MoveTo with human-like path (Bezier ease-in-out)
│   ├── cursor_path.go (131 LOC)
│   │   Bezier path generation, per-distance step count
│   ├── keyboard.go (65 LOC)
│   │   Key struct, scancodes for all printable + modifiers
│   └── touch.go (28 LOC)
│       Tap support
└── Tests
    ├── browser_connect_test.go (93 LOC)
    │   TestConnect fixture + port binding
    ├── page_navigate_strip_scheme_test.go (36 LOC)
    │   URL normalization (https:// → proper format)
    ├── input/cursor_path_test.go
    │   Bezier path generation (start, end, waypoints)
    ├── input/cursor_seeder_test.go
    │   OS position seeding (Windows GetCursorPos)
    └── input/modifier_test.go
        Keyboard modifier key sequences
```

## Entry points

**Browser lifecycle**:
- `Launch(chromePath string, opts ...Option) (*Browser, error)` — spawn Chrome
- `Connect(port int, opts ...Option) (*Browser, error)` — attach to existing Chrome on debug port
- `Browser.Close() error` — teardown (kill process if Launch, detach if Connect)
- `Browser.Done() <-chan struct{}` — signal when browser process exits (Launch path only)
- `Browser.PID() uint32` — process ID (Launch path; 0 for Connect)

**Page management**:
- `Browser.Current() *Page` — active tab
- `Browser.NewPage() (*Page, error)` — create tab
- `Browser.SwitchPageByIndex / ByURL / ByOrigin(arg)` — switch active tab
- `Browser.Pages() []*Page` — all open tabs
- `Browser.ClosePage(id string)` — close specific tab

**Page methods** (all sync, timeout via context in options):
- `Navigate(url, timeout) error`
- `WaitForReady(ctx) error`
- `Click(selector) error`, `Type(selector, text) error`
- `Screenshot(opts...) ([]byte, error)` — PNG
- `QuerySelector(sel) (*Node, error)`, `QuerySelectorAll(sel) ([]*Node, error)`
- `Evaluate(expr, out) error` — JS execution
- `Cookies().Get(name) (*Cookie, error)`, `Set(cookie) error`, `Delete(name) error`, `Clear() error`
- `Mouse().MoveTo(x, y) error`, `ClickAt(x, y) error`, `Drag(x, y) error`, etc.
- `Keyboard().Press(key) error`, `Type(text) error`, etc.
- `Frames()` → navigate iframes by URL / name / index

**Options** (functional pattern):
- `WithLogger(Logger)`
- `WithHeadless(bool)`
- `WithUserDataDir(dir)`
- `WithProxy(addr)`
- `WithInputBackend(BackendType)` — BackendCDP or BackendNative
- `WithNativeWindow(x, y, scale)` — screen offset for native backend
- `WithCursorProfile(profile)` — CursorHuman (Bezier), CursorFast (linear), CursorMachine (one-shot)
- `WithFlag(key, value)` — arbitrary chromedp allocator flag

## Data model

**Browser**:
- `ctx, cancel` — lifecycle context (canceled on Close)
- `allocCancel` — for allocator teardown
- `cfg *config` — merged options
- `pages []*Page` — tab list
- `current *Page` — active tab
- `attached bool` — true if Connect path (don't own process)
- `port int` — CDP port (Connect)
- `pid uint32` — Chrome PID (Launch)
- `cachedHWND uintptr` — Windows window handle

**Page**:
- `id string` — tab ID (UUID from CDP)
- `ctx, cancel` — tab lifecycle
- `cfg *config` — from parent Browser
- `browser *Browser` — backref for HWND lookup
- `backend input.Backend` — CDP or native dispatcher
- `cursor *input.Cursor` — mouse movement planner
- `keyboard *input.Keyboard` — key mappings
- `dialogHandler, consoleHandler` — event callbacks
- `cookiesAPI, mouseAPI, keyboardAPI, downloadsAPI` — namespace objects

**input.Backend** (interface):
- `Click(button MouseButton) error`
- `DoubleClick(button MouseButton) error`
- `MoveTo(x, y int) error`
- `Scroll(x, y, dx, dy int, duration time.Duration) error`
- `DragTo(x, y int, button MouseButton, duration time.Duration) error`
- `KeyChar(ctx context.Context, ch rune) error` — type single rune via backend
- `KeyCombo(ctx context.Context, vk uint16, mods Modifier) error` — press key with modifiers (for Ctrl+A, Enter, etc.)
- `InsertText(ctx context.Context, text string) error` — bulk-insert text (backend-dependent)
- `Tap(x, y int) error` — touch event

## External dependencies

| Import | Purpose | Version |
|--------|---------|---------|
| `github.com/chromedp/chromedp` | DevTools Protocol client | v0.15.1 |
| `github.com/chromedp/cdproto` | Chrome CDP types | (implicit) |
| `github.com/gobwas/ws` | WebSocket transport | (implicit) |
| `golang.org/x/sys` | Windows syscall bindings (user32.dll, kernel32.dll) | stdlib |

## Test strategy

- **Unit tests**: URL parsing (page_navigate_strip_scheme_test.go), Bezier path generation (input/cursor_path_test.go), key codes (input/modifier_test.go)
- **Fixture tests**: browser_connect_test.go sets up a headless Chrome instance for integration validation
- **Coverage**: light — math + parsing. No full end-to-end test harness.
- **No CI config** detected (no .github/workflows/, no Makefile test target)

## Platform matrix

| Feature | Windows | macOS | Linux |
|---------|---------|-------|-------|
| Launch | ✓ | ✓ | ✓ |
| Connect | ✓ | ✓ | ✓ |
| CDP input | ✓ | ✓ | ✓ |
| Native input | ✓ | (stub) | (stub) |
| HWND lookup | ✓ | N/A | N/A |
| Profile focus | ✓ | ✓ | ✓ |

Unsupported ops return `input.ErrNotSupported` with no panic.

## Build tags

- `//go:build windows` — for native backend + HWND logic
- `//go:build darwin || linux` — for stub implementations

No CGO required. Windows uses `syscall` package directly (user32.dll, kernel32.dll).
