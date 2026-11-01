import { TransactionForm } from "@/components/TransactionForm";
import { useTransactionQuery } from "@/hooks/queries/useTransactionsQuery";
import { describeError } from "@/lib/api/errors";
import { Feather } from "@expo/vector-icons";
import { useLocalSearchParams, useRouter } from "expo-router";
import { ActivityIndicator, Text, TouchableOpacity, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

/** Edit a transaction (PATCH /v1/transactions/{id}); opened by tapping a row on the Transactions tab. */
export default function EditTransactionScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const { data: tx, isLoading, isError, error, refetch } = useTransactionQuery(id);

  return (
    <SafeAreaView className="flex-1 bg-brand-body" edges={["top"]}>
      <View className="flex-row items-center gap-2 px-5 pt-3 pb-2">
        <TouchableOpacity onPress={() => router.back()} hitSlop={10}>
          <Feather name="chevron-left" size={22} color="#0B0E14" />
        </TouchableOpacity>
        <Text className="text-brand-bg text-xl font-semibold">
          Edit transaction
        </Text>
      </View>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator color="#4A9EFF" />
        </View>
      ) : isError || !tx ? (
        <View className="flex-1 items-center justify-center px-10">
          <Feather name="alert-circle" size={32} color="#FF6B4A" />
          <Text className="text-brand-text-muted text-sm mt-3 text-center">
            {describeError(error, "Couldn't load this transaction.")}
          </Text>
          <TouchableOpacity
            onPress={() => refetch()}
            className="mt-4 bg-brand-bg rounded-full px-4 py-2"
          >
            <Text className="text-white text-xs font-medium">Retry</Text>
          </TouchableOpacity>
        </View>
      ) : (
        <TransactionForm editing={tx} />
      )}
    </SafeAreaView>
  );
}
