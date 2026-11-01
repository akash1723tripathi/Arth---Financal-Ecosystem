import { useSignedIn } from "@/hooks/useSignedIn";
import { Redirect } from "expo-router";

export default function Index() {
  const { isSignedIn, isLoaded } = useSignedIn();

  if (!isLoaded) return null;

  if (isSignedIn) return <Redirect href="/(root)/(tabs)" />;

  return <Redirect href="/sign-in" />;
}
