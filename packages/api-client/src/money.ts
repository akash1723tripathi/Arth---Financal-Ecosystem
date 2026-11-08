/**
 * Money helpers shared by every Arth client (mobile now, web later).
 *
 * Rule: amounts are integer minor units (`amount_minor`) plus an ISO-4217 code, exactly as the API sends them.
 * Nothing in this file does floating-point arithmetic on an amount: user input is parsed by string handling,
 * and display strings are built from the integer's digits. Plain JS numbers are safe here because every API
 * amount is at most 999 999 999 999 999 (< 2^53), so the integer itself is always exact.
 *
 * The supported list mirrors `apps/api/internal/money` (SupportedCodes + isoExponents). Keep them in sync.
 */

export type CurrencyInfo = {
  code: string;
  name: string;
  symbol: string;
  /** ISO-4217 minor-unit exponent: 2 for INR (paise), 0 for JPY, 3 for KWD. */
  exponent: number;
};

/** Same order as SupportedCodes in apps/api/internal/money/money.go. */
export const SUPPORTED_CURRENCIES: readonly CurrencyInfo[] = [
  { code: "INR", name: "Indian Rupee", symbol: "₹", exponent: 2 },
  { code: "USD", name: "US Dollar", symbol: "$", exponent: 2 },
  { code: "EUR", name: "Euro", symbol: "€", exponent: 2 },
  { code: "GBP", name: "British Pound", symbol: "£", exponent: 2 },
  { code: "JPY", name: "Japanese Yen", symbol: "¥", exponent: 0 },
  { code: "AUD", name: "Australian Dollar", symbol: "A$", exponent: 2 },
  { code: "CAD", name: "Canadian Dollar", symbol: "CA$", exponent: 2 },
  { code: "SGD", name: "Singapore Dollar", symbol: "S$", exponent: 2 },
  { code: "AED", name: "UAE Dirham", symbol: "AED", exponent: 2 },
  { code: "CHF", name: "Swiss Franc", symbol: "CHF", exponent: 2 },
  { code: "CNY", name: "Chinese Yuan", symbol: "CN¥", exponent: 2 },
  { code: "KRW", name: "South Korean Won", symbol: "₩", exponent: 0 },
  { code: "HKD", name: "Hong Kong Dollar", symbol: "HK$", exponent: 2 },
  { code: "NZD", name: "New Zealand Dollar", symbol: "NZ$", exponent: 2 },
  { code: "SEK", name: "Swedish Krona", symbol: "SEK", exponent: 2 },
  { code: "NOK", name: "Norwegian Krone", symbol: "NOK", exponent: 2 },
  { code: "DKK", name: "Danish Krone", symbol: "DKK", exponent: 2 },
  { code: "ZAR", name: "South African Rand", symbol: "R", exponent: 2 },
  { code: "BRL", name: "Brazilian Real", symbol: "R$", exponent: 2 },
  { code: "MXN", name: "Mexican Peso", symbol: "MX$", exponent: 2 },
  { code: "THB", name: "Thai Baht", symbol: "฿", exponent: 2 },
  { code: "MYR", name: "Malaysian Ringgit", symbol: "RM", exponent: 2 },
  { code: "IDR", name: "Indonesian Rupiah", symbol: "Rp", exponent: 2 },
  { code: "PHP", name: "Philippine Peso", symbol: "₱", exponent: 2 },
  { code: "SAR", name: "Saudi Riyal", symbol: "SAR", exponent: 2 },
  { code: "QAR", name: "Qatari Riyal", symbol: "QAR", exponent: 2 },
  { code: "KWD", name: "Kuwaiti Dinar", symbol: "KWD", exponent: 3 },
  { code: "BHD", name: "Bahraini Dinar", symbol: "BHD", exponent: 3 },
  { code: "OMR", name: "Omani Rial", symbol: "OMR", exponent: 3 },
  { code: "LKR", name: "Sri Lankan Rupee", symbol: "LKR", exponent: 2 },
  { code: "NPR", name: "Nepalese Rupee", symbol: "NPR", exponent: 2 },
  { code: "BDT", name: "Bangladeshi Taka", symbol: "৳", exponent: 2 },
  { code: "PKR", name: "Pakistani Rupee", symbol: "PKR", exponent: 2 },
];

const BY_CODE: Record<string, CurrencyInfo> = {};
for (const c of SUPPORTED_CURRENCIES) BY_CODE[c.code] = c;

/** Largest amount the API accepts for a transaction or budget (and the bound for balances). */
export const MAX_AMOUNT_MINOR = 999_999_999_999_999;

export function currencyInfo(code: string): CurrencyInfo {
  return BY_CODE[code] ?? { code, name: code, symbol: code, exponent: 2 };
}

export function currencyExponent(code: string): number {
  return currencyInfo(code).exponent;
}

export function currencySymbol(code: string): string {
  return currencyInfo(code).symbol;
}

export function isSupportedCurrency(code: string): boolean {
  return code in BY_CODE;
}

// ───────────────────────────────────────────────────────────── parsing

export type MoneyParseError = "empty" | "syntax" | "precision" | "range" | "not_positive" | "negative";

export type MoneyParseResult = { ok: true; minor: number } | { ok: false; error: MoneyParseError };

