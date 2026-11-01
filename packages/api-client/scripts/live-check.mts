// Live check of the client + session core against a RUNNING local Arth API (dev only).
//   pnpm --filter @arth/api-client live-check            (API_URL defaults to http://localhost:8080)
// Registers a throwaway user, then: login → authenticated call → refresh, and proves that concurrent 401s
// trigger exactly ONE refresh (rotation with reuse detection has no grace window, DECISIONS Q-8).
// Needs Node >= 22.18 (TypeScript type stripping). No test runner, no extra dependencies.
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { createArthClient, parseMoneyInput, unwrap, isApiError } from "../src/index.ts";
import { createSession } from "../src/session.ts";

const BASE = (process.env.API_URL ?? "http://localhost:8080").replace(/\/+$/, "");
if (!/^http:\/\/(localhost|127\.0\.0\.1|10\.0\.2\.2|192\.168\.\d+\.\d+|10\.\d+\.\d+\.\d+)(:\d+)?$/.test(BASE)) {
  console.error(`Refusing to run against ${BASE}: local development URLs only.`);
  process.exit(1);
}

let refreshCalls = 0;
const countingFetch = (r: Request) => {
  if (r.url.endsWith("/v1/auth/refresh")) refreshCalls++;
  return fetch(r);
};

function memoryStorage() {
  let v: string | null = null;
  return { get: async () => v, set: async (t: string) => void (v = t), remove: async () => void (v = null), peek: () => v };
}

let clockOffset = 0;
let signedOutEvents = 0;
const storage = memoryStorage();
const session = createSession({
  baseUrl: BASE,
  storage,
  fetch: countingFetch,
  now: () => Date.now() + clockOffset,
  onSignedOut: () => void signedOutEvents++,
});
const api = createArthClient({ baseUrl: BASE, fetch: session.authedFetch });

const step = (s: string) => console.log(`ok  ${s}`);
const email = `live-check-${randomBytes(4).toString("hex")}@example.com`;
const password = `lc-${randomBytes(12).toString("hex")}`; // random, never printed

const health = await unwrap(api.GET("/healthz"));
step(`GET /healthz → ${health.status}`);

const reg = await unwrap(api.POST("/v1/auth/register", { body: { email, password, name: "Live Check", client: "MOBILE" } }));
assert.equal(reg.user.needs_onboarding, true);
step("POST /v1/auth/register → 201, needs_onboarding=true");

const login = await unwrap(api.POST("/v1/auth/login", { body: { email, password, client: "MOBILE" } }));
await session.start(login);
step("POST /v1/auth/login → 200, refresh token stored");

try {
  await unwrap(api.POST("/v1/auth/login", { body: { email, password: "wrong-password-123", client: "MOBILE" } }));
  assert.fail("wrong password must fail");
} catch (e) {
  assert.ok(isApiError(e, "invalid_credentials"));
  step("wrong password → 401 invalid_credentials (no refresh attempted)");
}

const me = await unwrap(api.GET("/v1/me"));
assert.equal(me.email, email);
step("GET /v1/me with bearer token → 200");

await unwrap(api.POST("/v1/me/onboarding", { body: { currency: "INR", timezone: "Asia/Kolkata", opening_balance_minor: 500000 } }));
const accounts = await unwrap(api.GET("/v1/accounts"));
const cats = await unwrap(api.GET("/v1/categories", { params: { query: { kind: "EXPENSE" } } }));
const parsed = parseMoneyInput("450.50", "INR");
assert.ok(parsed.ok);
const created = await unwrap(
  api.POST("/v1/transactions", {
    body: {
      account_id: accounts.data[0].id,
      category_id: cats.data[0].id,
      type: "EXPENSE",
      amount_minor: parsed.minor,
      occurred_on: new Date().toISOString().slice(0, 10),
      description: "live-check",
    },
  }),
);
assert.equal(created.transaction.amount_minor, 45050);
assert.equal(created.account.balance_minor, 500000 - 45050);
step(`"450.50" → amount_minor ${created.transaction.amount_minor}, balance ${created.account.balance_minor}`);

