---
description: Typecheck, lint and bundle the Android app (proves the mobile structure still compiles)
allowed-tools: Bash(pnpm verify:mobile *), Bash(pnpm mobile:typecheck *), Bash(pnpm mobile:lint *), Bash(pnpm mobile:bundle:android *)
---

From the repo root run, in order, stopping at the first failure:

1. `pnpm mobile:typecheck` (tsc --noEmit)
2. `pnpm mobile:lint` (eslint; warnings are allowed, errors are not)
3. `pnpm mobile:bundle:android` (expo export --platform android → `apps/mobile/dist`, gitignored)

If `pnpm` is not on PATH, say so and point to `docs/RUN_LOCALLY.md` (corepack). Do not install it yourself.

Report a small table: step, exit code, key output line (e.g. "0 errors, 2 warnings", bundle size). Show the real error text for any failure.
This is a build check, not a test of the running app; never describe it as "the app works".
