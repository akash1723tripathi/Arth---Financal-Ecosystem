import { AccountModal } from "@/components/AccountModal";
import { ChangePasswordModal } from "@/components/ChangePasswordModal";
import { CurrencyPicker } from "@/components/CurrencyPicker";
import { useSetDefaultAccount } from "@/hooks/mutations/useAccountMutations";
import { useLogout, useLogoutAll } from "@/hooks/mutations/useAuthMutations";
import { useAccountsQuery } from "@/hooks/queries/useAccountsQuery";
import { useMeQuery, useUserCurrency } from "@/hooks/queries/useMeQuery";
import { useUpdateMe } from "@/hooks/mutations/useMeMutations";
import { isApiError } from "@/lib/api/client";
import { Account, AccountType } from "@/lib/services/accounts";
import { formatPrice } from "@/lib/utils";
import { describeError } from "@/lib/api/errors";
import { Feather } from "@expo/vector-icons";
import { useState } from "react";
import {
  ActivityIndicator,
  Alert,
  ScrollView,
  Switch,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";

const ACCOUNT_ICON: Record<AccountType, keyof typeof Feather.glyphMap> = {
  CASH: "dollar-sign",
  BANK: "home",
  CREDIT_CARD: "credit-card",
  SAVINGS: "shield",
};

function SectionLabel({ children }: { children: string }) {
  return (
    <Text className="text-brand-text-muted text-[11px] uppercase tracking-wide mb-2 mt-6 mx-5">
      {children}
    </Text>
  );
}

function Row({
  icon,
  label,
  value,
  onPress,
  showChevron = true,
  danger = false,
}: {
  icon: keyof typeof Feather.glyphMap;
  label: string;
  value?: string;
  onPress?: () => void;
  showChevron?: boolean;
  danger?: boolean;
}) {
  return (
    <TouchableOpacity
      onPress={onPress}
      disabled={!onPress}
      className="flex-row items-center bg-white px-4 py-3.5 border-b border-[#F0EEE7] last:border-b-0"
    >
      <View className="w-8 h-8 rounded-full bg-[#F5F4F0] items-center justify-center mr-3">
        <Feather name={icon} size={15} color={danger ? "#FF6B4A" : "#5C5F68"} />
      </View>
      <Text
        className={`flex-1 text-sm ${
          danger ? "text-brand-coral" : "text-brand-bg"
        }`}
      >
        {label}
      </Text>
      {value && (
        <Text className="text-brand-text-secondary text-xs mr-2">{value}</Text>
      )}
      {showChevron && onPress && (
        <Feather name="chevron-right" size={16} color="#BDC3C7" />
      )}
    </TouchableOpacity>
  );
}

export default function ProfileScreen() {
  const { data: me } = useMeQuery();
  const { mutateAsync: logout } = useLogout();
  const { mutateAsync: logoutAll } = useLogoutAll();
  const currency = useUserCurrency();
  const { mutateAsync: updateMe } = useUpdateMe();
  const [biometricLock, setBiometricLock] = useState(false);

  const [modalVisible, setModalVisible] = useState(false);
  const [editingAccount, setEditingAccount] = useState<Account | null>(null);
  const [currencyPickerOpen, setCurrencyPickerOpen] = useState(false);
  const [changePasswordOpen, setChangePasswordOpen] = useState(false);

  const {
    data: accounts = [],
    isLoading: loadingAccounts,
    isError: accountsError,
  } = useAccountsQuery();
  const { mutateAsync: setDefaultAccount } = useSetDefaultAccount();

  // Avatar upload needs object storage (OPEN, D-OPEN-7); Me.avatar_url is always null until then.
  const handlePickAvatar = () => {
    Alert.alert("Coming soon", "Profile photos aren't available yet.");
  };

  const handleSignOut = () => {
    Alert.alert("Sign out", "Are you sure you want to sign out?", [
      { text: "Cancel", style: "cancel" },
      {
        text: "Sign out",
        style: "destructive",
        // The guards redirect to /sign-in once the session is cleared.
        onPress: () => logout(),
      },
    ]);
  };

  const handleSignOutAll = () => {
    Alert.alert(
      "Sign out of all devices",
      "This signs you out everywhere, including this phone.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Sign out all",
          style: "destructive",
          onPress: async () => {
            try {
              await logoutAll();
            } catch (err) {
              Alert.alert("Error", describeError(err, "Couldn't sign out of all devices."));
            }
          },
        },
      ]
    );
  };

  const closeModal = () => {
    setModalVisible(false);
    setEditingAccount(null);
  };

  const handleMadeDefault = async () => {
    if (!editingAccount) return;
    try {
      await setDefaultAccount(editingAccount.id);
      closeModal();
    } catch {
      Alert.alert("Error", "Couldn't set this as the default account.");
    }
  };

  const handleCurrencySelect = async (selected: { code: string }) => {
    setCurrencyPickerOpen(false);
    if (selected.code === currency) return;
    try {
      await updateMe({ currency: selected.code });
    } catch (err) {
      Alert.alert(
        "Error",
        isApiError(err, "currency_locked")
          ? "You can't change your currency after adding transactions."
          : describeError(err, "Couldn't update your currency.")
      );
    }
  };

  return (
    <SafeAreaView className="flex-1 bg-brand-body" edges={["top"]}>
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={{ paddingBottom: 100 }}
      >
        <View className="px-5 pt-3 pb-2">
          <Text className="text-brand-bg text-xl font-semibold">Profile</Text>
        </View>

        {/* User card */}
        <View className="mx-5 mt-2 bg-brand-bg rounded-2xl px-5 py-6 items-center">
          <TouchableOpacity
            onPress={handlePickAvatar}
            activeOpacity={0.8}
            className="w-20 h-20 rounded-full bg-[#1A1D26] items-center justify-center overflow-hidden border-2 border-[#2A2E3A]"
          >
            <Feather name="user" size={30} color="#8A8D96" />
            <View className="absolute bottom-0 inset-x-0 h-6 bg-black/50 items-center justify-center">
              <Feather name="camera" size={13} color="#F2EFE9" />
            </View>
          </TouchableOpacity>

          <Text className="text-white text-2xl font-bold mt-3.5">
            {me?.name ?? ""}
          </Text>
          <View className="flex-row items-center gap-1.5 mt-1">
            <Feather name="mail" size={11} color="#8A8D96" />
            <Text
              className="text-brand-text-secondary text-xs"
              numberOfLines={1}
            >
              {me?.email}
            </Text>
          </View>
        </View>

        {/* Accounts */}
        <SectionLabel>Accounts</SectionLabel>
        <View className="mx-5 rounded-2xl overflow-hidden border border-[#E8E6DF]">
          {loadingAccounts ? (
            <View className="bg-white px-4 py-5 items-center">
              <ActivityIndicator color="#5C5F68" />
            </View>
          ) : accountsError ? (
            <View className="bg-white px-4 py-5 items-center">
              <Text className="text-brand-text-muted text-xs">
                Couldn&apos;t load your accounts.
              </Text>
            </View>
          ) : (
            accounts.map((account) => (
              <Row
                key={account.id}
                icon={ACCOUNT_ICON[account.type]}
                label={account.name + (account.is_default ? " (default)" : "")}
                value={formatPrice(account.balance, currency)}
                onPress={() => {
                  setEditingAccount(account);
                  setModalVisible(true);
                }}
              />
            ))
          )}
          <Row
            icon="plus"
            label="Add account"
            onPress={() => {
              setEditingAccount(null);
              setModalVisible(true);
            }}
          />
        </View>

        {/* Preferences */}
        <SectionLabel>Preferences</SectionLabel>
        <View className="mx-5 rounded-2xl overflow-hidden border border-[#E8E6DF]">
          <Row
            icon="dollar-sign"
            label="Currency"
            value={currency}
            onPress={() => setCurrencyPickerOpen(true)}
          />

          <View className="flex-row items-center bg-white px-4 py-3.5">
            <View className="w-8 h-8 rounded-full bg-[#F5F4F0] items-center justify-center mr-3">
              <Feather name="lock" size={15} color="#5C5F68" />
            </View>
            <Text className="flex-1 text-sm text-brand-bg">Biometric lock</Text>
            <Switch value={biometricLock} onValueChange={setBiometricLock} />
          </View>
        </View>

        {/* Account actions */}
        <SectionLabel>Account</SectionLabel>
        <View className="mx-5 rounded-2xl overflow-hidden border border-[#E8E6DF]">
          <Row
            icon="key"
            label="Change password"
            onPress={() => setChangePasswordOpen(true)}
          />
          <Row
            icon="log-out"
            label="Sign out"
            onPress={handleSignOut}
            showChevron={false}
            danger
          />
          <Row
            icon="smartphone"
            label="Sign out of all devices"
            onPress={handleSignOutAll}
            showChevron={false}
            danger
          />
        </View>
      </ScrollView>

      {me && (
        <AccountModal
          visible={modalVisible}
          account={editingAccount}
          onClose={closeModal}
          onSaved={closeModal}
          onDeleted={closeModal}
          onMadeDefault={handleMadeDefault}
        />
      )}

      <ChangePasswordModal
        visible={changePasswordOpen}
        onClose={() => setChangePasswordOpen(false)}
      />

      <CurrencyPicker
        visible={currencyPickerOpen}
        selectedCode={currency}
        onSelect={handleCurrencySelect}
        onClose={() => setCurrencyPickerOpen(false)}
      />
    </SafeAreaView>
  );
}
