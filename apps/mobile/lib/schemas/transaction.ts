import { isMoneyInputSyntax } from "@arth/api-client";
import { z } from "zod";

// Amount: syntax only here. The exact conversion to minor units uses the selected account's currency on submit
// (parseMoneyInput: "450.50" INR → 45050). No floats.
export const transactionSchema = z.object({
  type: z.enum(["EXPENSE", "INCOME"]),
  amount: z
    .string()
    .min(1, "Enter an amount.")
    .refine((v) => isMoneyInputSyntax(v) && !v.trim().startsWith("-"), "Enter a valid amount."),
  categoryId: z.string().min(1, "Select a category."),
  accountId: z.string().min(1, "Select an account."),
  description: z.string().max(200, "Keep the description under 200 characters.").optional(),
  date: z.date(),
});

export type TransactionFormValues = z.infer<typeof transactionSchema>;
