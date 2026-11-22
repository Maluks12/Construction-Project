import * as Crypto from "expo-crypto";
import { useLocalSearchParams } from "expo-router";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { Button, Card, Field, H, Loading, Muted, Notice, PhotoStrip, Screen, T } from "@/components/ui";
import { ApiError, api, sitePath } from "@/lib/api";
import { takeEvidencePhoto } from "@/lib/evidence";
import { formatTime, formatWhen, now } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { Attendance, SignRequest, Talk, TalkPhotoRequest, TalkRequest, TalkRetractRequest } from "@/lib/types";
import { C, F, TONES } from "@/theme";

type Person = { name: string; firm: string | null; clock_number: string | null };

const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .map((w) => w[0])
    .join("")
    .slice(0, 3)
    .toUpperCase();

// One toolbox talk: people tap their name to sign on this phone
// (docs/api/toolbox.md). Names come from who signed in on site and hasn't
// signed yet (`not_signed`, M11), or are typed in for anyone else.
export default function TalkScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { site, me, can } = useSession();
  const [name, setName] = useState("");
  const [firm, setFirm] = useState("");
  const [clock, setClock] = useState("");
  const [retracting, setRetracting] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [hint, setHint] = useState("");

  const { data: talk, error: loadError, reload } = useLoad(
    async () => (site ? api<Talk>(sitePath(site.id, `/toolbox-talks/${id}`)) : null),
    [site?.id, id],
  );
  if (!talk || !site) return <Screen back title="Toolbox talk"><Loading error={loadError} onRetry={reload} /></Screen>;

  const open = !talk.ended_at;
  const standing = talk.attendance.filter((a) => !a.retraction);
  const total = standing.length + talk.not_signed.length;
  const path = (rest: string) => sitePath(site.id, `/toolbox-talks/${talk.id}${rest}`);

  async function run(key: string, fn: () => Promise<unknown>) {
    setBusy(key);
    setError("");
    setHint("");
    try {
      await fn();
      await reload();
    } catch (e) {
      setError(message(e));
      // 409 AlreadySigned: two people with one name are told apart by firm or clock number.
      if (e instanceof ApiError && typeof e.body.hint === "string") setHint(e.body.hint);
    } finally {
      setBusy("");
    }
  }

  // A new id per signature; the same person twice is the API's 409.
  const sign = (p: Person) =>
    run(`sign:${p.name}`, () => {
      const body: SignRequest = { name: p.name, firm: p.firm, clock_number: p.clock_number, method: "tap", client_signed_at: now() };
      return api<Attendance>(path(`/attendance/${Crypto.randomUUID()}`), { method: "PUT", body });
    });

  async function signTyped() {
    if (!name.trim()) return setError("Write the name as the person writes it.");
    await sign({ name: name.trim(), firm: firm.trim() || null, clock_number: clock.trim() || null });
    setName("");
    setFirm("");
    setClock("");
  }

  const addPhoto = () =>
    run("photo", async () => {
      const photo = await takeEvidencePhoto(site.id);
      if (!photo) return;
      const body: TalkPhotoRequest = { kind: "photo" };
      await api<Talk>(path(`/photos/${photo.id}`), { method: "PUT", body });
    });

  // Once someone has signed, only the end time can be set, once: the rest of
  // the talk is sent exactly as stored.
  const close = () =>
    run("close", () => {
      const body: TalkRequest = {
        topic_key: talk.topic_key,
        topic: talk.topic,
        presenter_name: talk.presenter_name,
        notes: talk.notes,
        started_at: talk.started_at,
        ended_at: now(),
        device_id: talk.device_id,
      };
      return api<Talk>(path(""), { method: "PUT", body });
    });

  const retract = (a: Attendance) =>
    run(`retract:${a.id}`, async () => {
      if (!reason.trim()) throw new Error("Write why the signature was a mistake.");
      const body: TalkRetractRequest = { reason: reason.trim(), client_retracted_at: now() };
      await api<Attendance>(path(`/attendance/${a.id}/retract`), { method: "POST", body });
      setRetracting(null);
      setReason("");
    });

  return (
    <Screen
      back
      eyebrow={`Toolbox talk · ${formatTime(talk.started_at)} · ${talk.presenter_name}`}
      title={talk.topic}
      footer={open ? <Button title="Close talk" busy={busy === "close"} disabled={busy !== ""} onPress={close} /> : undefined}
    >
      <View style={s.gap}>
        <View style={s.track}><View style={[s.bar, { width: `${total ? Math.round((standing.length / total) * 100) : 0}%` }]} /></View>
        <Muted>
          {standing.length} signed{talk.not_signed.length ? ` · ${talk.not_signed.length} signed in on site and not yet` : ""}
          {open ? "" : ` · closed ${formatTime(talk.ended_at!)}`}
        </Muted>
      </View>
      {error ? <Notice tone="bad">{hint ? `${error} ${hint}.` : error}</Notice> : null}

      {open && talk.not_signed.length > 0 && (
        <Card title="On site, not signed yet">
          {talk.not_signed.map((p) => (
            <View key={p.sign_in_id} style={s.row}>
              <View style={s.avatar}><Text style={s.initials}>{initials(p.name)}</Text></View>
              <View style={s.grow}>
                <T style={s.name}>{p.name}</T>
                <Muted>{[p.firm, p.clock_number].filter(Boolean).join(" · ") || "No firm given"}</Muted>
              </View>
              <Pressable accessibilityRole="button" disabled={busy !== ""} onPress={() => void sign(p)} style={s.sign}>
                <Text style={s.signText}>{busy === `sign:${p.name}` ? "Signing…" : "Tap to sign"}</Text>
              </Pressable>
            </View>
          ))}
        </Card>
      )}

      {open && (
        <View style={s.gap}>
          <H>Someone else</H>
          <Field label="Name" value={name} onChangeText={setName} maxLength={200} hint="As they write it. No ID numbers." />
          <Field label="Firm (optional)" value={firm} onChangeText={setFirm} maxLength={200} />
          <Field label="Clock number (optional)" value={clock} onChangeText={setClock} maxLength={50} />
          <Button title="Sign them in to this talk" variant="primary" busy={busy === `sign:${name.trim()}`} disabled={busy !== ""} onPress={() => void signTyped()} />
        </View>
      )}

      <Card title="Signed">
        {talk.attendance.length === 0 && <View style={s.row}><Muted>Nobody yet.</Muted></View>}
        {talk.attendance.map((a) => {
          const mayRetract = !a.retraction && (a.recorded_by === me?.user_id || can("manage_workers"));
          return (
            <View key={a.id} style={s.signed}>
              <View style={s.rowInner}>
                <View style={s.grow}>
                  <T style={[s.name, a.retraction && s.struck]}>{a.name}</T>
                  <Muted>
                    {[a.firm, a.clock_number].filter(Boolean).join(" · ")}
                    {a.retraction ? ` · withdrawn: ${a.retraction.reason}` : ` · ${formatTime(a.client_signed_at)}`}
                  </Muted>
                </View>
                {mayRetract && retracting !== a.id && (
                  <Pressable accessibilityRole="button" onPress={() => { setRetracting(a.id); setReason(""); }}>
                    <Text style={s.link}>Mistake?</Text>
                  </Pressable>
                )}
              </View>
              {retracting === a.id && (
                <View style={s.gap}>
                  <Field label="Why it was a mistake" value={reason} onChangeText={setReason} maxLength={500} />
                  <Button title="Withdraw signature" variant="danger" busy={busy === `retract:${a.id}`} disabled={busy !== ""} onPress={() => void retract(a)} />
                </View>
              )}
            </View>
          );
        })}
      </Card>

      <View style={s.gap}>
        <H>Photos</H>
        <PhotoStrip
          photos={talk.photos.map((p) => ({ id: p.evidence_id, note: p.kind === "paper_register" ? "Paper register" : formatTime(p.added_at) }))}
          onAdd={addPhoto}
          busy={busy === "photo"}
        />
      </View>
      <Muted>
        People can sign from the start until the talk is closed, or for 12 hours if it isn&apos;t. Anyone on site who
        doesn&apos;t sign is listed as absent or refused. Started {formatWhen(talk.started_at)}.
      </Muted>
    </Screen>
  );
}

