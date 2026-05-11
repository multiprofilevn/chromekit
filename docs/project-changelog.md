# chromekit — Changelog

All notable changes to the chromekit library are documented here. Pre-1.0, semver not strictly enforced — API may evolve.

## [v0.7.0] — 2026-05-11

### Added
- `Page.WaitForSelectorGone(selector, timeout)` — blocks until the selector no longer matches OR the matched element is detached/hidden (`offsetParent == null`). Sugar over `WaitForFunction`.
- `Page.SetSelectValue(selector, value)` — sets `<select>.value` and dispatches a bubbling `change` event so React/Vue/Angular state bindings update. Returns `ErrEvaluate` when the selector matches nothing.

Both methods are integration-tested (build tag `integration`).

## [Unreleased]

### Added

#### Human-like Keyboard Input
- **human typing with jittered cadence** (`feat(keyboard): add TypeHuman for human-like typing`)
  - `KeyboardAPI.TypeHuman(text, cfg ...input.HumanTypeConfig)` types at 80–220ms per character
  - Per-character typo simulation (default 5% chance): picks QWERTY-adjacent wrong key → `CorrectionPause` (250ms) → backspace via `VKBackspace` → retype correct character
  - Config fields: `MinDelay`, `MaxDelay`, `MistakeRate`, `CorrectionPause`, injectable `*rand.Rand` for determinism
  - Honors context cancellation between keystrokes

- **selector-based typing with round-trip verification** (`feat(page): add TypeSelector for form filling`)
  - `KeyboardAPI.TypeSelector(selector, text, cfg ...TypeSelectorConfig)` focuses element, types via TypeHuman, verifies value matches
  - Reads value via multi-branch JS: `.value` (input/textarea) → `.innerText` (contenteditable) → `.textContent` (fallback)
  - Retries up to `MaxAttempts` (default 5) on mismatch: clears field and retypes
  - `Strict=true` returns `ErrTypeSelectorMismatch` on final failure; `Strict=false` swallows mismatch (for async-reformatting fields like phone masks)
  - TrimSpace comparison (whitespace-insensitive matching)

#### API additions
- `input.VKBackspace const uint16 = 0x08` — virtual-key code for backspace (Win32)
- `input.HumanTypeConfig struct` — configure TypeHuman timing + mistake rate
- `input.Keyboard.TypeHuman(ctx, text, cfg)` — low-level human typing dispatch
- `TypeSelectorConfig struct` — configure TypeSelector retry + strictness + per-char cadence
- `ErrTypeSelectorMismatch error` — returned when Strict=true and value mismatches after MaxAttempts

---

## [v0.1] — 2026-05-10

### Added

#### Browser & Page Lifecycle
- **expose Done channel** (`feat(browser): expose Done channel and PID accessor`)
  - `Browser.Done() <-chan struct{}` signals when process exits (Launch mode)
  - Enables graceful reaping of Chrome subprocess
  - Useful for long-running bots that need to detect crashes

- **expose PID accessor** (`feat(browser): expose Done channel and PID accessor`)
  - `Browser.PID() uint32` returns Chrome process ID
  - Required for Windows HWND lookup + debugging
  - Returns 0 for Connect mode (browser.pid only set for Launch)

#### Native Input Backend
- **native backend Navigate via Chrome omnibox** (`feat(navigate): native backend Navigate via Chrome omnibox`)
  - Can now navigate using native input: focus omnibox, type URL, press Enter
  - Improves stealth (avoids CDP SetLocation events)
  - Requires window focus + visible UI (native backend constraint)

- **pid-based HWND lookup with title fallback** (`feat(input): pid-based HWND lookup with title fallback`)
  - Windows native backend now finds Chrome window handle via GetWindowThreadProcessId
  - Falls back to title search if PID lookup fails (more robust)
  - Fixes issues on systems with multiple Chrome processes

#### Bug Fixes
- **fix(close): keep chrome process alive after Connect+Close** (`fix(close): keep chrome process alive after Connect+Close`)
  - Connect mode now correctly preserves Chrome process on Close()
  - Distinguishes: Launch (owns process, kills on Close) vs Connect (attaches, leaves running)
  - Fixes lifecycle bug where Connect+Close was terminating user's browser

### Changed

#### Breaking Changes
- **require explicit Chrome path as first arg** (`feat(launch): require explicit Chrome path as first arg`)
  - `Launch()` now requires `chromePath string` as **first positional argument**
  - Removed fallback to chromedp's `exec.LookPath("chrome")` (often fails on Windows)
  - **Migration**: 
    ```go
    // Before:
    // b, _ := chromekit.Launch(opts...)
    
    // After:
    b, _ := chromekit.Launch(`C:\Program Files\Google\Chrome\Application\chrome.exe`)
    ```
  - Rationale: explicit is better than implicit; Windows users must provide path or use Connect
  - README quickstart updated to reflect this

