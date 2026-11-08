# Mobile screens: spec (also the spec for the web app)

This describes the Welth UI the app keeps (renamed to Arth), screen by screen. **Since the repoint (2026-11-01) the data comes from
the Go API; the behaviour changes are listed in §12** (read that first; §2–§10 still describe layout and the pre-repoint calls). The Next.js session mirrors these
screens (`docs/WEB_APP.md`), and the repoint session keeps the UI and swaps the data source. Paths are relative to `apps/mobile/`.
"Legacy calls" are what each screen calls today. "Target" is the Go endpoint (`docs/API.md`). Full call inventory with line numbers:
`docs/MIGRATION.md`.

## 0. Shared conventions

**Design tokens** (`tailwind.config.js`, `constants/theme.ts`; Welth palette, to be replaced later):

| Token | Hex | Used for |
| --- | --- | --- |
| `brand-bg` | `#0B0E14` | Dark hero header, primary buttons, selected pills, user chat bubbles |
| `brand-body` | `#F5F4F0` | Light page background |
| `brand-surface` / `-border` | `#141822` / `#232838` | Cards inside the dark header |
| `brand-text-primary` / `-secondary` / `-muted` | `#F2EFE9` / `#8A8D96` / `#5C5F68` | Text |
| `brand-blue` | `#1A85FF` | Auth buttons, links |
| accent blue | `#4A9EFF` | Active tab, spinners, assistant accents |
| `brand-coral` | `#FF6B4A` | Expenses, errors, destructive actions |
| `brand-success` | `#3DDC84` | Income |
| card border | `#E8E6DF` | White cards and inputs |
| AI gradient | `#0E9C79 → #1D6FAE` | AI action cards, scanner/voice buttons |
| recording gradient | `#FF6B4A → #FF3D71` | Voice stop button |

**Money display:** `lib/utils.ts` `formatPrice(value, currency = "INR")` → `Intl.NumberFormat` currency style, **0 decimals**, `en-IN` locale for INR.
Known bug: `components/TransactionRow.tsx:72` doesn't pass the user's currency, so rows always show ₹.
Target: `formatMinor(amount_minor, currency)` with correct decimals.

**Categories** (`constants/categories.ts`): 15 expense (food 🍔, groceries 🛒, transport 🚗, shopping 🛍️, entertainment 🎬, health 💊, utilities 💡,
rent 🏠, education 📚, travel ✈️, insurance 🛡️, subscriptions 📱, emi 🏦, personal_care 💇, other 📦) and 6 income (salary 💼, freelance 💻,
business 🏢, investment 📈, gift 🎁, other_income 💰), each with a label and colour. Target: `GET /v1/categories`.

**TransactionRow** (`components/TransactionRow.tsx`), used on Home and Transactions:

- White rounded card with a 3 px left border in the category colour.
- Left: 40 px circle (category colour at ~13% opacity) with the category emoji.
- Middle: the description (or the category label if empty), one line. Below it: an input-method icon (✎ manual, camera receipt, mic voice),
  a category label chip (category colour), and a "Flagged" warning if `is_flagged` (never set today).
- Right: `+amount` in green for income, `-amount` in coral for expense.
- When `onDelete` is given (Transactions screen), swiping left reveals a coral trash button.

**FormSheetModal** (`components/FormSheetModal.tsx`): bottom sheet (slide-up `Modal`, dimmed backdrop) with a title, the content, and a "Cancel"
link at the bottom. Used by AccountModal and BudgetModal.

**PillGroup** (`components/PillGroup.tsx`): horizontal scrolling row of rounded pills (optional emoji + label). The selected pill is dark (`brand-bg`) with white text.

**Feedback patterns:** inline red error text under fields; native `Alert.alert` for confirmations and failures; `ActivityIndicator` for loading;
pull-to-refresh on list screens.

## 1. Navigation map

