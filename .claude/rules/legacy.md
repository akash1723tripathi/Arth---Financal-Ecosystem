---
paths:
  - "legacy/**"
---

# legacy/ is reference only

- Do not edit, build, deploy or import anything under `legacy/`. It is Welth's Supabase backend, kept only so the Go
  worker and schema can be ported faithfully (see `docs/MIGRATION.md` §2).
- Read it to understand behaviour. Port the behaviour into `apps/api`; never copy code or prompts verbatim (licence: `docs/MIGRATION.md` §7).
- The whole folder is deleted once the Go worker and migrations exist.
