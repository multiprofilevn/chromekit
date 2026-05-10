# chromekit — System Architecture

## High-level design

chromekit wraps chromedp (which wraps Chrome DevTools Protocol) with two key abstractions:

1. **Lifecycle abstraction** — Browser owns a Chrome process (or attaches to one). Pages manage tabs.
2. **Input abstraction** — Backend interface hides CDP vs native input dispatch. Caller doesn't care which backend fired the click.

## Component diagram

```
┌─────────────────────────────────────────────────────────────┐
│ User code (e.g., examples/basic/main.go)                    │
│   b := Launch(path)                                         │
│   p := b.Current()                                          │
│   p.Navigate(...) → p.Click(...) → p.Screenshot()           │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ Browser (root package)                                      │
│  ├─ ctx, cancel          [lifecycle]                        │
│  ├─ pages []*Page        [tab list]                         │
│  ├─ current *Page        [active tab]                       │
│  ├─ port, pid            [connection info]                  │
│  ├─ attached bool        [Connect vs Launch mode]           │
│  ├─ cfg *config          [shared options]                   │
│  └─ chromedp.Allocator   [process lifecycle]                │
└──────────────────────┬──────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        │              │              │
        ▼              ▼              ▼
   ┌─────────┐  ┌─────────┐  ┌─────────┐
   │ Page 1  │  │ Page 2  │  │ Page N  │  (tabs)
   │ (ctx)   │  │ (ctx)   │  │ (ctx)   │
   │ backend │  │ backend │  │ backend │
   └────┬────┘  └────┬────┘  └────┬────┘
        │            │            │
        └────────────┼────────────┘
                     │
                     ▼
        ┌────────────────────────┐
        │ Backend interface      │
        │  (input abstraction)   │
        └────────┬────┬──────────┘
                 │    │
        ┌────────▼┐  ┌▼──────────┐
        │ CDP     │  │ Native    │  (swappable)
        │ impl    │  │ impl      │
        │ chromedp│  │ (winapi)  │
        └─────────┘  └───────────┘
                     │
        ┌────────────▼───────────┐
        │ OS / Chrome DevTools   │
        └────────────────────────┘
```

## Lifecycle modes

### Launch (own the process)

```
User code: b, _ := Launch("C:\\...\\chrome.exe")
                    │
                    ▼
         Create ExecAllocator
                    │
                    ▼
         spawn chrome.exe (OS process)
         record pid
                    │
                    ▼
         Create Context (with allocator)
                    │
                    ▼
         Return Browser with attached=false
                    │
         ┌──────────┴──────────┐
         │                     │
         ▼                     ▼
  [normal use]           b.Close()
  p.Navigate(...)            │
  p.Click(...)               ▼
         │            Kill chrome process
         │            Cancel contexts
         │            Flush goroutines
         │                   │
         └───────────────────▼
                    Done
```

Browser owns process lifecycle. `Done <-chan struct{}` signals when process exits (useful for reaping). PID accessible via `PID()` for debugging / focus / HWND lookup.

### Connect (reuse existing process)

```
User code: b, _ := Connect(9222)
                    │
                    ▼
         Lookup chrome process
         on debug port 9222
                    │
                    ▼
         Create Context (without allocator)
         Connect to remote CDP
                    │
                    ▼
         Return Browser with attached=true
                    │
         ┌──────────┴──────────┐
         │                     │
         ▼                     ▼
  [normal use]           b.Close()
  p.Navigate(...)            │
  p.Click(...)               ▼
         │            Detach from targets
         │            Cancel contexts
         │            Chrome stays alive
         │                   │
         └───────────────────▼
                    Done
```

Browser does NOT own process. Close() detaches gracefully, leaving chrome.exe running. Useful for attaching to a user's existing browser session (no data loss on script exit).

## Page lifecycle

Each Page holds its own context and input backend:

```
Browser.NewPage()
         │
         ▼
Create target via CDP
         │
         ▼
Get target ID + context
         │
         ▼
Create Page struct with:
  - id (target ID)
  - ctx, cancel (child of browser.ctx)
  - backend (lazily initialized from config)
  - namespace APIs (CookiesAPI, MouseAPI, etc.)
         │
         ▼
Return Page to caller

[Usage]:
p.Navigate()  → chromedp.Navigate in page.ctx
p.Click()     → backend.Click (CDP or native)
p.Close()     → cancel() + CDP close target
```

Pages are independent — closing one doesn't affect others. Each has its own context timeout inheritance from Browser. Input backend is shared with Browser config (all pages use same BackendType).