// Accounts: create → edit → make default → two-step delete.
const allCats = await unwrap(api.GET("/v1/categories"));
assert.equal(allCats.data.length, 21);
const hdfc = await unwrap(api.POST("/v1/accounts", { body: { name: "HDFC", type: "BANK" } }));
assert.equal(hdfc.currency, "INR");
const edited = await unwrap(api.PATCH("/v1/accounts/{id}", { params: { path: { id: hdfc.id } }, body: { name: "HDFC Savings", type: "SAVINGS" } }));
assert.equal(edited.name, "HDFC Savings");
await unwrap(api.POST("/v1/transactions", {
  body: { account_id: hdfc.id, category_id: cats.data[1].id, type: "EXPENSE", amount_minor: 100, occurred_on: new Date().toISOString().slice(0, 10) },
}));
const madeDefault = await unwrap(api.POST("/v1/accounts/{id}/make-default", { params: { path: { id: hdfc.id } } }));
assert.equal(madeDefault.is_default, true);
const list = await unwrap(api.GET("/v1/accounts"));
assert.equal(list.data[0].id, hdfc.id);
assert.equal(list.data.filter((a) => a.is_default).length, 1);
try {
  await unwrap(api.DELETE("/v1/accounts/{id}", { params: { path: { id: hdfc.id } } }));
  assert.fail("deleting the default account must fail");
} catch (e) {
  assert.ok(isApiError(e, "default_account"), String(e));
}
await unwrap(api.POST("/v1/accounts/{id}/make-default", { params: { path: { id: accounts.data[0].id } } }));
try {
  await unwrap(api.DELETE("/v1/accounts/{id}", { params: { path: { id: hdfc.id } } }));
  assert.fail("delete with transactions must ask for cascade");
} catch (e) {
  assert.ok(isApiError(e, "account_has_transactions"), String(e));
  assert.equal(e.problem?.transaction_count, 1);
}
await unwrap(api.DELETE("/v1/accounts/{id}", { params: { path: { id: hdfc.id }, query: { cascade: true } } }));
step("accounts: create 201 (INR) → PATCH 200 → make-default 200 (one default) → delete default 409 default_account → delete 409 account_has_transactions (transaction_count 1) → cascade 204; categories 21");

// Transactions: keyset pages, filters, edit, delete (balances move server-side).
const cash = accounts.data[0].id;
const today = new Date().toISOString().slice(0, 10);
for (const [amount, description] of [[1000, "coffee beans"], [2000, "Coffee shop"], [3000, "train"]] as const) {
  await unwrap(api.POST("/v1/transactions", { body: { account_id: cash, category_id: cats.data[0].id, type: "EXPENSE", amount_minor: amount, occurred_on: today, description } }));
}
const page1 = await unwrap(api.GET("/v1/transactions", { params: { query: { limit: 2 } } }));
assert.equal(page1.data.length, 2);
assert.ok(page1.next_cursor);
const page2 = await unwrap(api.GET("/v1/transactions", { params: { query: { limit: 2, cursor: page1.next_cursor! } } }));
assert.equal(page2.data.length, 2);
assert.equal(new Set([...page1.data, ...page2.data].map((t) => t.id)).size, 4);
const coffee = await unwrap(api.GET("/v1/transactions", { params: { query: { q: "coffee", type: "EXPENSE" } } }));
assert.equal(coffee.data.length, 2);
const target = coffee.data[0];
const patched = await unwrap(api.PATCH("/v1/transactions/{id}", { params: { path: { id: target.id } }, body: { amount_minor: 9999, description: "" } }));
assert.equal(patched.transaction.amount_minor, 9999);
assert.equal(patched.transaction.description, null);
const before = (await unwrap(api.GET("/v1/accounts/{id}", { params: { path: { id: cash } } }))).balance_minor;
await unwrap(api.DELETE("/v1/transactions/{id}", { params: { path: { id: target.id } } }));
const after = (await unwrap(api.GET("/v1/accounts/{id}", { params: { path: { id: cash } } }))).balance_minor;
assert.equal(after - before, 9999);
const daily = await unwrap(api.GET("/v1/reports/daily", { params: { query: { from: today, to: today } } }));
assert.equal(daily.data.length, 1);
step(`transactions: 2 keyset pages (no overlap) → q=coffee 2 rows → PATCH 9999 + clear description → DELETE restores balance (+9999) → daily report expense ${daily.data[0].expense_minor}`);

