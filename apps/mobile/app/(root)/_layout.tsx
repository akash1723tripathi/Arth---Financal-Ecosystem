import { useMeQuery } from "@/hooks/queries/useMeQuery";
import { useSignedIn } from "@/hooks/useSignedIn";
import { useUserSync } from "@/hooks/useUserSync";
import { describeError } from "@/lib/api/errors";
import { API_URL, BACKEND } from "@/lib/config";
import { useUserStore } from "@/store/userStore";
import { Feather } from "@expo/vector-icons";
import { Redirect, Slot, usePathname } from "expo-router";
import { useEffect, useState } from "react";
import { ActivityIndicator, Text, TouchableOpacity, View } from "react-native";

function useMinimumSplash() {
  const [minLoadDone, setMinLoadDone] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setMinLoadDone(true), 1500);
    return () => clearTimeout(t);
  }, []);
  return minLoadDone;
}

function Splash() {
  return (
    <View className="flex-1 bg-brand-body items-center justify-center">
      <ActivityIndicator size="large" color="#1A1D26" />
    </View>
  );
}

/** Own auth: gate on the session, then on GET /v1/me → needs_onboarding. */
function RootGroupLayout() {
  const { isSignedIn, isLoaded } = useSignedIn();
  const pathname = usePathname();
  const minLoadDone = useMinimumSplash();
  const { data: me, isError, error, refetch, isRefetching } = useMeQuery();

  if (!isLoaded) return null;
  if (!isSignedIn) return <Redirect href="/sign-in" />;
  if (!me && isError) {
    return (
      <View className="flex-1 bg-brand-body items-center justify-center px-10">
        <Feather name="alert-circle" size={32} color="#FF6B4A" />
        <Text className="text-brand-text-muted text-sm mt-3 text-center">
          {describeError(error, "Couldn't load your profile.")}
        </Text>
        {__DEV__ && (
          <Text className="text-brand-text-muted text-[11px] mt-2 text-center">
            DEV · API: {API_URL}
          </Text>
        )}
        <TouchableOpacity
          onPress={() => refetch()}
          disabled={isRefetching}
          className="mt-4 bg-brand-bg rounded-full px-4 py-2"
        >
          <Text className="text-white text-xs font-medium">
            {isRefetching ? "Retrying…" : "Retry"}
          </Text>
        </TouchableOpacity>
      </View>
    );
  }
  if (!minLoadDone || !me) return <Splash />;
  if (me.needs_onboarding && pathname !== "/onboarding") {
    return <Redirect href="/(root)/onboarding" />;
  }

  return <Slot />;
}

// LEGACY (EXPO_PUBLIC_BACKEND=supabase): Clerk gate + useUserSync. Removed with Clerk.
function LegacyRootGroupLayout() {
  const { isSignedIn, isLoaded } = useSignedIn();
  const needsOnboarding = useUserStore((state) => state.needsOnboarding);
  const pathname = usePathname();
  const minLoadDone = useMinimumSplash();

  useUserSync();

  if (!isLoaded) return null;
  if (!isSignedIn) return <Redirect href="/sign-in" />;
  if (!minLoadDone || needsOnboarding === null) return <Splash />;
  if (needsOnboarding && pathname !== "/onboarding") return <Redirect href="/(root)/onboarding" />;

  return <Slot />;
}

export default BACKEND === "supabase" ? LegacyRootGroupLayout : RootGroupLayout;
