# apps/web: Next.js web app (placeholder)

Not started; it comes after the Go API. Before building:

- The spec is `docs/MOBILE_SCREENS.md` (the web mirrors every mobile screen) plus `docs/WEB_APP.md` (routes, structure).
- The web app is a pure client of the Go API through `@arth/api-client` + TanStack Query. No direct DB access and no provider SDKs.
- Auth uses the Go API's own auth (`docs/AUTH.md`). The web token transport (httpOnly cookie vs bearer) is an OPEN decision in `docs/DECISIONS.md`.
- Same hard rules as the root `CLAUDE.md` (no secrets in `NEXT_PUBLIC_*`, integer money, ask before adding dependencies).