// Budgets: overall + per category, progress computed by the server.
const budgetInput = parseMoneyInput("60,000", "INR");
assert.ok(budgetInput.ok && budgetInput.minor === 6000000);
const overall = await unwrap(api.PUT("/v1/budgets/overall", { body: { amount_minor: budgetInput.minor } }));
assert.equal(overall.category_id, null);
await unwrap(api.PUT("/v1/budgets/categories/{category_id}", { params: { path: { category_id: cats.data[0].id } }, body: { amount_minor: 100000 } }));
const budgets = await unwrap(api.GET("/v1/budgets"));
assert.equal(budgets.data[0].category_id, null);
const catBudget = budgets.data.find((b) => b.category_id === cats.data[0].id)!;
assert.equal(catBudget.remaining_minor, catBudget.amount_minor - catBudget.spent_minor);
await unwrap(api.DELETE("/v1/budgets/categories/{category_id}", { params: { path: { category_id: cats.data[0].id } } }));
await unwrap(api.DELETE("/v1/budgets/categories/{category_id}", { params: { path: { category_id: cats.data[0].id } } }));
assert.equal((await unwrap(api.GET("/v1/budgets"))).data.length, 1);
step(`budgets: PUT overall 200 + category 200 → list overall first, category spent ${catBudget.spent_minor} / ${catBudget.amount_minor} (${catBudget.percent_used}%) → DELETE 204 twice (idempotent)`);

// Dashboard: the server's aggregates are consistent with the accounts and transactions.
const dash = await unwrap(api.GET("/v1/dashboard"));
const inrAccounts = (await unwrap(api.GET("/v1/accounts"))).data.filter((a) => a.currency === dash.currency);
assert.equal(dash.total_balance_minor, inrAccounts.reduce((sum, a) => sum + a.balance_minor, 0));
assert.ok(dash.recent.length <= 5);
const amounts = dash.expense_by_category.map((c) => c.amount_minor);
assert.deepEqual(amounts, [...amounts].sort((a, b) => b - a));
assert.equal(dash.month.net_minor, dash.month.income_minor - dash.month.expense_minor);
step(`dashboard: total ${dash.total_balance_minor} = Σ ${dash.currency} balances, expense ${dash.month.expense_minor}, ${dash.expense_by_category.length} categories largest first, ${dash.recent.length} recent`);

// CSV export, exactly as the app calls it (text through the authed fetch).
const csv = await api.GET("/v1/transactions/export", { params: { query: { format: "csv", from: today, to: today } }, parseAs: "text" });
assert.equal(csv.response.status, 200);
assert.match(csv.response.headers.get("Content-Disposition") ?? "", /filename="arth-transactions-.*\.csv"/);
assert.ok(typeof csv.data === "string" && csv.data.includes("450.50"));
step(`export: 200 text/csv, ${String(csv.data).trim().split("\n").length - 1} rows, contains "450.50", ${csv.response.headers.get("Content-Disposition")}`);

try {
  await unwrap(api.PATCH("/v1/me", { body: { currency: "USD" } }));
  assert.fail("currency change after transactions must fail");
} catch (e) {
  assert.ok(isApiError(e, "currency_locked"), String(e));
}
const renamed = await unwrap(api.PATCH("/v1/me", { body: { name: "Live Check Renamed" } }));
assert.equal(renamed.name, "Live Check Renamed");
step("PATCH /v1/me currency after a transaction → 409 currency_locked; name change → 200");

// Proactive refresh: pretend the access token expired; 5 parallel calls → exactly 1 refresh.
refreshCalls = 0;
clockOffset = 16 * 60 * 1000;
await Promise.all(Array.from({ length: 5 }, () => unwrap(api.GET("/v1/me"))));
assert.equal(refreshCalls, 1, `expected 1 refresh, got ${refreshCalls}`);
clockOffset = 0;
step("5 parallel calls with an expired access token → exactly 1 refresh, all 200");

