// Seed a demo user into a RUNNING local Arth API so the dashboard and charts look populated (dev only).
//
//   pnpm dev:seed                         (API at http://localhost:8080)
//   $env:API_URL="http://10.0.2.2:8080"; pnpm dev:seed
//   $env:DEMO_EMAIL="me@example.com"; $env:DEMO_PASSWORD="..."; pnpm dev:seed
//
// Registers a demo user (credentials printed below; the password is random unless DEMO_PASSWORD is set),
// completes onboarding (INR, Asia/Kolkata), creates 2 extra accounts, ~60 transactions over the last
// 3 months across several categories, and an overall + per-category budget.
// Plain Node >= 20 (built-in fetch). No dependencies. All amounts are integer minor units (paise).
import { randomBytes } from "node:crypto";

const API = (process.env.API_URL ?? "http://localhost:8080").replace(/\/+$/, "");
const LOCAL = /^http:\/\/(localhost|127\.0\.0\.1|10\.0\.2\.2|10\.\d+\.\d+\.\d+|192\.168\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+\.\d+)(:\d+)?$/;
if (!LOCAL.test(API)) {
  console.error(`Refusing to seed ${API}: dev-seed only runs against localhost, 10.0.2.2 or a private LAN address.`);
  process.exit(1);
}

let accessToken = "";

