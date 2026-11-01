/**
 * Platform-neutral session core for own auth (docs/AUTH.md §6). Apps plug in their token storage:
 * mobile uses expo-secure-store (apps/mobile/lib/api/session.ts); scripts/live-check.mts uses memory.
 *
 * Refresh tokens rotate with reuse detection and NO grace window (DECISIONS Q-8): presenting a used token
 * revokes the whole session. So every refresh goes through ONE shared in-flight promise. Requests that get
 * `401 unauthorized` wait for it and are retried once. If the server refuses the refresh, the session ends
 * (tokens cleared, `onSignedOut`). Only code `unauthorized` triggers a refresh (docs/API.md §1).
 */
import { ApiError, toApiError } from "./index.ts";
import type { components } from "./schema.ts";

type TokenPair = components["schemas"]["TokenPair"];

/** Where the refresh token lives. The access token is only ever kept in memory. */
export interface RefreshTokenStorage {
  get(): Promise<string | null>;
  set(token: string): Promise<void>;
  remove(): Promise<void>;
}

export interface SessionOptions {
  baseUrl: string;
  storage: RefreshTokenStorage;
  /** Called after every new token pair (login, register, refresh). */
  onTokens?: (pair: TokenPair) => void;
  /** Called after the session ended locally (refused refresh, logout, missing token). */
  onSignedOut?: () => void;
  /** fetch implementation; read at call time so tests can wrap it. Default: globalThis.fetch. */
  fetch?: (request: Request) => Promise<Response>;
  /** Refresh this long before the access token expires. Default 30 s. */
  expiryMarginMs?: number;
  now?: () => number;
}

export type RestoreResult = "signedIn" | "signedOut" | "offline";

const AUTH_FREE_PATH = /\/v1\/auth\/(login|register|refresh)(\?|$)/;

export function createSession(opts: SessionOptions) {
  const now = opts.now ?? (() => Date.now());
  const margin = opts.expiryMarginMs ?? 30_000;
  const doFetch = (r: Request) => (opts.fetch ?? globalThis.fetch)(r);

  let accessToken: string | null = null;
  let accessExpiresAt = 0;
  let refreshInFlight: Promise<boolean> | null = null;

  async function start(pair: TokenPair): Promise<void> {
    await opts.storage.set(pair.refresh_token);
    accessToken = pair.access_token;
    accessExpiresAt = now() + pair.access_expires_in * 1000;
    opts.onTokens?.(pair);
  }

  async function end(): Promise<void> {
    accessToken = null;
    accessExpiresAt = 0;
    try {
      await opts.storage.remove();
    } finally {
      opts.onSignedOut?.();
    }
  }

  async function doRefresh(): Promise<boolean> {
    const refreshToken = await opts.storage.get();
    if (!refreshToken) {
      await end();
      return false;
    }
    let res: Response;
    try {
      res = await doFetch(
        new Request(`${opts.baseUrl}/v1/auth/refresh`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ refresh_token: refreshToken }),
        }),
      );
    } catch (err) {
      // Offline / API unreachable: keep the session, the caller sees a network error and can retry.
      throw new ApiError({ status: 0, code: "network", message: err instanceof Error ? err.message : "Network request failed" });
    }
    if (res.ok) {
      await start((await res.json()) as TokenPair);
      return true;
    }
    const body = await res.json().catch(() => undefined);
    if (res.status === 429 || res.status >= 500) {
      throw toApiError(res, body); // the refresh token was not consumed: keep it
    }
    await end(); // 401 refresh_invalid (unknown, expired, revoked or reused): the session is gone
    return false;
  }

  /** One shared in-flight refresh. true = new tokens, false = session ended. Rejects on network errors. */
  function refresh(): Promise<boolean> {
    if (!refreshInFlight) {
      refreshInFlight = doRefresh().finally(() => {
        refreshInFlight = null;
      });
    }
    return refreshInFlight;
  }

  async function ensureFreshAccessToken(): Promise<string | null> {
    if (!accessToken || now() > accessExpiresAt - margin) await refresh();
    return accessToken;
  }

  /** App start: turn a stored refresh token into a live session. */
  async function restore(): Promise<RestoreResult> {
    const token = await opts.storage.get().catch(() => null);
    if (!token) return "signedOut";
    try {
      return (await refresh()) ? "signedIn" : "signedOut";
    } catch {
      return "offline";
    }
  }

  function withBearer(request: Request, token: string | null): Request {
    const headers = new Headers(request.headers);
    if (token) headers.set("Authorization", `Bearer ${token}`);
    else headers.delete("Authorization");
    return new Request(request, { headers });
  }

  async function problemCode(res: Response): Promise<string | undefined> {
    try {
      const body = (await res.clone().json()) as { code?: unknown };
      return typeof body.code === "string" ? body.code : undefined;
    } catch {
      return undefined;
    }
  }

  /** fetch for the typed client: bearer token, proactive refresh, one serialized refresh + single retry on 401. */
  async function authedFetch(request: Request): Promise<Response> {
    if (AUTH_FREE_PATH.test(request.url)) return doFetch(request);
    const retryCopy = request.clone();
    const tokenUsed = await ensureFreshAccessToken();
    const res = await doFetch(withBearer(request, tokenUsed));
    if (res.status !== 401 || (await problemCode(res)) !== "unauthorized") return res;
    // Another request may have refreshed meanwhile: then just retry with the newer token.
    if (accessToken === tokenUsed) {
      const refreshed = await refresh();
      if (!refreshed) return res;
    }
    return doFetch(withBearer(retryCopy, accessToken));
  }

  return {
    start,
    end,
    refresh,
    restore,
    ensureFreshAccessToken,
    authedFetch,
    /** For tests/diagnostics only. */
    peekAccessToken: () => accessToken,
  };
}

export type Session = ReturnType<typeof createSession>;