```text
app/_layout.tsx        Root: GestureHandlerRootView → QueryClientProvider → ClerkProvider → <Slot/>  (throws if the Clerk key is missing)
app/index.tsx  "/"     Redirect only: signed in → /(root)/(tabs); else → /sign-in (renders nothing while Clerk loads)
app/(auth)/            Stack, no header. Signed-in users are redirected to "/"
  sign-in              §2
  sign-up              §3
app/(root)/_layout.tsx Gate: Clerk loading → nothing; signed out → /sign-in; runs useUserSync; shows a centred large spinner
                       (#1A1D26 on brand-body) for at least 1.5 s and until needsOnboarding is known; needsOnboarding → /(root)/onboarding
  onboarding           §4
  (tabs)/_layout.tsx   Tab bar: Home · Transactions · Add · Assistant · Profile
    index              §5 Home (dashboard)
    transactions       §6
    add-transaction    §7  (accepts ?action=scan|voice|manual)
    assistant          §8
    profile            §9
```

Tab bar: on **Android** (and Expo Go) a JS `Tabs` bar: white background, top border `#E8E6DF`, height 70, Feather icons home/list/plus-circle/cpu/user,
active `#4A9EFF`, inactive `#5C5F68`, labels Home/Transactions/Add/Assistant/Profile. On **iOS** `NativeTabs` with SF Symbols, dark background `#0B0E14`.

Target (repoint session): Clerk state is replaced by an `AuthProvider` (tokens in expo-secure-store). `useUserSync` is replaced by `GET /v1/me`
(`needs_onboarding`). The gate logic stays the same.

## 2. Sign in: `app/(auth)/sign-in.tsx`

**Purpose:** log in with email + password.

**View A (default), top to bottom, centred column, `brand-body` background, keyboard-avoiding:**

1. Logo image (`assets/images/welth.png`, 144×64; still the Welth artwork).
2. "Welcome back" (3xl bold), "Sign in to your account" (muted).
3. Email input (placeholder "Email Address", no auto-capitalise). Errors: zod "Enter a valid email." / "Email is required." and Clerk `identifier` errors.
4. Password input (secure). Errors: zod "Password is required." and Clerk `password` errors.
5. "Sign In" full-width blue button. Shows a white spinner while the request is in flight (disabled).
6. "Don't have an account? **Sign Up**" → `/sign-up`.

**View B ("needs client trust": Clerk new-device check):** logo, "Verify your account", code input ("Enter verification code"), "Verify" button,
"I need a new code" link (resend), "Start over" link (reset).

**Behaviour:** on success, navigate to `/`. `needs_second_factor` sends a phone code, but there is no screen to enter it (dead end; known problem).

- **Legacy calls:** Clerk `signIn.password`, `signIn.finalize`, `signIn.mfa.sendEmailCode/verifyEmailCode/sendPhoneCode`.
- **Target:** `POST /v1/auth/login`. View B disappears (no new-device check in MVP). "Forgot password?" is added once the email provider is decided.

## 3. Sign up: `app/(auth)/sign-up.tsx`

**View A:**

1. Logo.
2. "Create account", "Track your money, powered by AI".
3. First name + Last name side by side (errors "First name is required." / "Last name is required.").
4. Email (zod email errors + Clerk errors).
5. Password (secure, "Password must be at least 8 characters.").
6. "Sign Up" blue button with a spinner.
7. "Already have an account? **Sign in**" → `/sign-in`.

**View B (email verification, shown when Clerk reports an unverified email):** logo, "Verify your account", "We sent a code to {email}",
code input, "Verify", "I need a new code", "Start over". On success → `/`.

- **Legacy calls:** Clerk `signUp.password`, `signUp.verifications.sendEmailCode/verifyEmailCode`, `signUp.finalize`.
- **Target:** `POST /v1/auth/register {email, password, name}` (name = first + last), which returns tokens. View B only returns once
  email verification exists (OPEN).

## 4. Onboarding: `app/(root)/onboarding.tsx`

**Purpose:** first-run setup (shown when the user has no currency).

**Layout (centred column):**

1. Logo.
2. "Let's get you set up" (3xl bold), "A couple of quick details to personalise your experience."
3. **Starting balance:** label, input row with the selected currency symbol as a prefix, numeric keyboard, placeholder "e.g. 50000".
   Errors: "Please enter a starting balance." / "Please enter a valid starting balance." (must be > 0; commas allowed).