async function call(method, path, body) {
  const res = await fetch(`${API}${path}`, {
    method,
    headers: {
      ...(body ? { "Content-Type": "application/json" } : {}),
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const err = new Error(`${method} ${path} → ${res.status} ${data?.code ?? ""} ${data?.detail ?? ""}`.trim());
    err.status = res.status;
    err.code = data?.code;
    throw err;
  }
  return data;
}

// Deterministic pseudo-random numbers (same shape of data on every run).
let seed = 20261101;
const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
const pick = (arr) => arr[Math.floor(rnd() * arr.length)];
/** Random amount in whole rupees between lo and hi, plus random paise, as integer minor units. */
const rupees = (lo, hi) => (lo + Math.floor(rnd() * (hi - lo + 1))) * 100 + Math.floor(rnd() * 100);

function ymd(d) {
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}
function daysAgo(n) {
  const d = new Date();
  d.setHours(12, 0, 0, 0);
  d.setDate(d.getDate() - n);
  return d;
}

async function main() {
  try {
    await call("GET", "/readyz");
  } catch (e) {
    console.error(`The API at ${API} is not ready (${e.message}). Start it first: pnpm dev:db, then pnpm dev:api.`);
    process.exit(1);
  }

  const password = process.env.DEMO_PASSWORD || `demo-${randomBytes(6).toString("hex")}`;
  let email = process.env.DEMO_EMAIL || "demo@example.com";

  let pair;
  try {
    pair = await call("POST", "/v1/auth/register", { email, password, name: "Demo User", client: "MOBILE" });
  } catch (e) {
    if (e.code !== "email_taken") throw e;
    if (process.env.DEMO_PASSWORD) {
      console.error(`${email} already exists. Delete it first (reset the DB: see docs/PREVIEW_LOCALLY.md) or use another DEMO_EMAIL.`);
      process.exit(1);
    }
    email = `demo+${Date.now()}@example.com`;
    console.log(`demo@example.com already exists, creating ${email} instead.`);
    pair = await call("POST", "/v1/auth/register", { email, password, name: "Demo User", client: "MOBILE" });
  }
  accessToken = pair.access_token;

  await call("POST", "/v1/me/onboarding", { currency: "INR", timezone: "Asia/Kolkata", opening_balance_minor: 2500000 });

  const cash = (await call("GET", "/v1/accounts")).data.find((a) => a.is_default);
  const bank = await call("POST", "/v1/accounts", { name: "HDFC Savings", type: "SAVINGS", opening_balance_minor: 15000000 });
  const card = await call("POST", "/v1/accounts", { name: "ICICI Credit Card", type: "CREDIT_CARD", opening_balance_minor: 0 });

  const cats = (await call("GET", "/v1/categories")).data;
  const cat = (key) => {
    const c = cats.find((x) => x.key === key);
    if (!c) throw new Error(`category ${key} missing`);
    return c.id;
  };

  const txs = [];
  const add = (daysBack, type, key, account, amount, description) =>
    txs.push({ occurred_on: ymd(daysAgo(daysBack)), type, category_id: cat(key), account_id: account.id, amount_minor: amount, description });

  // Income: salary on the 1st of each of the last 3 months (+ this month if past the 1st), some freelance.
  for (let m = 0; m < 3; m++) {
    const d = new Date();
    d.setDate(1);
    d.setMonth(d.getMonth() - m);
    const back = Math.round((daysAgo(0) - d) / 86400000);
    if (back >= 0 && back <= 92) add(back, "INCOME", "salary", bank, 8500000, "Salary");
  }
  add(40, "INCOME", "freelance", bank, 1800000, "Logo design project");
  add(12, "INCOME", "freelance", bank, 950000, "Website fixes");
  add(55, "INCOME", "investment", bank, 245050, "Mutual fund dividend");

  // Fixed monthly expenses.
  for (const back of [3, 33, 63]) {
    add(back, "EXPENSE", "rent", bank, 2200000, "Rent");
    add(back + 1, "EXPENSE", "utilities", bank, rupees(1400, 2600), "Electricity bill");
    add(back + 2, "EXPENSE", "subscriptions", card, 64900, "Netflix");
  }

  // Everyday spending spread over ~90 days.
  const everyday = [
    ["food", [180, 950], ["Swiggy order", "Zomato dinner", "Lunch with team", "Chai & snacks", "Biryani takeaway"], [card, cash]],
    ["groceries", [450, 3200], ["BigBasket", "Blinkit groceries", "Vegetables", "DMart run"], [card, bank]],
    ["transport", [60, 650], ["Uber", "Ola auto", "Metro card top-up", "Petrol"], [cash, card]],
    ["shopping", [700, 4800], ["Myntra", "Amazon order", "Shoes", "Books"], [card]],
    ["entertainment", [250, 1500], ["Movie tickets", "Concert", "Gaming"], [card]],
    ["health", [150, 2200], ["Pharmacy", "Doctor visit"], [cash, bank]],
    ["personal_care", [200, 1200], ["Haircut", "Salon"], [cash]],
  ];
  // A few inside the current month (even on the 1st), so this month's breakdown chart is never empty.
  const dayOfMonth = new Date().getDate();
  for (let i = 0; i < 8; i++) {
    const [key, [lo, hi], descriptions, accs] = everyday[i % everyday.length];
    add(Math.floor(rnd() * dayOfMonth), "EXPENSE", key, pick(accs), rupees(lo, hi), pick(descriptions));
  }
  while (txs.length < 60) {
    const [key, [lo, hi], descriptions, accs] = pick(everyday);
    add(Math.floor(rnd() * 88), "EXPENSE", key, pick(accs), rupees(lo, hi), pick(descriptions));
  }
  // A known amount to verify parsing end to end (₹450.50 → 45050).
  add(0, "EXPENSE", "groceries", cash, 45050, "Groceries (₹450.50 check)");

  for (const t of txs) await call("POST", "/v1/transactions", { ...t, input_method: "MANUAL" });

  await call("PUT", "/v1/budgets/overall", { amount_minor: 6000000 });
  for (const [key, amount] of [["food", 800000], ["groceries", 1000000], ["shopping", 600000], ["entertainment", 300000]]) {
    await call("PUT", `/v1/budgets/categories/${cat(key)}`, { amount_minor: amount });
  }

  const dash = await call("GET", "/v1/dashboard");
  const inr = (minor) => `₹${(BigInt(minor) / 100n).toLocaleString("en-IN")}.${String(Math.abs(minor) % 100).padStart(2, "0")}`;

  console.log("\nSeeded the local Arth API (dev only)");
  console.log(`  API          ${API}`);
  console.log(`  accounts     ${cash.name}, ${bank.name}, ${card.name}`);
  console.log(`  transactions ${txs.length} over the last 3 months`);
  console.log(`  budgets      overall ₹60,000 + food, groceries, shopping, entertainment`);
  console.log(`  dashboard    total ${inr(dash.total_balance_minor)} · this month income ${inr(dash.month.income_minor)} · expense ${inr(dash.month.expense_minor)}`);
  console.log("\nDemo login (dev only, do not reuse this password anywhere):");
  console.log(`  email     ${email}`);
  console.log(`  password  ${password}`);
}

main().catch((e) => {
  console.error(`Seeding failed: ${e.message}`);
  if (e.code === "rate_limited") console.error("Register is limited to 5 per hour per IP. Restart the API (pnpm dev:api) to reset the in-memory limits.");
  process.exit(1);
});
