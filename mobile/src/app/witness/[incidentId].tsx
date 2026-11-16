import * as Crypto from "expo-crypto";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useState } from "react";
import { View } from "react-native";

import { Button, Field, H, Muted, Notice, PhotoStrip, Screen } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { takeEvidencePhoto } from "@/lib/evidence";
import { formatTime, isoToSASTInput, now, sastInputToISO, shortId } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message } from "@/lib/use-load";
import type { StatementRequest, WitnessStatement } from "@/lib/types";

export default function Witness() {
  const { incidentId } = useLocalSearchParams<{ incidentId: string }>();
  const { site } = useSession();
  const router = useRouter();
  const [statementId] = useState(() => Crypto.randomUUID());
  const [name, setName] = useState("");
  const [contact, setContact] = useState("");
  const [when, setWhen] = useState(() => isoToSASTInput(now()));
  const [statement, setStatement] = useState("");
  const [photo, setPhoto] = useState<{ id: string; note: string } | null>(null);
  const [busy, setBusy] = useState<"" | "save" | "photo">("");
  const [error, setError] = useState("");

  async function addPhoto() {
    if (!site) return;
    setBusy("photo");
    setError("");
    try {
      const taken = await takeEvidencePhoto(site.id);
      if (taken) setPhoto({ id: taken.id, note: `${formatTime(taken.capturedAt)} · sealed` });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function save() {
    if (!site) return;
    const at = sastInputToISO(when);
    if (!at) return setError("Write when it was taken as YYYY-MM-DD HH:MM.");
    setBusy("save");
    setError("");
    try {
      const body: StatementRequest = {
        witness_name: name.trim(),
        witness_contact: contact.trim() || null,
        statement: statement.trim(),
        taken_at: at,
        evidence_id: photo?.id ?? null,
      };
      await api<WitnessStatement>(sitePath(site.id, `/incidents/${incidentId}/witness-statements/${statementId}`), { method: "PUT", body });
      router.back();
    } catch (e) {
      setError(message(e));
      setBusy("");
    }
  }

  return (
    <Screen
      back
      title="Witness statement"
      sub={`INC-${shortId(incidentId)}`}
      footer={<Button title="Save statement" variant="primary" busy={busy === "save"} disabled={!name.trim() || !statement.trim() || busy !== ""} onPress={save} />}
    >
      <Field label="Witness name" value={name} onChangeText={setName} maxLength={200} />
      <Field label="Their role, employer or contact" value={contact} onChangeText={setContact} maxLength={200} />
      <Field label="When taken" value={when} onChangeText={setWhen} hint="Site time, as YYYY-MM-DD HH:MM" keyboardType="numbers-and-punctuation" />
      <Field label="What they saw, in their words" value={statement} onChangeText={setStatement} multiline maxLength={20000} />
      <View style={{ gap: 8 }}>
        <H>Signed copy</H>
        <Muted>Photograph the paper statement once the witness has signed it.</Muted>
        <PhotoStrip photos={photo ? [photo] : []} onAdd={photo ? undefined : addPhoto} busy={busy === "photo"} />
      </View>
      <Notice>A statement can&apos;t be edited once saved. To correct it, take a further statement; both stay on the incident.</Notice>
      {error ? <Notice tone="bad">{error}</Notice> : null}
    </Screen>
  );
}
