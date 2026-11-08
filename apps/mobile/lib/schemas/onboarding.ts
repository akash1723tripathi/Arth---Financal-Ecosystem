import { isMoneyInputSyntax } from "@arth/api-client";
import { z } from "zod";

// Syntax only; the exact conversion to minor units (with the chosen currency's decimals) happens on submit
// with parseMoneyInput. No floats.
export const onboardingSchema = z.object({
  startingBalance: z
    .string()
    .min(1, "Please enter a starting balance.")
    .refine((v) => isMoneyInputSyntax(v) && !v.trim().startsWith("-"), "Please enter a valid starting balance."),
});

export type OnboardingFormValues = z.infer<typeof onboardingSchema>;
