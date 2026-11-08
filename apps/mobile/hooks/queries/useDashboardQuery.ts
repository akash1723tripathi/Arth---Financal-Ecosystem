import { api, unwrap } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useQuery } from "@tanstack/react-query";

/**
 * GET /v1/dashboard: everything Home shows, aggregated by the server in the user's currency and timezone:
 * total balance, this month's income/expense/net, overall budget progress, expense by category (largest
 * first) and the 5 most recent transactions. Transfers are excluded from the aggregates.
 */
export function useDashboardQuery(month?: string) {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.dashboard(month),
    queryFn: () => unwrap(api.GET("/v1/dashboard", { params: { query: month ? { month } : {} } })),
    enabled: signedIn,
  });
}
