import type { CreateTransactionRequest, UpdateTransactionRequest } from "@arth/api-client";
import { api, unwrap } from "@/lib/api/client";
import { MONEY_QUERY_ROOTS } from "@/lib/query/keys";
import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";

function invalidateMoney(queryClient: QueryClient) {
  MONEY_QUERY_ROOTS.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
}

/** POST /v1/transactions. The server updates the account balance in the same DB transaction. */
export function useCreateTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateTransactionRequest) => unwrap(api.POST("/v1/transactions", { body })),
    onSuccess: () => invalidateMoney(queryClient),
  });
}

/** PATCH /v1/transactions/{id}: omitted fields stay unchanged; "" clears the description. */
export function useUpdateTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateTransactionRequest }) =>
      unwrap(api.PATCH("/v1/transactions/{id}", { params: { path: { id } }, body })),
    onSuccess: () => invalidateMoney(queryClient),
  });
}

/** DELETE /v1/transactions/{id}. The server reverses the balance from the stored row. */
export function useDeleteTransaction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/v1/transactions/{id}", { params: { path: { id } } })),
    onSuccess: () => invalidateMoney(queryClient),
  });
}
