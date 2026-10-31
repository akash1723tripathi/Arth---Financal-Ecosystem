# apps/mobile: Expo app (DRAFT)

> Draft. The app still lives at the repo root (`app/`, `components/`, `hooks/`, `lib/`, …) until the Month 1 move.
> These rules apply to those folders now and to `apps/mobile/` afterwards. Carried over from the Welth `AGENTS.md`.

## Expo HAS CHANGED

Read the exact versioned docs at https://docs.expo.dev/versions/v54.0.0/ before writing any code.
(From the original Welth `AGENTS.md`. When this file moves, move `AGENTS.md` here too, or delete it once this file supersedes it.)

## Stack

Expo SDK 54 · Expo Router 6 (typed routes, React Compiler on) · React Native 0.81 (New Architecture) · `@clerk/expo` ·
TanStack Query 5 · Zustand · react-hook-form + zod · NativeWind 4 (Tailwind 3) · `react-native-gifted-charts` · date-fns.
Native modules in use: expo-camera, expo-image-picker, expo-audio, expo-file-system (new `File`/`Directory` API), expo-sharing, expo-secure-store.

## Layout

```text
app/(auth)/            sign-in, sign-up (custom Clerk flows: password, email code, MFA)
app/(root)/onboarding  first-run setup
app/(root)/(tabs)/     index (home), transactions, add-transaction, assistant, profile
components/            modals (Account, Budget, CurrencyPicker, ReceiptScanner, VoiceRecorder), rows, pills
hooks/queries, hooks/mutations   TanStack Query hooks: the ONLY place screens get server data
lib/query/             QueryClient + query keys
lib/schemas/           zod form schemas
store/userStore.ts     Zustand: UI/session state only
```

Known breakage in the copy (fix first, see `docs/MIGRATION.md` §0): `app/_layout.tsx`, `app/index.tsx`, `app/(root)/_layout.tsx`
are duplicates of other files, and `tsconfig.json` is the Deno one.

## Rules

- **Server data goes only through `@arth/api-client` + TanStack Query hooks.** Screens never call `fetch` or a client directly.
  During migration, hooks pick the Supabase or API implementation via `EXPO_PUBLIC_BACKEND`. Screens must not know which.
- **No secrets in the app.** Only `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY`, `EXPO_PUBLIC_API_BASE_URL` and `EXPO_PUBLIC_APP_ENV` (plus transitional Supabase vars).
  `EXPO_PUBLIC_GEMINI_API_KEY` must be gone before any release build. All AI calls go to `/v1/ai/*`.
- **Money:** the API sends `amount_minor` integers. Display with `formatMinor`. Parse user input with `parseMoneyInput` → integer.
  Never `parseFloat` or float arithmetic on amounts.
- **Dates:** send `occurred_on` as a local `yyyy-MM-dd` (`format(date, "yyyy-MM-dd")`), never `toISOString()` for a picked day.
- **Lists:** use `useInfiniteQuery` with `next_cursor`. Never fetch "all transactions". Aggregates come from `/v1/dashboard` and `/v1/reports/*`.
- **Mutations** invalidate `["accounts"]`, `["transactions"]`, `["dashboard"]`, `["budgets"]` as relevant. Show `ApiError.problem.code`-specific
  messages (e.g. `account_has_transactions` → confirm cascade).
- **Platform:** Android first. Test every change on an Android emulator or device. iOS uses `NativeTabs`, Android uses JS `Tabs` (`app/(root)/(tabs)/_layout.tsx`). Keep both working.
- **Styling:** NativeWind classes. Raw hex only where className can't reach (`constants/theme.ts`). Keep `tailwind.config.js` and `theme.ts` in sync.
- **Features not in MVP** (voice entry, assistant) stay behind a feature flag and are hidden in production builds until their server endpoints exist.
- **Third-party code:** screens and components are Welth originals. When you substantially rewrite one, note it in `docs/MIGRATION.md` §7.
- Ask before adding any dependency (`npx expo install` for Expo modules so the versions match SDK 54).

## Commands (planned)

| Task | Command |
| --- | --- |
| Dev server | `pnpm --filter @arth/mobile start` (today at the root: `npx expo start -c`) |
| Android | `pnpm --filter @arth/mobile android` |
| Lint / types | `pnpm --filter @arth/mobile lint` · `pnpm --filter @arth/mobile typecheck` |
| Health check | `npx expo-doctor` |
| Dev client build | `eas build -p android --profile development` |
| OTA update | `eas update --channel preview` |
