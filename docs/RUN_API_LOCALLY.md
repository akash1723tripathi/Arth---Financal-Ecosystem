# Run the Go API locally (Windows PowerShell)

Steps for **Windows PowerShell 5.1+** from a fresh clone. Variable **names** only appear here. The commands below generate random
dev-only values into gitignored `.env` files; never commit them or paste them anywhere.

## 1. Prerequisites (one-time)

| Tool | Check | Install |
| --- | --- | --- |
| Go 1.26+ | `go version` | <https://go.dev/dl/> |
| Docker Desktop (running) | `docker info --format '{{.ServerVersion}}'` | <https://www.docker.com/products/docker-desktop/> |
| curl.exe (ships with Windows 10+) | `curl.exe --version` | built in |

sqlc is not installed locally; it runs from the `sqlc/sqlc:1.30.0` Docker image. goose runs inside the API binary (`migrate`).
`make` is optional (`mingw32-make` works if MSYS2 is installed); `apps\api\scripts\dev.ps1` has the same tasks.

## 2. Create the two `.env` files (random dev secrets)

```powershell
cd D:\codes\Projects\Arth-react-native-finance-platform

function New-Secret([int]$bytes) {
  $b = New-Object byte[] $bytes
  [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
  -join ($b | ForEach-Object { $_.ToString('x2') })
}
$pw  = New-Secret 24      # Postgres password (dev only)
$key = New-Secret 48      # access-token signing key, 96 hex chars (>= 32 bytes required)

# Root .env: read by docker-compose.yml
@"
POSTGRES_USER=arth
POSTGRES_PASSWORD=$pw
POSTGRES_DB=arth
POSTGRES_PORT=5432
"@ | Set-Content -Encoding ascii .env

# apps/api/.env: read by the API, the worker and the tests (names: apps/api/.env.example)
@"
APP_ENV=local
PORT=8080
LOG_LEVEL=debug
DATABASE_URL=postgres://arth:$pw@127.0.0.1:5432/arth?sslmode=disable
MIGRATIONS_DATABASE_URL=postgres://arth:$pw@127.0.0.1:5432/arth?sslmode=disable
TEST_DATABASE_URL=postgres://arth:$pw@127.0.0.1:5432/arth_test?sslmode=disable
AUTH_ACCESS_TOKEN_SIGNING_KEY=$key
AUTH_ACCESS_TOKEN_TTL=15m
AUTH_REFRESH_TOKEN_TTL=720h
AUTH_SESSION_MAX_AGE=2160h
CORS_ALLOWED_ORIGINS=http://localhost:3000
APP_PUBLIC_URL=http://localhost:3000
AI_DRIVER=fake
EMAIL_DRIVER=log
STORAGE_DRIVER=none
"@ | Set-Content -Encoding ascii apps\api\.env
Remove-Variable pw, key
git check-ignore .env apps/api/.env     # must print both paths (= ignored)
```

## 3. Start Postgres 16 and create the test database

```powershell
docker compose up -d db                  # or: pnpm db:up
docker exec arth-postgres pg_isready -U arth -d arth
docker exec arth-postgres psql -U arth -d arth -c "CREATE DATABASE arth_test"   # once; "already exists" is fine
```

## 4. Migrate and start the API

```powershell
cd apps\api
go run ./cmd/api migrate up              # applies 00001_init.sql, 00002_seed_categories.sql
go run ./cmd/api migrate status
go run ./cmd/api                         # = serve; listens on :8080, Ctrl+C for graceful shutdown
```

`./scripts/dev.ps1 <task>` wraps the same commands: `run`, `worker`, `test`, `vet`, `build`, `migrate-up`, `migrate-down`,
`migrate-status`, `generate`, `sqlc`, `docker-build`, `help`.

## 5. Try it (second PowerShell window)

These use `Invoke-RestMethod` with `ConvertTo-Json` bodies. In Windows PowerShell 5.1, piping a JSON string into `curl.exe` adds a UTF-8
byte-order mark, and the API rejects that body with `400 bad_request` (verified). `curl.exe` is fine for GET requests. Note that in Windows
PowerShell, `curl` without `.exe` is an alias for `Invoke-WebRequest`. `Invoke-RestMethod` throws on 4xx/5xx; the status is in
`$_.Exception.Response.StatusCode`.