## Data flow: Navigate → Click → Screenshot

```
User code:
  p.Navigate(url, 30*time.Second)

Step 1: Navigate
  │
  ├─ Create timeout context from deadline
  ├─ chromedp.Navigate(url) in page.ctx
  ├─ wait for page.FrameStoppedLoading
  └─ return

Step 2: Click(selector)
  │
  ├─ QuerySelector to get element
  ├─ Get BoundingBox (CSS coords)
  ├─ Route to backend (CDP or native)
  │
  ├─ If CDP:
  │  └─ chromedp.MouseClickXY(x, y)
  │     └─ DevTools Protocol event
  │        └─ Chrome browser
  │
  ├─ If Native (Windows):
  │  ├─ Get Chrome window HWND
  │  ├─ Get window offset + DPI
  │  ├─ Convert CSS (x,y) → Screen (x,y)
  │  ├─ SetCursorPos(screen_x, screen_y)
  │  ├─ SendInput(mouse click) — syscall
  │  └─ Chrome receives WM_LBUTTONDOWN
  │
  └─ return

Step 3: Screenshot()
  │
  ├─ chromedp.CaptureScreenshot()
  ├─ Receive PNG bytes
  └─ return []byte
```

Native backend requires visible window + focus. CDP works anytime (even headless). CDP events may be flagged as synthetic; native events are indistinguishable from user clicks.

## Input backend abstraction

```
Page.Click() calls backend.Click(MouseLeft)
                      │
                      ▼
            ┌─────────────────────┐
            │ Backend interface   │
            │ (input/backend.go)  │
            └──────────┬──────────┘
                       │
          ┌────────────┼────────────┐
          │            │            │
          ▼            ▼            ▼
    CDP impl      Native Windows   macOS/Linux stub
    (chromedp)    (user32.dll)      (returns ErrNotSupported)
          │            │            │
          └────────────┼────────────┘
                       │
                       ▼
                   Chrome process
```

Backend interface hides implementation. Page doesn't know if it's using chromedp events or syscalls. Config selects backend at Browser.Launch() / .Connect() time. All pages inherit the same backend.

**Backend methods**:
- `Click(button)`, `DoubleClick(button)`, `RightClick(button)`
- `MoveTo(x, y)` — human-like path via cursor.MoveTo (Bezier + ease-in-out)
- `Scroll(x, y, dx, dy, duration)` — wheel event or screen scroll
- `DragTo(x, y, button, duration)`
- `KeyChar(ch rune)`, `KeyCombo(vk, mods)`, `InsertText(text)` — keyboard dispatch
- `Tap(x, y)` — touch event

Native backend uses `input.Cursor` (Bezier path planner) + OS syscalls. CDP backend is direct chromedp dispatch.

## Keyboard input flow: TypeSelector

`KeyboardAPI.TypeSelector(selector, text, cfg)` implements a form-filling pipeline:

```
User code: p.Keyboard().TypeSelector("input.email", "test@example.com")
                    │
                    ▼
          ┌─────────────────────────┐
          │ TypeSelector (loop)     │
          │ attempt = 1..MaxAttempts│
          └────────┬────────────────┘
                   │
          1. Focus selector
             ├─ Try MouseAPI.FocusElement (click focus)
             └─ Fallback JS: el.focus()
                   │
          2. Clear input (Ctrl+A + Delete)
                   │
          3. Type text via TypeHuman
             ├─ random jitter 80-220ms per char
             ├─ 5% mistake rate (typo + backspace + retype)
             └─ honors context cancellation
                   │
          4. Read value via multi-branch JS
             ├─ .value (input/textarea)
             ├─ .innerText (contenteditable)
             └─ .textContent (fallback)
                   │
          5. Verify (TrimSpace compare)
             ├─ Match → return nil
             └─ Mismatch + attempt < MaxAttempts → goto 2 (retry)
                   │
        Strict=true:  return ErrTypeSelectorMismatch
        Strict=false: return nil (swallow mismatch)
```

Config options:
- `Strict bool` — if true, final mismatch returns error; false swallows it (for async-reformatting fields like phone masks)
- `MaxAttempts int` — total attempts before giving up (default 5)
- `Human input.HumanTypeConfig` — per-char timing + mistake rate override

Use case: filling form fields where value lags behind keystrokes or JavaScript reformats input (currency, phone numbers, etc.).

## Configuration & options

