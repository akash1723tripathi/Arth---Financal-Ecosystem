import type { AccountType } from "@arth/api-client";
import { api, isApiError, unwrap } from "@/lib/api/client";
import { MONEY_QUERY_ROOTS } from "@/lib/query/keys";
import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";

function invalidateMoney(queryClient: QueryClient) {
  MONEY_QUERY_ROOTS.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
}

export function useCreateAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    // Currency defaults to the user's currency on the server; opening balance defaults to 0.
    mutationFn: (payload: { name: string; type: AccountType }) =>
      unwrap(api.POST("/v1/accounts", { body: payload })),
    onSuccess: () => invalidateMoney(queryClient),
  });
}

export function useUpdateAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ accountId, payload }: { accountId: string; payload: { name: string; type: AccountType } }) =>
      unwrap(api.PATCH("/v1/accounts/{id}", { params: { path: { id: accountId } }, body: payload })),
    onSuccess: () => invalidateMoney(queryClient),
  });
}

export type DeleteAccountResult = { deleted: true } | { deleted: false; transactionCount: number };

/**
 * Two-step delete (same UX as before): without `force` the server answers 409 account_has_transactions with
 * `transaction_count`, which the screen confirms; with `force` it deletes with ?cascade=true.
 * 409 default_account is thrown (the default account cannot be deleted).
 */
export function useDeleteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ accountId, force = false }: { accountId: string; force?: boolean }): Promise<DeleteAccountResult> => {
      try {
        await unwrap(
          api.DELETE("/v1/accounts/{id}", {
            params: { path: { id: accountId }, query: force ? { cascade: true } : {} },
          })
        );
        return { deleted: true };
      } catch (err) {
        if (!force && isApiError(err, "account_has_transactions")) {
          const count = err.problem?.transaction_count;
          return { deleted: false, transactionCount: typeof count === "number" ? count : 0 };
        }
        throw err;
      }
    },
    onSuccess: (result) => {
      if (result.deleted) invalidateMoney(queryClient);
    },
  });
}

/** POST /v1/accounts/{id}/make-default (the previous default is unset in the same DB transaction). */
export function useSetDefaultAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (accountId: string) =>
      unwrap(api.POST("/v1/accounts/{id}/make-default", { params: { path: { id: accountId } } })),
    onSuccess: () => invalidateMoney(queryClient),
  });
}
