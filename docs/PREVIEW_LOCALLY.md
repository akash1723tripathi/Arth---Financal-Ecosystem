# Preview the app locally (Windows, Android emulator, local Go API)

Goal: from a fresh clone to "I see the populated dashboard on the emulator", in **Windows PowerShell**.
The app talks only to the local Go API (`apps/api`) and local Postgres. No Clerk, Supabase or AI keys are needed.
Variable **names** only appear here; generated dev values live in gitignored `.env` files.

## 0. Prerequisites (one-time)

| Tool | Check | Notes |
| --- | --- | --- |
| Node.js ≥ 22.18 (24 tested) | `node --version` | 22.18+ also runs the TypeScript check scripts |
| pnpm 10 | `pnpm --version` | `npm install -g pnpm@10.34.9` (or `corepack enable pnpm` as admin) |
| Go 1.26+ | `go version` | API |
| Docker Desktop (running) | `docker info --format '{{.ServerVersion}}'` | Postgres 16 |
| Android Studio + an emulator | `adb devices`, `emulator -list-avds` | SDK Platform 36, Platform-Tools, Emulator; a Pixel AVD with a Google Play image (API 35/36). Details and PATH setup: `docs/RUN_LOCALLY.md` §1 |

## 1. Install and create the env files (one-time)

```powershell
cd D:\codes\Projects\Arth-react-native-finance-platform
pnpm install
```

Create the root `.env` (Postgres for docker-compose) and `apps\api\.env` (API) exactly as in `docs/RUN_API_LOCALLY.md` §2: it
generates a random Postgres password and signing key into both files. Names used: `POSTGRES_USER`, `POSTGRES_PASSWORD`,
`POSTGRES_DB`, `POSTGRES_PORT` (root) and `APP_ENV`, `PORT`, `DATABASE_URL`, `MIGRATIONS_DATABASE_URL`, `TEST_DATABASE_URL`,
`AUTH_ACCESS_TOKEN_SIGNING_KEY`, `AUTH_*_TTL`, `CORS_ALLOWED_ORIGINS`, `APP_PUBLIC_URL`, `AI_DRIVER=fake`, `EMAIL_DRIVER=log`,
`STORAGE_DRIVER=none` (api).

The mobile app needs **no** `.env` on the emulator: it defaults to `http://10.0.2.2:8080`. Only create `apps\mobile\.env` (from
`apps\mobile\.env.example`) to set `EXPO_PUBLIC_API_URL` for a physical phone (§6).

## 2. Run it (four terminals, all from the repo root)

```powershell
# Terminal 1: Postgres (returns immediately)
pnpm dev:db

# Terminal 2: the API (applies migrations, then serves on :8080; Ctrl+C stops it)
pnpm dev:api

# Terminal 3: demo data (once the API prints "api listening")
pnpm dev:seed
#   → prints the demo login, e.g.  email demo@example.com  password demo-xxxxxxxxxxxx

# Terminal 4: start the emulator, then Metro
emulator -avd <your_avd_name>      # or start it from Android Studio → Device Manager
adb devices                        # expect: emulator-5554   device
pnpm dev:mobile                    # = expo start; press  a  to open the app on the emulator
```

Pressing **`a`** makes Expo CLI install the Expo Go version that matches SDK 54 on the emulator and open the app.

On the emulator: **Sign In** with the printed email and password → the dashboard shows the total balance, this month's income and
expense, the budget bar, the expense donut and recent transactions. (Or tap **Sign Up** to create your own user; you then see
onboarding first.)

`pnpm dev` is not a single command on purpose: four separate terminals keep the API log and Metro readable.

## 3. Reset (wipe the database and reseed)

```powershell
docker compose down -v      # DELETES all local data (the Postgres volume)
pnpm dev:db
pnpm dev:api                # re-applies the migrations (restart it if it was running)
pnpm dev:seed
```

Without a reset, `pnpm dev:seed` creates `demo+<timestamp>@example.com` when `demo@example.com` already exists.
Set `$env:DEMO_EMAIL` / `$env:DEMO_PASSWORD` before `pnpm dev:seed` to choose them. In the app, the old session is gone after a
reset: sign in again.

## 4. Useful checks while it runs

```powershell
curl.exe -s http://localhost:8080/readyz                       # {"status":"ready"}
docker exec -it arth-postgres psql -U arth -d arth              # SQL shell (\q to quit)
pnpm check:money                                                # money parsing/formatting checks
pnpm --filter @arth/api-client live-check                       # client + refresh logic against the running API
```

The API's rate limits are in memory: **register 5/hour per IP, login 20 per 15 min per IP**. Restarting `pnpm dev:api` resets them.

## 5. Troubleshooting (network traps first)

