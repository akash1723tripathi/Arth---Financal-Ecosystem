---
description: Summarise project status from docs/PROGRESS.md (done, next, known problems)
allowed-tools: Read, Bash(git log *), Bash(git status *), Bash(git branch --show-current)
---

Read `docs/PROGRESS.md` and `docs/ROADMAP.md`, then run `git branch --show-current`, `git status --short` and `git log --oneline -15`.

Reply with:

1. **Current phase** (one line, from PROGRESS.md).
2. **Done**: bullet list, newest first (max 8).
3. **Next**: the next 3–5 concrete tasks, in order.
4. **Known problems / blockers**: from PROGRESS.md, plus any uncommitted changes.
5. **Drift**: anything in `git log` that PROGRESS.md or ROADMAP.md does not reflect yet (suggest `/update-docs` if so).

Do not modify any file. Keep it under 30 lines.
