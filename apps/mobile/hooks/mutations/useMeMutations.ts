import { api, unwrap } from "@/lib/api/client";
import { MONEY_QUERY_ROOTS, queryKeys } from "@/lib/query/keys";
import { useMutation, useQueryClient } from "@tanstack/react-query";

/** Device IANA time zone (the API validates it); "today" and "this month" follow it on the server. */
export function deviceTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** POST /v1/me/onboarding: currency, timezone and the default account's opening balance, in one DB transaction. */
export function useCompleteOnboarding() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { currency: string; opening_balance_minor: number }) =>
      unwrap(api.POST("/v1/me/onboarding", { body: { ...body, timezone: deviceTimezone() } })),
    onSuccess: (me) => {
      queryClient.setQueryData(queryKeys.me, me);
      MONEY_QUERY_ROOTS.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
    },
  });
}

/** PATCH /v1/me (name, currency, timezone, email preferences). */
export function useUpdateMe() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { name?: string; currency?: string; timezone?: string }) =>
      unwrap(api.PATCH("/v1/me", { body })),
    onSuccess: (me) => {
      queryClient.setQueryData(queryKeys.me, me);
      MONEY_QUERY_ROOTS.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
    },
  });
}
