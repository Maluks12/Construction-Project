import { useState } from "react";
import { KeyboardAvoidingView, Platform, ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Button, Field, H, Icon, Muted, Notice, T } from "@/components/ui";
import { useSession } from "@/lib/session";
import { message } from "@/lib/use-load";
import { C, F } from "@/theme";

export default function SignIn() {
  const { signIn } = useSession();
  const insets = useSafeAreaInsets();
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit() {
    setBusy(true);
    setError("");
    try {
      await signIn(login, password);
    } catch (e) {
      setError(message(e));
      setBusy(false);
    }
  }

  return (
    <KeyboardAvoidingView style={s.root} behavior={Platform.OS === "ios" ? "padding" : undefined}>
      <ScrollView contentContainerStyle={s.scroll} keyboardShouldPersistTaps="handled">
        <View style={[s.top, { paddingTop: insets.top + 36 }]}>
          <View style={s.brand}>
            <View style={s.mark}><Icon name="shield" color={C.white} size={22} /></View>
            <H style={s.title}>SiteProof</H>
          </View>
          <T style={s.lede}>Inspections, incidents and evidence from site, even without signal.</T>
        </View>
        <View style={s.form}>
          <H style={s.h2}>Sign in</H>
          <Field label="Email or mobile number" value={login} onChangeText={setLogin} autoCapitalize="none" autoCorrect={false} keyboardType="email-address" textContentType="username" placeholder="you@company.co.za or 082 123 4567" />
          <Field label="Password" value={password} onChangeText={setPassword} secureTextEntry textContentType="password" onSubmitEditing={submit} />
          {error ? <Notice tone="bad">{error}</Notice> : null}
          <Button title="Sign in" variant="primary" busy={busy} disabled={!login || !password} onPress={submit} />
          <Muted>Got an invite by SMS or email? Open the link in it to set a password and join your team, then sign in here.</Muted>
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  root: { flex: 1, backgroundColor: C.bg },
  scroll: { flexGrow: 1 },
  top: { backgroundColor: C.header, paddingHorizontal: 22, paddingBottom: 30, gap: 14 },
  brand: { flexDirection: "row", alignItems: "center", gap: 12 },
  mark: { width: 38, height: 38, borderRadius: 9, backgroundColor: C.accent, alignItems: "center", justifyContent: "center" },
  title: { fontFamily: F.headBold, fontSize: 26, color: C.white },
  lede: { color: C.headerText, fontSize: 15, lineHeight: 22 },
  form: { padding: 22, gap: 16 },
  h2: { fontSize: 22 },
});