const s = StyleSheet.create({
  grow: { flex: 1 },
  gap: { gap: 8 },
  track: { height: 8, borderRadius: 4, backgroundColor: C.lineSoft, overflow: "hidden" },
  bar: { height: 8, backgroundColor: C.header },
  row: { flexDirection: "row", alignItems: "center", gap: 12, paddingHorizontal: 14, paddingVertical: 10, minHeight: 60, borderTopWidth: 1, borderTopColor: C.lineSoft },
  signed: { gap: 8, paddingHorizontal: 14, paddingVertical: 10, borderTopWidth: 1, borderTopColor: C.lineSoft },
  rowInner: { flexDirection: "row", alignItems: "center", gap: 12 },
  avatar: { width: 38, height: 38, borderRadius: 19, backgroundColor: TONES.neutral.bg, alignItems: "center", justifyContent: "center" },
  initials: { fontFamily: F.semi, fontSize: 13, color: TONES.neutral.fg },
  name: { fontFamily: F.semi },
  struck: { textDecorationLine: "line-through", color: C.muted },
  sign: { minHeight: 44, justifyContent: "center", paddingHorizontal: 14, borderRadius: 10, borderWidth: 1, borderColor: C.accent },
  signText: { fontFamily: F.semi, fontSize: 14, color: C.accent },
  link: { fontFamily: F.semi, fontSize: 13, color: C.accent },
});
