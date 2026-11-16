// The phone app's building blocks, styled after the SiteProof prototype:
// dark header, white cards on warm paper, pill labels, 52pt buttons that are
// easy to hit with gloves on.

import { Link, useRouter, type Href } from "expo-router";
import type { ReactNode } from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  type TextInputProps,
  type TextProps,
  type ViewStyle,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import Svg, { Path } from "react-native-svg";

import { C, F, ICONS, TONES, type Tone } from "@/theme";

export function Icon({ name, size = 22, color = C.ink, width = 2 }: { name: keyof typeof ICONS; size?: number; color?: string; width?: number }) {
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth={width} strokeLinecap="round" strokeLinejoin="round">
      <Path d={ICONS[name]} />
    </Svg>
  );
}

export function T({ style, ...rest }: TextProps) {
  return <Text {...rest} style={[s.text, style]} />;
}
export const H = ({ style, ...rest }: TextProps) => <Text accessibilityRole="header" {...rest} style={[s.h, style]} />;
export const Muted = ({ style, ...rest }: TextProps) => <Text {...rest} style={[s.muted, style]} />;

export function Pill({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <View style={[s.pill, { backgroundColor: TONES[tone].bg }]}>
      <Text style={[s.pillText, { color: TONES[tone].fg }]}>{children}</Text>
    </View>
  );
}

export function Card({ title, children, style }: { title?: string; children: ReactNode; style?: ViewStyle }) {
  return (
    <View style={[s.card, style]}>
      {title && <H style={s.cardTitle}>{title}</H>}
      {children}
    </View>
  );
}

// A tappable row inside a card: title, grey line under it, pill on the right.
export function Row({ title, sub, right, href, onPress }: { title: string; sub?: string; right?: ReactNode; href?: Href; onPress?: () => void }) {
  const body = (
    <View style={s.row}>
      <View style={s.grow}>
        <T style={s.rowTitle}>{title}</T>
        {sub ? <Muted>{sub}</Muted> : null}
      </View>
      {right}
    </View>
  );
  if (href) {
    return (
      <Link href={href} asChild>
        <Pressable accessibilityRole="link">{body}</Pressable>
      </Link>
    );
  }
  return onPress ? <Pressable accessibilityRole="button" onPress={onPress}>{body}</Pressable> : body;
}

type ButtonProps = {
  title: string;
  onPress?: () => void;
  variant?: "primary" | "secondary" | "danger" | "now";
  busy?: boolean;
  disabled?: boolean;
  icon?: keyof typeof ICONS;
  style?: ViewStyle;
};

export function Button({ title, onPress, variant = "secondary", busy, disabled, icon, style }: ButtonProps) {
  const filled = variant === "primary" || variant === "now";
  const bg = variant === "primary" ? C.accent : variant === "now" ? C.critical : C.surface;
  const fg = filled ? C.white : variant === "danger" ? TONES.bad.fg : C.ink;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: disabled || busy, busy }}
      disabled={disabled || busy}
      onPress={onPress}
      style={({ pressed }) => [s.btn, { backgroundColor: bg, borderColor: filled ? bg : C.lineStrong, opacity: disabled ? 0.5 : pressed ? 0.85 : 1 }, style]}
    >
      {busy ? <ActivityIndicator color={fg} /> : icon ? <Icon name={icon} color={fg} size={20} /> : null}
      <Text style={[s.btnText, { color: fg }]}>{title}</Text>
    </Pressable>
  );
}

export function Field({ label: text, hint, ...input }: { label: string; hint?: string } & TextInputProps) {
  return (
    <View style={s.field}>
      <T style={s.label}>{text}</T>
      <TextInput
        accessibilityLabel={text}
        placeholderTextColor={C.muted}
        {...input}
        style={[s.input, input.multiline && s.inputMulti, input.style]}
      />
      {hint ? <Muted style={s.hint}>{hint}</Muted> : null}
    </View>
  );
}

// Tap-to-toggle choice, used for incident types, weather and yes/no answers.
export function Chip({ title, on, onPress }: { title: string; on: boolean; onPress: () => void }) {
  return (
    <Pressable
      accessibilityRole="checkbox"
      accessibilityState={{ checked: on }}
      onPress={onPress}
      style={[s.chip, on && s.chipOn]}
    >
      <Text style={[s.chipText, on && s.chipTextOn]}>{title}</Text>
    </Pressable>
  );
}

