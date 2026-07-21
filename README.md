# chromekit

Go package wrapping [chromedp](https://github.com/chromedp/chromedp) with a Puppeteer-like API. No more juggling `context.Context` — just `b := Launch(); p := b.Current(); p.Click(...)`.

## Install

```bash
go get github.com/tuwibu/chromekit
```

## Quickstart — attach to running Chrome

Start Chrome first with a debug port:

```
chrome.exe --remote-debugging-port=9222 --user-data-dir=C:\tmp\profile
```

Then attach via `Connect`:

```go
package main

import (
    "log"
    "os"
    "time"

    "github.com/tuwibu/chromekit"
)

func main() {
    b, err := chromekit.Connect(9222)
    if err != nil {
        log.Fatal(err)
    }
    defer b.Close()

    p := b.Current()
    p.Navigate("https://example.com", 30*time.Second)
    p.ClickElement("a")

    img, _ := p.Screenshot()
    os.WriteFile("out.png", img, 0o644)
}
```

Use `Launch` instead if you want chromekit to spawn Chrome itself. The first
arg is the absolute path to the Chrome / Chromium binary; pass `""` to fall
back to chromedp's default `exec.LookPath("chrome")` (often fails on Windows
where chrome.exe is not on PATH):

```go
b, _ := chromekit.Launch(
    `C:\Program Files\Google\Chrome\Application\chrome.exe`,
    chromekit.WithHeadless(false),
)
```

`Connect` reuses the user's tabs and never kills Chrome on `Close()`. `Launch` owns the process and shuts it down.

## Core API

| Layer    | Method                                        | What it does                              |
|----------|-----------------------------------------------|-------------------------------------------|
| Browser  | `Connect(port, opts ...Option)`                | attach to a Chrome already running on the debug port |
| Browser  | `Launch(chromePath, opts ...Option)`           | spawn a new Chrome process at chromePath (or `""` for default lookup) |
| Browser  | `NewPage()` / `Pages()` / `Current()`          | manage tabs                               |
| Browser  | `SwitchPageByIndex/URL/Origin`                 | switch active tab                         |
| Browser  | `ClosePage(id)` / `Close()`                    | clean teardown (see note below)           |
| Page     | `Navigate(url, timeout)` / `Reload` / `GoBack` / `GoForward` | navigation             |
| Page     | `WaitForReady` / `WaitForSelector` / `WaitForChangeURL` / `WaitForFunction` | sync |
| Page     | `Click(sel)` / `ClickElement(sel)` / `Type(sel, text)` / `Hover(sel)` | high-level shortcut |
| Page     | `Evaluate(expr, out)` / `EvalSelector(sel, fn, out)` / `EvalSelectorAll`   | run JS |
| Page     | `Screenshot()` / `FullScreenshot(q)` / `ScreenshotClip(x,y,w,h)` / `ScreenshotElement(sel)` | PNG capture |
| Page     | `QuerySelector` / `QuerySelectorAll` / `BoundingBox` / `GetText` / `GetAttribute` / `ScrollIntoView` | DOM |
| Page     | `SetExtraHeaders(map)` / `SetInputFiles(sel, paths...)` | request shaping / upload   |
| Page     | `OnDialog(handler)` / `OnConsole(handler)`     | event listeners                          |
| Page     | `Frames()` / `MainFrame()` / `FrameByURL(re)` / `FrameByName(name)` | iframe access      |
| Page     | `Cookies()` → `Get/Set/Delete/Clear`           | cookie namespace                          |
| Page     | `Mouse()` → `MoveTo/ClickAt/Scroll/Tap/Hover/DoubleClick/RightClick/Drag/DragAndDrop` | mouse |
| Page     | `Keyboard()` → `Press/Down/Up/Type/TypeFast/TypeHuman` | keyboard namespace                        |
| Page     | `Keyboard().TypeSelector(sel, text, cfg...)` | type text via TypeHuman, verify round-trip (Strict/MaxAttempts)      |
| Page     | `Storage().Local()/Session()` → `Get/Set/Remove/Clear` | web storage                       |
| Page     | `Downloads()` → `SetPath/Wait`                 | download interception                     |
| Browser  | `GrantPermissions(origin, perms...)` / `ResetPermissions()` | permission overrides         |

### Closing the primary page

The first tab chromekit binds on `Launch` / `Connect` (the *primary* page) shares
its context with the `Browser` itself. `ClosePage(primaryID)` and
`primary.Close()` therefore refuse to run — cancelling that context would take
every other tab with it, and on the `Launch` path kill the Chrome process.
`ClosePage` returns `ErrCannotClosePrimaryPage`; `ClosePagesNotMatching` skips
the primary regardless of URL. Use `b.Close()` to shut the browser down.

## Options

| Option | Meaning |
|---|---|
| `WithLogger(l)` | inject a `Logger` (default: no-op) |
| `WithUserDataDir(dir)` | persist Chrome profile across runs |
| `WithHeadless(bool)` | toggle headless mode (default false) |
| `WithProxy(addr)` | route Chrome through a proxy |
| `WithInputBackend(t)` | `BackendCDP` (default) or `BackendNative` |
| `WithNativeWindow(x, y, scale)` | set window offset + DPI for native backend |
| `WithFlag(key, value)` | pass arbitrary chromedp flag |

## Input backends

|                    | CDP (default)                            | Native                                   |
|--------------------|------------------------------------------|------------------------------------------|
| Mechanism          | chromedp dispatches DevTools events      | OS APIs — winapi `SendInput` on Windows  |
| Works when tab is  | any state, even hidden / not focused     | only when the window is focused & visible|
| Headless           | yes                                      | no                                       |
| Bot detection      | sometimes flagged (`isTrusted=false`)    | hard to detect — events come from OS     |
| Cross-platform     | yes                                      | Windows only (macOS/Linux: stub error)   |
| Dependencies       | none                                     | none — uses `syscall` + `user32.dll`     |

Pick CDP for general automation. Pick Native when scraping a site that fingerprints synthetic events. Combining `WithHeadless(true)` + `BackendNative` returns `ErrInvalidConfig` — native needs a real window.

## Platform support

| OS      | CDP  | Native       |
|---------|------|--------------|
| Windows | yes  | yes          |
| macOS   | yes  | no — stub    |
| Linux   | yes  | no — stub    |

On macOS/Linux, calling a native method returns `input.ErrNotSupported`. Detect and fall back to CDP if needed.

## Limitations

- No stealth plugins (`navigator.webdriver` hide, UA rotation) — out of scope
- No network interception (mock response, block resources)
- No profile zip / cloud sync — that belongs in your app, not the library
- Native input on macOS/Linux returns `ErrNotSupported`

## Examples

- [examples/basic](examples/basic/main.go) — CDP backend, runs anywhere
- [examples/native](examples/native/main.go) — Windows-only, native input

## License

MIT
