/**
 * The app's single money module. All amount input and display goes through these helpers (re-exported from
 * @arth/api-client so web uses the same code): integer minor units in, integer minor units out, no floats.
 */
import { currencyExponent, type MoneyParseError } from "@arth/api-client";

export {
  currencyExponent,
  currencySymbol,
  formatMinor,
  isMoneyInputSyntax,
  minorToInput,
  parseMoneyInput,
} from "@arth/api-client";

/** User-facing text for a failed parseMoneyInput. `invalid` is the screen's existing "Enter a valid …" text. */
export function moneyInputErrorText(error: MoneyParseError, currency: string, invalid: string): string {
  if (error === "precision") {
    const exp = currencyExponent(currency);
    return exp === 0
      ? `${currency} amounts can't have decimals.`
      : `${currency} amounts have at most ${exp} decimal places.`;
  }
  if (error === "range") return "That amount is too large.";
  return invalid;
}
