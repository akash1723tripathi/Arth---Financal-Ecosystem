// Plain Node checks for src/money.ts (no test runner). Run from the repo root: `pnpm check:money`.
// Needs Node >= 22.18 (built-in TypeScript type stripping).
import assert from "node:assert/strict";
import {
  formatMinor,
  isMoneyInputSyntax,
  minorToInput,
  parseMoneyInput,
  SUPPORTED_CURRENCIES,
} from "../src/money.ts";

let passed = 0;
function check(name: string, fn: () => void) {
  fn();
  passed++;
  console.log(`ok  ${name}`);
}

const ok = (minor: number) => ({ ok: true, minor });
const err = (error: string) => ({ ok: false, error });

check("parse: 450.50 INR → 45050 (the canonical example)", () => {
  assert.deepEqual(parseMoneyInput("450.50", "INR"), ok(45050));
  assert.deepEqual(parseMoneyInput("450.5", "INR"), ok(45050));
  assert.deepEqual(parseMoneyInput("450", "INR"), ok(45000));
});

check("parse: grouping commas and spaces are ignored", () => {
  assert.deepEqual(parseMoneyInput("1,23,456.78", "INR"), ok(12345678));
  assert.deepEqual(parseMoneyInput(" 1 000 ", "USD"), ok(100000));
});

check("parse: leading/trailing dot", () => {
  assert.deepEqual(parseMoneyInput(".75", "INR"), ok(75));
  assert.deepEqual(parseMoneyInput("12.", "INR"), ok(1200));
});

check("parse: classic float traps stay exact", () => {
  assert.deepEqual(parseMoneyInput("0.1", "USD"), ok(10));
  assert.deepEqual(parseMoneyInput("0.29", "USD"), ok(29)); // 0.29 * 100 = 28.999999999999996 in floats
  assert.deepEqual(parseMoneyInput("1.005", "KWD"), ok(1005));
  assert.deepEqual(parseMoneyInput("19.99", "EUR"), ok(1999));
});

check("parse: exponent per currency (JPY 0, KWD 3)", () => {
  assert.deepEqual(parseMoneyInput("1500", "JPY"), ok(1500));
  assert.deepEqual(parseMoneyInput("1500.0", "JPY"), ok(1500));
  assert.deepEqual(parseMoneyInput("1500.5", "JPY"), err("precision"));
  assert.deepEqual(parseMoneyInput("1.234", "KWD"), ok(1234));
});

check("parse: excess precision is rejected, zeros are not", () => {
  assert.deepEqual(parseMoneyInput("10.505", "INR"), err("precision"));
  assert.deepEqual(parseMoneyInput("10.500", "INR"), ok(1050));
});

check("parse: empty, junk, zero, negative", () => {
  assert.deepEqual(parseMoneyInput("", "INR"), err("empty"));
  assert.deepEqual(parseMoneyInput("   ", "INR"), err("empty"));
  assert.deepEqual(parseMoneyInput("abc", "INR"), err("syntax"));
  assert.deepEqual(parseMoneyInput("1e3", "INR"), err("syntax"));
  assert.deepEqual(parseMoneyInput("1.2.3", "INR"), err("syntax"));
  assert.deepEqual(parseMoneyInput("+5", "INR"), err("syntax"));
  assert.deepEqual(parseMoneyInput(".", "INR"), err("syntax"));
  assert.deepEqual(parseMoneyInput("0", "INR"), err("not_positive"));
  assert.deepEqual(parseMoneyInput("0.00", "INR"), err("not_positive"));
  assert.deepEqual(parseMoneyInput("0", "INR", { allowZero: true }), ok(0));
  assert.deepEqual(parseMoneyInput("-5", "INR"), err("negative"));
  assert.deepEqual(parseMoneyInput("-5.25", "INR", { allowNegative: true }), ok(-525));
});

check("parse: range limit 999 999 999 999 999 minor", () => {
  assert.deepEqual(parseMoneyInput("9999999999999.99", "INR"), ok(999999999999999));
  assert.deepEqual(parseMoneyInput("10000000000000", "INR"), err("range"));
  assert.deepEqual(parseMoneyInput("99999999999999999999", "INR"), err("range"));
});

check("format: INR uses Indian grouping and 2 decimals", () => {
  assert.equal(formatMinor(45050, "INR"), "₹450.50");
  assert.equal(formatMinor(12345678, "INR"), "₹1,23,456.78");
  assert.equal(formatMinor(10454950, "INR"), "₹1,04,549.50");
  assert.equal(formatMinor(5, "INR"), "₹0.05");
  assert.equal(formatMinor(0, "INR"), "₹0.00");
});

check("format: other currencies, exponents and symbols", () => {
  assert.equal(formatMinor(123456789, "USD"), "$1,234,567.89");
  assert.equal(formatMinor(1500, "JPY"), "¥1,500");
  assert.equal(formatMinor(1234, "KWD"), "KWD 1.234");
  assert.equal(formatMinor(99, "EUR"), "€0.99");
});

check("format: signs", () => {
  assert.equal(formatMinor(-45050, "INR"), "-₹450.50");
  assert.equal(formatMinor(45050, "INR", { signDisplay: "always" }), "+₹450.50");
  assert.equal(formatMinor(-45050, "INR", { signDisplay: "never" }), "₹450.50");
  assert.equal(formatMinor(0, "INR", { signDisplay: "always" }), "₹0.00");
});

check("format: trimZeroFraction", () => {
  assert.equal(formatMinor(5000000, "INR", { trimZeroFraction: true }), "₹50,000");
  assert.equal(formatMinor(5000050, "INR", { trimZeroFraction: true }), "₹50,000.50");
});

check("format: rejects non-integers", () => {
  assert.throws(() => formatMinor(1.5, "INR"), RangeError);
});

check("minorToInput round-trips through parseMoneyInput", () => {
  assert.equal(minorToInput(45050, "INR"), "450.50");
  assert.equal(minorToInput(5000000, "INR"), "50000");
  assert.equal(minorToInput(-525, "INR"), "-5.25");
  assert.equal(minorToInput(1500, "JPY"), "1500");
  for (const c of SUPPORTED_CURRENCIES) {
    for (const v of [1, 7, 99, 100, 45050, 123456789, 999999999999999]) {
      assert.deepEqual(parseMoneyInput(minorToInput(v, c.code), c.code), ok(v), `${c.code} ${v}`);
    }
  }
});

check("isMoneyInputSyntax", () => {
  assert.equal(isMoneyInputSyntax("1,234.5"), true);
  assert.equal(isMoneyInputSyntax("abc"), false);
});

check("supported list matches the Go allowlist size (33)", () => {
  assert.equal(SUPPORTED_CURRENCIES.length, 33);
});

console.log(`\n${passed} money checks passed`);