### Fixed
- Windows native input now reliably targets Chrome window even with multiple processes
- Connect + Close lifecycle now idempotent (can safely call Close multiple times)
- HWND lookup resilient to window renames / state changes

### Documentation
- README: updated Launch quickstart with explicit chrome path
- README: clarified Connect vs Launch lifecycle behavior
- Added examples: basic/main.go (CDP), native/main.go (Windows native)
- Code godoc comments on all exported symbols

---

## [v0.0] — Initial Release

### Added
- **Root package `chromekit/`**:
  - Browser: Launch, Connect, Close
  - Page management: NewPage, SwitchPage, ClosePage, Current, Pages
  - Navigation: Navigate, Reload, GoBack, GoForward
  - Wait helpers: WaitForReady, WaitForSelector, WaitForChangeURL, WaitForFunction
  - DOM queries: QuerySelector(All), BoundingBox, GetText, GetAttribute, ScrollIntoView
  - Evaluate: Evaluate, EvalSelector, EvalSelectorAll
  - Screenshots: Screenshot, FullScreenshot, ScreenshotClip, ScreenshotElement
  - Input shortcuts: Click, ClickElement, Type, Hover
  - Frames: Frames, MainFrame, FrameByURL, FrameByName
  - Namespaced APIs:
    - Cookies: Get, Set, Delete, Clear
    - Mouse: MoveTo, ClickAt, Scroll, Tap, Hover, DoubleClick, RightClick, Drag, DragAndDrop
    - Keyboard: Press, Down, Up, Type, TypeFast
    - Downloads: SetPath, Wait
    - Storage: Local(), Session() → Get, Set, Remove, Clear
  - Events: OnDialog, OnConsole with handler registration
  - Permissions: GrantPermissions, ResetPermissions
  - Headers: SetExtraHeaders
  - Upload: SetInputFiles

- **Subpackage `input/`**:
  - Backend interface (CDP vs native abstraction)
  - CDP implementation (chromedp integration)
  - Native Windows implementation (user32.dll + kernel32.dll syscalls)
  - macOS / Linux stubs (return ErrNotSupported)
  - Cursor: Bezier path generation for human-like mouse movement
  - Keyboard: key code mapping for all printable + modifier keys
  - TouchScreen: tap support

- **Configuration**:
  - Functional options pattern (Option func type)
  - WithLogger, WithHeadless, WithUserDataDir, WithProxy
  - WithInputBackend (BackendCDP | BackendNative)
  - WithNativeWindow (x, y, scale for native input)
  - WithCursorProfile (CursorHuman | CursorFast | CursorMachine)
  - WithFlag (arbitrary chromedp allocator flag)

- **Error handling**:
  - Sentinel errors: ErrContextCanceled, ErrPageNotFound, ErrNavigate, ErrTimeout, ErrEvaluate, ErrNativeNotSupported, ErrInvalidConfig, ErrFrameDetached, ErrDownloadCanceled

- **Platform support**:
  - Windows: full (CDP + native)
  - macOS: CDP only (native returns ErrNotSupported)
  - Linux: CDP only (native returns ErrNotSupported)

- **Tests**:
  - browser_connect_test.go: fixture + Connect validation
  - page_navigate_strip_scheme_test.go: URL normalization
  - input/cursor_path_test.go: Bezier path generation
  - input/cursor_seeder_test.go: OS cursor position seeding
  - input/modifier_test.go: keyboard modifier sequences

### Notes
- Pre-1.0 library — API may change
- No CI/CD pipeline set up yet
- Test coverage is light (unit tests only, no end-to-end)
- Documentation: README + godoc comments
- License: MIT

---

## Versioning

Starting from v0.1, this project uses Semantic Versioning (SemVer):
- **Major (X.0.0)**: breaking API changes
- **Minor (0.Y.0)**: new features, backward compatible
- **Patch (0.0.Z)**: bug fixes, backward compatible

Pre-1.0 releases (v0.x.y) may include breaking changes in minor versions. Once 1.0.0 ships, SemVer will be strictly enforced.

## How to upgrade

### From v0.1 to v0.2+ (when released)
Check the **Breaking Changes** section in each release. If only `Added` or `Fixed` sections apply, upgrade is safe.

### To current HEAD (development)
Users can import at HEAD for latest features:
```bash
go get github.com/tuwibu/chromekit@latest
# or specific commit:
go get github.com/tuwibu/chromekit@9f894ab
```

---

## Maintenance

- **Contributors**: Report bugs via GitHub issues
- **Pull requests**: Must include CHANGELOG entry in the PR description
- **Releases**: Tagged by maintainers once phases complete (see development-roadmap.md)
