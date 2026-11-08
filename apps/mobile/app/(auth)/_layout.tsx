import { useSignedIn } from "@/hooks/useSignedIn";
import { Redirect, Stack } from "expo-router";

export default function AuthLayout() {
  const { isSignedIn, isLoaded } = useSignedIn();

  if (!isLoaded) return null;

  if (isSignedIn) return <Redirect href="/" />;

  return <Stack screenOptions={{ headerShown: false }} />;
}
