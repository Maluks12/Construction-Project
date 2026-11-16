import * as Crypto from "expo-crypto";
import { useRouter } from "expo-router";
import { useState } from "react";
import { StyleSheet, View } from "react-native";

import { Button, Card, Chip, Field, Loading, Muted, Notice, PhotoStrip, Row, Screen, T } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { takeEvidencePhoto } from "@/lib/evidence";
import { formatTime, isoToSASTInput, now, sastInputToISO, shortId } from "@/lib/format";
import { incidentTypes } from "@/lib/regime";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { ChecklistCatalog, Incident, PersonInput, SubmitVersionRequest } from "@/lib/types";
import { F } from "@/theme";

type PersonDraft = { name: string; employee: boolean; injured: boolean; injury: string };

export default function Report() {
  const { site } = useSession();
  const router = useRouter();
  // Made up once per report, so sending it twice can't create two incidents.
  const [incidentId, setIncidentId] = useState(() => Crypto.randomUUID());
  const [when, setWhen] = useState(() => isoToSASTInput(now()));
  const [types, setTypes] = useState<string[]>([]);
  const [people, setPeople] = useState<PersonDraft[]>([]);
  const [where, setWhere] = useState("");
  const [what, setWhat] = useState("");
  const [photos, setPhotos] = useState<{ id: string; note: string }[]>([]);
  const [busy, setBusy] = useState<"" | "send" | "photo">("");
  const [error, setError] = useState("");
  const [sent, setSent] = useState<Incident | null>(null);

  // The incident types belong to the site's regime.
  const { data: catalog, error: loadError, reload } = useLoad(
    async () => (site ? api<ChecklistCatalog>(sitePath(site.id, "/checklists")) : null),
    [site?.id],
  );
  const defs = incidentTypes(catalog?.regime.code);
  const reportable = defs.some((t) => t.reportable && types.includes(t.key));
  const setPerson = (i: number, patch: Partial<PersonDraft>) => setPeople(people.map((p, n) => (n === i ? { ...p, ...patch } : p)));

  async function addPhoto() {
    if (!site) return;
    setBusy("photo");
    setError("");
    try {
      const photo = await takeEvidencePhoto(site.id);
      if (photo) setPhotos([...photos, { id: photo.id, note: `${formatTime(photo.capturedAt)} · sealed` }]);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function submit() {
    if (!site) return;
    const occurred = sastInputToISO(when);
    if (!occurred) return setError("Write when it happened as YYYY-MM-DD HH:MM, for example 2026-11-16 08:55.");
    if (types.length === 0) return setError("Pick at least one type.");
    setBusy("send");
    setError("");
    try {
      const body: SubmitVersionRequest = {
        taxonomy_codes: types,
        occurred_at: occurred,
        // COIDA's clock runs from when the employer knew. On site that is
        // when it happened; the dashboard can correct it in an amendment.
        employer_aware_at: people.some((p) => p.employee && p.injured) ? occurred : null,
        location: where.trim() || null,
        description: what.trim() || null,
        people: people.map<PersonInput>((p) => ({
          name: p.name.trim() || null,
          employee_of_org: p.employee,
          injured: p.injured,
          injury_description: p.injured ? p.injury.trim() || null : null,
        })),
        evidence_ids: photos.map((p) => p.id),
        client_submitted_at: now(),
      };
      setSent(await api<Incident>(sitePath(site.id, `/incidents/${incidentId}/versions/1`), { method: "PUT", body }));
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  function another() {
    setIncidentId(Crypto.randomUUID());
    setWhen(isoToSASTInput(now()));
    setTypes([]);
    setPeople([]);
    setWhere("");
    setWhat("");
    setPhotos([]);
    setSent(null);
  }

  if (sent) {
    const callNow = sent.deadlines.find((d) => d.state === "due_now");
    const open = sent.deadlines.filter((d) => d.state === "open" || d.state === "overdue");
    return (
      <Screen tab="Report" title="Report sent" sub={`INC-${shortId(sent.id)} · ${site?.name ?? ""}`}>
        <Notice tone="ok">Saved as version 1. It can be amended on the dashboard; every version is kept.</Notice>
        {callNow && <Button title="Call the inspector now, then log it" variant="now" icon="phone" onPress={() => router.push(`/call/${callNow.id}`)} />}
        {open.length > 0 && (
          <Card title="Deadlines this started">
            {open.map((d) => <Row key={d.id} title={d.label} sub={`${d.source_ref}${d.due_on ? ` · by the end of ${d.due_on}` : ""}`} />)}
          </Card>
        )}
        <Button title="Take a witness statement" onPress={() => router.push(`/witness/${sent.id}`)} />
        <Button title="Report another incident" onPress={another} />
      </Screen>
    );
  }

  return (
    <Screen
      tab="Report"
      title="Report an incident"
      sub={site?.name}
      footer={<Button title="Submit report" variant="primary" busy={busy === "send"} disabled={busy !== ""} onPress={submit} />}
    >
      {!catalog ? (
        <Loading error={loadError} onRetry={reload} />
      ) : defs.length === 0 ? (
        <Notice tone="warn">This app doesn&apos;t have the incident types for this site&apos;s regulations yet.</Notice>
      ) : (
        <>
          <Field label="When did it happen?" value={when} onChangeText={setWhen} hint="Site time, as YYYY-MM-DD HH:MM" keyboardType="numbers-and-punctuation" />

          <View style={s.gap}>
            <T style={s.label}>What happened? <Muted>Pick every one that applies</Muted></T>
            <View style={s.chips}>
              {defs.map((t) => (
                <Chip key={t.key} title={t.label} on={types.includes(t.key)} onPress={() => setTypes(types.includes(t.key) ? types.filter((k) => k !== t.key) : [...types, t.key])} />
              ))}
            </View>
            {reportable && <Notice tone="bad">Reportable. Submitting starts the legal clocks, which can include a call to the inspector now.</Notice>}
          </View>

          <View style={s.gap}>
            <T style={s.label}>People involved</T>
            {people.map((p, i) => (
              <Card key={i} style={s.person}>
                <Field label="Name (optional)" value={p.name} onChangeText={(name) => setPerson(i, { name })} maxLength={200} />
                <View style={s.chips}>
                  <Chip title="Employee of this company" on={p.employee} onPress={() => setPerson(i, { employee: !p.employee })} />
                  <Chip title="Injured" on={p.injured} onPress={() => setPerson(i, { injured: !p.injured })} />
                </View>
                {p.injured && <Field label="Injury" value={p.injury} onChangeText={(injury) => setPerson(i, { injury })} maxLength={2000} />}
                <Button title="Remove" onPress={() => setPeople(people.filter((_, n) => n !== i))} />
              </Card>
            ))}
            <Button title="+ Add person" onPress={() => setPeople([...people, { name: "", employee: true, injured: false, injury: "" }])} />
          </View>

          <Field label="Where" value={where} onChangeText={setWhere} maxLength={500} />
          <Field label="Describe it" value={what} onChangeText={setWhat} multiline maxLength={5000} />

          <View style={s.gap}>
            <T style={s.label}>Photos <Muted>Optional, up to 20</Muted></T>
            <PhotoStrip photos={photos} onAdd={photos.length < 20 ? addPhoto : undefined} busy={busy === "photo"} />
            <Muted>Photos are sealed with the time they were taken.</Muted>
          </View>
          {error ? <Notice tone="bad">{error}</Notice> : null}
        </>
      )}
    </Screen>
  );
}

const s = StyleSheet.create({
  gap: { gap: 10 },
  label: { fontFamily: F.semi, fontSize: 14 },
  chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  person: { padding: 12, gap: 10 },
});
