import { isApiError } from "@arth/api-client";

/** Human text for the API's single error format (docs/API.md §2). Screens pass their own fallback text. */
export function describeError(err: unknown, fallback = "Something went wrong. Please try again."): string {
  if (!isApiError(err)) return fallback;
  switch (err.code) {
    case "network":
      return "Can't reach the server. Check your connection and try again.";
    case "rate_limited": {
      const s = err.retryAfterSeconds;
      if (!s) return "Too many attempts. Please wait a moment and try again.";
      const minutes = Math.ceil(s / 60);
      return `Too many attempts. Try again in ${minutes} minute${minutes === 1 ? "" : "s"}.`;
    }
    default:
      return fallback;
  }
}