```powershell
$B = "http://localhost:8080"
$J = @{ ContentType = "application/json" }
Invoke-RestMethod "$B/healthz"                      # status: ok
Invoke-RestMethod "$B/readyz"                       # status: ready

# Register (201, returns a TokenPair and creates the default "Cash" account)
$reg = Invoke-RestMethod -Method Post "$B/v1/auth/register" @J -Body (@{
  email = "me@example.com"; password = "correct horse battery staple"; name = "Me"; client = "MOBILE" } | ConvertTo-Json)

# Login (200). Keep the tokens in variables, not in files or chat.
$login = Invoke-RestMethod -Method Post "$B/v1/auth/login" @J -Body (@{
  email = "me@example.com"; password = "correct horse battery staple"; client = "MOBILE" } | ConvertTo-Json)
$H = @{ Authorization = "Bearer $($login.access_token)" }

# Authenticated calls
Invoke-RestMethod "$B/v1/me" -Headers $H
Invoke-RestMethod -Method Post "$B/v1/me/onboarding" -Headers $H @J -Body (@{
  currency = "INR"; timezone = "Asia/Kolkata"; opening_balance_minor = 500000 } | ConvertTo-Json)
$acct = Invoke-RestMethod -Method Post "$B/v1/accounts" -Headers $H @J -Body (@{
  name = "HDFC Savings"; type = "SAVINGS"; opening_balance_minor = 10000000 } | ConvertTo-Json)
$cat = (Invoke-RestMethod "$B/v1/categories?kind=EXPENSE" -Headers $H).data[0].id
$today = (Get-Date).ToString("yyyy-MM-dd")
$tx = Invoke-RestMethod -Method Post "$B/v1/transactions" -Headers $H @J -Body (@{
  account_id = $acct.id; category_id = $cat; type = "EXPENSE"; amount_minor = 45050; occurred_on = $today; description = "Groceries" } | ConvertTo-Json)
$tx.account.balance_minor                          # 9954950
Invoke-RestMethod "$B/v1/dashboard" -Headers $H    # total_balance_minor 10454950, month.expense_minor 45050
Invoke-RestMethod "$B/v1/transactions/export?format=csv&from=$today&to=$today" -Headers $H
curl.exe -s "$B/v1/transactions?limit=20" -H "Authorization: Bearer $($login.access_token)"

# Refresh rotates the refresh token. Presenting the old one again is "reuse": 401 and the session is revoked.
$pair = Invoke-RestMethod -Method Post "$B/v1/auth/refresh" @J -Body (@{ refresh_token = $login.refresh_token } | ConvertTo-Json)
Invoke-RestMethod -Method Post "$B/v1/auth/logout" -Headers @{ Authorization = "Bearer $($pair.access_token)" } @J -Body (@{
  refresh_token = $pair.refresh_token } | ConvertTo-Json)   # 204
```

Verified on 2026-11-01 with Windows PowerShell 5.1.26100 against a local server. Output: register `needs_onboarding=True`;
balance after the transaction `9954950`; dashboard `total=10454950 expense=45050`; the CSV line `2026-11-01,EXPENSE,450.50,INR,Food & Dining,HDFC Savings,Groceries,MANUAL`;
refresh issued a new pair; re-using the old refresh token returned `401`; no token returned `401`.

Amounts are integer minor units (`45050` = ₹450.50). Errors come back as `application/problem+json` with a stable `code`.
The full contract is `api/openapi.yaml` (overview: `docs/API.md`).

## 6. Tests, vet, codegen

```powershell
cd apps\api
go vet ./...
go test ./...                            # integration tests use TEST_DATABASE_URL (arth_test); Postgres must be running
go generate ./internal/httpapi/gen/      # after editing api/openapi.yaml
docker run --rm -v "${PWD}:/src" -w /src sqlc/sqlc:1.30.0 generate   # after editing db/queries/*.sql
```

## 7. Docker image (optional)

```powershell
cd apps\api
docker build -t arth-api:dev .           # multi-stage, distroless, ~30 MB
docker run --rm --network arth-react-native-finance-platform_default --env-file .env `
  -e DATABASE_URL=... -p 8080:8080 arth-api:dev   # inside Docker the DB host is "db", not 127.0.0.1
```

## 8. Troubleshooting

| Symptom | Fix |
| --- | --- |
| `failed to connect to the docker API` | Start Docker Desktop and wait until it says "Engine running" |
| `POSTGRES_PASSWORD ... Set POSTGRES_PASSWORD in .env` | Root `.env` missing: step 2 |
| `/readyz` → 503 `migrations_pending` | `go run ./cmd/api migrate up` |
| `AUTH_ACCESS_TOKEN_SIGNING_KEY must be at least 32 bytes` | Regenerate `apps/api/.env` (step 2) |
| Tests are skipped with "TEST_DATABASE_URL not set" | `TEST_DATABASE_URL` missing from `apps/api/.env` |
| `password authentication failed` after regenerating `.env` | The volume keeps the old password: `docker compose down -v` (**deletes local data**), then step 3 |
| Port 5432 already in use | Set `POSTGRES_PORT=5433` in `.env` and use that port in the URLs |
