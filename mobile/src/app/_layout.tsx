import { Inter_400Regular, Inter_500Medium, Inter_600SemiBold } from "@expo-google-fonts/inter";
import { SpaceGrotesk_600SemiBold, SpaceGrotesk_700Bold } from "@expo-google-fonts/space-grotesk";
import { useFonts } from "expo-font";
import { Redirect, Stack, useSegments } from "expo-router";
import * as SplashScreen from "expo-splash-screen";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";

import { SessionProvider, useSession } from "@/lib/session";
import { C } from "@/theme";

SplashScreen.preventAutoHideAsync();

// Signed-out people only ever see the sign-in screen, and signed-in people
// never see it. This is only the front door: the API checks the token on
// every request.
function Gate() {
  const { status } = useSession();
  const onSignIn = useSegments()[0] === "sign-in";

  useEffect(() => {
    if (status !== "loading") void SplashScreen.hideAsync();
  }, [status]);

  if (status === "loading") return null;
  if (status === "out" && !onSignIn) return <Redirect href="/sign-in" />;
  if (status === "in" && onSignIn) return <Redirect href="/" />;
  return <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: C.bg } }} />;
}

export default function RootLayout() {
  const [fontsLoaded] = useFonts({
    Inter_400Regular,
    Inter_500Medium,
    Inter_600SemiBold,
    SpaceGrotesk_600SemiBold,
    SpaceGrotesk_700Bold,
  });
  if (!fontsLoaded) return null;

  return (
    <SessionProvider>
      <StatusBar style="light" />
      <Gate />
    </SessionProvider>
  );
}
