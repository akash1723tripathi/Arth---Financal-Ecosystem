import { FormSheetModal } from "@/components/FormSheetModal";
import { PillGroup } from "@/components/PillGroup";
import { COLORS } from "@/constants/theme";
import { useDeleteBudget, usePutBudget } from "@/hooks/mutations/useBudgetMutations";
import { useBudgetsQuery } from "@/hooks/queries/useBudgetsQuery";
import { useCategories } from "@/hooks/queries/useCategoriesQuery";
import { useUserCurrency } from "@/hooks/queries/useMeQuery";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { minorToInput, moneyInputErrorText, parseMoneyInput } from "@/lib/money";
import { useEffect, useMemo, useState } from "react";
import { Text, TextInput, TouchableOpacity, View } from "react-native";

const OVERALL = "overall";

/**
 * Monthly budget sheet: the overall budget (PUT /v1/budgets/overall) or one per expense category
 * (PUT /v1/budgets/categories/{id}). Budgets are in the user's currency.
 */
export function BudgetModal({
  visible,
  onClose,
  onSaved,
}: {
  visible: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const currency = useUserCurrency();
  const { data: budgets = [] } = useBudgetsQuery();
  const { expense } = useCategories();
  const { mutateAsync: putBudget, isPending: saving } = usePutBudget();
  const { mutateAsync: deleteBudget, isPending: deleting } = useDeleteBudget();

  const [scope, setScope] = useState<string>(OVERALL);
  const [amount, setAmount] = useState("");
  const [error, setError] = useState("");

  const current = useMemo(
    () => budgets.find((b) => (scope === OVERALL ? b.category_id === null : b.category_id === scope)),
    [budgets, scope]
  );

  useEffect(() => {
    if (visible) setScope(OVERALL);
  }, [visible]);

  useEffect(() => {
    if (visible) {
      setAmount(current ? minorToInput(current.amount_minor, current.currency) : "");
      setError("");
    }
  }, [visible, current]);

  const categoryId = scope === OVERALL ? null : scope;
  const scopeName =
    scope === OVERALL ? "Monthly budget" : `${expense.find((c) => c.id === scope)?.name ?? "Category"} budget`;

  const handleSave = async () => {
    const parsed = parseMoneyInput(amount, currency); // "50,000" INR → 5000000, no floats
    if (!parsed.ok) {
      setError(moneyInputErrorText(parsed.error, currency, "Enter a valid monthly budget."));
      return;
    }
    setError("");
    try {
      await putBudget({ categoryId, amountMinor: parsed.minor });
      onSaved();
    } catch (err) {
      setError(
        isApiError(err, "validation_failed")
          ? "Enter a valid monthly budget."
          : describeError(err)
      );
    }
  };

  const handleRemove = async () => {
    try {
      await deleteBudget(categoryId);
      onSaved();
    } catch (err) {
      setError(describeError(err));
    }
  };

  return (
    <FormSheetModal
      visible={visible}
      title={budgets.length > 0 ? "Edit monthly budget" : "Set monthly budget"}
      onClose={onClose}
    >
      <View className="mb-4">
        <PillGroup
          options={[
            { key: OVERALL, label: "Overall" },
            ...expense.map((c) => ({ key: c.id, label: c.name, icon: c.icon ?? undefined })),
          ]}
          value={scope}
          onChange={setScope}
        />
      </View>

      <Text className="text-brand-bg text-xs font-medium mb-1.5">
        {scopeName} ({currency})
      </Text>
      <TextInput
        value={amount}
        onChangeText={(v) => {
          setError("");
          setAmount(v);
        }}
        placeholder="e.g. 50000"
        placeholderTextColor={COLORS.placeholder}
        keyboardType="numeric"
        autoFocus
        className="bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 mb-5 text-sm text-brand-bg"
      />

      {error ? (
        <Text className="text-brand-coral text-xs mb-3">{error}</Text>
      ) : null}

      <TouchableOpacity
        onPress={handleSave}
        disabled={saving}
        className="bg-brand-bg rounded-xl py-4 items-center mb-3"
        activeOpacity={0.85}
      >
        <Text className="text-white text-sm font-semibold">
          {saving ? "Saving…" : "Save budget"}
        </Text>
      </TouchableOpacity>

      {current && (
        <TouchableOpacity onPress={handleRemove} disabled={deleting} className="py-3 items-center">
          <Text className="text-brand-coral text-sm font-medium">
            {deleting ? "Removing…" : "Remove budget"}
          </Text>
        </TouchableOpacity>
      )}
    </FormSheetModal>
  );
}
