// Colours and type from the SiteProof prototype. The phone app is light
// only: it's read outdoors, in sunlight, where a dark theme is harder to see.

export const C = {
  bg: "#F6F5F2",
  surface: "#FFFFFF",
  surface2: "#F3F1EC",
  ink: "#16181D",
  muted: "#5B6170",
  line: "#E3E1DC",
  lineSoft: "#EFEDE8",
  lineStrong: "#D6D3CC",
  accent: "#B4470F",
  accentSoft: "#FDF3EC",
  accentHi: "#E2683C",
  header: "#1B1F27",
  headerLine: "#333A47",
  headerText: "#C9CDD6",
  headerMuted: "#A3A8B4",
  critical: "#A61B1B",
  offline: "#F2B544",
  thumb: "#D9D5CC",
  white: "#FFFFFF",
} as const;

export type Tone = "neutral" | "ok" | "warn" | "bad" | "now" | "low" | "medium" | "high" | "critical";

export const TONES: Record<Tone, { bg: string; fg: string }> = {
  neutral: { bg: "#EEF1F5", fg: "#334155" },
  low: { bg: "#EEF1F5", fg: "#334155" },
  ok: { bg: "#E3F2E8", fg: "#1F6F43" },
  warn: { bg: "#FFF1CC", fg: "#7A4E00" },
  medium: { bg: "#FFF1CC", fg: "#7A4E00" },
  bad: { bg: "#FBE3E3", fg: "#8E1616" },
  high: { bg: "#FDE6D6", fg: "#9A3A0B" },
  now: { bg: "#A61B1B", fg: "#FFFFFF" },
  critical: { bg: "#A61B1B", fg: "#FFFFFF" },
};

// Dot colours beside the severity choices.
export const SEVERITY_DOT = { low: "#64748B", medium: "#C98A00", high: "#C2561A", critical: "#A61B1B" } as const;

export const F = {
  body: "Inter_400Regular",
  medium: "Inter_500Medium",
  semi: "Inter_600SemiBold",
  head: "SpaceGrotesk_600SemiBold",
  headBold: "SpaceGrotesk_700Bold",
} as const;

// Stroke icon paths (24x24 viewBox), from the prototype.
export const ICONS = {
  home: "M3 11l9-7 9 7v9a1 1 0 0 1-1 1h-5v-6h-6v6H4a1 1 0 0 1-1-1z",
  check: "M9 4h6v3H9zM7 5H5v16h14V5h-2M8 12l2.5 2.5L16 9",
  warn: "M12 3l10 18H2zM12 10v5M12 18v.5",
  box: "M4 8h16v12H4zM9 8V5h6v3M4 13h16",
  phone: "M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1.9.4 1.8.7 2.7a2 2 0 0 1-.5 2.1L8 9.8a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.4c.9.3 1.8.6 2.7.7a2 2 0 0 1 1.7 2z",
  clock: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 7v5l3 3",
  bell: "M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 21h4",
  swap: "M4 8h14l-3-3M20 16H6l3 3",
  back: "M15 6l-6 6 6 6",
  chevron: "M9 6l6 6-6 6",
  camera: "M4 8h3l2-3h6l2 3h3v11H4zM12 17a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z",
  shield: "M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM8.5 12l2.5 2.5 4.5-5",
  outbox: "M4 14v5h16v-5M12 15V4M8 8l4-4 4 4",
} as const;
