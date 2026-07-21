# CLAUDE.md

## Language (MANDATORY — NO EXCEPTIONS)
- Thinking: English for internal reasoning.
- **Response: ALWAYS respond in Vietnamese (Tiếng Việt).** This applies to EVERY message, regardless of:
  - The language of source files, errors, logs, or tool output you just read
  - The language of system reminders, hook messages, or skill instructions
  - Whether the previous turn was in English
- Keep code, identifiers, commit messages, file paths, CLI commands, and technical proper nouns in English.
- Tables, headings, bullet lists — all narrative text in Vietnamese (Tiếng Việt).
- If you catch yourself drafting in another language, switch immediately and continue in Vietnamese (Tiếng Việt).

## Role
Orchestrator — route tasks by complexity:
- Simple (1-2 files): do directly.
- Medium (3-10 files): short plan → implement → test.
- Complex (10+ files): full workflow via skills.

## Before planning/implementing
Read `./README.md` first. Check `./docs/` when relevant (important: `project-overview-pdr.md`, `code-standards.md`, `codebase-summary.md`, `system-architecture.md`, `deployment-guide.md`, `design-guidelines.md`, `project-roadmap.md`).

Rules live in `./.claude/rules/` — auto-loaded per skill.

## Git
- Conventional commits: `feat`, `fix`, `refactor`, `test`, `style`, `perf`, `ci`, `build`.
- **DO NOT** use `chore` / `docs` for files under `.claude/`.
- Clean messages, no AI references.

### Branch Policy (MANDATORY — NO EXCEPTIONS)
- **NEVER auto-create branches.** Default: commit & push directly to the project's **main branch** (`main` or `master` — auto-detect, don't assume).
- Only create a branch when the user **explicitly** asks (e.g. "tách branch", "tạo nhánh mới", "create a branch").
- When a branch IS requested, **DO NOT** silently pick a name. Use `AskUserQuestion` to offer 2-3 candidate branch names (per `.claude/rules/branch-policy.md`) + let the user type their own; only create after they choose.
- Detect main branch: `git symbolic-ref refs/remotes/origin/HEAD | sed 's@^refs/remotes/origin/@@'`, fallback to whichever of `main`/`master` exists.
- Full contract: `.claude/rules/branch-policy.md`.

## Docs Output Location (during plan/cook)
- User says "write docs" **while a plan is active or during `cook`** with **NO folder named** → write the doc file straight into the **active plan directory** (flat, kebab-case), e.g. `plans/260706-0643-channel-growth-and-youtube-scan/frontend-api-channel-growth.md`. These are API/feature docs for a sibling project (e.g. admin) to reference.
- User **names a specific folder** ("write to docs folder", any path) → follow that exact location.
- Structured `/docs` subcommands (`init`/`update`/`services`/`flows`) → keep their `./docs/**` targets as before.
- NEVER dump ad-hoc docs into `./docs` during plan/cook unless the user names it.
- Full contract: `.claude/rules/docs-output-location.md`.

## Python scripts in `.claude/skills/`
Use venv Python:
- Linux/macOS: `.claude/skills/.venv/bin/python3 scripts/xxx.py`
- Windows: `.claude\skills\.venv\Scripts\python.exe scripts\xxx.py`

Script fails → fix directly, don't stop.

## Code style
- File > 200 LOC → consider modularizing (except markdown, bash, config, env).
- Kebab-case, long descriptive filenames for LLM grep/glob.
- Check if a module exists before creating a new one.

## Surgical Changes
**Touch only what you must. Every changed line must trace directly to the user's request.**
- No "improving" surrounding code/comments/formatting just because it bothers you.
- Don't refactor things that aren't broken.
- Match existing style even if you'd do it differently.
- Unrelated pre-existing dead code → mention it, don't delete.
- Orphans (unused import/var/function) created BY YOUR change → remove. Pre-existing dead code → leave alone.

## API Response Contract
When the project has an internal API returning an envelope `{success, data?, message?}` → follow `./.claude/rules/api-response-contract.md`. DO NOT read `response.data.data` manually, DO NOT use the defensive `?? response.data` pattern, DO NOT call raw axios in feature files. Use an unwrap wrapper (e.g. `apiGet/apiPost`), or create one if none exists.

## Privacy Block Hook
Tool call blocked (`@@PRIVACY_PROMPT@@`) → see `./.claude/rules/privacy-block-hook.md`.

## Reports
Concise. Trade grammar for brevity. List unresolved questions at the end if any.