4. **Currency:** label, a select row showing "{symbol} {code} — {name}" (default INR) with a chevron. Opens CurrencyPicker (§10.3).
5. Error text "Something went wrong. Please try again." on failure.
6. "Get started" dark button ("Saving…" while saving).

**Behaviour:** on success, set the store currency, clear needsOnboarding, replace the route with the tabs.

- **Legacy calls (4, not atomic):** update `users.currency`; select the default account; insert a "Starting balance" INCOME transaction (`other_income`);
  update the account balance.
- **Target:** `POST /v1/me/onboarding {currency, timezone, opening_balance_minor}`. The starting balance becomes the account's opening balance
  (it no longer counts as income). The timezone comes from the device.

## 5. Home (dashboard): `app/(root)/(tabs)/index.tsx`

**Purpose:** at-a-glance money overview and quick entry. This is the most important screen; mirror it exactly on the web `/dashboard`.

### 5.1 Layout, top to bottom

`SafeAreaView` (top edge, dark), then a vertical `ScrollView` with a light (`brand-body`) background and pull-to-refresh.

**A. Dark hero header** (`brand-bg`, bottom corners radius 28, padding 20/20/22):

1. **Top row:**
   - Left: light logo (`welth-light.png`, width 80).
   - Right: two right-aligned lines, a greeting (12 px, secondary: "Good morning" before 12:00, "Good afternoon" before 18:00, else
     "Good evening"; device local time) and the user's **first name** (16 px, primary; "there" if missing).
   - Then a **38 px round avatar**: the profile photo if the user has one, else a Feather "user" icon on `#1A1D26`. Tapping it opens the Profile tab.
2. **Balance block:**
   - "Total balance" (12 px, secondary).
   - **Total balance** in large type (38 px, medium, tight tracking) = sum of `balance` over all accounts, formatted in the user's currency (0 decimals).
   - Below: two inline figures. ↗ (green `#3DDC84`) **this month's income**, and ↘ (coral `#FF6B4A`) **this month's expenses**.
     "This month" = the device-local calendar month.
3. **Quick actions:** three equal cards in a row (`brand-surface`, border `brand-surface-border`, radius 16). Each has a 36 px tinted icon
   circle (colour at ~15% opacity) and an 11 px label:
   - "AI Receipt Scan": camera, `#1A85FF` → Add tab with `action=scan`
   - "Voice Entry": mic, `#FF6B4A` → Add tab with `action=voice`
   - "Add Manually": plus, `#3DDC84` → Add tab with `action=manual`

**B. Light body** (padding 20):

4. **Ask-AI card** (white, radius 18, border): small blue dot icon, "Ask AI anything about your money" (muted), and a blue → arrow. Tapping it opens the Assistant tab.
5. **Monthly budget card** (white, tappable; opens BudgetModal §10.2):
   - Header row: "Monthly budget" + a small edit (pencil) icon.
   - With a budget: "{spent this month} of {budget} spent" (12 px, secondary), then an 8 px track (`#F0EEE7`) with a filled bar.
     The bar width is `min(round(spent / budget × 100), 100)%`. Its colour is coral at ≥ 100%, yellow `#F7DC6F` at ≥ 80%, green `#3DDC84` otherwise.
   - Without a budget: "Tap to set a monthly spending budget".
6. **Expense breakdown (this month)** card, **shown only if there is at least one expense this month:**
   - Title "Expense breakdown (this month)".
   - Row: a **donut chart** (gifted-charts `PieChart`, radius 60, inner radius 38, white centre), one slice per expense category this month,
     coloured by category, sized by amount.
   - Legend on the right: the top **6** categories by amount (descending). Each line has a colour dot, the category label (11 px, secondary) and the amount (11 px, dark).
7. **Recent transactions:**
   - Header row: "Recent transactions" with "See all" on the right (→ Transactions tab).
   - **Loading** (accounts or transactions loading): centred blue spinner.
   - **Empty:** inbox icon + "No transactions yet".
   - Otherwise: the **5 most recent** transactions (by date, newest first) as TransactionRow, without swipe-delete.

**C.** BudgetModal is mounted (hidden) for step 5.

