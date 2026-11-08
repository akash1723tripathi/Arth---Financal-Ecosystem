import { api, unwrap } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useQuery } from "@tanstack/react-query";

/**
 * GET /v1/budgets?month=YYYY-MM: overall budget first (category_id null), then category budgets, each with
 * spent / remaining / percent_used computed by the server for the month (default: current month, user's timezone).
 */
export function useBudgetsQuery(month?: string) {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.budgets(month),
    queryFn: async () =>
      (await unwrap(api.GET("/v1/budgets", { params: { query: month ? { month } : {} } }))).data,
    enabled: signedIn,
  });
}
