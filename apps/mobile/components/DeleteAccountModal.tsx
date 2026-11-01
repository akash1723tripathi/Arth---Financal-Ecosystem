import { FormSheetModal } from "@/components/FormSheetModal";
import { COLORS } from "@/constants/theme";
import { useDeleteAccount } from "@/hooks/mutations/useAuthMutations";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { useEffect, useState } from "react";
import { Text, TextInput, TouchableOpacity } from "react-native";

/** Account deletion (Play requirement): re-enter the password, then DELETE /v1/me removes everything. */
export function DeleteAccountModal({
  visible,
  onClose,
}: {
  visible: boolean;
  onClose: () => void;
}) {
  const { mutateAsync: deleteAccount, isPending: deleting } = useDeleteAccount();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (visible) {
      setPassword("");
      setError("");
    }
  }, [visible]);

  const handleDelete = async () => {
    if (!password) {
      setError("Enter your password to confirm.");
      return;
    }
    setError("");
    try {
      // On success the session is cleared and the guards show the sign-in screen.
      await deleteAccount(password);
    } catch (err) {
      setError(isApiError(err, "invalid_credentials") ? "Incorrect password." : describeError(err));
    }
  };

  return (
    <FormSheetModal visible={visible} title="Delete account" onClose={onClose}>
      <Text className="text-brand-text-secondary text-sm mb-4">
        This permanently deletes your account, accounts, transactions and budgets, and signs you out
        on every device. This can&apos;t be undone.
      </Text>
      <Text className="text-brand-bg text-xs font-medium mb-1.5">Password</Text>
      <TextInput
        value={password}
        onChangeText={(v) => {
          setError("");
          setPassword(v);
        }}
        secureTextEntry
        autoComplete="current-password"
        placeholder="Your password"
        placeholderTextColor={COLORS.placeholder}
        className="bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 text-sm text-brand-bg mb-3"
      />
      {error ? <Text className="text-brand-coral text-xs mb-3">{error}</Text> : null}

      <TouchableOpacity
        onPress={handleDelete}
        disabled={deleting}
        className="bg-brand-coral rounded-xl py-4 items-center mb-3"
        activeOpacity={0.85}
      >
        <Text className="text-white text-sm font-semibold">
          {deleting ? "Deleting…" : "Delete my account"}
        </Text>
      </TouchableOpacity>
    </FormSheetModal>
  );
}