export type ParseOptions = {
  /** Allow a leading "-" (opening balances of credit cards). Default false. */
  allowNegative?: boolean;
  /** Allow exactly zero. Default false (transactions and budgets must be > 0). */
  allowZero?: boolean;
};

/**
 * Parse what a user typed ("1,23,456.50", "450.5", ".75", "12.") into integer minor units of `currency`.
 * Commas and spaces are treated as grouping and ignored. Extra fractional digits are accepted only if they
 * are zeros ("10.500" INR); anything that would need rounding is rejected as `precision`.
 */
export function parseMoneyInput(input: string, currency: string, opts: ParseOptions = {}): MoneyParseResult {
  const exp = currencyExponent(currency);
  let s = input.replace(/[\s,  ]/g, "");
  if (s === "") return { ok: false, error: "empty" };
  if (s.length > 40) return { ok: false, error: "range" };

  let negative = false;
  if (s.startsWith("-")) {
    negative = true;
    s = s.slice(1);
  }
  if (!/^(\d+\.?\d*|\.\d+)$/.test(s)) return { ok: false, error: "syntax" };

  const dot = s.indexOf(".");
  const intPart = dot === -1 ? s : s.slice(0, dot);
  let fracPart = dot === -1 ? "" : s.slice(dot + 1);

  if (fracPart.length > exp) {
    if (/[^0]/.test(fracPart.slice(exp))) return { ok: false, error: "precision" };
    fracPart = fracPart.slice(0, exp);
  }
  const digits = ((intPart || "0") + fracPart.padEnd(exp, "0")).replace(/^0+(?=\d)/, "");
  if (digits.length > 15) return { ok: false, error: "range" };

  // A string of at most 15 decimal digits converts to an exact integer (< 2^53): no rounding can happen.
  const magnitude = Number(digits);
  if (magnitude > MAX_AMOUNT_MINOR) return { ok: false, error: "range" };
  if (magnitude === 0) {
    return opts.allowZero ? { ok: true, minor: 0 } : { ok: false, error: "not_positive" };
  }
  if (negative) {
    if (!opts.allowNegative) return { ok: false, error: "negative" };
    return { ok: true, minor: -magnitude };
  }
  return { ok: true, minor: magnitude };
}

/** True when `input` looks like an amount at all (any currency). For form schemas; the exact check needs the currency. */
export function isMoneyInputSyntax(input: string): boolean {
  const s = input.replace(/[\s,  ]/g, "");
  return /^-?(\d+\.?\d*|\.\d+)$/.test(s);
}

// ───────────────────────────────────────────────────────────── formatting

function splitDigits(minor: number, exp: number): { negative: boolean; intPart: string; fracPart: string } {
  if (!Number.isSafeInteger(minor)) throw new RangeError(`amount_minor must be a safe integer, got ${minor}`);
  let digits = String(minor);
  const negative = digits.startsWith("-");
  if (negative) digits = digits.slice(1);
  digits = digits.padStart(exp + 1, "0");
  return {
    negative,
    intPart: digits.slice(0, digits.length - exp),
    fracPart: exp > 0 ? digits.slice(digits.length - exp) : "",
  };
}

function groupDigits(intPart: string, currency: string): string {
  if (currency === "INR") {
    // Indian grouping: last three digits, then pairs (12,34,56,789).
    if (intPart.length <= 3) return intPart;
    const last3 = intPart.slice(-3);
    const rest = intPart.slice(0, -3).replace(/\B(?=(\d{2})+(?!\d))/g, ",");
    return `${rest},${last3}`;
  }
  return intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

export type FormatOptions = {
  /** "auto": "-" only for negatives (default). "always": also "+" for positives. "never": no sign. */
  signDisplay?: "auto" | "always" | "never";
  /** Hide the fraction when it is all zeros (₹50,000 instead of ₹50,000.00). Default false. */
  trimZeroFraction?: boolean;
};

/** Format integer minor units for display: formatMinor(45050, "INR") → "₹450.50"; formatMinor(1500, "JPY") → "¥1,500". */
export function formatMinor(minor: number, currency: string, opts: FormatOptions = {}): string {
  const info = currencyInfo(currency);
  const { negative, intPart, fracPart } = splitDigits(minor, info.exponent);
  const showFraction = fracPart !== "" && !(opts.trimZeroFraction && /^0+$/.test(fracPart));
  const number = groupDigits(intPart, currency) + (showFraction ? `.${fracPart}` : "");
  const symbol = /[A-Za-z]$/.test(info.symbol) ? `${info.symbol} ` : info.symbol;
  const isZero = /^0+$/.test(intPart + fracPart);
  const sign = opts.signDisplay === "never" ? "" : negative ? "-" : opts.signDisplay === "always" && !isZero ? "+" : "";
  return `${sign}${symbol}${number}`;
}

/** Minor units → an editable input string without grouping or symbol: 45050 INR → "450.50", 5000000 INR → "50000". */
export function minorToInput(minor: number, currency: string): string {
  const { negative, intPart, fracPart } = splitDigits(minor, currencyExponent(currency));
  const frac = /^0*$/.test(fracPart) ? "" : `.${fracPart}`;
  return `${negative ? "-" : ""}${intPart}${frac}`;
}
