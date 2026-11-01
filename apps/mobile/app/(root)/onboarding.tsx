import { ALL_CURRENCIES, CurrencyPicker } from "@/components/CurrencyPicker";
import { useCompleteOnboarding } from "@/hooks/mutations/useMeMutations";
import { isApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errors";
import { onboardingSchema, OnboardingFormValues } from "@/lib/schemas/onboarding";
import { moneyInputErrorText, parseMoneyInput } from "@/lib/money";
import { zodResolver } from "@hookform/resolvers/zod";
import { Feather } from "@expo/vector-icons";
import { router } from "expo-router";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import {
  Image,
  KeyboardAvoidingView,
  Platform,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

export default function OnboardingScreen() {
  const { mutateAsync: completeOnboarding, isPending: saving } =
    useCompleteOnboarding();

  const {
    control,
    handleSubmit,
    setError: setFieldError,
    formState: { errors: formErrors },
  } = useForm<OnboardingFormValues>({
    resolver: zodResolver(onboardingSchema),
    mode: "onBlur",
    defaultValues: { startingBalance: "" },
  });

  const [selectedCurrency, setSelectedCurrency] = useState(
    ALL_CURRENCIES.find((c) => c.code === "INR") ?? ALL_CURRENCIES[0]
  );
  const [pickerOpen, setPickerOpen] = useState(false);
  const [error, setError] = useState("");

  const handleSave = async ({ startingBalance }: OnboardingFormValues) => {
    setError("");
    // String → integer minor units in the chosen currency (e.g. "50,000.50" INR → 5000050). No floats.
    const parsed = parseMoneyInput(startingBalance, selectedCurrency.code);
    if (!parsed.ok) {
      setFieldError("startingBalance", {
        message: moneyInputErrorText(
          parsed.error,
          selectedCurrency.code,
          "Please enter a valid starting balance."
        ),
      });
      return;
    }

    try {
      // One call, one DB transaction: currency + timezone + the default account's opening balance
      // (the starting balance is no longer recorded as income).
      await completeOnboarding({
        currency: selectedCurrency.code,
        opening_balance_minor: parsed.minor,
      });
    } catch (err) {
      if (!isApiError(err, "already_onboarded")) {
        setError(
          isApiError(err, "currency_locked")
            ? "Your account already has transactions in another currency."
            : describeError(err)
        );
        return;
      }
    }
    router.replace("/(root)/(tabs)");
  };

  return (
    <SafeAreaView className="flex-1 bg-brand-body" edges={["top"]}>
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        className="flex-1"
      >
        <View className="flex-1 px-6 justify-center -mt-16">
          <Image
            source={require("../../assets/images/welth.png")}
            className="w-32 h-14 mb-10"
            resizeMode="contain"
          />
          <Text className="text-[#1A1D26] text-3xl font-bold mb-2">
            Let&apos;s get you set up
          </Text>
          <Text className="text-brand-text-muted text-sm mb-10">
            A couple of quick details to personalise your experience.
          </Text>

          {/* Starting balance */}
          <Text className="text-brand-bg text-xs font-medium mb-1.5">
            Starting balance
          </Text>
          <View className="flex-row items-center bg-white border border-[#E8E6DF] rounded-xl px-4 mb-1">
            <Text className="text-brand-text-secondary text-sm mr-2">
              {selectedCurrency.symbol}
            </Text>
            <Controller
              control={control}
              name="startingBalance"
              render={({ field: { value, onChange } }) => (
                <TextInput
                  value={value}
                  onChangeText={(v) => {
                    setError("");
                    onChange(v);
                  }}
                  placeholder="e.g. 50000"
                  placeholderTextColor="#8A8D96"
                  keyboardType="numeric"
                  returnKeyType="done"
                  className="flex-1 py-3.5 text-sm text-brand-bg"
                />
              )}
            />
          </View>
          {formErrors.startingBalance && (
            <Text className="text-brand-coral text-xs mb-4">
              {formErrors.startingBalance.message}
            </Text>
          )}
          <View className="mb-4" />

          {/* Currency picker */}
          <Text className="text-brand-bg text-xs font-medium mb-1.5">
            Currency
          </Text>
          <TouchableOpacity
            onPress={() => setPickerOpen(true)}
            className="flex-row items-center justify-between bg-white border border-[#E8E6DF] rounded-xl px-4 py-3.5 mb-6"
          >
            <Text className="text-sm text-brand-bg">
              {selectedCurrency.symbol} {selectedCurrency.code} —{" "}
              {selectedCurrency.name}
            </Text>
            <Feather name="chevron-down" size={16} color="#8A8D96" />
          </TouchableOpacity>

          {error ? (
            <Text className="text-brand-coral text-xs mb-4">{error}</Text>
          ) : null}

          <TouchableOpacity
            onPress={handleSubmit(handleSave)}
            disabled={saving}
            className="bg-brand-bg rounded-xl py-4 items-center"
            activeOpacity={0.85}
          >
            <Text className="text-white text-sm font-semibold">
              {saving ? "Saving…" : "Get started"}
            </Text>
          </TouchableOpacity>
        </View>
      </KeyboardAvoidingView>

      <CurrencyPicker
        visible={pickerOpen}
        selectedCode={selectedCurrency.code}
        onSelect={(currency) => {
          setSelectedCurrency(currency);
          setPickerOpen(false);
        }}
        onClose={() => setPickerOpen(false)}
      />
    </SafeAreaView>
  );
}
