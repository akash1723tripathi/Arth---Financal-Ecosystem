import { api, unwrap } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useQuery } from "@tanstack/react-query";

/** GET /v1/accounts: default account first, then by creation time. */
export function useAccountsQuery() {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.accountList,
    queryFn: async () => (await unwrap(api.GET("/v1/accounts"))).data,
    enabled: signedIn,
  });
}