// Reactive refresh: the server rejects the access token (401 unauthorized) for 6 parallel calls → exactly 1 refresh.
refreshCalls = 0;
const rt = storage.peek()!;
await session.start({ ...login, access_token: "not-a-valid-jwt", refresh_token: rt, access_expires_in: 900 });
const results = await Promise.all(Array.from({ length: 6 }, () => unwrap(api.GET("/v1/accounts"))));
assert.equal(results.length, 6);
assert.equal(refreshCalls, 1, `expected 1 refresh, got ${refreshCalls}`);
step("6 parallel calls answered 401 unauthorized → exactly 1 refresh, each retried once → all 200");

// The session survived (no reuse detected): one more explicit refresh still works.
assert.equal(await session.refresh(), true);
await unwrap(api.GET("/v1/dashboard"));
step("session still valid after the parallel burst (no reuse revocation): refresh → 200, GET /v1/dashboard → 200");

// Negative control: what the serialization prevents. Two raw refreshes with the same token at once.
const sameToken = storage.peek()!;
const raw = await Promise.all(
  [0, 1].map(() =>
    fetch(`${BASE}/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: sameToken }),
    }).then((r) => r.status),
  ),
);
assert.deepEqual([...raw].sort(), [200, 401]);
step(`negative control: 2 UNserialized refreshes with one token → statuses ${raw.join(",")} (reuse detected)`);

// …and that revoked the session: the next call 401s, the refresh is refused, the session ends locally.
signedOutEvents = 0;
try {
  await unwrap(api.GET("/v1/me"));
  assert.fail("expected the revoked session to fail");
} catch (e) {
  assert.ok(isApiError(e, "unauthorized") || isApiError(e, "refresh_invalid"), String(e));
}
assert.equal(storage.peek(), null);
assert.equal(signedOutEvents, 1);
step("revoked session → refresh refused → tokens cleared, onSignedOut fired once");

const login2 = () => unwrap(api.POST("/v1/auth/login", { body: { email, password: currentPassword, client: "MOBILE" } }));
let currentPassword = password;

// Change password: this session continues, the old password stops working.
await session.start(await login2());
const newPassword = `lc2-${randomBytes(12).toString("hex")}`;
await unwrap(api.POST("/v1/auth/password", { body: { current_password: currentPassword, new_password: newPassword } }));
currentPassword = newPassword;
await unwrap(api.GET("/v1/me"));
try {
  await unwrap(api.POST("/v1/auth/login", { body: { email, password, client: "MOBILE" } }));
  assert.fail("old password must fail");
} catch (e) {
  assert.ok(isApiError(e, "invalid_credentials"));
}
step("POST /v1/auth/password → 204; this session still works; old password → 401 invalid_credentials");

// Logout (this device): revokes the session owning the refresh token.
const loggedOut = await session.ensureFreshAccessToken();
await unwrap(api.POST("/v1/auth/logout", { body: { refresh_token: storage.peek()! } }));
const afterLogout = await fetch(`${BASE}/v1/me`, { headers: { Authorization: `Bearer ${loggedOut}` } });
assert.equal(afterLogout.status, 401);
await session.end();
step("POST /v1/auth/logout → 204; the old access token → 401");

// Logout-all: every session ends, including this one.
await session.start(await login2());
const other = await login2(); // a second "device"
await unwrap(api.POST("/v1/auth/logout-all"));
const otherAfter = await fetch(`${BASE}/v1/me`, { headers: { Authorization: `Bearer ${other.access_token}` } });
assert.equal(otherAfter.status, 401);
await session.end();
step("POST /v1/auth/logout-all → 204; another device's token → 401");

// Clean up: log in again and delete the throwaway user.
await session.start(await login2());
await unwrap(api.DELETE("/v1/me", { body: { password: currentPassword } }));
step("DELETE /v1/me → 204 (throwaway user removed)");

console.log("\nlive check passed");
