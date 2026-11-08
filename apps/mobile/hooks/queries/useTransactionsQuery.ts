import { api, unwrap } from "@/lib/api/client";
import { queryKeys, type TransactionListFilters } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

export const TRANSACTIONS_PAGE_SIZE = 30;

/**
 * GET /v1/transactions with keyset pagination (newest first). Filters are server-side, including the
 * description search `q`. The cursor is opaque and tied to the filters, so a filter change starts a new query.
 */
export function useTransactionsInfiniteQuery(filters: TransactionListFilters = {}) {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  const q = filters.q?.trim() || undefined;
  return useInfiniteQuery({
    queryKey: queryKeys.transactionList({ type: filters.type ?? null, accountId: filters.accountId ?? null, q: q ?? null }),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/v1/transactions", {
          params: {
            query: {
              limit: TRANSACTIONS_PAGE_SIZE,
              ...(pageParam ? { cursor: pageParam } : {}),
              ...(filters.type ? { type: filters.type } : {}),
              ...(filters.accountId ? { account_id: filters.accountId } : {}),
              ...(q ? { q: q.slice(0, 100) } : {}),
            },
          },
        })
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: signedIn,
  });
}

/** GET /v1/transactions/{id} (edit screen). */
export function useTransactionQuery(id: string | undefined) {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.transaction(id ?? ""),
    queryFn: () => unwrap(api.GET("/v1/transactions/{id}", { params: { path: { id: id! } } })),
    enabled: signedIn && !!id,
  });
}
