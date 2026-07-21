# chromekit — Changelog

All notable changes to the chromekit library are documented here. Pre-1.0, semver not strictly enforced — API may evolve.

## [v0.7.0] — 2026-05-11

### Added
- `Page.WaitForSelectorGone(selector, timeout)` — blocks until the selector no longer matches OR the matched element is detached/hidden (`offsetParent == null`). Sugar over `WaitForFunction`.
- `Page.SetSelectValue(selector, value)` — sets `<select>.value` and dispatches a bubbling `change` event so React/Vue/Angular state bindings update. Returns `ErrEvaluate` when the selector matches nothing.

Both methods are integration-tested (build tag `integration`).

## [Unreleased]

### Fixed — leak & concurrency audit (2026-07-20)

Findings from a full leak/goroutine/handle audit, each verified against real
Chrome. Measurements are goroutine counts from the stress harness.

| Area | Before | After |
|---|---|---|
| Tab closed by the page / user (`window.close()`, OAuth popup) | `Page` never removed; +3 goroutines and ~15KB per tab, forever | evicted within ~1s; counts flat over 50 closes |
| `NewPage` → `ClosePage` churn | already clean | unchanged (regression-tested) |
| `Connect`/`Close` cycle | ~7 goroutines + ~30KB | flat over 30 cycles |

- **Closing the primary tab no longer kills the browser.** The first tab shares
  its context with the `Browser`, so `ClosePage`/`Page.Close` on it cancelled
  every sibling and, on the `Launch` path, the Chrome process.
  `ClosePagesNotMatching` hit this routinely. Both now return
  `ErrCannotClosePrimaryPage`; `ClosePagesNotMatching` skips it.
- **Externally destroyed tabs are cleaned up.** Browser-level
  `Target.targetDestroyed` subscription, with target discovery enabled
  explicitly so it also works on the `Connect` path (chromedp only enables it
  for connections it bootstraps itself).
- **`pagesMu` is never held across a CDP call.** `SwitchPageByIndex` used to
  hold the write lock through `BringToFront`'s three-second activation poll,
  stalling every `Pages`/`Current`/`NewPage`/`ClosePage` caller meanwhile.
- **Mouse and input operations are bounded.** Every CDP input dispatch carries a
  5s timeout — `Input.dispatchMouseEvent` only acknowledges once the renderer
  consumes it, so a wedged tab parked whole gestures indefinitely. `Navigate`
  and friends now apply their `timeout` to the navigation itself, not only the
  readiness wait; `Click`/`Type` gained `DefaultActionTimeout`.
- **`Cursor` serializes gestures.** Concurrent `MoveTo` on one page was a data
  race on the tracked position and interleaved two paths into one jittering
  cursor.
- **Dialog handlers no longer run on chromedp's dispatch goroutine.** A handler
  that called back into its own page deadlocked the tab permanently, freezing
  every pending CDP reply including in-flight mouse acknowledgements.
- **Console handlers are bounded** (8 concurrent, messages truncated at 4KB) —
  a page in a `console.log` loop could previously spawn goroutines without limit.
- **`Downloads().Wait()`** discards events from downloads that already finished
  unwaited, so it no longer returns a previous download's filename, and
  snapshots its channel so it cannot race `SetPath`. Events belonging to the
  download currently being waited on are kept — Chrome reports
  `downloadWillBegin` asynchronously, so they often arrive before `Wait` is
  entered.
- **Frame cache** keeps its dirty flag when the tree changes mid-rebuild, and
  `Frame.IsAttached()` now actually flips to false on detach.
- **Native input on Windows**: modifiers are released on every exit path (a
  mid-combo `SendInput` failure left Ctrl/Alt/Shift stuck down system-wide);
  `Focus`'s ALT tap is serialized with other input; `syscall.NewCallback` is
  allocated once instead of per lookup (Go guarantees only 1024 slots, so a
  long-running fleet eventually panicked); cached `HWND` is validated before
  use and title matching is scoped to the browser's own PID.
