import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { Button, Card, Chip, Field, H, Muted, Notice, SampleNote, Screen, T } from "@/components/ui";
import { diary } from "@/lib/sample";
import { useSession } from "@/lib/session";
import { C, F } from "@/theme";

// Three things are enough for a day; everything under "More detail" is
// optional and folded away until someone wants it.
export default function Diary() {
  const { site } = useSession();
  const [weather, setWeather] = useState("Thunderstorms");
  const [open, setOpen] = useState<Record<string, boolean>>({});

  return (
    <Screen back title={`Site diary · ${diary.day}`} sub={`${site?.name ?? "Site"} · closes at midnight`} footer={<Button title="Save diary" variant="primary" disabled />}>
      <SampleNote needs="the site diary, M10" />
      <Notice>Three things and you&apos;re done. Add more below if the day needs it.</Notice>

      <View style={s.gap}>
        <T style={s.label}>Weather</T>
        <View style={s.chips}>
          {diary.weather.map((w) => <Chip key={w} title={w} on={weather === w} onPress={() => setWeather(w)} />)}
        </View>
      </View>
      <Field label="Headcount" defaultValue={diary.headcount} keyboardType="number-pad" hint={diary.headcountHint} />
      <Field label="Work done" defaultValue={diary.work} multiline />

      <H>More detail · optional</H>
      {diary.sections.map((sec) => (
        <Card key={sec.key}>
          <Pressable accessibilityRole="button" accessibilityState={{ expanded: !!open[sec.key] }} onPress={() => setOpen({ ...open, [sec.key]: !open[sec.key] })} style={s.head}>
            <Text style={s.plus}>{open[sec.key] ? "−" : "+"}</Text>
            <T style={[s.label, s.grow]}>{sec.title}</T>
            <Muted>{sec.summary}</Muted>
          </Pressable>
          {open[sec.key] &&
            sec.rows.map(([name, value]) => (
              <View key={name} style={s.row}>
                <T style={s.grow}>{name}</T>
                <Muted style={s.value}>{value}</Muted>
              </View>
            ))}
        </Card>
      ))}
      <Muted>
        Today&apos;s incidents and toolbox talks are linked automatically. After midnight the diary closes; anything added
        later is a dated addendum, and the original stays as it was.
      </Muted>
    </Screen>
  );
}

const s = StyleSheet.create({
  grow: { flex: 1 },
  gap: { gap: 8 },
  label: { fontFamily: F.semi, fontSize: 14 },
  chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  head: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 14, minHeight: 52 },
  plus: { fontFamily: F.semi, fontSize: 20, color: C.accent, width: 16, textAlign: "center" },
  row: { flexDirection: "row", gap: 10, paddingHorizontal: 14, paddingVertical: 10, borderTopWidth: 1, borderTopColor: C.lineSoft },
  value: { flexShrink: 1, textAlign: "right" },
});