export function Notice({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return (
    <View style={[s.notice, { backgroundColor: TONES[tone].bg }]}>
      <Text style={[s.noticeText, { color: TONES[tone].fg }]}>{children}</Text>
    </View>
  );
}

// On every screen that isn't connected to the API yet.
export function SampleNote({ needs }: { needs: string }) {
  return (
    <Notice tone="warn">
      Sample data. This screen shows the planned layout and isn&apos;t connected yet ({needs}). Nothing here is saved.
    </Notice>
  );
}

export function PhotoStrip({ photos, onAdd, busy }: { photos: { id: string; note: string }[]; onAdd?: () => void; busy?: boolean }) {
  return (
    <View style={s.photos}>
      {photos.map((p) => (
        <View key={p.id} style={s.photo}>
          <View style={s.thumb}><Icon name="camera" color={C.muted} /></View>
          <Muted style={s.photoNote}>{p.note}</Muted>
        </View>
      ))}
      {onAdd && (
        <Pressable accessibilityRole="button" accessibilityLabel="Take a photo" disabled={busy} onPress={onAdd} style={[s.thumb, s.addPhoto]}>
          {busy ? <ActivityIndicator color={C.accent} /> : <Text style={s.addPhotoText}>+ Photo</Text>}
        </Pressable>
      )}
    </View>
  );
}

const TABS: { label: string; href: Href; icon: keyof typeof ICONS }[] = [
  { label: "Today", href: "/", icon: "home" },
  { label: "Inspect", href: "/inspect", icon: "check" },
  { label: "Report", href: "/report", icon: "warn" },
  { label: "Toolbox", href: "/toolbox", icon: "box" },
];

function TabBar({ active }: { active: string }) {
  const insets = useSafeAreaInsets();
  const router = useRouter();
  return (
    <View accessibilityRole="tablist" style={[s.tabs, { paddingBottom: Math.max(insets.bottom, 6) }]}>
      {TABS.map((t) => {
        const on = t.label === active;
        return (
          // replace, not push: tabs swap places instead of stacking up.
          <Pressable key={t.label} accessibilityRole="tab" accessibilityState={{ selected: on }} onPress={() => router.replace(t.href)} style={s.tab}>
            <Icon name={t.icon} color={on ? C.accent : C.muted} />
            <Text style={[s.tabText, on && s.tabTextOn]}>{t.label}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

// Every screen: the dark header, a scrolling body, then either the tab bar
// (the four main screens) or a pinned footer of buttons.
export function Screen({
  title,
  eyebrow,
  sub,
  back,
  headerRight,
  tab,
  footer,
  children,
}: {
  title: string;
  eyebrow?: string;
  sub?: ReactNode;
  back?: boolean;
  headerRight?: ReactNode;
  tab?: string;
  footer?: ReactNode;
  children: ReactNode;
}) {
  const insets = useSafeAreaInsets();
  const router = useRouter();
  return (
    <View style={s.screen}>
      <View style={[s.header, { paddingTop: insets.top + 14 }]}>
        {eyebrow ? <Text style={s.eyebrow}>{eyebrow}</Text> : null}
        <View style={s.headerRow}>
          {back && (
            <Pressable accessibilityRole="button" accessibilityLabel="Back" onPress={() => (router.canGoBack() ? router.back() : router.replace("/"))} style={s.headerBtn}>
              <Icon name="back" color={C.white} />
            </Pressable>
          )}
          <Text accessibilityRole="header" style={[s.headerTitle, s.grow]} numberOfLines={2}>{title}</Text>
          {headerRight}
        </View>
        {typeof sub === "string" ? <Text style={s.headerSub}>{sub}</Text> : sub}
      </View>
      <ScrollView style={s.grow} contentContainerStyle={s.body} keyboardShouldPersistTaps="handled">
        {children}
      </ScrollView>
      {footer ? <View style={[s.footer, { paddingBottom: Math.max(insets.bottom, 12) }]}>{footer}</View> : null}
      {tab ? <TabBar active={tab} /> : null}
    </View>
  );
}

export function HeaderButton({ icon, label: text, onPress, dot }: { icon: keyof typeof ICONS; label: string; onPress: () => void; dot?: boolean }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={text} onPress={onPress} style={s.headerBtn}>
      <Icon name={icon} color={C.white} />
      {dot && <View style={s.dot} />}
    </Pressable>
  );
}

// What a loading or failed screen shows in place of its content.
export function Loading({ error, onRetry }: { error?: string; onRetry?: () => void }) {
  if (error) {
    return (
      <View style={s.gap}>
        <Notice tone="bad">{error}</Notice>
        {onRetry && <Button title="Try again" onPress={onRetry} />}
      </View>
    );
  }
  return <ActivityIndicator color={C.accent} style={s.spinner} />;
}

const s = StyleSheet.create({
  grow: { flex: 1 },
  gap: { gap: 12 },
  spinner: { marginTop: 40 },
  text: { fontFamily: F.body, fontSize: 15, color: C.ink },
  h: { fontFamily: F.head, fontSize: 16, color: C.ink },
  muted: { fontFamily: F.body, fontSize: 13, color: C.muted },
  pill: { borderRadius: 999, paddingHorizontal: 10, paddingVertical: 3, alignSelf: "flex-start" },
  pillText: { fontFamily: F.semi, fontSize: 12 },
  card: { backgroundColor: C.surface, borderWidth: 1, borderColor: C.line, borderRadius: 12, overflow: "hidden" },
  cardTitle: { paddingHorizontal: 14, paddingTop: 14, paddingBottom: 10 },
  row: { flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 14, paddingVertical: 12, minHeight: 56, borderTopWidth: 1, borderTopColor: C.lineSoft },
  rowTitle: { fontFamily: F.semi },
  btn: { minHeight: 52, borderRadius: 10, borderWidth: 1, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8, paddingHorizontal: 16 },
  btnText: { fontFamily: F.semi, fontSize: 16 },
  field: { gap: 6 },
  label: { fontFamily: F.semi, fontSize: 14 },
  hint: { fontSize: 12 },
  input: { minHeight: 48, borderWidth: 1, borderColor: C.lineStrong, borderRadius: 10, paddingHorizontal: 12, paddingVertical: 10, fontFamily: F.body, fontSize: 15, color: C.ink, backgroundColor: C.surface },
  inputMulti: { minHeight: 96, textAlignVertical: "top" },
  chip: { minHeight: 44, justifyContent: "center", borderWidth: 1, borderColor: C.lineStrong, borderRadius: 999, paddingHorizontal: 14, backgroundColor: C.surface },
  chipOn: { borderColor: C.accent, backgroundColor: C.accentSoft },
  chipText: { fontFamily: F.medium, fontSize: 14, color: C.ink },
  chipTextOn: { fontFamily: F.semi, color: C.accent },
  notice: { borderRadius: 10, paddingHorizontal: 12, paddingVertical: 10 },
  noticeText: { fontFamily: F.body, fontSize: 13, lineHeight: 19 },
  photos: { flexDirection: "row", flexWrap: "wrap", gap: 10 },
  photo: { width: 84 },
  photoNote: { fontSize: 11, marginTop: 4 },
  thumb: { width: 84, height: 64, borderRadius: 8, backgroundColor: C.thumb, alignItems: "center", justifyContent: "center" },
  addPhoto: { backgroundColor: C.surface, borderWidth: 1, borderColor: C.lineStrong, borderStyle: "dashed" },
  addPhotoText: { fontFamily: F.semi, fontSize: 13, color: C.accent },
  screen: { flex: 1, backgroundColor: C.bg },
  header: { backgroundColor: C.header, paddingHorizontal: 18, paddingBottom: 16 },
  eyebrow: { fontFamily: F.body, fontSize: 12, color: C.headerMuted, textTransform: "uppercase", letterSpacing: 0.7 },
  headerRow: { flexDirection: "row", alignItems: "center", gap: 10, marginTop: 2 },
  headerTitle: { fontFamily: F.head, fontSize: 22, color: C.white },
  headerSub: { fontFamily: F.body, fontSize: 13, color: C.headerText, marginTop: 6 },
  headerBtn: { width: 44, height: 44, borderWidth: 1, borderColor: C.headerLine, borderRadius: 10, alignItems: "center", justifyContent: "center" },
  dot: { position: "absolute", top: 6, right: 6, width: 9, height: 9, borderRadius: 5, backgroundColor: C.accentHi },
  body: { padding: 16, gap: 14 },
  footer: { backgroundColor: C.surface, borderTopWidth: 1, borderTopColor: C.line, paddingHorizontal: 16, paddingTop: 12, gap: 10 },
  tabs: { flexDirection: "row", backgroundColor: C.surface, borderTopWidth: 1, borderTopColor: C.line, paddingTop: 6 },
  tab: { flex: 1, minHeight: 52, alignItems: "center", justifyContent: "center", gap: 2 },
  tabText: { fontFamily: F.medium, fontSize: 12, color: C.muted },
  tabTextOn: { fontFamily: F.semi, color: C.accent },
});