- **`Connect`/`Close` releases everything and keeps the user's tabs.** Cancelling
  a chromedp context closes the tab bound to it, so `Close()` used to skip
  cancellation entirely and leak the whole connection — roughly 7 goroutines per
  cycle. It now detaches tabs it does not own from chromedp before cancelling
  (`Context.Target = nil` makes chromedp's cleanup skip `Target.closeTarget`),
  so teardown is complete while the user's tabs and Chrome process survive. The
  older explanation that cancelling triggered `Browser.close` was wrong:
  chromedp routes `RemoteAllocator` contexts around that branch.
- **`Browser.Done()` closes after `Close()` on the Connect path.** It only
  followed the chromedp context, which that path deliberately never cancelled,
  so anything waiting on `<-b.Done()` blocked forever.
- `WithModifyCmd` no longer silently drops chromedp's process-group setup, which
  is what reaps Chrome when the Go process dies abruptly.

### Added

#### Exec Hook & Input Backend Getter
- **exec.Cmd mutation hook** (`feat: WithModifyCmd option`)
  - `WithModifyCmd(fn func(*exec.Cmd)) Option` injects raw flags before Chrome starts
  - Use for single-dash flags like `-disable-features=` on GPM-Login Chromium forks
  - Hook fires after chromedp builds default args, before exec.Cmd.Start
  - Safe to mutate `Cmd.Args`; do not mutate `Cmd.Process`

- **input backend accessor** (`feat(browser): expose InputBackend() getter`)
  - `Browser.InputBackend() BackendType` reports which backend the browser was launched with
  - For orchestrators (e.g. chromefleet) branching behavior on native vs CDP
  - Returns `BackendCDP` if browser or config is nil

#### Keyboard Utilities
- **clear input field** (`feat(keyboard): add ClearInput`)
  - `KeyboardAPI.ClearInput(selector ...string) error` — Ctrl+A then Delete
  - With selector: focuses via `chromedp.Focus` first (DOM-level)
  - Without selector: assumes caller has already focused the field
  - Returns `ErrEvaluate` if focus fails

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

### Changed

#### Breaking: Allocator Defaults No Longer Bundled
- **commit `270ca69` refactor(launch): drop chromedp default allocator options**
  - `chromedp.DefaultExecAllocatorOptions` no longer prepended to the flag list
  - **Rationale**: hidden defaults leaked flags callers explicitly tried to avoid; per-engine profiles (gologin / gpmlogin / stock Chrome) need wildly different argv
  - **Migration**: `buildAllocatorOptions` starts from an empty option set; only `headless` + caller `WithFlag(k, v)` entries are applied. Any chromedp default flag you relied on (e.g. `disable-extensions`, `no-first-run`) must now be re-added explicitly via `chromekit.WithFlag("flag-name", value)`. There is no public splat of `chromedp.DefaultExecAllocatorOptions`.
  - Simplifies flag composition for non-standard Chrome forks

#### Platform-specific Improvements
- **PID capture from chromedp allocator** (`fix(launch): capture chrome PID from chromedp allocator`)
  - Browser PID now captured from `chromedp.FromContext(ctx).Browser.Process()` right after `chromedp.Run` succeeds
  - Windows HWND lookup no longer depends on page title heuristic
  - `findChromeWindowByPID` resolves the visible window reliably even on blank pages at automation start

### Pending (Uncommitted)

#### Page Lifecycle
- **BringToFront rewrite** (page.go uncommitted)
  - Now calls `page.BringToFront()` (CDP Page.bringToFront) as primary primitive
  - Then `target.ActivateTarget` (non-fatal) for tab-strip sync
  - Reason: ActivateTarget alone no-ops when session-restore/extension tabs race activation; native-input HWND lookups need tab-strip selection in sync
  - **Status**: under review, not yet shipped

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
