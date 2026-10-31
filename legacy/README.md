# legacy/ (reference only, will be deleted)

Welth's original backend, kept only as a reference while the Go backend replaces it. Nothing here is built, deployed or imported.

- `supabase/functions/`: Deno edge functions `check-budget-alerts` and `weekly-tips` (→ future Go worker jobs; see `docs/MIGRATION.md` §2).
  `weekly-tips/index.ts` imports `../../../constants/categories.ts`, a path that no longer resolves after the move. It is left as is (not built).
- `scripts/schema.sql`, `cron_jobs.sql`, `seed.sql`: the Supabase schema, pg_cron schedule and seed data.
- `WELTH_README.md`: the original course README (Supabase setup instructions).

Delete this folder once the Go worker and migrations exist (roadmap).
