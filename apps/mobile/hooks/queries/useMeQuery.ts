import { api, unwrap } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useQuery } from "@tanstack/react-query";

/** GET /v1/me: the signed-in user (name, email, currency, timezone, needs_onboarding). */
export function useMeQuery() {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.me,
    queryFn: () => unwrap(api.GET("/v1/me")),
    enabled: signedIn,
    staleTime: 60_000,
  });
}

/** The user's default currency (INR until onboarding, as the API does). */
export function useUserCurrency(): string {
  return useMeQuery().data?.currency ?? "INR";
}
