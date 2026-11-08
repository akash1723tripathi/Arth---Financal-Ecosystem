import type { InputMethod, Transaction } from "@arth/api-client";
import {
  TRANSFER_DISPLAY,
  toCategoryDisplay,
  useCategories,
} from "@/hooks/queries/useCategoriesQuery";
import { formatMinor } from "@/lib/money";
import { Feather } from "@expo/vector-icons";
import Swipeable from "react-native-gesture-handler/ReanimatedSwipeable";
import { Text, TouchableOpacity, View } from "react-native";

const INPUT_METHOD_ICON: Record<InputMethod, keyof typeof Feather.glyphMap> = {
  MANUAL: "edit-3",
  RECEIPT_SCAN: "camera",
  VOICE: "mic",
  RECURRING: "repeat",
  IMPORT: "upload",
};

export function TransactionRow({
  tx,
  onPress,
  onDelete,
}: {
  tx: Transaction;
  onPress?: () => void;
  onDelete?: () => void;
}) {
  const { byId } = useCategories();
  const isTransfer = tx.type === "TRANSFER_IN" || tx.type === "TRANSFER_OUT";
  const config = isTransfer
    ? TRANSFER_DISPLAY
    : toCategoryDisplay(tx.category_id ? byId.get(tx.category_id) : undefined);
  const isIncoming = tx.type === "INCOME" || tx.type === "TRANSFER_IN";

  const row = (
    <TouchableOpacity
      activeOpacity={onPress ? 0.7 : 1}
      disabled={!onPress}
      onPress={onPress}
      className="flex-row items-center bg-white rounded-2xl border border-[#E8E6DF] pl-3 pr-3.5 py-4"
      style={{ borderLeftWidth: 3, borderLeftColor: config.color }}
    >
      <View
        className="w-10 h-10 rounded-full items-center justify-center mr-3"
        style={{ backgroundColor: `${config.color}22` }}
      >
        <Text className="text-lg">{config.icon}</Text>
      </View>

      <View className="flex-1">
        <Text className="text-brand-bg text-sm font-medium" numberOfLines={1}>
          {tx.description || config.label}
        </Text>
        <View className="flex-row items-center gap-1.5 mt-0.5">
          <Feather
            name={INPUT_METHOD_ICON[tx.input_method]}
            size={11}
            color="#8A8D96"
          />
          <View
            className="px-1.5 py-0.5 rounded-full"
            style={{ backgroundColor: `${config.color}1A` }}
          >
            <Text className="text-[10px] font-medium" style={{ color: config.color }}>
              {config.label}
            </Text>
          </View>
        </View>
      </View>

      {/* Each row in its own currency (was always ₹ before: MIGRATION §6.5). */}
      <Text
        className={`text-sm font-medium ${
          isIncoming ? "text-brand-success" : "text-brand-coral"
        }`}
      >
        {isIncoming ? "+" : "-"}
        {formatMinor(tx.amount_minor, tx.currency)}
      </Text>
    </TouchableOpacity>
  );

  if (!onDelete) {
    return <View className="mb-2.5">{row}</View>;
  }

  return (
    <View className="mb-2.5">
      <Swipeable
        overshootRight={false}
        renderRightActions={() => (
          <TouchableOpacity
            onPress={onDelete}
            className="bg-brand-coral rounded-2xl ml-2 w-16 items-center justify-center"
          >
            <Feather name="trash-2" size={18} color="#fff" />
          </TouchableOpacity>
        )}
      >
        {row}
      </Swipeable>
    </View>
  );
}