| Symptom | Cause / fix |
| --- | --- |
| App shows "Can't reach the server" + `DEV · API: http://10.0.2.2:8080` | The API isn't running (terminal 2), or it listens on another port. Check `curl.exe http://localhost:8080/readyz` on the PC |
| Works in the emulator browser at `http://10.0.2.2:8080/healthz`? | If **no**: the emulator can't reach the PC. Cold-boot the AVD. As an alternative to 10.0.2.2: `adb reverse tcp:8080 tcp:8080`, then set `EXPO_PUBLIC_API_URL=http://localhost:8080` in `apps\mobile\.env` and restart Metro with `-c` |
| Why not `localhost`? | Inside the emulator `localhost` is the emulator itself. `10.0.2.2` is the special alias for the PC's loopback |
| Physical phone | See §6 |
| `adb` not recognised / no device | `docs/RUN_LOCALLY.md` §1 step 5 (PATH) and §6. `adb kill-server; adb start-server` |
| Changed `.env` but the app still uses the old URL | `EXPO_PUBLIC_*` values are inlined at bundle time: stop Metro and run `pnpm --filter @arth/mobile start -- -c` (clears the cache) |
| "Cleartext HTTP traffic not permitted" | Only in a **release** native build: Android blocks plain `http://` there. Expo Go and debug dev builds allow it. Use HTTPS for anything but local dev (don't add a config plugin for this without deciding it first) |
| Sign in says "Too many attempts" | The in-memory login limit. Restart `pnpm dev:api` |
| Seed fails with `rate_limited` | Register is 5/hour per IP. Restart `pnpm dev:api` |
| `POSTGRES_PASSWORD ... Set POSTGRES_PASSWORD in .env` | Root `.env` missing (§1) |
| `password authentication failed` | The DB volume has an older password: reset (§3) |
| `/readyz` → `migrations_pending` | `pnpm dev:api` runs `migrate up` first; if you started the API another way: `go -C apps/api run ./cmd/api migrate up` |
| Red screen "Unable to resolve module react-native-css-interop/jsx-runtime" | `nodeLinker: hoisted` was removed from `pnpm-workspace.yaml`; restore it, delete `node_modules`, `pnpm install` |
| Metro can't be reached from the emulator | Allow Node through Windows Firewall (private networks); `adb reverse tcp:8081 tcp:8081` |

## 6. Physical Android phone on the same Wi-Fi

1. Find the PC's LAN IP: `ipconfig` → "IPv4 Address" of the Wi-Fi adapter (e.g. `192.168.1.23`).
2. `apps\mobile\.env`: `EXPO_PUBLIC_API_URL=http://192.168.1.23:8080`, then `pnpm dev:mobile -- -c`.
3. Windows Firewall: allow inbound TCP 8080 on **private** networks for the API (Windows asks the first time `api.exe`/`go` listens; or
   run once in an admin PowerShell: `New-NetFirewallRule -DisplayName "Arth API dev" -Direction Inbound -Protocol TCP -LocalPort 8080 -Profile Private -Action Allow`).
   Make sure the Wi-Fi network profile is "Private".
4. Open Expo Go on the phone and scan the QR code from Metro. Phone and PC must be on the same network (no client isolation).

## 7. Manual test checklist (per screen, with the database check)

Run SQL with `docker exec -it arth-postgres psql -U arth -d arth`. Replace `<email>` with the user you signed in as.
Handy: `\x` toggles expanded output.

### Auth

- [ ] Sign up with a new email (first + last name, password ≥ 8) → onboarding opens.
  DB: `select email, name, currency, onboarded_at from users where email='<email>';` → name "First Last", currency NULL.
  `select name, type, currency, is_default from accounts where user_id=(select id from users where email='<email>');` → one "Cash", INR, default.
- [ ] Sign up again with the same email (any case) → "An account with this email already exists."
- [ ] Sign up with `password` → "Choose a stronger password…" (`422 weak_password`).
- [ ] Sign in with a wrong password → "Incorrect email or password." (no sign-out, no refresh).
- [ ] Kill the app and reopen → still signed in (session restored from secure storage, no sign-in screen).
- [ ] Leave the app idle > 15 min (access token lifetime), then pull to refresh → data loads (silent refresh).
  DB: `select count(*) filter (where used_at is not null) used, count(*) total from refresh_tokens rt join sessions s on s.id=rt.session_id join users u on u.id=s.user_id where u.email='<email>';` → used grows by one per refresh, never a revoked session with `revoke_reason='REFRESH_REUSE'`.
- [ ] Profile → Change password (wrong current → "Your current password is incorrect."; right → "Password changed").
  DB: `select revoke_reason, count(*) from sessions where user_id=(select id from users where email='<email>') group by 1;` → other sessions `PASSWORD_CHANGED`, this one NULL.
- [ ] Profile → Sign out → sign-in screen. DB: newest session `revoke_reason='LOGOUT'`.
- [ ] Profile → Sign out of all devices → sign-in screen. DB: every session revoked (`LOGOUT_ALL`).
- [ ] Stop the API, reopen the app → "Can't reach the server…" with Retry (and the API URL in dev). Start the API, Retry → app loads.

### Onboarding

- [ ] Enter `50,000.50`, pick INR → "Get started" → dashboard.
  DB: `select currency, timezone from users where email='<email>';` → INR, your device zone (e.g. Asia/Kolkata).
  `select opening_balance_minor, balance_minor from accounts where is_default and user_id=(select id from users where email='<email>');` → 5000050, 5000050.
  No "Starting balance" transaction: `select count(*) from transactions where user_id=(select id from users where email='<email>');` → 0.
- [ ] Pick JPY and enter `100.5` → "JPY amounts can't have decimals."

### Home (dashboard)

- [ ] Total balance = sum of the account balances shown in Profile (server total, `GET /v1/dashboard`).
- [ ] ↗ income / ↘ expense = this month in your timezone. DB:
  `select type, sum(amount_minor) from transactions where user_id=(select id from users where email='<email>') and occurred_on >= date_trunc('month', now() at time zone 'Asia/Kolkata')::date group by type;`
- [ ] Budget card: "₹x of ₹y spent", bar colour green < 80 %, yellow ≥ 80 %, coral ≥ 100 %; per-category budgets listed under it.
- [ ] Donut + legend show this month's expense categories, largest first; recent list shows 5 rows with the right currency symbol.
- [ ] Release build only: no "Voice Entry" action and no "Ask AI" card (dev builds show them marked DEV).

### Transactions

- [ ] List scrolls and loads more pages at the end (keyset pagination, 30 per page).
- [ ] Type pills and account pills filter; search (`q`, description, server-side) filters after typing.
- [ ] Daily income vs expense chart matches the month (server `GET /v1/reports/daily`).
- [ ] Swipe left → delete → confirm. DB: the row is gone and the account balance moved back:
  `select name, balance_minor from accounts where user_id=(select id from users where email='<email>');`
- [ ] Tap a row → Edit transaction → change the amount to `99.99` → save.
  DB: `select amount_minor, updated_at from transactions where id='<id>';` → 9999.
- [ ] A USD account's transaction shows `$`, not `₹` (create a USD account via Profile is not possible in the UI; check with the seed's
  INR data that rows show `₹` with two decimals, e.g. `₹450.50`).

