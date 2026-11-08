import type { Category } from "@arth/api-client";
import { api, unwrap } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import { useAuthStore } from "@/store/authStore";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

/** GET /v1/categories: system categories (INCOME + EXPENSE) in display order. They rarely change. */
export function useCategoriesQuery() {
  const signedIn = useAuthStore((s) => s.status === "signedIn");
  return useQuery({
    queryKey: queryKeys.categories,
    queryFn: async () => (await unwrap(api.GET("/v1/categories"))).data,
    enabled: signedIn,
    staleTime: 60 * 60 * 1000,
  });
}

/** Display data for a transaction's category (transfer legs have no category). */
export type CategoryDisplay = { label: string; icon: string; color: string };

const FALLBACK: CategoryDisplay = { label: "Other", icon: "📦", color: "#BDC3C7" };
export const TRANSFER_DISPLAY: CategoryDisplay = { label: "Transfer", icon: "🔁", color: "#8A8D96" };

export function toCategoryDisplay(category: Category | undefined): CategoryDisplay {
  if (!category) return FALLBACK;
  return {
    label: category.name,
    icon: category.icon ?? FALLBACK.icon,
    color: category.color ?? FALLBACK.color,
  };
}

/** id → Category lookup plus the INCOME / EXPENSE lists for pickers. */
export function useCategories() {
  const query = useCategoriesQuery();
  const derived = useMemo(() => {
    const list = query.data ?? [];
    const byId = new Map<string, Category>(list.map((c) => [c.id, c]));
    return {
      byId,
      expense: list.filter((c) => c.kind === "EXPENSE"),
      income: list.filter((c) => c.kind === "INCOME"),
    };
  }, [query.data]);
  return { ...query, ...derived };
}