### 5.2 States and behaviour

- Pull-to-refresh refetches accounts, transactions and budget. The spinner shows while accounts or transactions are refetching.
- There is **no error state**. If a query fails, the totals show 0 and the list shows empty or loading. The web version should add an error state.
- All figures are derived client-side from the full transaction list (`useMemo`): total balance, month income, month expense, breakdown, recent 5.

### 5.3 Data

- **Hooks:** `useAccountsQuery`, `useTransactionsQuery()` (no filters = every transaction), `useBudgetQuery`, `useUserStore(currency)`, Clerk `useUser`
  (first name, avatar).
- **Legacy calls:** Supabase `accounts` select, `transactions` select (all rows), `budgets` select. Clerk user profile.
- **Target:** one `GET /v1/dashboard?month=YYYY-MM` →
  `{currency, total_balance_minor, month: {income_minor, expense_minor, net_minor}, budget: BudgetProgress|null, expense_by_category: [{category_id, amount_minor}], recent: Transaction[5]}`,
  plus `GET /v1/me` (name, avatar) and `GET /v1/categories` (labels, colours, emoji). Months follow the user's timezone. Transfers are excluded.

## 6. Transactions: `app/(root)/(tabs)/transactions.tsx`

**Header area (fixed, padding 20):**

1. Row: "Transactions" (xl, semibold). On the right, a 36 px round white **export** button (download icon; a spinner while exporting).
2. **Search** input (white rounded box, search icon, placeholder "Search transactions", clear ✕ when non-empty). Filters **client-side** by
   description or category key (case-insensitive).
3. **Type filter pills:** All · Income · Expense. Selected = dark. This filters the query.
4. **Account pills** (horizontal scroll): "All Accounts" + one pill per account. This filters the query.

**List** (`FlatList`, pull-to-refresh refetches transactions + accounts, bottom padding 100):

- **List header card** (only if the filtered list is non-empty): "Daily income vs expense" with a legend (green dot Income, coral dot Expense).
  Below it, a **horizontally scrollable bar chart** (gifted-charts `BarChart`): one green + one coral bar per day from the 1st of the current month
  to today. The date label "d MMM" sits under each income bar (7 px). Settings: bar width 6, spacing 4, height 120, width `max(days × 9, 280)`,
  3 sections, no Y-axis labels, rounded tops. It uses the type/account-filtered list (ignores search).
- **Rows:** TransactionRow with swipe-left delete → Alert "Delete transaction / Are you sure you want to delete this transaction?" Cancel / Delete (destructive).
  On failure: Alert "Error / Couldn't delete this transaction."
- **Empty:** inbox icon + "No matching transactions" (when searching) or "No transactions yet".
- **Loading:** centred spinner. **Error:** alert-circle icon, "Couldn't load transactions.", "Retry" dark pill button.

**Floating action button:** 56 px dark circle with "+" at bottom-right (bottom 90) → Add tab.

**Export:** takes the currently loaded list (type/account filters applied, search ignored) and keeps the **last 30 days**. It writes
`transactions-YYYY-MM-DD.csv` to the cache with the columns `Date (yyyy-MM-dd), Type, Category (key), Description, Amount, Input Method`, then opens the
share sheet. If no rows: Alert "Nothing to export / No transactions in the export window." On failure: Alert "Error / Couldn't export transactions."

- **Hooks:** `useTransactionsQuery({type, accountId})`, `useAccountsQuery`, `useDeleteTransaction`.
- **Legacy calls:** Supabase `transactions` select with filters; delete + balance read/write (3 calls); expo-file-system + expo-sharing.
- **Target:** `GET /v1/transactions` (keyset pages via `useInfiniteQuery`; `q` for search, now server-side), `GET /v1/reports/daily`,
  `DELETE /v1/transactions/{id}`, `GET /v1/transactions/export?format=csv&from&to`. Web: a table instead of cards, with the same filters and chart.

## 7. Add transaction: `app/(root)/(tabs)/add-transaction.tsx`

**States before the form:** accounts loading → spinner. Accounts error → alert icon + "Couldn't load your accounts." No accounts → alert icon +
"You need an account before adding a transaction."

