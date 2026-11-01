import { useAuthStore } from "@/store/authStore";

/** Route-guard state from the own-auth session (docs/AUTH.md §6). */
export function useSignedIn(): { isLoaded: boolean; isSignedIn: boolean } {
  const status = useAuthStore((s) => s.status);
  return { isLoaded: status !== "loading", isSignedIn: status === "signedIn" };
}
