import { api, unwrap } from "@/lib/api/client";
import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";

function invalidateBudgets(queryClient: QueryClient) {
  queryClient.invalidateQueries({ queryKey: ["budgets"] });
  queryClient.invalidateQueries({ queryKey: ["dashboard"] });
}

/** PUT /v1/budgets/overall or /v1/budgets/categories/{id} (categoryId null = overall). Currency = the user's. */
export function usePutBudget() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ categoryId, amountMinor }: { categoryId: string | null; amountMinor: number }) =>
      categoryId
        ? unwrap(
            api.PUT("/v1/budgets/categories/{category_id}", {
              params: { path: { category_id: categoryId } },
              body: { amount_minor: amountMinor },
            })
          )
        : unwrap(api.PUT("/v1/budgets/overall", { body: { amount_minor: amountMinor } })),
    onSuccess: () => invalidateBudgets(queryClient),
  });
}

/** DELETE a budget (idempotent on the server). */
export function useDeleteBudget() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (categoryId: string | null) =>
      categoryId
        ? unwrap(
            api.DELETE("/v1/budgets/categories/{category_id}", {
              params: { path: { category_id: categoryId } },
            })
          )
        : unwrap(api.DELETE("/v1/budgets/overall")),
    onSuccess: () => invalidateBudgets(queryClient),
  });
}
