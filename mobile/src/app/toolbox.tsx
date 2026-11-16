import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { Button, Card, Muted, SampleNote, Screen, T } from "@/components/ui";
import { toolboxTalk } from "@/lib/sample";
import { C, F, TONES } from "@/theme";

export default function Toolbox() {
  const [signed, setSigned] = useState(toolboxTalk.signed);
  const count = Object.keys(signed).length;
  const total = toolboxTalk.people.length;

  return (
    <Screen
      tab="Toolbox"
      eyebrow={toolboxTalk.when}
      title={toolboxTalk.topic}
      footer={<Button title="Close talk" disabled />}
    >
      <SampleNote needs="toolbox talks, M10" />
      <View style={s.gap}>
        <View style={s.track}><View style={[s.bar, { width: `${Math.round((count / total) * 100)}%` }]} /></View>
        <Muted>{count} of {total} signed</Muted>
      </View>
      <Card>
        {toolboxTalk.people.map(([name, firm], i) => (
          <View key={name} style={s.row}>
            <View style={s.avatar}><Text style={s.initials}>{name.split(" ").map((w) => w[0]).join("")}</Text></View>
            <View style={s.grow}>
              <T style={s.name}>{name}</T>
              <Muted>{firm}</Muted>
            </View>
            {signed[i] ? (
              <Text style={s.signed}>Signed {signed[i]}</Text>
            ) : (
              <Pressable accessibilityRole="button" onPress={() => setSigned({ ...signed, [i]: "now" })} style={s.sign}>
                <Text style={s.signText}>Tap to sign</Text>
              </Pressable>
            )}
          </View>
        ))}
      </Card>
      <Muted>Unsigned people are recorded as absent or refused.</Muted>
    </Screen>
  );
}

const s = StyleSheet.create({
  grow: { flex: 1 },
  gap: { gap: 6 },
  track: { height: 8, borderRadius: 4, backgroundColor: C.lineSoft, overflow: "hidden" },
  bar: { height: 8, backgroundColor: C.header },
  row: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 14, paddingVertical: 10, minHeight: 60, borderTopWidth: 1, borderTopColor: C.lineSoft },
  avatar: { width: 38, height: 38, borderRadius: 19, backgroundColor: TONES.neutral.bg, alignItems: "center", justifyContent: "center" },
  initials: { fontFamily: F.semi, fontSize: 13, color: TONES.neutral.fg },
  name: { fontFamily: F.semi },
  signed: { fontFamily: F.semi, fontSize: 13, color: TONES.ok.fg },
  sign: { minHeight: 44, justifyContent: "center", paddingHorizontal: 14, borderRadius: 10, borderWidth: 1, borderColor: C.accent },
  signText: { fontFamily: F.semi, fontSize: 14, color: C.accent },
});
