import * as Crypto from "expo-crypto";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { Button, Card, Chip, Field, H, Loading, Muted, Notice, PhotoStrip, Row, Screen, T } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { takeEvidencePhoto } from "@/lib/evidence";
import { formatDateOnly, formatTime, label, now, sastDay } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { AddendumRequest, Diary, FirmList, LabourRow, RevisionRequest, Sections, SignInDay, WeatherCondition } from "@/lib/types";
import { C, F } from "@/theme";

const WEATHER: WeatherCondition[] = ["fine", "cloudy", "rain", "heavy_rain", "thunderstorm", "wind", "hot", "cold", "mist"];

type LabourDraft = { id: string; firm: string; headcount: string; own_team: boolean; trade: string | null; worker_ids?: string[] };

const SECTION_TITLES: Record<Exclude<keyof Sections, "labour">, string> = {
  weather_times: "Weather times",
  plant: "Plant and equipment",
  instructions: "Instructions received",
  delays: "Delays",
  visitors: "Visitors",
  deliveries: "Deliveries",
};

// Today's site diary (docs/api/diary.md). Three things are required: the
// weather, the headcount and the work done. Labour by firm can be filled in
// here; the other optional sections already on the diary are kept as saved.
export default function DiaryScreen() {
  const { site } = useSession();
  const today = sastDay(now());
  // One id per save, kept until the save goes through, so a retry after a
  // dropped connection is the same revision (200), not a second one.
  const [revisionId, setRevisionId] = useState(() => Crypto.randomUUID());
  const [addendumId, setAddendumId] = useState(() => Crypto.randomUUID());
  const [draft, setDraft] = useState<{
    weather: WeatherCondition[];
    note: string;
    headcount: string;
    work: string;
    labour: LabourDraft[];
  } | null>(null);
  const [photos, setPhotos] = useState<{ id: string; note: string }[]>([]);
  const [reason, setReason] = useState("");
  const [addNote, setAddNote] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");

  const base = site ? `/v1/construction/sites/${encodeURIComponent(site.id)}` : "";
  const { data, error: loadError, reload } = useLoad(async () => {
    if (!site) return null;
    const [diary, day, firms] = await Promise.all([
      // 404: no diary yet today (the first save creates it).
      api<Diary>(`${base}/diary/${today}`).catch((e) => {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }),
      api<SignInDay>(`/v1/sites/${encodeURIComponent(site.id)}/sign-ins?date=${today}`).catch(() => null),
      api<FirmList>(`${base}/diary-firms`).catch(() => ({ firms: [] })),
    ]);
    return { diary, day, firms: firms.firms };
  }, [site?.id, today]);

  if (!site) return <Screen back title="Site diary"><Muted>You haven&apos;t been added to a site yet.</Muted></Screen>;
  if (!data) return <Screen back title="Site diary"><Loading error={loadError} onRetry={reload} /></Screen>;

  const { diary, day, firms } = data;
  const current = diary?.current;
  // Sign-ins plus anyone who only signed a toolbox talk: the diary's own
  // suggestion once it exists, the day register's count before.
  const suggested = diary?.headcount_suggested_now ?? day?.headcount ?? null;
  const form = draft ?? {
    weather: current?.weather.conditions ?? [],
    note: current?.weather.note ?? "",
    headcount: current ? String(current.headcount) : suggested !== null ? String(suggested) : "",
    work: current?.work_done ?? "",
    labour: (current?.sections.labour ?? []).map((r) => ({ ...r, headcount: String(r.headcount) })),
  };
  const edit = (patch: Partial<typeof form>) => setDraft({ ...form, ...patch });
  const setRow = (i: number, patch: Partial<LabourDraft>) => edit({ labour: form.labour.map((r, n) => (n === i ? { ...r, ...patch } : r)) });
  const kept = (Object.keys(SECTION_TITLES) as (keyof typeof SECTION_TITLES)[]).filter((k) => {
    const v = current?.sections[k];
    return Array.isArray(v) ? v.length > 0 : !!v;
  });

  async function addPhoto() {
    setBusy("photo");
    setError("");
    try {
      const photo = await takeEvidencePhoto(site!.id);
      if (photo) setPhotos([...photos, { id: photo.id, note: `${formatTime(photo.capturedAt)} · sealed` }]);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function save() {
    const headcount = Number(form.headcount);
    if (form.weather.length === 0) return setError("Pick at least one kind of weather.");
    if (form.headcount.trim() === "" || !Number.isInteger(headcount) || headcount < 0) return setError("Write the headcount as a whole number.");
    if (!form.work.trim()) return setError("Write what work was done.");
    const labour: LabourRow[] = [];
    for (const r of form.labour) {
      if (!r.firm.trim()) continue;
      const n = Number(r.headcount);
      if (!Number.isInteger(n) || n < 0) return setError(`Write the headcount for ${r.firm} as a whole number.`);
      labour.push({ id: r.id, firm: r.firm.trim(), own_team: r.own_team, trade: r.trade, headcount: n, ...(r.worker_ids ? { worker_ids: r.worker_ids } : {}) });
    }
    setBusy("save");
    setError("");
    try {
      // The whole diary every time, based on the revision this phone saw.
      const sections: Sections = { ...(current?.sections ?? {}) };
      if (labour.length) sections.labour = labour;
      else delete sections.labour;
      const body: RevisionRequest = {
        base_revision_id: current?.id ?? null,
        weather: { conditions: form.weather, note: form.note.trim() || null },
        headcount,
        work_done: form.work.trim(),
        sections,
        evidence_ids: [...(current?.evidence_ids ?? []), ...photos.map((p) => p.id)],
        client_saved_at: now(),
      };
      await api<Diary>(`${base}/diary/${today}/revisions/${revisionId}`, { method: "PUT", body });
      setRevisionId(Crypto.randomUUID());
      setDraft(null);
      setPhotos([]);
      setSaved(`Saved ${formatTime(now())}.`);
      await reload();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  // After midnight the diary takes addenda only; the original never changes.
  async function addAddendum() {
    if (!reason.trim()) return setError("Write why you're adding to it.");
    if (!addNote.trim() && photos.length === 0) return setError("Add a note or a photo.");
    setBusy("addendum");
    setError("");
    try {
      const body: AddendumRequest = {
        reason: reason.trim(),
        note: addNote.trim() || null,
        evidence_ids: photos.map((p) => p.id),
        client_added_at: now(),
      };
      await api(`${base}/diary/${today}/addenda/${addendumId}`, { method: "PUT", body });
      setAddendumId(Crypto.randomUUID());
      setReason("");
      setAddNote("");
      setPhotos([]);
      setSaved("Addendum added.");
      await reload();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  const closed = diary?.closed ?? false;

  return (
    <Screen
      back
      title={`Site diary · ${formatDateOnly(today)}`}
      sub={`${site.name} · ${closed ? "closed" : "closes at midnight"}`}
      footer={
        closed ? (
          <Button title="Add addendum" variant="primary" busy={busy === "addendum"} disabled={busy !== ""} onPress={addAddendum} />
        ) : (
          <Button title="Save diary" variant="primary" busy={busy === "save"} disabled={busy !== ""} onPress={save} />
        )
      }
    >
      {diary && diary.conflict_revision_ids.length > 0 && (
        <Notice tone="bad">
          Another phone saved this diary without seeing your last save. What&apos;s below is the newest; check it and save
          again to settle it. Both stay on record.
        </Notice>
      )}
      {current?.received_after_close && <Notice tone="warn">The last save reached the server after midnight.</Notice>}
      {saved ? <Notice tone="ok">{saved}</Notice> : null}

      {closed ? (
        <>
          <Notice>After midnight the diary is closed. Anything new is a dated addendum; the original stays as it was.</Notice>
          {current && (
            <Card title="As saved">
              <Row title="Weather" sub={current.weather.conditions.map(label).join(", ")} />
              <Row title="Headcount" sub={String(current.headcount)} />
              <Row title="Work done" sub={current.work_done} />
            </Card>
          )}
          <Field label="Why you're adding to it" value={reason} onChangeText={setReason} maxLength={500} />
          <Field label="Note" value={addNote} onChangeText={setAddNote} multiline maxLength={4000} />
        </>
      ) : (
        <>
          <Notice>Three things and you&apos;re done. Add labour by firm if the day needs it.</Notice>
          <View style={s.gap}>
            <T style={s.label}>Weather</T>
            <View style={s.chips}>
              {WEATHER.map((w) => {
                const on = form.weather.includes(w);
                return <Chip key={w} title={label(w)} on={on} onPress={() => edit({ weather: on ? form.weather.filter((x) => x !== w) : [...form.weather, w] })} />;
              })}
            </View>
          </View>
          <Field label="Weather note (optional)" value={form.note} onChangeText={(t) => edit({ note: t })} maxLength={500} />
          <Field
            label="Headcount"
            value={form.headcount}
            onChangeText={(t) => edit({ headcount: t })}
            keyboardType="number-pad"
            hint={suggested !== null ? `${suggested} signed in or signed a toolbox talk today. Change it if more were on site.` : undefined}
          />
          <Field label="Work done" value={form.work} onChangeText={(t) => edit({ work: t })} multiline maxLength={4000} />

          <H>Labour by firm · optional</H>
          {form.labour.map((r, i) => (
            <Card key={r.id} style={s.pad}>
              <Field label="Firm" value={r.firm} onChangeText={(t) => setRow(i, { firm: t })} maxLength={200} />
              {firms.length > 0 && !r.firm && (
                <View style={s.chips}>
                  {firms.slice(0, 6).map((f) => <Chip key={f} title={f} on={false} onPress={() => setRow(i, { firm: f })} />)}
                </View>
              )}
              <Field label="People" value={r.headcount} onChangeText={(t) => setRow(i, { headcount: t })} keyboardType="number-pad" />
              <View style={s.inline}>
                <Chip title="Our own team" on={r.own_team} onPress={() => edit({ labour: form.labour.map((x, n) => ({ ...x, own_team: n === i ? !x.own_team : false })) })} />
                <Pressable accessibilityRole="button" onPress={() => edit({ labour: form.labour.filter((_, n) => n !== i) })}>
                  <Text style={s.link}>Remove</Text>
                </Pressable>
              </View>
            </Card>
          ))}
          <Button
            title="Add a firm"
            onPress={() => edit({ labour: [...form.labour, { id: Crypto.randomUUID(), firm: "", headcount: "", own_team: false, trade: null }] })}
          />
          {kept.length > 0 && (
            <Muted>Also on this diary, kept as saved: {kept.map((k) => SECTION_TITLES[k]).join(", ")}.</Muted>
          )}
        </>
      )}

      <View style={s.gap}>
        <H>Photos</H>
        <PhotoStrip
          photos={[...(closed ? [] : (current?.evidence_ids ?? []).map((id) => ({ id, note: "Saved" }))), ...photos]}
          onAdd={addPhoto}
          busy={busy === "photo"}
        />
      </View>
      {error ? <Notice tone="bad">{error}</Notice> : null}
      <Muted>
        Today&apos;s incidents and toolbox talks are linked automatically
        {diary ? ` (${diary.toolbox_talk_ids.length} talks, ${diary.incident_ids.length} incidents)` : ""}.
      </Muted>
    </Screen>
  );
}

const s = StyleSheet.create({
  gap: { gap: 8 },
  pad: { padding: 14, gap: 10 },
  label: { fontFamily: F.semi, fontSize: 14 },
  chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  inline: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 10 },
  link: { fontFamily: F.semi, fontSize: 14, color: C.accent },
});
