import { TransactionForm } from "@/components/TransactionForm";
import { Text, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

export default function AddTransactionScreen() {
  return (
    <SafeAreaView className="flex-1 bg-brand-body" edges={["top"]}>
      <View className="px-5 pt-3 pb-2">
        <Text className="text-brand-bg text-xl font-semibold">
          Add transaction
        </Text>
      </View>
      <TransactionForm />
    </SafeAreaView>
  );
}
