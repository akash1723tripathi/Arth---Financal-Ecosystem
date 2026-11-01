# apps/mobile: Expo app

## Expo has changed: read the versioned docs first

Read the exact versioned docs at <https://docs.expo.dev/versions/v54.0.0/> before writing any code. Training-data knowledge of Expo is
likely out of date. (Carried over from the original Welth `AGENTS.md`.)

## Stack

Expo SDK 54 (`expo` 54.0.x) · Expo Router 6 (typed routes, React Compiler on) · React Native 0.81 (New Architecture) · React 19.1 ·
TanStack Query 5 · Zustand 5 · react-hook-form + zod 4 · NativeWind 4 (Tailwind 3) · react-native-gifted-charts · date-fns 4.
Native modules: expo-camera, expo-image-picker, expo-audio, expo-file-system (new `File`/`Directory` API), expo-sharing, expo-secure-store.

**Backend:** only the Arth Go API (`apps/api`) through `@arth/api-client` (generated from `api/openapi.yaml`). Clerk, Supabase and the
client-side Gemini key were removed in the repoint (2026-11-01). Base URL: `EXPO_PUBLIC_API_URL` (default `http://10.0.2.2:8080` on Android).

## Folder structure (real)

```text
app/_layout.tsx                  QueryClientProvider + GestureHandlerRootView; restores the session (secure store) at start
app/index.tsx                    redirect: signed in → /(root)/(tabs), else → /sign-in
app/(auth)/_layout.tsx           Stack; redirects signed-in users to /
app/(auth)/sign-in.tsx, sign-up.tsx   email + password on POST /v1/auth/login|register
app/(root)/_layout.tsx           auth gate + GET /v1/me (needs_onboarding) + 1.5 s splash + "can't reach the server" retry; a Stack
app/(root)/onboarding.tsx        currency + starting balance → POST /v1/me/onboarding
app/(root)/transaction/[id].tsx  edit a transaction (PATCH)
app/(root)/(tabs)/_layout.tsx    NativeTabs on iOS, JS Tabs on Android (Assistant tab only in __DEV__)
app/(root)/(tabs)/index.tsx      Home = GET /v1/dashboard
app/(root)/(tabs)/transactions.tsx, add-transaction.tsx, assistant.tsx (dev-only placeholder), profile.tsx
components/                      TransactionForm (add + edit), modals (Account, Budget, ChangePassword, DeleteAccount, CurrencyPicker,
                                 ReceiptScanner, VoiceRecorder [dev only], FormSheet), rows, pills, cards
hooks/queries, hooks/mutations   TanStack Query hooks over the typed client (screens get server data only through these)
lib/api/                         client.ts (typed client), session.ts (secure-store refresh token + shared session core), errors.ts
lib/money.ts                     THE money module (parse input → minor units, format minor units); re-exports @arth/api-client money
lib/ai/extraction.ts             receipt upload to POST /v1/ai/receipt-extractions; voice placeholder (no endpoint yet)
lib/config.ts  lib/query/  lib/schemas/  lib/utils/exportTransactions.ts   API URL, QueryClient + keys, zod schemas, CSV export
store/authStore.ts               Zustand: session status (loading | signedIn | signedOut)
constants/                       theme colours (categories come from GET /v1/categories)
assets/images/                   icons, splash, logos (still Welth artwork: to replace)
```

The `@/*` alias maps to `apps/mobile/*` (`tsconfig.json`). Full screen spec: `docs/MOBILE_SCREENS.md`.

## Rules

- Server data flows through TanStack Query hooks in `hooks/` (`unwrap(api.GET(...))`). Screens never call the client directly.
- **Auth:** never call `/v1/auth/refresh` yourself. All requests go through `authedFetch` (lib/api/session.ts), which shares ONE in-flight
  refresh (rotation has no grace window: two parallel refreshes revoke the session, DECISIONS Q-8) and retries once on `401 unauthorized`.
- **No secrets in the bundle.** Everything `EXPO_PUBLIC_*` is public. Never add a new provider key; AI calls move behind the Go API.
- **Money:** integer `*_minor` from the API. Parse input with `parseMoneyInput(text, currency)`, display with `formatMinor(minor, currency)`
  (`lib/money.ts`). Never `parseFloat`, never float maths on amounts; totals come from the server.
- **Dates:** a picked day is sent as local `yyyy-MM-dd` (`format(date, "yyyy-MM-dd")`), never `toISOString()`.
- **Android first:** iOS uses `NativeTabs` and Android uses JS `Tabs`; keep both working.
- **Styling:** NativeWind classes. Raw hex only where className cannot reach (`constants/theme.ts`, kept in sync with `tailwind.config.js`).
- UI and behaviour stay as they are until a session explicitly changes them. No redesigns, no new features unasked.
- Use `npx expo install <pkg>` (run inside `apps/mobile`) so versions match SDK 54, and **ask before adding any dependency**.
- Screens and components are Welth originals. Note substantial rewrites in `docs/MIGRATION.md` §7.

## Commands (run from the repo root)

| Task | Command |
| --- | --- |
| Dev server | `pnpm mobile` (= `expo start`); press `a` for the Android emulator |
| Typecheck / lint / bundle | `pnpm mobile:typecheck` · `pnpm mobile:lint` · `pnpm mobile:bundle:android` |
| All three | `pnpm verify:mobile` or `/verify-mobile` |
| Dependency check | `cd apps/mobile && npx expo install --check` |

pnpm uses `nodeLinker: hoisted`. With isolated installs, Metro could not resolve NativeWind's `react-native-css-interop/jsx-runtime`.
