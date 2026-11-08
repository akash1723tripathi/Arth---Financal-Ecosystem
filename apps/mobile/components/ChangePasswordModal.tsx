import { FormSheetModal } from "@/components/FormSheetModal";
import { COLORS } from "@/constants/theme";
import { useChangePassword } from "@/hooks/mutations/useAuthMutations";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { ChangePasswordFormValues, changePasswordSchema } from "@/lib/schemas/auth";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { Alert, Text, TextInput, TouchableOpacity } from "react-native";

export function ChangePasswordModal({
  visible,
  onClose,
}: {
  visible: boolean;
  onClose: () => void;
}) {
  const { mutateAsync: changePassword, isPending: saving } = useChangePassword();
  const [error, setError] = useState("");

  const {
    control,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ChangePasswordFormValues>({
    resolver: zodResolver(changePasswordSchema),
    mode: "onBlur",
    defaultValues: { currentPassword: "", newPassword: "" },
  });

  useEffect(() => {
    if (visible) {
      reset({ currentPassword: "", newPassword: "" });
      setError("");
    }
  }, [visible, reset]);

  const onSubmit = async (values: ChangePasswordFormValues) => {
    setError("");
    try {
      await changePassword(values);
      onClose();
      Alert.alert("Password changed", "You've been signed out on your other devices.");
    } catch (err) {
      if (isApiError(err, "invalid_credentials")) {
        setError("Your current password is incorrect.");
      } else if (isApiError(err, "weak_password")) {
        setError("Choose a stronger password: 8–128 characters and not a common password.");
      } else {
        setError(describeError(err));
      }
    }
  };

  return (
    <FormSheetModal visible={visible} title="Change password" onClose={onClose}>
      <Text className="text-brand-bg text-xs font-medium mb-1.5">Current password</Text>
      <Controller
        control={control}
        name="currentPassword"
        render={({ field: { value, onChange, onBlur } }) => (
          <TextInput
            value={value}
            onChangeText={(v) => {
              setError("");
              onChange(v);
            }}
            onBlur={onBlur}
            secureTextEntry
            autoComplete="current-password"
            placeholderTextColor={COLORS.placeholder}
            className="bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 text-sm text-brand-bg mb-1"
          />
        )}
      />
      {errors.currentPassword && (
        <Text className="text-brand-coral text-xs mb-2">{errors.currentPassword.message}</Text>
      )}
      <Text className="text-brand-bg text-xs font-medium mb-1.5 mt-3">New password</Text>
      <Controller
        control={control}
        name="newPassword"
        render={({ field: { value, onChange, onBlur } }) => (
          <TextInput
            value={value}
            onChangeText={(v) => {
              setError("");
              onChange(v);
            }}
            onBlur={onBlur}
            secureTextEntry
            autoComplete="new-password"
            placeholder="At least 8 characters"
            placeholderTextColor={COLORS.placeholder}
            className="bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 text-sm text-brand-bg mb-1"
          />
        )}
      />
      {errors.newPassword && (
        <Text className="text-brand-coral text-xs mb-2">{errors.newPassword.message}</Text>
      )}
      <Text className="text-brand-coral text-xs mb-3 mt-2">{error}</Text>

      <TouchableOpacity
        onPress={handleSubmit(onSubmit)}
        disabled={saving}
        className="bg-brand-bg rounded-xl py-4 items-center mb-3"
        activeOpacity={0.85}
      >
        <Text className="text-white text-sm font-semibold">
          {saving ? "Saving…" : "Change password"}
        </Text>
      </TouchableOpacity>
    </FormSheetModal>
  );
}
