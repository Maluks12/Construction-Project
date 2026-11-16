import { useLocalSearchParams, useRouter } from "expo-router";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { Button, Field, H, Loading, Muted, Notice, PhotoStrip, Pill, Screen, T } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { takeEvidencePhoto } from "@/lib/evidence";
import { formatTime, now } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { AnswerRequest, ChecklistCatalog, Inspection, InspectionItem, Result, Severity } from "@/lib/types";
import { C, F, SEVERITY_DOT } from "@/theme";

const RESULTS: { key: Result; text: string; color: string }[] = [
  { key: "pass", text: "Pass", color: "#1F6F43" },
  { key: "fail", text: "Fail", color: "#A61B1B" },
  { key: "na", text: "N/A", color: "#334155" },
];

type Draft = { result: Result | null; severity: Severity | null; comment: string; photos: { id: string; note: string }[] };

const draftOf = (it: InspectionItem): Draft => ({
  result: it.result,
  severity: it.severity,
  comment: it.comment ?? "",
  photos: it.evidence.map((e) => ({ id: e.evidence_id, note: `${formatTime(e.client_captured_at)}${e.captured_before_start ? " · before start" : ""}` })),
});

export default function Answer() {
  const { id, item } = useLocalSearchParams<{ id: string; item?: string }>();
  const { site } = useSession();
  const router = useRouter();
  const [index, setIndex] = useState<number | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState<"" | "save" | "photo">("");
  const [error, setError] = useState("");

  const { data, error: loadError, reload } = useLoad(async () => {
    if (!site) return null;
    const [inspection, catalog] = await Promise.all([
      api<Inspection>(sitePath(site.id, `/inspections/${id}`)),
      api<ChecklistCatalog>(sitePath(site.id, "/checklists")),
    ]);
    return { inspection, severities: catalog.severities };
  }, [site?.id, id]);

  // Answers saved in this visit, laid over what was loaded.
  const [saved, setSaved] = useState<Record<string, InspectionItem>>({});
  const items = (data?.inspection.items ?? []).map((it) => saved[it.key] ?? it);

  // Open at the item asked for, else the first one not answered yet. Set
  // while rendering (once, when the inspection arrives) rather than in an
  // effect, so the screen never paints the wrong item first.
  if (data && index === null) {
    const wanted = item ? items.findIndex((it) => it.key === item) : -1;
    const firstOpen = items.findIndex((it) => it.result === null);
    const start = wanted >= 0 ? wanted : firstOpen >= 0 ? firstOpen : 0;
    setIndex(start);
    setDraft(draftOf(items[start]));
  }

  if (!data || index === null || !draft) {
    return <Screen back title="Inspection"><Loading error={loadError} onRetry={reload} /></Screen>;
  }
  const { inspection } = data;
  const current = items[index];
  const answered = items.filter((it) => it.result !== null).length;
  const locked = inspection.status !== "in_progress";

  const go = (n: number) => {
    setError("");
    setIndex(n);
    setDraft(draftOf(items[n]));
  };

  async function addPhoto() {
    if (!site || !draft) return;
    setBusy("photo");
    setError("");
    try {
      const photo = await takeEvidencePhoto(site.id);
      if (photo) setDraft({ ...draft, photos: [...draft.photos, { id: photo.id, note: `${formatTime(photo.capturedAt)} · sealed` }] });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function save() {
    if (!site || !draft?.result || index === null) return;
    if (draft.result === "fail" && !draft.severity) return setError("Choose how severe the fail is.");
    setBusy("save");
    setError("");
    try {
      // The whole answer every time: sending it again replaces it, photos included.
      const body: AnswerRequest = {
        result: draft.result,
        severity: draft.result === "fail" ? draft.severity : null,
        comment: draft.comment.trim() || null,
        evidence_ids: draft.photos.map((p) => p.id),
        client_answered_at: now(),
      };
      const answer = await api<InspectionItem>(sitePath(site.id, `/inspections/${id}/items/${encodeURIComponent(current.key)}`), { method: "PUT", body });
      setSaved((all) => ({ ...all, [answer.key]: answer }));
      if (index + 1 < items.length) go(index + 1);
      else router.push(`/inspection/${id}/complete`);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  return (
    <Screen
      back
      title={inspection.template_title}
      sub={`Item ${index + 1} of ${items.length} · ${answered} answered`}
      footer={
        <View style={s.footer}>
          <Button title="Previous" disabled={index === 0} onPress={() => go(index - 1)} style={s.grow} />
          {locked ? (
            <Button title="Next" disabled={index + 1 >= items.length} onPress={() => go(index + 1)} style={s.grow} />
          ) : (
            <Button title={index + 1 < items.length ? "Save & next" : "Save & review"} variant="primary" busy={busy === "save"} disabled={!draft.result || busy !== ""} onPress={save} style={s.grow} />
          )}
        </View>
      }
    >
      <View>
        <H style={s.prompt}>{current.prompt}</H>
        <Muted>{current.source_ref ?? "No regulation given"}</Muted>
      </View>

      {locked && <Notice>This inspection is {inspection.status}. It can&apos;t be changed.</Notice>}

      <View style={s.seg}>
        {RESULTS.map((r) => {
          const on = draft.result === r.key;
          return (
            <Pressable
              key={r.key}
              accessibilityRole="radio"
              accessibilityState={{ selected: on, disabled: locked }}
              disabled={locked}
              onPress={() => setDraft({ ...draft, result: r.key, severity: r.key === "fail" ? draft.severity : null })}
              style={[s.segBtn, on && { backgroundColor: r.color, borderColor: r.color }]}
            >
              <Text style={[s.segText, on && s.segTextOn]}>{r.text}</Text>
            </Pressable>
          );
        })}
      </View>

      {draft.result === "fail" && (
        <View style={s.gap}>
          <T style={s.label}>How severe? <Muted>Required for a fail</Muted></T>
          {data.severities.map((level) => {
            const on = draft.severity === level.key;
            return (
              <Pressable key={level.key} accessibilityRole="radio" accessibilityState={{ selected: on }} disabled={locked} onPress={() => setDraft({ ...draft, severity: level.key })} style={[s.sev, on && s.sevOn]}>
                <View style={[s.dot, { backgroundColor: SEVERITY_DOT[level.key] }]} />
                <View style={s.grow}>
                  <T style={s.label}>{level.label}</T>
                  <Muted>{level.definition}</Muted>
                </View>
              </Pressable>
            );
          })}
        </View>
      )}

      <View style={s.gap}>
        <T style={s.label}>Photos {draft.result === "fail" && <Muted>At least one before you can complete</Muted>}</T>
        <PhotoStrip photos={draft.photos} onAdd={locked ? undefined : addPhoto} busy={busy === "photo"} />
        {draft.photos.length > 0 && !locked && <Pill tone="ok">Sealed with the time taken</Pill>}
      </View>

      <Field label="Comment" value={draft.comment} onChangeText={(comment) => setDraft({ ...draft, comment })} multiline maxLength={2000} editable={!locked} />
      {error ? <Notice tone="bad">{error}</Notice> : null}
    </Screen>
  );
}

const s = StyleSheet.create({
  grow: { flex: 1 },
  gap: { gap: 10 },
  footer: { flexDirection: "row", gap: 10 },
  prompt: { fontSize: 20, lineHeight: 26, marginBottom: 4 },
  label: { fontFamily: F.semi, fontSize: 14 },
  seg: { flexDirection: "row", gap: 8 },
  segBtn: { flex: 1, minHeight: 52, borderRadius: 10, borderWidth: 1, borderColor: C.lineStrong, backgroundColor: C.surface, alignItems: "center", justifyContent: "center" },
  segText: { fontFamily: F.semi, fontSize: 16, color: C.ink },
  segTextOn: { color: C.white },
  sev: { flexDirection: "row", gap: 12, padding: 12, borderRadius: 10, borderWidth: 1, borderColor: C.lineStrong, backgroundColor: C.surface },
  sevOn: { borderWidth: 2, padding: 11, borderColor: C.accent, backgroundColor: C.accentSoft },
  dot: { width: 12, height: 12, borderRadius: 6, marginTop: 4 },
});
