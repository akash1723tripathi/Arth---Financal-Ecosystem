import { api, unwrap } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useQuery } from "@tanstack/react-query";

/** GET /v1/reports/daily: zero-filled income/expense per day (≤ 93 days), oldest first. Transfers excluded. */
export function useDailyReportQuery(from: string, to: string, accountId?: string | null) {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.dailyReport(from, to, accountId),
    queryFn: async () =>
      (
        await unwrap(
          api.GET("/v1/reports/daily", {
            params: { query: { from, to, ...(accountId ? { account_id: accountId } : {}) } },
          })
        )
      ).data,
    enabled: signedIn,
  });
}
