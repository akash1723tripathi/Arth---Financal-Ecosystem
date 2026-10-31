# apps/mobile: Expo app

## Expo has changed: read the versioned docs first

Read the exact versioned docs at <https://docs.expo.dev/versions/v54.0.0/> before writing any code. Training-data knowledge of Expo is
likely out of date. (Carried over from the original Welth `AGENTS.md`.)

## Stack

Expo SDK 54 (`expo` 54.0.x) · Expo Router 6 (typed routes, React Compiler on) · React Native 0.81 (New Architecture) · React 19.1 ·
TanStack Query 5 · Zustand 5 · react-hook-form + zod 4 · NativeWind 4 (Tailwind 3) · react-native-gifted-charts · date-fns 4.
Native modules: expo-camera, expo-image-picker, expo-audio, expo-file-system (new `File`/`Directory` API), expo-sharing, expo-secure-store.

**To be replaced (legacy Welth backend, do not remove yet):** `@clerk/expo` (auth), `@supabase/supabase-js` (data), and direct Gemini
`fetch` calls with `EXPO_PUBLIC_GEMINI_API_KEY`. They are swapped for the Go API in the "repoint mobile" session.
The full call inventory is in `docs/MIGRATION.md`.

## Folder structure (real)

```text
app/_layout.tsx                  ClerkProvider + QueryClientProvider + GestureHandlerRootView, imports global.css
app/index.tsx                    redirect: signed in → /(root)/(tabs), else → /sign-in
app/(auth)/_layout.tsx           Stack; redirects signed-in users to /
app/(auth)/sign-in.tsx, sign-up.tsx   custom Clerk flows (password, email code, MFA/client trust)
app/(root)/_layout.tsx           auth gate + useUserSync + onboarding redirect + 1.5 s splash spinner
app/(root)/onboarding.tsx        currency + starting balance
app/(root)/(tabs)/_layout.tsx    NativeTabs on iOS, JS Tabs on Android
app/(root)/(tabs)/index.tsx      Home / dashboard
app/(root)/(tabs)/transactions.tsx, add-transaction.tsx, assistant.tsx, profile.tsx
components/                      modals (Account, Budget, CurrencyPicker, ReceiptScanner, VoiceRecorder, FormSheet), rows, pills, cards
hooks/queries, hooks/mutations   TanStack Query hooks (screens get server data only through these)
hooks/useSupabase.ts, useUserSync.ts   legacy Clerk→Supabase glue (to be replaced)
lib/services/                    data + AI calls (Supabase, Gemini: to be replaced)
lib/query/  lib/schemas/  lib/utils*   QueryClient + keys, zod form schemas, formatting, CSV export
store/userStore.ts               Zustand: currency + needsOnboarding
types/  constants/               domain types, categories (21 keys), theme colours
assets/images/                   icons, splash, logos (still Welth artwork: to replace)
```

The `@/*` alias maps to `apps/mobile/*` (`tsconfig.json`). Full screen spec: `docs/MOBILE_SCREENS.md`.

## Rules

- Server data flows through TanStack Query hooks in `hooks/`. Screens should not call clients directly. Today `onboarding.tsx`, `profile.tsx`
  and `useUserSync.ts` still do; that is legacy and gets fixed in the repoint session.
- **No secrets in the bundle.** Everything `EXPO_PUBLIC_*` is public. Never add a new provider key; AI calls move behind the Go API.
- **Money:** do not add new float maths. The legacy code uses `parseFloat`; the repoint session switches to integer `amount_minor` from the API.
- **Dates:** a picked day should be sent as local `yyyy-MM-dd`, not `toISOString()` (legacy code does the latter).
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
