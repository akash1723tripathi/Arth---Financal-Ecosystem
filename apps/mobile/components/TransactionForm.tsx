import type { Account, Category, EntryInputMethod, EntryType, Transaction } from "@arth/api-client";
import { AIActionCard } from "@/components/AIActionCard";
import { CalendarPicker } from "@/components/CalendarPicker";
import { PillGroup } from "@/components/PillGroup";
import { ReceiptScannerModal } from "@/components/ReceiptScannerModal";
import { VoiceRecorderModal } from "@/components/VoiceRecorderModal";
import { AI_GRADIENT, AI_GRADIENT_REVERSE } from "@/constants/theme";
import {
  useCreateTransaction,
  useUpdateTransaction,
} from "@/hooks/mutations/useTransactionMutations";
import { useAccountsQuery } from "@/hooks/queries/useAccountsQuery";
import { useCategories } from "@/hooks/queries/useCategoriesQuery";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { minorToInput, moneyInputErrorText, parseMoneyInput } from "@/lib/money";
import { TransactionFormValues, transactionSchema } from "@/lib/schemas/transaction";
import {
  ExtractedTransaction,
  extractTransactionFromReceipt,
} from "@/lib/services/extractTransaction";
import { Feather } from "@expo/vector-icons";
import { zodResolver } from "@hookform/resolvers/zod";
import { format, isValid, parse } from "date-fns";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import {
  ActivityIndicator,
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";

const TYPE_OPTIONS = [
  { key: "EXPENSE" as const, label: "Expense" },
  { key: "INCOME" as const, label: "Income" },
];

/** A draft from an AI extraction, normalised for the form (amount as an input string, never a float). */
export type ExtractionDraft = {
  type: EntryType | null;
  amountInput: string | null;
  categoryId: string | null;
  description: string | null;
  occurredOn: string | null; // yyyy-MM-dd
  transcript: string | null;
};

function parseDay(day: string): Date | null {
  const d = parse(day, "yyyy-MM-dd", new Date());
  return isValid(d) ? d : null;
}

function defaultCategoryId(expense: Category[]): string {
  return (expense.find((c) => c.key === "food") ?? expense[0])?.id ?? "";
}

function createDefaults(accounts: Account[], expense: Category[]): TransactionFormValues {
  return {
    type: "EXPENSE",
    amount: "",
    categoryId: defaultCategoryId(expense),
    accountId: accounts[0]?.id ?? "",
    description: "",
    date: new Date(),
  };
}

function editDefaults(tx: Transaction): TransactionFormValues {
  return {
    type: tx.type === "INCOME" ? "INCOME" : "EXPENSE",
    amount: minorToInput(tx.amount_minor, tx.currency),
    categoryId: tx.category_id ?? "",
    accountId: tx.account_id,
    description: tx.description ?? "",
    date: parseDay(tx.occurred_on) ?? new Date(),
  };
}

// LEGACY adapter (client-side Gemini), replaced by the server receipt endpoint in the receipt-scan step.
function legacyToDraft(result: ExtractedTransaction, categories: Category[]): ExtractionDraft {
  return {
    type: result.type,
    amountInput: result.amount != null ? String(result.amount) : null,
    categoryId: categories.find((c) => c.key === result.category)?.id ?? null,
    description: result.description,
    occurredOn: result.date ? result.date.slice(0, 10) : null,
    transcript: result.transcript,
  };
}

/**
 * The add / edit transaction form (Welth "Add transaction" screen, now on the Go API).
 * Create mode shows the AI capture cards and honours `?action=scan|voice`.
 */
export function TransactionForm({ editing }: { editing?: Transaction }) {
  const router = useRouter();
  const params = useLocalSearchParams<{ action?: string }>();
  const isEdit = !!editing;

  const { data: accounts = [], isLoading: loadingAccounts, isError: accountsError } = useAccountsQuery();
  const { byId: categoryById, expense, income, isLoading: loadingCategories, data: categoryList = [] } = useCategories();
  const { mutateAsync: createTransaction, isPending: creating } = useCreateTransaction();
  const { mutateAsync: updateTransaction, isPending: updating } = useUpdateTransaction();
  const saving = creating || updating;

  const [error, setError] = useState("");
  const [datePickerOpen, setDatePickerOpen] = useState(false);
  const [inputMethod, setInputMethod] = useState<EntryInputMethod>("MANUAL");
  const [voiceTranscript, setVoiceTranscript] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);
  const [scannerOpen, setScannerOpen] = useState(false);
  const [voiceModalOpen, setVoiceModalOpen] = useState(false);

  const {
    control,
    handleSubmit,
    watch,
    setValue,
    setError: setFieldError,
    reset: resetForm,
    formState: { errors },
  } = useForm<TransactionFormValues>({
    resolver: zodResolver(transactionSchema),
    mode: "onBlur",
    defaultValues: editing ? editDefaults(editing) : createDefaults([], []),
  });

  const type = watch("type");
  const categoryId = watch("categoryId");
  const accountId = watch("accountId");
  const date = watch("date");

  const categories = type === "INCOME" ? income : expense;
  const selectedAccount = accounts.find((a) => a.id === accountId);

  useEffect(() => {
    if (editing) {
      resetForm(editDefaults(editing));
    } else if (accounts.length > 0 && expense.length > 0) {
      resetForm(createDefaults(accounts, expense));
    }
  }, [editing, accounts, expense, resetForm]);

  const applyExtraction = (draft: ExtractionDraft) => {
    const draftCategory = draft.categoryId ? categoryById.get(draft.categoryId) : undefined;
    const draftType = draft.type ?? type;
    const categoryFits = !!draftCategory && draftCategory.kind === draftType;

    if (draft.type) setValue("type", draft.type);
    if (categoryFits) setValue("categoryId", draftCategory.id);
    else if (draft.type && draft.type !== type) {
      setValue("categoryId", draft.type === "INCOME" ? income[0]?.id ?? "" : defaultCategoryId(expense));
    }
    if (draft.amountInput != null) setValue("amount", draft.amountInput);
    if (draft.description) setValue("description", draft.description);
    if (draft.occurredOn) {
      const parsedDate = parseDay(draft.occurredOn);
      if (parsedDate && parsedDate <= new Date()) setValue("date", parsedDate);
    }

    const missing = [draft.amountInput == null && "amount", !categoryFits && "category"].filter(Boolean);
    if (missing.length > 0) {
      Alert.alert(
        "Review before saving",
        `Couldn't confidently read the ${missing.join(" and ")}. Please fill it in.`
      );
    }
  };

  const handleReceiptCaptured = async (base64: string, mimeType: string) => {
    setScannerOpen(false);
    setScanning(true);
    try {
      const extracted = await extractTransactionFromReceipt(base64, mimeType);
      applyExtraction(legacyToDraft(extracted, categoryList));
      setInputMethod("RECEIPT_SCAN");
    } catch (err) {
      console.error("Receipt scan failed:", err);
      Alert.alert("Error", "Couldn't read that receipt. Try again or enter it manually.");
    } finally {
      setScanning(false);
    }
  };

  const handleVoiceExtracted = (result: ExtractedTransaction) => {
    applyExtraction(legacyToDraft(result, categoryList));
    setVoiceTranscript(result.transcript);
    setInputMethod("VOICE");
  };

  useEffect(() => {
    if (isEdit) return;
    if (params.action === "scan") {
      setScannerOpen(true);
      router.setParams({ action: undefined });
    } else if (params.action === "voice") {
      setVoiceModalOpen(true);
      router.setParams({ action: undefined });
    }
  }, [isEdit, params.action, router]);

  const onSubmit = async (values: TransactionFormValues) => {
    setError("");
    const account = accounts.find((a) => a.id === values.accountId);
    if (!account) {
      setFieldError("accountId", { message: "Select an account." });
      return;
    }
    // "450.50" → 45050 in the account's currency (string handling, no floats).
    const parsed = parseMoneyInput(values.amount, account.currency);
    if (!parsed.ok) {
      setFieldError("amount", {
        message: moneyInputErrorText(parsed.error, account.currency, "Enter a valid amount."),
      });
      return;
    }
    const occurredOn = format(values.date, "yyyy-MM-dd"); // the picked local day, not toISOString()
    const description = values.description?.trim() ?? "";

    try {
      if (editing) {
        await updateTransaction({
          id: editing.id,
          body: {
            account_id: values.accountId,
            category_id: values.categoryId,
            type: values.type,
            amount_minor: parsed.minor,
            occurred_on: occurredOn,
            description, // "" clears it
          },
        });
        router.back();
        return;
      }

      await createTransaction({
        account_id: values.accountId,
        category_id: values.categoryId,
        type: values.type,
        amount_minor: parsed.minor,
        occurred_on: occurredOn,
        ...(description ? { description } : {}),
        input_method: inputMethod,
        ...(inputMethod === "VOICE" && voiceTranscript ? { voice_transcript: voiceTranscript } : {}),
      });
    } catch (err) {
      if (isApiError(err, "currency_mismatch")) {
        setError("You can't move this transaction to an account in another currency.");
      } else if (isApiError(err, "validation_failed")) {
        setError("Please check the amount, category and date.");
      } else {
        setError(describeError(err));
      }
      return;
    }

    resetForm(createDefaults(accounts, expense));
    setInputMethod("MANUAL");
    setVoiceTranscript(null);
    if (router.canGoBack()) {
      router.back();
    } else {
      router.replace("/(root)/(tabs)/transactions");
    }
  };

  return (
    <>
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        className="flex-1"
      >
        {loadingAccounts || loadingCategories ? (
          <View className="flex-1 items-center justify-center">
            <ActivityIndicator color="#4A9EFF" />
          </View>
        ) : accountsError ? (
          <View className="flex-1 items-center justify-center px-10">
            <Feather name="alert-circle" size={32} color="#FF6B4A" />
            <Text className="text-brand-text-muted text-sm mt-3 text-center">
              Couldn&apos;t load your accounts.
            </Text>
          </View>
        ) : accounts.length === 0 ? (
          <View className="flex-1 items-center justify-center px-10">
            <Feather name="alert-circle" size={32} color="#FF6B4A" />
            <Text className="text-brand-text-muted text-sm mt-3 text-center">
              You need an account before adding a transaction.
            </Text>
          </View>
        ) : (
          <ScrollView
            showsVerticalScrollIndicator={false}
            contentContainerStyle={{
              paddingHorizontal: 20,
              paddingBottom: 100,
            }}
          >
            {/* AI capture shortcuts */}
            {!isEdit && (
              <View className="flex-row gap-2.5 mb-4">
                <AIActionCard
                  icon="camera"
                  title="Scan receipt"
                  subtitle="Snap a photo"
                  colors={AI_GRADIENT}
                  onPress={() => setScannerOpen(true)}
                />
                <AIActionCard
                  icon="mic"
                  title="Voice log"
                  subtitle="Just say it"
                  colors={AI_GRADIENT_REVERSE}
                  onPress={() => setVoiceModalOpen(true)}
                />
              </View>
            )}

            {/* Type toggle */}
            <View className="flex-row bg-white rounded-xl border border-[#E8E6DF] p-1 mb-4">
              {TYPE_OPTIONS.map((t) => (
                <TouchableOpacity
                  key={t.key}
                  onPress={() => {
                    if (t.key === type) return;
                    setValue("type", t.key);
                    setValue(
                      "categoryId",
                      t.key === "INCOME" ? income[0]?.id ?? "" : defaultCategoryId(expense)
                    );
                  }}
                  className={`flex-1 py-2 rounded-lg items-center ${
                    type === t.key ? "bg-brand-bg" : ""
                  }`}
                >
                  <Text
                    className={`text-xs font-medium ${
                      type === t.key
                        ? "text-white"
                        : "text-brand-text-secondary"
                    }`}
                  >
                    {t.label}
                  </Text>
                </TouchableOpacity>
              ))}
            </View>

            {/* Amount */}
            <Text className="text-brand-bg text-xs font-medium mb-1.5">
              Amount{selectedAccount ? ` (${selectedAccount.currency})` : ""}
            </Text>
            <Controller
              control={control}
              name="amount"
              render={({ field: { value, onChange, onBlur } }) => (
                <TextInput
                  value={value}
                  onChangeText={(v) => {
                    setError("");
                    onChange(v);
                  }}
                  onBlur={onBlur}
                  placeholder="0"
                  placeholderTextColor="#8A8D96"
                  keyboardType="numeric"
                  className="bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 text-sm text-brand-bg"
                />
              )}
            />
            {errors.amount && (
              <Text className="text-brand-coral text-xs mt-1.5">
                {errors.amount.message}
              </Text>
            )}
            <View className="mb-4" />

            {/* Category */}
            <Text className="text-brand-bg text-xs font-medium mb-1.5">
              Category
            </Text>
            <View className="mb-4">
              <PillGroup
                options={categories.map((c) => ({
                  key: c.id,
                  label: c.name,
                  icon: c.icon ?? undefined,
                }))}
                value={categoryId}
                onChange={(key) => setValue("categoryId", key)}
              />
            </View>
            {errors.categoryId && (
              <Text className="text-brand-coral text-xs -mt-2 mb-3">
                {errors.categoryId.message}
              </Text>
            )}

            {/* Account */}
            <Text className="text-brand-bg text-xs font-medium mb-1.5">
              Account
            </Text>
            <View className="mb-1">
              <PillGroup
                options={accounts.map((a) => ({ key: a.id, label: a.name }))}
                value={accountId}
                onChange={(key) => setValue("accountId", key)}
              />
            </View>
            {errors.accountId && (
              <Text className="text-brand-coral text-xs mb-3">
                {errors.accountId.message}
              </Text>
            )}
            <View className="mb-3" />

            {/* Date */}
            <Text className="text-brand-bg text-xs font-medium mb-1.5">
              Date
            </Text>
            <TouchableOpacity
              onPress={() => setDatePickerOpen((v) => !v)}
              className="flex-row items-center justify-between bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 mb-1"
            >
              <Text className="text-sm text-brand-bg">
                {format(date, "d MMM yyyy")}
              </Text>
              <Feather name="calendar" size={16} color="#5C5F68" />
            </TouchableOpacity>

            {datePickerOpen && (
              <View className="bg-white border border-[#E8E6DF] rounded-xl mb-4 overflow-hidden">
                <CalendarPicker
                  value={date}
                  maximumDate={new Date()}
                  onChange={(selectedDate) => {
                    setValue("date", selectedDate);
                    setDatePickerOpen(false);
                  }}
                />
              </View>
            )}
            {!datePickerOpen && <View className="mb-4" />}

            {/* Description */}
            <Text className="text-brand-bg text-xs font-medium mb-1.5">
              Description (optional)
            </Text>
            <Controller
              control={control}
              name="description"
              render={({ field: { value, onChange, onBlur } }) => (
                <TextInput
                  value={value}
                  onChangeText={onChange}
                  onBlur={onBlur}
                  maxLength={200}
                  placeholder="e.g. Swiggy order"
                  placeholderTextColor="#8A8D96"
                  className="bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 mb-4 text-sm text-brand-bg"
                />
              )}
            />

            {error ? (
              <Text className="text-brand-coral text-xs mb-4">{error}</Text>
            ) : null}

            <TouchableOpacity
              onPress={handleSubmit(onSubmit)}
              disabled={saving}
              className="bg-brand-bg rounded-xl py-4 items-center mb-2"
              activeOpacity={0.85}
            >
              <Text className="text-white text-sm font-semibold">
                {saving ? "Saving…" : isEdit ? "Save changes" : "Save transaction"}
              </Text>
            </TouchableOpacity>
          </ScrollView>
        )}
      </KeyboardAvoidingView>

      {scanning && (
        <View className="absolute inset-0 items-center justify-center bg-black/40">
          <View className="bg-white rounded-2xl px-6 py-5 items-center">
            <ActivityIndicator color="#4A9EFF" />
            <Text className="text-brand-bg text-sm mt-3">Reading receipt…</Text>
          </View>
        </View>
      )}

      {!isEdit && (
        <>
          <VoiceRecorderModal
            visible={voiceModalOpen}
            onClose={() => setVoiceModalOpen(false)}
            onExtracted={handleVoiceExtracted}
          />
          <ReceiptScannerModal
            visible={scannerOpen}
            onClose={() => setScannerOpen(false)}
            onCaptured={handleReceiptCaptured}
          />
        </>
      )}
    </>
  );
}