**Form, top to bottom** (title "Add transaction", scrolls, keyboard-avoiding):

1. **AI capture cards** (two side by side, 108 px tall, radius 18, animated diagonal gradient sweep):
   - "Scan receipt / Snap a photo" (camera) → ReceiptScannerModal (§10.4)
   - "Voice log / Just say it" (mic) → VoiceRecorderModal (§10.5)
2. **Type toggle:** a segmented control with Expense | Income (selected = dark). Switching type resets the category to the first one of that type.
3. **Amount:** numeric input, placeholder "0". Errors "Enter an amount." / "Enter a valid amount." (> 0; commas allowed).
4. **Category:** PillGroup of the categories for the current type (emoji + label). Default "food".
5. **Account:** PillGroup of accounts (default = the first account; the list is ordered default-first). Error "Select an account."
6. **Date:** a row showing "d MMM yyyy" with a calendar icon. Tapping toggles an **inline calendar** (react-native-ui-datepicker, single date,
   max = today, today ringed). Picking a date closes it. Default today.
7. **Description (optional):** text input, placeholder "e.g. Swiggy order".
8. Error text "Something went wrong. Please try again."
9. "Save transaction" dark button ("Saving…").

**Overlay** while a receipt is being read: dimmed screen with a white card, spinner and "Reading receipt…".

**Route param:** `?action=scan` opens the scanner and `?action=voice` opens the voice modal on arrival. The param is then cleared. `manual` just shows the form.

**AI extraction result handling** (`applyExtraction`):

