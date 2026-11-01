/**
 * Mobile session: the shared session core from @arth/api-client (one serialized refresh, retry once on
 * `401 unauthorized`, sign out when the refresh is refused) with the refresh token stored ONLY in
 * expo-secure-store. The access token stays in memory. Design: docs/AUTH.md §6, DECISIONS Q-8.
 */
import { createSession } from "@arth/api-client/session";
import * as SecureStore from "expo-secure-store";

import { API_URL } from "@/lib/config";
import { queryClient } from "@/lib/query/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";

const REFRESH_TOKEN_KEY = "arth.refresh_token";

function setStatus(status: "signedIn" | "signedOut") {
  if (useAuthStore.getState().status !== status) useAuthStore.getState().setStatus(status);
}

export const session = createSession({
  baseUrl: API_URL,
  storage: {
    get: () => SecureStore.getItemAsync(REFRESH_TOKEN_KEY),
    set: (token) => SecureStore.setItemAsync(REFRESH_TOKEN_KEY, token),
    remove: () => SecureStore.deleteItemAsync(REFRESH_TOKEN_KEY),
  },
  onTokens: (pair) => {
    queryClient.setQueryData(queryKeys.me, pair.user);
    setStatus("signedIn");
  },
  onSignedOut: () => {
    queryClient.clear();
    setStatus("signedOut");
  },
});

export const authedFetch = session.authedFetch;

export function getStoredRefreshToken(): Promise<string | null> {
  return SecureStore.getItemAsync(REFRESH_TOKEN_KEY);
}

/** App start. Offline with a stored token counts as signed in; screens show a retry state. */
export async function restoreSession(): Promise<void> {
  const result = await session.restore();
  setStatus(result === "signedOut" ? "signedOut" : "signedIn");
}
