/**
 * @arth/api-client: typed access to the Arth API.
 *
 * `schema.ts` is GENERATED from api/openapi.yaml by openapi-typescript (`pnpm gen:api` from the repo root).
 * Never edit it by hand. Everything here is platform-neutral (no React Native, no DOM-only APIs) so the
 * Next.js app can reuse it. Token storage and refresh live in each app (mobile: apps/mobile/lib/api).
 */
import createClient, { type Client, type ClientOptions } from "openapi-fetch";
import type { components, paths } from "./schema.ts";

export type { components, paths } from "./schema.ts";
export * from "./money.ts";

type Schemas = components["schemas"];

export type Me = Schemas["Me"];
export type TokenPair = Schemas["TokenPair"];
export type Category = Schemas["Category"];
export type Account = Schemas["Account"];
export type AccountType = Schemas["AccountType"];
export type Transaction = Schemas["Transaction"];
export type TransactionType = Schemas["TransactionType"];
export type EntryType = Schemas["EntryType"];
export type InputMethod = Schemas["InputMethod"];
export type EntryInputMethod = Schemas["EntryInputMethod"];
export type TransactionPage = Schemas["TransactionPage"];
export type CreateTransactionRequest = Schemas["CreateTransactionRequest"];
export type UpdateTransactionRequest = Schemas["UpdateTransactionRequest"];
export type BudgetProgress = Schemas["BudgetProgress"];
export type Dashboard = Schemas["Dashboard"];
export type DailyTotals = Schemas["DailyTotals"];
export type Extraction = Schemas["Extraction"];
export type Problem = Schemas["Problem"];
export type FieldError = Schemas["FieldError"];

export type ArthClient = Client<paths>;

/** Create the typed client. Pass a custom `fetch` to add auth headers / refresh handling. */
export function createArthClient(options: ClientOptions & { baseUrl: string }): ArthClient {
  return createClient<paths>(options);
}

/**
 * Error thrown by `unwrap`. `code` is the API's stable problem code (docs/API.md §2), or one of the
 * client-side codes `network` (no response) and `unknown` (a response that is not problem+json).
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail?: string;
  readonly fieldErrors: FieldError[];
  readonly requestId?: string;
  readonly problem?: Problem;
  readonly retryAfterSeconds?: number;

  constructor(init: {
    status: number;
    code: string;
    message?: string;
    problem?: Problem;
    retryAfterSeconds?: number;
  }) {
    super(init.message ?? init.problem?.detail ?? init.problem?.title ?? init.code);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.problem = init.problem;
    this.detail = init.problem?.detail;
    this.fieldErrors = init.problem?.errors ?? [];
    this.requestId = init.problem?.request_id;
    this.retryAfterSeconds = init.retryAfterSeconds;
  }

  /** Message for a given body field (`amount_minor`), if the server reported one. */
  fieldMessage(field: string): string | undefined {
    return this.fieldErrors.find((e) => e.field === field)?.message;
  }
}

export function isApiError(err: unknown, code?: string): err is ApiError {
  return err instanceof ApiError && (code === undefined || err.code === code);
}

function isProblem(body: unknown): body is Problem {
  return typeof body === "object" && body !== null && typeof (body as { code?: unknown }).code === "string";
}

/** Build an ApiError from any failed response (body already parsed or not). */
export function toApiError(response: Response, body: unknown): ApiError {
  const retry = response.headers.get("Retry-After");
  const retryAfterSeconds = retry && /^\d+$/.test(retry) ? Number(retry) : undefined;
  if (isProblem(body)) {
    return new ApiError({ status: response.status, code: body.code, problem: body, retryAfterSeconds });
  }
  return new ApiError({ status: response.status, code: "unknown", message: `HTTP ${response.status}`, retryAfterSeconds });
}

type FetchResult<T> = { data?: T; error?: unknown; response: Response };

/**
 * Await an openapi-fetch call and return its data, or throw an ApiError.
 *   const me = await unwrap(api.GET("/v1/me"));
 * Network failures (fetch rejects) become ApiError code "network".
 */
export async function unwrap<T>(call: Promise<FetchResult<T>>): Promise<T> {
  let result: FetchResult<T>;
  try {
    result = await call;
  } catch (err) {
    if (err instanceof ApiError) throw err;
    throw new ApiError({ status: 0, code: "network", message: err instanceof Error ? err.message : "Network request failed" });
  }
  if (result.error !== undefined || !result.response.ok) {
    throw toApiError(result.response, result.error);
  }
  return result.data as T;
}
