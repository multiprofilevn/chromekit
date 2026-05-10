# chromekit — Project Overview & PDR

## What is chromekit?

Go library wrapping chromedp with a Puppeteer-like API. Removes boilerplate around Chrome DevTools Protocol: no manual context threading, no allocator juggling, just clean Page/Browser/namespace methods.

## Who it's for

- Go automation engineers who want browser control without context plumbing
- Bot builders needing native input (mouse/keyboard) on Windows for bot-evasion
- Teams automating Chrome tasks across Windows, macOS, Linux (with graceful fallback for unsupported features)

## Scope (in)

- **Browser lifecycle**: Launch / Connect / Close, page management (new/switch/list)
- **Navigation**: Navigate / Reload / GoBack / GoForward + sync helpers (WaitForReady, WaitForChangeURL)
- **DOM queries**: QuerySelector(All), BoundingBox, GetText, GetAttribute, ScrollIntoView
- **Input abstraction**: Two backends (CDP + native winapi). Human-like cursor paths (Bezier). Keyboard with raw key codes.
- **Namespaces**: Mouse, Keyboard, Cookies, Storage, Downloads, Frames (iframes)
- **JavaScript eval**: Evaluate / EvalSelector + console/dialog event handlers
- **Request shaping**: SetExtraHeaders, SetInputFiles (form upload), SetProxy
- **Cross-platform**: Windows (both backends) + macOS/Linux (CDP only, native returns ErrNotSupported)

## Non-scope (out)

- Stealth plugins (navigator.webdriver hide, UA rotation) — belongs in applications, not the library
- Network interception (mock/block responses) — use chromedp directly if needed
- Profile sync, cloud storage, backup — application concern, not library
- Macros / DSL for test writers — keep API flat

## Success metrics

1. **Puppeteer parity**: common use cases (navigate, click, type, screenshot, wait) work with same ergonomics
2. **Native bot-evasion viability**: Windows native backend produces events indistinguishable from user input (OS-level syscalls)
3. **Graceful platform fallback**: macOS/Linux calls return ErrNotSupported, not panic. Tests pass everywhere.
4. **Minimal deps**: chromedp + cdproto + gobwas/ws. No CGO except stdlib syscall (winapi on Windows).
5. **Pre-1.0 stability**: API still evolving (recent: require explicit Chrome path, expose Done channel); maintain semantic versioning for beta releases.

## Current status

- **Library maturity**: pre-1.0, active development
- **Platform matrix**: Windows ✓ (all features), macOS/Linux ✓ (CDP only)
- **Active stream**: Native backend hardening — PID/HWND lookup, lifecycle correctness, focus handling
- **Release strategy**: no tags yet; importing at HEAD
- **Test coverage**: light — URL stripping, cursor math; no integration harness

## Related docs

- **API reference**: See [../README.md](../README.md) for exhaustive method table, options, input backends
- **Code structure**: [codebase-summary.md](./codebase-summary.md)
- **Architecture**: [system-architecture.md](./system-architecture.md)
- **Development**: [development-roadmap.md](./development-roadmap.md)