```
defaultConfig()
├─ logger: noopLogger
├─ headless: false
├─ inputBackend: BackendCDP
├─ cursorProfile: CursorHuman
├─ nativeWindow: {Scale: 1.0}
└─ flags: {}

User applies With* options:
  Launch(path, 
    WithHeadless(true),
    WithInputBackend(BackendNative),
    WithCursorProfile(CursorFast),
    WithNativeWindow(100, 200, 1.5)
  )

Result config shared to all pages.
Validation on Launch:
  if headless && inputBackend == BackendNative:
    return ErrInvalidConfig  (native needs visible window)
```

## Event handling

Pages can register listeners for dialog + console events:

```
p.OnDialog(func(ev DialogEvent) DialogAction {
    if ev.Type == "alert" {
        return DialogActionAccept
    }
    return DialogActionDismiss
})

p.OnConsole(func(ev ConsoleEvent) {
    log.Println(ev.Message)
})

[Internal]:
Page.listenersOnce (sync.Once) initializes listeners.
Page spawns goroutines listening to CDP events.
Dialog/console events dispatched to registered handlers.
```

Handlers are synchronous. If you need async, spawn a goroutine inside the handler.

## Frame navigation

Pages can contain iframes. `Frame` provides nav by URL / name:

```
p.Frames()                    → []*Frame
p.MainFrame()                 → *Frame
p.FrameByURL(regexp)          → *Frame (or nil)
p.FrameByName(name)           → *Frame (or nil)

[Internal]:
Frame struct holds:
  - id (CDP frame ID)
  - parentId
  - url, name
  - executionContextId

frameCtxByID map keeps latest context ID per frame.
Queries routed to correct frame context.
```

Frames are read-only — you can't create iframes, only navigate/query inside them.

## Platform strategy

**Windows**: Full support for both CDP + native.
- native backend uses SetCursorPos + SendInput syscalls
- HWND lookup via GetWindowThreadProcessId (PID → window handle)
- Title fallback if PID lookup fails

**macOS / Linux**: CDP only.
- native backend imports return `input.ErrNotSupported`
- stubs in `backend_native_darwin.go` / `backend_native_linux.go`
- no CGO, clean build-tag separation

```go
// +build windows
func (b *Backend) Click(...) { /* winapi SendInput */ }

// +build !windows
func (b *Backend) Click(...) { return input.ErrNotSupported }
```

## Error handling strategy

- **Sentinel errors**: all in `errors.go` (ErrTimeout, ErrNavigate, etc.)
- **Wrapped errors**: high-level funcs wrap low-level errors with context
- **Platform fallback**: native ops return ErrNotSupported gracefully (no panic)
- **Timeout handling**: context.WithTimeout passed to all sync methods

Example:
```go
func (p *Page) Navigate(url string, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(p.ctx, timeout)
    defer cancel()
    
    err := chromedp.Navigate(url).Do(ctx)
    if err == context.DeadlineExceeded {
        return fmt.Errorf("%w: %s", ErrTimeout, url)
    }
    if err != nil {
        return fmt.Errorf("%w: %s", ErrNavigate, err)
    }
    return nil
}
```

## Goroutine lifecycle

**Browser**:
- Main context for allocator + targets
- Spawned on Launch / Connect, canceled on Close
- No leaked goroutines (chromedp handles cleanup)

**Page**:
- Child context (canceled when Page.Close)
- Listeners goroutine (if OnDialog / OnConsole registered) — runs until cancel
- No blocking goroutines — all timeout-aware

**No global state**: all state is instance-bound (Browser or Page struct fields). Thread-safe via fine-grained mutexes.

## Integration points

| Layer | Interface | Example |
|-------|-----------|---------|
| Caller ↔ Browser | public funcs: Launch, Connect | user calls Launch("C:\\...\\chrome.exe") |
| Browser ↔ Page | Browser.pages, Browser.current | page mgmt, switching |
| Page ↔ Backend | input.Backend interface | Click routes to CDP or native |
| Backend ↔ OS | syscall, chromedp | SendInput syscalls, DevTools events |
| Page ↔ Listeners | goroutine dispatcher | dialog/console handled async |

## Future extensibility

Current design allows:
- **New input backends**: implement `input.Backend` interface, select via WithInputBackend(Type)
- **New namespaces**: add methods like `p.Network()` → NetworkAPI (without breaking Page API)
- **New options**: add With* functions + config fields (backward compatible via functional options)
- **New platforms**: add build-tag files + backend stubs (Windows → Windows 11/Server, macOS M1/Intel, Linux distros)

Not allowed without major version bump:
- Change Browser/Page method signatures
- Change exported error var names
- Change Option func signature
