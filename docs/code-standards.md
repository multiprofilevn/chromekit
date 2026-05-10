# chromekit — Code Standards

## Language & style

**Go conventions**:
- Follow `gofmt` for formatting
- Package names lowercase, single word when possible (`chromekit`, `input`)
- File names snake_case (e.g. `page_navigate.go`, `browser_focus_windows.go`)
- Exported identifiers (types, funcs, consts, vars) must have godoc comments
- Error vars as sentinels in `errors.go`, prefixed `Err*` (e.g. `ErrTimeout`)

**Non-Go files** (docs, scripts):
- Kebab-case filenames (e.g. `project-overview-pdr.md`, `code-standards.md`)
- Kebab-case for shell scripts (e.g. `validate-docs.sh`)

Per [CLAUDE.md](../CLAUDE.md):
- File > 200 LOC → consider modularizing (chromekit does this: 32 files in root, 12 in `input/`)
- No "improving" pre-existing code unrelated to the task — surgical edits only

## Package structure

**Root package (`chromekit/`)**:
- Core types: `Browser`, `Page`, namespace APIs (CookiesAPI, MouseAPI, etc.)
- Entry points: `Launch()`, `Connect()`
- Lifecycle: `Close()`, teardown logic
- Options & config: `options.go` + functional Option pattern
- Errors: `errors.go` sentinel vars
- Logger: interface + stub

**Subpackage (`input/`)**:
- Backend abstraction: `Backend` interface + 3 implementations (CDP, native Windows, stubs)
- Input primitives: Cursor, Keyboard, TouchScreen
- Platform-specific: build tags (`//go:build windows`, `//go:build darwin || linux`)

**Examples** (`examples/`):
- `basic/main.go` — CDP backend, works on all platforms
- `native/main.go` — Windows native input showcase

**Tests** (co-located with source):
- `*_test.go` in same package
- Unit tests: math, parsing, URL handling
- Fixture tests: browser lifecycle (Connect, close, teardown)

## API design patterns

### Namespaces (method chaining)

Page exposes functional namespaces to avoid flat method explosion:

```go
// Instead of: page.ClickMouse(x, y), page.MoveMouse(x, y), page.Scroll(...)
// Use namespace methods:
page.Mouse().ClickAt(x, y)
page.Keyboard().Type("text")
page.Cookies().Get(name)
page.Storage().Local().Get(key)
page.Frames() // no (), returns slice
```

**Namespace types**:
- `CookiesAPI` — Get / Set / Delete / Clear
- `MouseAPI` — MoveTo / ClickAt / Scroll / Tap / Hover / DoubleClick / RightClick / Drag / DragAndDrop
- `KeyboardAPI` — Press / Down / Up / Type / TypeFast / TypeHuman / TypeSelector
- `DownloadsAPI` — SetPath / Wait
- `StorageAPI` → `.Local()` / `.Session()` → Get / Set / Remove / Clear

### Option pattern

Functional options for configuration:

```go
func Launch(chromePath string, opts ...Option) (*Browser, error) {
    cfg := defaultConfig()
    for _, opt := range opts {
        opt(cfg)
    }
    // ...
}

type Option func(*config)

func WithHeadless(h bool) Option {
    return func(c *config) { c.headless = h }
}
```

All options in `options.go`. Public types: `Option`, `BackendType` (iota enum), `CursorProfile` (iota enum). Internal struct: `config`.

### Error handling

- **Sentinel errors**: all in `errors.go`, exported `Err*` vars using `errors.New()`
- **Wrapping**: use `fmt.Errorf("context: %w", err)` to add context
- **Platform fallback**: unsupported ops (e.g. native backend on macOS) return `input.ErrNotSupported` — caller decides whether to retry with CDP or fail

Example from `browser_focus_other.go`:
```go
// +build darwin linux
func (b *Browser) focus() error {
    return input.ErrNotSupported
}
```

Windows impl in `browser_focus_windows.go`:
```go
// +build windows
func (b *Browser) focus() error {
    // actual HWND lookup + SetForegroundWindow
}
```

### Sync & context

- **Context**: no passed through method signatures. Instead, store in Browser/Page struct and use `context.WithTimeout()` at high-level methods (Navigate, Wait*, Click, etc.)
- **Locks**: fine-grained RWMutex per resource (pages, frames, listeners) — not global
- **Lifecycle**: `ctx, cancel` in Browser + Page. Canceling parent cancels children.
- **Graceful shutdown**: `Close()` cancels contexts, detaches targets (Connect path) or kills process (Launch path), flushes goroutines

### Platform-specific code

Use build tags + filename convention:

```
file_windows.go     // +build windows
file_darwin.go      // +build darwin
file_linux.go       // +build linux
file_other.go       // +build !windows,!darwin,!linux (or omit, files default to all platforms)
```

Example: `browser_focus_windows.go` (HWND lookup via GetWindowThreadProcessId), `browser_focus_other.go` (stub returning ErrNativeNotSupported).

No CGO. Windows syscalls via `golang.org/x/sys/windows` + `syscall` package builtin.

