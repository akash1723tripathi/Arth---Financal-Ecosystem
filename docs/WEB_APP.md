# Web app (`apps/web`)

Next.js (App Router, latest stable at scaffold time). Hosting is OPEN (`docs/DECISIONS.md` D-OPEN-5). It is a **pure client of the Go API**:
no database access, no business logic, no provider SDKs. Auth is Arth's own (`docs/AUTH.md`). The token transport is OPEN (D-OPEN-6).
The screen-by-screen spec is `docs/MOBILE_SCREENS.md`.

## 1. Structure

```text
apps/web/
  app/
    layout.tsx                 AuthProvider + QueryProvider + theme
    (marketing)/page.tsx       landing (public)
    (marketing)/privacy/       public
    (marketing)/terms/         public
    (marketing)/delete-account/  public explainer + signed-in "delete my account" (Play requirement)
    unsubscribe/page.tsx       public; posts token to /v1/public/email-preferences/unsubscribe
    sign-in/page.tsx           email + password → POST /v1/auth/login
    sign-up/page.tsx           name + email + password → POST /v1/auth/register
    forgot-password/, reset-password/   [OPEN: needs the email provider]
    onboarding/page.tsx        currency + timezone + opening balance
    (app)/layout.tsx           signed-in shell: sidebar/topbar, redirects to /onboarding if needs_onboarding
    (app)/dashboard/page.tsx
    (app)/transactions/page.tsx
    (app)/transactions/new/page.tsx       ?mode=manual|scan
    (app)/transactions/[id]/page.tsx      edit
    (app)/accounts/page.tsx
    (app)/budgets/page.tsx
    (app)/settings/page.tsx
    (app)/assistant/page.tsx   [POST]
  middleware.ts                redirect signed-out users away from (app)/* and /onboarding (exact mechanism depends on D-OPEN-6)
  lib/auth.ts                  AuthProvider: session state, login/register/logout, refresh handling
  lib/api.ts                   createApiClient + useApi() hook
  lib/queries/*.ts             TanStack Query hooks (same names/keys as mobile)
  components/                  UI
  .env.example
```

## 2. Routes mirror the mobile screens

| Mobile (Expo Router) | Web route | Notes |
| --- | --- | --- |
| `app/(auth)/sign-in.tsx`, `sign-up.tsx` (today: Clerk custom flows) | `/sign-in`, `/sign-up` | Same fields and copy as mobile. No email-code step until the email provider is decided. |
| `app/(root)/onboarding.tsx` | `/onboarding` | Same fields + timezone (`Intl.DateTimeFormat().resolvedOptions().timeZone`). |
| `app/(root)/(tabs)/index.tsx` (Home) | `/dashboard` | Balance, month income/expense, budget bar, category pie, recent 5. One `GET /v1/dashboard`. |
| `app/(root)/(tabs)/transactions.tsx` | `/transactions` | Table with filters (type, account), search, daily bar chart, CSV export button, infinite or paged list. |
| `app/(root)/(tabs)/add-transaction.tsx` (+ `ReceiptScannerModal`, `VoiceRecorderModal`) | `/transactions/new` | Receipt = file input / drag-drop → `POST /v1/ai/receipt-extractions` → prefill. Voice is POST. |
| *(no screen; new)* | `/transactions/[id]` | Edit/delete. |
| `profile.tsx` → Accounts section + `AccountModal` | `/accounts` | CRUD, make-default, delete with the transaction-count confirm. |
| `BudgetModal` | `/budgets` | Overall budget for MVP; per-category later. |
| `profile.tsx` → Preferences + Sign out + `CurrencyPicker` | `/settings` | Currency, timezone, email prefs, change password, log out / log out everywhere, delete account. Biometric lock is mobile-only. |
| `assistant.tsx` | `/assistant` | POST. |

## 3. Shared types via `packages/api-client`

- `import type { Transaction, Account, Me } from "@arth/api-client"`: generated from `api/openapi.yaml`. **No hand-written API types** in either app.
- `createApiClient({ baseUrl: process.env.NEXT_PUBLIC_API_BASE_URL, getToken })` returns a typed client. Errors arrive as `ApiError` with `.problem.code`.
- `@arth/api-client/money`: `formatMinor(amount_minor, currency, locale)` and `parseMoneyInput(str, currency)`. The **same** helpers the mobile app uses.
- Query-key conventions are shared by naming (`["transactions", filters]`, `["accounts"]`, `["dashboard", month]`, `["budgets", month]`, `["me"]`).
  If the duplication hurts, move the hooks into a shared package later (that needs a decision: new package).

## 4. Auth (own, via the Go API)

- Same users and endpoints as mobile: `/v1/auth/register|login|refresh|logout|logout-all|password` (`docs/API.md`).
- Token transport is OPEN (D-OPEN-6, `docs/AUTH.md` §6): (a) httpOnly cookies from the API, (b) an in-memory access token + httpOnly refresh cookie,
  or (c) a Next.js BFF. Never `localStorage`.
- With (a) or (b), the browser calls the API directly (CORS allowlist + credentials). With (c), Next.js route handlers proxy.
- Client components fetch through TanStack Query. Server-side prefetching is optional and depends on the transport.

## 5. What to reuse conceptually from the mobile code

| From mobile | Reuse how |
| --- | --- |
| `lib/query/keys.ts`, `lib/query/client.ts` (`staleTime 30s`, `retry 1`) | Same key shapes and defaults. |
| `hooks/queries/*`, `hooks/mutations/*` | Same hook names and invalidation rules (`accounts`, `transactions`, `dashboard`, `budgets`). |
| `lib/schemas/*.ts` (zod) | Same validation rules, rewritten to work with minor units (`parseMoneyInput`). Candidate for a shared package later. |
| `constants/categories.ts` | **Do not copy.** Categories (name, icon, colour) come from `GET /v1/categories`. |
| `add-transaction.tsx` `applyExtraction` (`:103-130`) | Same review-before-save UX: prefill, flag missing amount/category, never auto-save. |
| Two-step delete in `AccountModal.tsx:73-106` | Same flow, driven by `409 account_has_transactions`. |
| Design tokens (`tailwind.config.js` brand palette) | New Arth palette, shared conceptually (Tailwind on web; NativeWind on mobile). The Welth palette is copied: replace it. |

## 6. Open choices (ask before adding)

UI kit (e.g. shadcn/ui), charts library (e.g. Recharts), form library (`react-hook-form` + zod, which mobile already uses), date utils
(`date-fns`, which mobile already uses). Reusing what mobile already uses keeps the dependency count down; the UI kit and charts library are new choices to approve.
