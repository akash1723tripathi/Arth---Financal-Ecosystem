import { create } from "zustand";

/**
 * Session status for the route guards.
 * - "loading": restoring the session from secure storage at app start
 * - "signedIn": a refresh token exists (the access token may still need a refresh)
 * - "signedOut": no session; the guards send the user to /sign-in
 */
export type AuthStatus = "loading" | "signedIn" | "signedOut";

interface AuthStore {
  status: AuthStatus;
  setStatus: (status: AuthStatus) => void;
}

export const useAuthStore = create<AuthStore>((set) => ({
  status: "loading",
  setStatus: (status) => set({ status }),
}));
