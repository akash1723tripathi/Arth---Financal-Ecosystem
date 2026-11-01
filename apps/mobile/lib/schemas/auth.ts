import { z } from "zod";

// Server password policy (docs/AUTH.md §4): 8–128 characters, not a common password (checked by the server).
const newPassword = z
  .string()
  .min(8, "Password must be at least 8 characters.")
  .max(128, "Password must be at most 128 characters.");

export const signUpSchema = z.object({
  firstName: z.string().trim().min(1, "First name is required.").max(50, "First name is too long."),
  lastName: z.string().trim().min(1, "Last name is required.").max(49, "Last name is too long."),
  email: z.email("Enter a valid email.").trim().min(1, "Email is required."),
  password: newPassword,
});

export type SignUpFormValues = z.infer<typeof signUpSchema>;

export const signInSchema = z.object({
  email: z.email("Enter a valid email.").trim().min(1, "Email is required."),
  password: z.string().min(1, "Password is required."),
});

export type SignInFormValues = z.infer<typeof signInSchema>;

export const changePasswordSchema = z.object({
  currentPassword: z.string().min(1, "Enter your current password."),
  newPassword,
});

export type ChangePasswordFormValues = z.infer<typeof changePasswordSchema>;

// LEGACY (Clerk email codes), removed with Clerk.
export const codeSchema = z.object({
  code: z.string().min(1, "Enter the verification code."),
});

export type CodeFormValues = z.infer<typeof codeSchema>;
