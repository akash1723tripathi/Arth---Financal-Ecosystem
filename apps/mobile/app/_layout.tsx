import { restoreSession } from "@/lib/api/session";
import { BACKEND } from "@/lib/config";
import { queryClient } from "@/lib/query/client";
import { ClerkProvider } from "@clerk/expo";
import { tokenCache } from "@clerk/expo/token-cache";
import { QueryClientProvider } from "@tanstack/react-query";
import { Slot } from "expo-router";
import { useEffect } from "react";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import "../global.css";

// LEGACY (EXPO_PUBLIC_BACKEND=supabase): removed with Clerk.
function LegacyClerkRoot({ children }: { children: React.ReactNode }) {
  const publishableKey = process.env.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY;
  if (!publishableKey) {
    throw new Error("Add your Clerk Publishable Key to the .env file");
  }
  return (
    <ClerkProvider publishableKey={publishableKey} tokenCache={tokenCache}>
      {children}
    </ClerkProvider>
  );
}

/** Own auth: restore the session from secure storage once at start (status "loading" until done). */
function AuthBootstrap({ children }: { children: React.ReactNode }) {
  useEffect(() => {
    restoreSession();
  }, []);
  return <>{children}</>;
}

const AuthRoot = BACKEND === "supabase" ? LegacyClerkRoot : AuthBootstrap;

export default function RootLayout() {
  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={queryClient}>
        <AuthRoot>
          <Slot />
        </AuthRoot>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