- Sets the type, the category (only if it's valid for that type), the amount, the description, and the date (only if valid and not in the future).
- If the amount or category could not be read: Alert "Review before saving / Couldn't confidently read the amount and category. Please fill it in."
- Marks `input_method` as RECEIPT_SCAN or VOICE and keeps the voice transcript. **Nothing is saved automatically.**
- Receipt failure: Alert "Error / Couldn't read that receipt. Try again or enter it manually."

**On save success:** reset the form and the input method, then go back (or replace with the Transactions tab).

- **Hooks:** `useAccountsQuery`, `useCreateTransaction`, Clerk `useUser` (user id).
- **Legacy calls:** Gemini directly for receipt/voice; Supabase insert + balance read/write.
- **Target:** `POST /v1/ai/receipt-extractions` (multipart), `POST /v1/ai/voice-extractions` (POST), `POST /v1/transactions` with `amount_minor`
  and `occurred_on`. Web: a file input/drag-drop instead of the camera. Voice is hidden until POST.

## 8. Assistant: `app/(root)/(tabs)/assistant.tsx`

**Layout:**

1. Title "Assistant".
2. **Chat list:** assistant bubbles on the left (white, bordered), user bubbles on the right (dark, white text), max 85% width.
   The first message (assistant): "Hi! Ask me anything about your spending or budgets in last 30 days."
3. While waiting: a left bubble with a small spinner.
4. **Suggested prompts** (only while there is just the welcome message), as tappable chips that send immediately:
   - "How much did I spend on food this month?"
   - "What's my biggest expense this week?"
   - "Am I over budget anywhere?"
5. **Input row** (bottom padding 90 to clear the tab bar): rounded input "Ask about your money...", submit on enter, and a 44 px dark round
   send button (arrow-up). Both are disabled (60% opacity) while sending.

**Behaviour:** each send refetches all transactions + the budget, builds a text summary (last 30 days, totals, by category, budget line, up to 40
recent transactions) and asks Gemini. Errors produce the assistant bubble "Sorry, something went wrong answering that. Try again."
Chat history lives in component state only and is not persisted.

- **Legacy calls:** Supabase reads + Gemini (client key).
- **Target (POST):** `POST /v1/assistant/messages {message, history}` (the server builds the context). Hidden in MVP release builds.

## 9. Profile: `app/(root)/(tabs)/profile.tsx`

**Layout (scroll):**

1. Title "Profile".
2. **User card** (dark, radius 16, centred):
   - 80 px round **avatar**: the photo or a user icon. A translucent bottom strip shows a camera icon. Tapping it opens the photo library
     (square crop, quality 0.7) and uploads; a spinner shows while uploading. Permission denied: Alert "Permission needed…". Failure: Alert "Couldn't upload your photo…".
   - **Full name** (2xl bold, white).
   - Mail icon + **email** (12 px, one line).
3. **Section "ACCOUNTS"** (uppercase 11 px label, a bordered rounded list):
   - Each account row: an icon in a 32 px circle (Cash = dollar-sign, Bank = home, Credit card = credit-card, Savings = shield), the name
     (+ " (default)"), the balance on the right (in the user's currency), and a chevron. Tapping it opens AccountModal in edit mode.
   - Loading: spinner row. Error: "Couldn't load your accounts."
   - Last row "+ Add account" opens AccountModal in create mode.
4. **Section "PREFERENCES":**
   - "Currency" row showing the code. Opens CurrencyPicker; on select it updates the user currency. Failure: Alert "Couldn't update your currency."
   - "Biometric lock" row with a **Switch**. UI only: not persisted, does nothing.
5. **Section "ACCOUNT":** "Sign out" (coral, log-out icon) → Alert "Sign out / Are you sure…" Cancel / Sign out → sign out → `/sign-in`.

- **Hooks:** `useAccountsQuery`, `useSetDefaultAccount`, `useUserStore`, Clerk `useUser`/`useAuth`, `useSupabase` (direct currency update).
- **Legacy calls:** Clerk `setProfileImage`, `signOut`; Supabase `users` update; account queries/mutations.
- **Target:** `GET/PATCH /v1/me`, `POST /v1/auth/logout` (+ "Log out everywhere" via `logout-all`), account endpoints. Avatar upload needs
  object storage (OPEN) and falls back to the icon. Add "Change password" and "Delete account" (`DELETE /v1/me`, required by Play), plus
  email-preference toggles (alerts, weekly tips) once the email provider exists.

## 10. Modals and components

### 10.1 AccountModal (`components/AccountModal.tsx`, FormSheetModal)

- Title "Add account" / "Edit account".
- "Name" input (placeholder "e.g. HDFC Savings"). Error "Please enter an account name."
- "Type" pills: Cash · Bank · Credit card · Savings (wrap).
- Error text. "Add account" / "Save changes" ("Saving…") dark button.
- Edit mode: "Make default" (blue; hidden for the default account) and "Delete account" (coral).
- Delete: if the account has transactions, Alert "Delete account / This will also delete N transaction(s). This can't be undone." Cancel / Delete.
  The default account can currently be deleted (target: `409 default_account`).
- Legacy: Supabase accounts insert/update/delete + a transaction count. Target: `POST/PATCH/DELETE /v1/accounts…`, `make-default`.

### 10.2 BudgetModal (`components/BudgetModal.tsx`, FormSheetModal)

- Title "Set monthly budget" / "Edit monthly budget".
- "Monthly budget" numeric input (autofocus, placeholder "e.g. 50000", prefilled when editing). Error "Enter a valid monthly budget."
- "Save budget" button.
- Legacy: Supabase budgets upsert. Target: `PUT /v1/budgets/overall {amount_minor}`.

### 10.3 CurrencyPicker (`components/CurrencyPicker.tsx`)

- Page-sheet modal: a search field "Search currency…" (autofocus) + "Cancel".
- List rows: symbol · code (bold) · name, with a blue ✓ on the selected currency. Filters by code or name.
- Source: the `currency-codes` package, minus codes without a real symbol. Target: an explicit supported-currency list (matching the Go ISO-4217 table).

### 10.4 ReceiptScannerModal (`components/ReceiptScannerModal.tsx`)

- Full-screen black modal with a live back camera (requests permission on open).
- A dashed white 3:4 frame overlay in the centre.
- Top bar: ✕ close (left), a pill with a robot icon and "Align the receipt" (centre).
- Bottom: a gallery button (left, opens the library) and a 72 px gradient **capture** button with a white ring (centre; spinner while capturing).
- Permission permanently denied: dark overlay with a camera-off icon, "Camera access is off. Enable it in Settings to scan receipts.", "Close".
- Output: base64 JPEG (camera quality 0.6; library 0.7) → the Add screen calls the extractor.

### 10.5 VoiceRecorderModal (`components/VoiceRecorderModal.tsx`)

- Bottom sheet over a dark blur, with a dark gradient panel (top radius 28) and a drag handle.
- Header: robot icon + "AI VOICE LOG" (teal, uppercase).
- States:

  | State | Title | Subtitle | Visual | Button |
  | --- | --- | --- | --- | --- |
  | idle | "Tell me about a transaction" | `"I spent 400 on groceries yesterday"` | two pulse rings | mic (AI gradient) |
  | recording | "Listening…" | timer `m:ss` | faster pulse rings, the orb breathes | stop ■ (coral→pink gradient) |
  | processing | "Understanding that…" | "Transcribing and extracting the details" | spinning gradient ring | disabled |
  | error | alert icon | "Couldn't process that. Check your microphone permission and try again." | — | "Close" |

- A "Cancel" link (disabled while processing).
- Records m4a (high quality), then sends it to the extractor (target: `POST /v1/ai/voice-extractions`, POST).

### 10.6 Other components

`AIActionCard` (animated gradient card, §7), `GradientIconButton` (round gradient icon button with an optional ring and spinner),
`CalendarPicker` (inline single-date calendar with a max date).

## 11. What the web app should mirror vs adapt

| Mobile | Web |
| --- | --- |
| Tab bar | Sidebar (desktop) / bottom bar (narrow), same five sections + Accounts and Budget as their own pages |
| Bottom-sheet modals | Dialogs |
| Swipe-to-delete | Row action menu (delete with confirm) |
| Camera scanner | File upload / drag-and-drop |
| Share sheet CSV | Browser download |
| Pull-to-refresh | Refetch on focus + a refresh button |
| Missing error states on Home | Add an error state per card |

## 12. Changes made by the repoint to the Go API (2026-11-01)

| Screen | Change |
| --- | --- |
| All | Money is integer minor units: input via `parseMoneyInput` (exact, per-currency decimals, "450.50" → 45050), display via `formatMinor` with the currency's decimals (₹450.50, ¥1,500; Indian grouping for INR). Transaction rows use each transaction's own currency (the always-₹ bug is fixed). Categories (name, emoji, colour) come from `GET /v1/categories` |
| Root gate | Own session (refresh token in secure storage); `GET /v1/me` decides onboarding. New error state "Can't reach the server…" + Retry (shows the API URL in dev builds) |
| Sign in / Sign up | Email + password only (no verify-code views, no phone-code dead end). Errors: "Incorrect email or password.", "An account with this email already exists.", weak-password text, rate-limit text. Password 8–128 characters |
| Onboarding | One call. The starting balance becomes the default account's opening balance (no "Starting balance" income row). Timezone = device zone |
| Home | Figures from `GET /v1/dashboard` (server months in the user's timezone). Budget card also lists per-category budgets. Error state with Retry. Voice quick action and Ask-AI card only in dev builds |
| Transactions | Keyset pages (30, load more at the end). Search is server-side (`q`, description only; it no longer matches category names). Chart from `GET /v1/reports/daily`. Tap a row → **Edit transaction** screen. Export downloads the server CSV (last 30 days, account filter applied, type filter and search ignored) named `arth-transactions-<from>-<to>.csv` |
| Add / Edit transaction | Shared form; edit uses `PATCH`. Amount label shows the account currency. Receipt scan uploads the photo to the server (dev label "demo extraction"); voice card only in dev builds and explains it has no endpoint yet |
| Assistant | Hidden in release builds; dev builds show the chat marked "DEV ONLY · not connected" with a placeholder reply |
| Budget sheet | Scope pills (Overall + expense categories), "Remove budget" |
| Profile | Name/email from `/v1/me`. Avatar tap → "Profile photos aren't available yet." (storage OPEN). Currency change refused after transactions (`currency_locked`). New rows: Change password, Sign out of all devices, Delete account (password), About → Privacy policy (placeholder until `EXPO_PUBLIC_PRIVACY_POLICY_URL` is set). Biometric switch unchanged (still UI only) |
| Account sheet | Deleting the default account shows "You can't delete your default account…" |
