---
description: Sync docs/ROADMAP.md checkboxes and docs/PROGRESS.md with what git log shows was actually done
allowed-tools: Read, Edit, Bash(git log *), Bash(git diff *), Bash(git show *), Bash(git status *)
---

1. Read `docs/PROGRESS.md` (note the last commit it mentions) and `docs/ROADMAP.md`.
2. Run `git log --oneline --no-merges -40` and, for commits newer than the last one PROGRESS.md mentions, `git show --stat <sha>`.
3. Update the docs to match the evidence:
   - Tick ROADMAP.md boxes **only** for work a commit clearly completed. Never tick on intent.
   - In PROGRESS.md, update "Done", "Next", "Known problems", and the "Last updated" line (date + latest commit sha).
   - If the phase changed, update the Status section in the root `CLAUDE.md` (keep it short).
4. Do not change decisions (`docs/DECISIONS.md`) or invent work. If something is ambiguous, list it under "Needs confirmation" in your reply.
5. Show `git diff --stat` for the doc changes. Do not commit unless I ask; when I do, use `docs: sync roadmap and progress`.