### Add transaction

- [ ] Amount `450.50`, Expense, Groceries, Cash, today → Save.
  DB: `select amount_minor, currency, occurred_on, input_method from transactions where user_id=(select id from users where email='<email>') order by created_at desc limit 1;`
  → **45050**, INR, today's local date (not yesterday: no `toISOString()` drift), MANUAL.
- [ ] Amount `450.505` → "INR amounts have at most 2 decimal places." Amount `0` → "Enter a valid amount."
- [ ] Pick a date in the past from the calendar → `occurred_on` is exactly that day.
- [ ] Scan receipt (camera or gallery) → form pre-filled from the fake AI driver, a "DEV · demo extraction" label in dev builds,
  then Save. DB: `input_method = 'RECEIPT_SCAN'`.

### Budget

- [ ] Home → budget card → "Overall" → `60000` → Save. DB: `select category_id, amount_minor, currency from budgets where user_id=(select id from users where email='<email>');` → NULL, 6000000, INR.
- [ ] Pick a category (e.g. Food) → `8000` → Save → it appears under the overall bar. DB: a row with that `category_id`, 800000.
- [ ] "Remove budget" → row deleted.

### Profile

- [ ] Accounts list: name, "(default)", balance in the account's currency.
- [ ] Add account "HDFC" (Bank) → appears. Edit → rename → saved. Make default → "(default)" moves. DB: exactly one `is_default`.
- [ ] Delete an account with transactions → "This will also delete N transactions…" → Delete. DB: account and its transactions gone.
- [ ] Delete the default account → "You can't delete your default account…".
- [ ] Currency → pick USD after adding transactions → "You can't change your currency after adding transactions." (409 `currency_locked`).
- [ ] Export (Transactions tab ⬇) → share sheet with `arth-transactions-<from>-<to>.csv`; amounts are decimal strings (`450.50`).
- [ ] Privacy policy row → placeholder message.
- [ ] Delete account → enter a wrong password → "Incorrect password."; right password → signed out.
  DB: `select count(*) from users where email='<email>';` → 0 (accounts, transactions, budgets, sessions cascade).