## Module organization

| Concern | Files | Rationale |
|---------|-------|-----------|
| Lifecycle | `browser.go`, `browser_connect.go`, `browser_pages.go` | entry points + tab mgmt |
| Navigation | `page_navigate.go`, `page_wait.go` | high-level user actions |
| DOM access | `page_dom.go`, `page_eval.go` | element queries + JS |
| Input | `page_action.go`, `page_input.go`, `page_*_api.go` | Click / Type / Mouse / Keyboard |
| Events | `page_listeners.go`, `page_dialog.go`, `page_console.go` | handlers + dispatchers |
| Config | `options.go`, `logger.go`, `doc.go` | shared setup |
| Errors | `errors.go` | sentinel vars |
| Platforms | `browser_focus_*.go`, `input/backend_native_*.go` | build-tag splits |
| Examples | `examples/basic/`, `examples/native/` | user docs |

Split rule: if a file hits ~150 LOC and groups a cohesive concern (e.g. Mouse API methods), split into `page_mouse_api.go` + keep rest in page.go. Exceptions: `page.go` is foundation (struct + lifecycle) and is allowed to be ~134 LOC.

## Naming conventions

**Types** (exported):
- `Browser`, `Page`, `Frame`, `Node` (singular, noun)
- `CookiesAPI`, `MouseAPI`, `KeyboardAPI` (suffix for namespace types)
- `BackendType`, `CursorProfile`, `MouseButton` (descriptive, enum types)

**Functions** (exported):
- Constructors: `Launch()`, `Connect()`, `NewPage()` — no factory prefix
- Methods: verb-first: `Click()`, `Navigate()`, `WaitForSelector()`, `Evaluate()`
- Queries: noun-first: `QuerySelector()`, `Pages()`, `Frames()`

**Constants** (exported):
- `BackendCDP`, `BackendNative` (enum values, PascalCase)
- `CursorHuman`, `CursorFast`, `CursorMachine`
- `MouseLeft`, `MouseRight`, `MouseMiddle`

**Private**:
- `config`, `nativeWindow`, `frameCtxByID` (lowercase, even for structs)

**Vars** (exported, sentinel errors):
- `ErrTimeout`, `ErrNavigate`, `ErrNativeNotSupported` (prefix `Err`, PascalCase)

## Testing

**Test placement**:
- Co-located: `page_navigate_test.go` next to `page_navigate.go`
- Table-driven tests preferred for unit tests

**Coverage focus**:
- Critical path: URL normalization, timeout handling
- Math: Bezier path generation, cursor seeding
- Parsing: option merging, config validation

**What's out of scope for now**:
- Full end-to-end integration tests (would require running Chrome instance in CI)
- Flaky platform-specific tests (HWND lookup, focus events) — manual validation OK

**Running tests**:
```bash
go test ./...          # all tests
go test ./input/...    # input subpackage only
go test -v ./...       # verbose output
```

## Dependencies & imports

**Keep minimal**:
- `chromedp` + `cdproto` — Chrome automation (required)
- `gobwas/ws` — transport (implicit, via chromedp)
- `golang.org/x/sys` — Windows syscalls only (conditional via build tags)
- Standard library (context, sync, time, fmt, errors)

**No extras**: don't add logging frameworks, HTTP libs, or reflection tricks — keep the API direct.

**Import order** (enforced by gofmt):
1. Standard library
2. External (github.com/...)
3. Local (chromekit/...)

Example:
```go
import (
    "context"
    "fmt"
    "sync"

    "github.com/chromedp/chromedp"
    "github.com/tuwibu/chromekit/input"
)
```

## Breaking changes

- **Pre-1.0 library** — minor version bumps may break API. Semantic versioning applies (vX.Y.Z).
- **Recent breaking change**: Launch now requires explicit Chrome path as first arg (before: fell back to chromedp default). Updated README quickstart.
- **Future**: freeze API once 1.0 ships; use extension patterns (WithFlag, namespace methods) for new features instead of altering signatures.

## Godoc & comments

**Exported identifiers must have docstrings**:
```go
// Browser owns a Chrome process and the pages opened on it.
type Browser struct { ... }

// Launch starts Chrome with the given options and opens the first blank page.
// Caller MUST defer b.Close() to avoid leaking the Chrome process.
func Launch(chromePath string, opts ...Option) (*Browser, error) { ... }
```

**Internal helper comments** (non-exported):
- Only when logic is non-obvious
- Avoid restating the code

**README as canonical API reference**: Godoc is reference; README is guide + examples. Don't duplicate the method table in code comments.

## Code review checklist

- [ ] Follows gofmt (`go fmt ./...`)
- [ ] Exported symbols have godoc comments
- [ ] Error handling: returns error, wraps with context
- [ ] Platform-specific code uses build tags + filename convention
- [ ] Locks are fine-grained, not global
- [ ] Context lifecycle: matches Browser/Page lifecycle
- [ ] No dead imports or unused variables
- [ ] Examples compile and run
- [ ] Tests pass: `go test ./...`
- [ ] No refactoring of unrelated code (surgical only)
