import { BACKEND } from "@/lib/config";
import { useAuthStore } from "@/store/authStore";
import { useAuth } from "@clerk/expo";

type SignedIn = { isLoaded: boolean; isSignedIn: boolean };

function useSignedInApi(): SignedIn {
  const status = useAuthStore((s) => s.status);
  return { isLoaded: status !== "loading", isSignedIn: status === "signedIn" };
}

// LEGACY (EXPO_PUBLIC_BACKEND=supabase): removed with Clerk.
function useSignedInClerk(): SignedIn {
  const { isLoaded, isSignedIn } = useAuth();
  return { isLoaded, isSignedIn: !!isSignedIn };
}

/** Route-guard state. The backend switch is a build-time constant, so the hook choice never changes at runtime. */
export const useSignedIn = BACKEND === "supabase" ? useSignedInClerk : useSignedInApi;
