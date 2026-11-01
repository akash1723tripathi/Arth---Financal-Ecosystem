import type { Transaction } from "@arth/api-client";
import { TransactionRow } from "@/components/TransactionRow";
import { useAccountsQuery } from "@/hooks/queries/useAccountsQuery";
import { useDailyReportQuery } from "@/hooks/queries/useReportsQuery";
import { useTransactionsInfiniteQuery } from "@/hooks/queries/useTransactionsQuery";
import { useDeleteTransaction } from "@/hooks/mutations/useTransactionMutations";
import { describeError } from "@/lib/api/errors";
import { Feather } from "@expo/vector-icons";
import { useRouter } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  FlatList,
  RefreshControl,
  ScrollView,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { format, parse, startOfMonth } from "date-fns";
import { BarChart } from "react-native-gifted-charts";
import { SafeAreaView } from "react-native-safe-area-context";

const FILTERS = ["All", "Income", "Expense"] as const;

/** Debounce the search box so the server-side `q` filter isn't hit on every keystroke. */
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return debounced;
}

export default function TransactionsScreen() {
  const router = useRouter();

  const [activeFilter, setActiveFilter] =
    useState<(typeof FILTERS)[number]>("All");
  const [activeAccountId, setActiveAccountId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [exporting, setExporting] = useState(false);
  const q = useDebounced(search.trim(), 300);

  const typeFilter: "INCOME" | "EXPENSE" | null =
    activeFilter === "Income"
      ? "INCOME"
      : activeFilter === "Expense"
      ? "EXPENSE"
      : null;

  const {
    data,
    isLoading: transactionsLoading,
    isRefetching: transactionsRefetching,
    isError: transactionsError,
    refetch: refetchTransactions,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useTransactionsInfiniteQuery({ type: typeFilter, accountId: activeAccountId, q });
  const { data: accounts = [], refetch: refetchAccounts } = useAccountsQuery();
  const { mutateAsync: removeTransaction } = useDeleteTransaction();

  const transactions = useMemo(
    () => data?.pages.flatMap((page) => page.data) ?? [],
    [data]
  );

  // Chart: this month (1st → today), server-side daily totals for the selected account.
  const today = new Date();
  const from = format(startOfMonth(today), "yyyy-MM-dd");
  const to = format(today, "yyyy-MM-dd");
  const { data: daily = [], refetch: refetchDaily } = useDailyReportQuery(from, to, activeAccountId);

  const loading = transactionsLoading;
  const refreshing = transactionsRefetching && !isFetchingNextPage;
  const error = transactionsError;

  const loadData = () => {
    refetchTransactions();
    refetchAccounts();
    refetchDaily();
  };

  // Bar heights are minor units straight from the API (display only). The type filter hides the other series.
  const dailyIncomeExpense = useMemo(
    () =>
      daily.flatMap((d) => [
        {
          value: typeFilter === "EXPENSE" ? 0 : d.income_minor,
          label: format(parse(d.date, "yyyy-MM-dd", new Date()), "d MMM"),
          frontColor: "#3DDC84",
        },
        { value: typeFilter === "INCOME" ? 0 : d.expense_minor, frontColor: "#FF6B4A" },
      ]),
    [daily, typeFilter]
  );

  const handleExport = async () => {
    if (exporting) return;
    setExporting(true);
    try {
      Alert.alert("Export", "CSV export moves to the server in the next step.");
    } finally {
      setExporting(false);
    }
  };

  const handleDelete = (tx: Transaction) => {
    Alert.alert(
      "Delete transaction",
      "Are you sure you want to delete this transaction?",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: async () => {
            try {
              await removeTransaction(tx.id);
            } catch (err) {
              Alert.alert("Error", describeError(err, "Couldn't delete this transaction."));
            }
          },
        },
      ]
    );
  };

  const isTransfer = (tx: Transaction) =>
    tx.type === "TRANSFER_IN" || tx.type === "TRANSFER_OUT";

  return (
    <SafeAreaView className="flex-1 bg-brand-body" edges={["top"]}>
      <View className="px-5 pt-3 pb-2">
        <View className="flex-row items-center justify-between mb-3">
          <Text className="text-brand-bg text-xl font-semibold">
            Transactions
          </Text>
          <TouchableOpacity
            onPress={handleExport}
            disabled={exporting}
            className="w-9 h-9 rounded-full bg-white border border-[#E8E6DF] items-center justify-center"
          >
            {exporting ? (
              <ActivityIndicator size="small" color="#5C5F68" />
            ) : (
              <Feather name="download" size={15} color="#5C5F68" />
            )}
          </TouchableOpacity>
        </View>

        <View className="flex-row items-center gap-2 bg-white rounded-xl border border-[#E8E6DF] px-3.5 py-2.5 mb-2.5">
          <Feather name="search" size={15} color="#8A8D96" />
          <TextInput
            value={search}
            onChangeText={setSearch}
            placeholder="Search transactions"
            placeholderTextColor="#8A8D96"
            className="flex-1 text-xs text-brand-bg"
          />
          {search.length > 0 && (
            <TouchableOpacity onPress={() => setSearch("")}>
              <Feather name="x" size={15} color="#8A8D96" />
            </TouchableOpacity>
          )}
        </View>

        <View className="flex-row gap-2 mb-2.5">
          {FILTERS.map((filter) => (
            <TouchableOpacity
              key={filter}
              onPress={() => setActiveFilter(filter)}
              className={`px-3.5 py-1.5 rounded-full border ${
                activeFilter === filter
                  ? "bg-brand-bg border-brand-bg"
                  : "bg-white border-[#E8E6DF]"
              }`}
            >
              <Text
                className={`text-xs ${
                  activeFilter === filter
                    ? "text-white"
                    : "text-brand-text-secondary"
                }`}
              >
                {filter}
              </Text>
            </TouchableOpacity>
          ))}
        </View>

        <ScrollView horizontal showsHorizontalScrollIndicator={false}>
          <View className="flex-row gap-2">
            <TouchableOpacity
              onPress={() => setActiveAccountId(null)}
              className={`px-3.5 py-1.5 rounded-full border ${
                activeAccountId === null
                  ? "bg-brand-bg border-brand-bg"
                  : "bg-white border-[#E8E6DF]"
              }`}
            >
              <Text
                className={`text-xs ${
                  activeAccountId === null
                    ? "text-white"
                    : "text-brand-text-secondary"
                }`}
              >
                All Accounts
              </Text>
            </TouchableOpacity>
            {accounts.map((account) => (
              <TouchableOpacity
                key={account.id}
                onPress={() => setActiveAccountId(account.id)}
                className={`px-3.5 py-1.5 rounded-full border ${
                  activeAccountId === account.id
                    ? "bg-brand-bg border-brand-bg"
                    : "bg-white border-[#E8E6DF]"
                }`}
              >
                <Text
                  className={`text-xs ${
                    activeAccountId === account.id
                      ? "text-white"
                      : "text-brand-text-secondary"
                  }`}
                >
                  {account.name}
                </Text>
              </TouchableOpacity>
            ))}
          </View>
        </ScrollView>
      </View>

      {loading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator color="#4A9EFF" />
        </View>
      ) : error ? (
        <View className="flex-1 items-center justify-center px-10">
          <Feather name="alert-circle" size={32} color="#FF6B4A" />
          <Text className="text-brand-text-muted text-sm mt-3 text-center">
            Couldn&apos;t load transactions.
          </Text>
          <TouchableOpacity
            onPress={() => loadData()}
            className="mt-4 bg-brand-bg rounded-full px-4 py-2"
          >
            <Text className="text-white text-xs font-medium">Retry</Text>
          </TouchableOpacity>
        </View>
      ) : (
        <FlatList
          data={transactions}
          keyExtractor={(item) => item.id}
          renderItem={({ item }) => (
            <TransactionRow
              tx={item}
              onPress={
                isTransfer(item)
                  ? undefined
                  : () =>
                      router.push({
                        pathname: "/(root)/transaction/[id]",
                        params: { id: item.id },
                      })
              }
              onDelete={() => handleDelete(item)}
            />
          )}
          contentContainerStyle={{
            paddingHorizontal: 20,
            paddingTop: 8,
            paddingBottom: 100,
          }}
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) fetchNextPage();
          }}
          ListFooterComponent={
            isFetchingNextPage ? (
              <View className="py-4 items-center">
                <ActivityIndicator color="#4A9EFF" />
              </View>
            ) : null
          }
          refreshControl={
            <RefreshControl refreshing={refreshing} onRefresh={loadData} />
          }
          ListHeaderComponent={
            transactions.length > 0 ? (
              <View className="bg-white rounded-2xl border border-[#E8E6DF] p-4 mb-4">
                <View className="flex-row justify-between items-center mb-3">
                  <Text className="text-brand-bg text-xs font-medium">
                    Daily income vs expense
                  </Text>
                  <View className="flex-row gap-3">
                    <View className="flex-row items-center gap-1">
                      <View className="w-2 h-2 rounded-full bg-brand-success" />
                      <Text className="text-[10px] text-brand-text-secondary">
                        Income
                      </Text>
                    </View>
                    <View className="flex-row items-center gap-1">
                      <View className="w-2 h-2 rounded-full bg-brand-coral" />
                      <Text className="text-[10px] text-brand-text-secondary">
                        Expense
                      </Text>
                    </View>
                  </View>
                </View>
                <ScrollView horizontal showsHorizontalScrollIndicator={false}>
                  <BarChart
                    data={dailyIncomeExpense}
                    width={Math.max(dailyIncomeExpense.length * 9, 280)}
                    height={120}
                    barWidth={6}
                    spacing={4}
                    hideYAxisText
                    xAxisColor="#E8E6DF"
                    yAxisColor="transparent"
                    rulesColor="#F0EEE7"
                    noOfSections={3}
                    xAxisLabelTextStyle={{ color: "#8A8D96", fontSize: 7 }}
                    isThreeD={false}
                    roundedTop
                  />
                </ScrollView>
              </View>
            ) : null
          }
          ListEmptyComponent={
            <View className="items-center justify-center py-20">
              <Feather name="inbox" size={32} color="#BDC3C7" />
              <Text className="text-brand-text-muted text-sm mt-3">
                {search ? "No matching transactions" : "No transactions yet"}
              </Text>
            </View>
          }
        />
      )}

      {/* Add transaction FAB - navigates to the Add tab */}
      <TouchableOpacity
        onPress={() => router.push("/(root)/(tabs)/add-transaction")}
        className="absolute right-5 w-14 h-14 rounded-full bg-brand-bg items-center justify-center shadow-lg"
        style={{ bottom: 90 }}
        activeOpacity={0.85}
      >
        <Feather name="plus" size={24} color="#fff" />
      </TouchableOpacity>
    </SafeAreaView>
  );
}
