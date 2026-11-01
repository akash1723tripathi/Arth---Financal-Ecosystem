# @arth/api-client

TypeScript access to the Arth API, shared by `apps/mobile` (now) and `apps/web` (next).

| File | What |
| --- | --- |
| `src/schema.ts` | **Generated** from `api/openapi.yaml` by openapi-typescript. Never edit; run `pnpm gen:api` from the repo root after a spec change |
| `src/index.ts` | `createArthClient` (openapi-fetch), type aliases (`Me`, `Account`, `Transaction`, …), `ApiError` + `unwrap` for the problem+json format |
| `src/money.ts` | `parseMoneyInput` (string → integer minor units, no floats), `formatMinor`, `minorToInput`, the 33 supported currencies |
| `src/session.ts` | Platform-neutral session core: pluggable refresh-token storage, ONE shared in-flight refresh, retry once on `401 unauthorized` |

The package ships TypeScript source (`main: src/index.ts`); Metro and Next.js compile it. Imports inside it use explicit `.ts`
extensions so Node can run the check scripts with built-in type stripping (Node ≥ 22.18).

```powershell
pnpm gen:api                                   # regenerate src/schema.ts
pnpm check:money                               # plain Node checks for money.ts
pnpm --filter @arth/api-client live-check      # client + session core against a running local API (dev only)
pnpm --filter @arth/api-client typecheck
```
