export type TransactionListFilters = {
  type?: "INCOME" | "EXPENSE" | null;
  accountId?: string | null;
  q?: string | null;
};

export const queryKeys = {
  // Own auth: the user comes from the token, so keys need no user id; the cache is cleared on sign-out.
  me: ["me"] as const,
  categories: ["categories"] as const,
  accountList: ["accounts", "list"] as const,
  transactionList: (filters: TransactionListFilters = {}) => ["transactions", "list", filters] as const,
  transaction: (id: string) => ["transactions", "detail", id] as const,
  budgets: (month?: string) => ["budgets", month ?? "current"] as const,
  dashboard: (month?: string) => ["dashboard", month ?? "current"] as const,
  dailyReport: (from: string, to: string, accountId?: string | null) =>
    ["reports", "daily", from, to, accountId ?? null] as const,
};

/** Every query a money write can change (balances, lists, aggregates). */
export const MONEY_QUERY_ROOTS = [["accounts"], ["transactions"], ["budgets"], ["dashboard"], ["reports"]] as const;
