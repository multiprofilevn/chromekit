# chromekit — Development Roadmap

## Current status (pre-1.0)

**Library maturity**: Alpha. API still evolving. No semantic version tags yet — users import at HEAD.

**Active development**: Native backend hardening — lifecycle correctness, PID/HWND lookup, focus/navigate ops.

**Test coverage**: Light. Unit tests focus on URL parsing + cursor math. No integrated end-to-end harness or CI pipeline.

**Release strategy**: TBD. Once 1.0 ships, will adopt semantic versioning + changelog discipline.

## Recent milestone (last 7 commits, as of 2026-05-17)

| Commit | Change | Impact |
|--------|--------|--------|
| `656d3d3` | feat: WithModifyCmd option | Hooks *exec.Cmd before Chrome starts — inject raw argv for GPM-Login forks. |
| `7ead567` | feat(keyboard): add ClearInput + CDP KeyCombo | Wipe focused input via Ctrl+A + Delete; optional selector support. |
| `45b1377` | fix(launch): capture chrome PID from chromedp allocator | PID from chromedp's process handle → enables reliable HWND lookup (no title heuristic). |
| `bc1becb` | feat(browser): expose InputBackend() getter | Reports launched input backend (CDP or native); for orchestrators branching behavior. |
| `ca551df` | feat(keyboard): add TypeHuman + TypeSelector | Slow jittered typing with configurable mistake rate; selector-based form filling with retry. |
| `270ca69` | refactor(launch): drop chromedp default allocator options | Breaking: callers now own full flag list. Per-engine profiles (gologin/gpmlogin/stock) need different argv. |
| `6d96d37` | feat(page): add WaitForSelectorGone + SetSelectValue | Wait for element detach/hide; set <select>.value + fire change event for frameworks. |

## Planned work (in-scope)

### Phase 1: Native backend stabilization (ACTIVE)

**Goal**: Native input works reliably on Windows without false-positive bot detection.

- [x] PID-based HWND lookup (commit c5767f2)
- [x] Navigate via omnibox (commit 9ce34d6)
- [x] Focus handling + lifecycle (commits 986891e, 9f894ab)
- [ ] Keyboard modifiers (Shift, Ctrl, Alt) — cursor_seeder test exists, impl needs hardening
- [ ] Drag-and-drop edge cases (multi-step sequences, target bounds)
- [ ] Screenshot timing (capture during native scroll without race)
- **Owner**: @tuwibu
- **Est. completion**: Q2 2026

### Phase 2: Ergonomics polish

**Goal**: API surface matches Puppeteer parity for common cases.

- [ ] Page.goto(url) alias for Navigate (Puppeteer compat)
- [ ] Timeout as optional param (vs config-only)
- [ ] WaitForSelector optional visibility check (is element scrolled into view?)
- [ ] TypeFast option (type without per-char delay, for CI)
- **Rationale**: users familiar with Puppeteer should feel at home
- **Est. completion**: Q2 2026

### Phase 3: Test harness

**Goal**: End-to-end tests against a real Chrome instance.

- [ ] Docker compose: Chrome + test runner
- [ ] GitHub Actions CI workflow (Windows + macOS + Linux)
- [ ] Test suite:
  - Basic navigation + screenshot
  - Input backends (CDP vs native) cross-check
  - Platform matrix: Windows (all), macOS (CDP), Linux (CDP)
  - Timeout handling
  - Page lifecycle (new/close/switch)
- **Rationale**: catch regressions, document platform coverage
- **Blocker**: Docker on Windows needs WSL2; may split into local fixture + headless CI
- **Est. completion**: Q3 2026

### Phase 4: 1.0 release

**Gate**: all phases above complete, no known crashes, API stable for 4+ weeks.

- [ ] Lock API (no breaking changes)
- [ ] First semantic tag (v1.0.0)
- [ ] CHANGELOG entry (from project-changelog.md)
- [ ] Release notes on GitHub
- **Est. completion**: Q3 2026

## Out of scope (per project-overview-pdr.md)

- **Stealth plugins**: navigator.webdriver hiding, User-Agent rotation → application concern
- **Network interception**: mock/block responses → use chromedp directly
- **Profile sync / cloud**: → application concern
- **macOS / Linux native input**: requires Quartz / X11 bindings, too platform-specific. CDP suffices.
- **Headless + native combo**: native needs visible window; hard constraint.

## Known gaps

### Keyboard modifier sequences

Test exists (`input/modifier_test.go`), but implementation may be incomplete:
- Shift+A → capital A (shift-down, type 'a', shift-up)
- Ctrl+C → copy (should check actual clipboard, hard on all platforms)
- Alt+Tab → window switch (intentionally blocked for safety)

### Drag-and-drop resilience

Native backend drag-to-drop currently linear path. Needs:
- Multiple checkpoints (if destination is off-screen, scroll + retry)
- Duration-based speed (slower = more realistic)

### Screenshot during native scroll

Scroll backend dispatches OS scroll events while page is rendering. Potential race:
- Native backend scrolls ✓
- Page hasn't painted yet
- Screenshot captures old position

### Platform coverage

- Windows: full, tested
- macOS: CDP only (no native), untested (no CI)
- Linux: CDP only (no native), untested (no CI)

## Dependency updates

| Dependency | Current | Notes |
|-----------|---------|-------|
| chromedp | v0.15.1 | critical for CDP. New Chrome versions auto-support. |
| Go | 1.26.2 | modern syntax (generics, clear). No lower bound set yet. |
| syscall / golang.org/x/sys | stdlib | Windows bindings. Always updated via stdlib release cycle. |

No external logging, no HTTP, no reflection — minimal surface area.

## Success metrics (from project-overview-pdr.md)

1. **Puppeteer parity**: common cases (navigate, click, type, screenshot) work with same ergonomics → Phase 2
2. **Native bot-evasion viability**: Windows native backend produces OS-level events → Phase 1 complete + Phase 3 validation
3. **Graceful platform fallback**: unsupported ops return ErrNotSupported, tests pass everywhere → Phase 3
4. **Minimal deps**: only chromedp + stdlib → already achieved
5. **Pre-1.0 stability**: semantic versioning for beta → Phase 4

## Next immediate step

**Owner**: @tuwibu
**Task**: Stabilize keyboard modifier sequences (Shift, Ctrl, Alt) + add tests covering shift+letter → capital.
**Acceptance**: input/modifier_test.go passes; native backend can type "Hello" (capital H).
**Est. time**: 2-3 days

## Release checklist (when 1.0 ready)

- [ ] All phases complete
- [ ] No open bugs in GitHub issues
- [ ] README updated with "Stable API" note
- [ ] CHANGELOG covers all features since initial commit
- [ ] Tag: git tag -a v1.0.0 -m "Release v1.0.0"
- [ ] Push tag: git push origin v1.0.0
- [ ] GitHub release created from tag
- [ ] go.pkg.dev reflects new version
