import * as Crypto from "expo-crypto";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useState } from "react";
import { StyleSheet, View } from "react-native";

import { Button, Card, Field, H, Loading, Muted, Notice, PhotoStrip, Pill, Screen, T } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { takeEvidencePhoto } from "@/lib/evidence";
import { formatDateOnly, formatTime, formatWhen, label, now, shortId } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { Action, CloseoutRequest } from "@/lib/types";

export default function ActionScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { site } = useSession();
  const router = useRouter();
  const [closeoutId] = useState(() => Crypto.randomUUID());
  const [photos, setPhotos] = useState<{ id: string; note: string }[]>([]);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<"" | "save" | "photo">("");
  const [error, setError] = useState("");

  const { data: action, error: loadError, reload } = useLoad(
    async () => (site ? api<Action>(sitePath(site.id, `/actions/${id}`)) : null),
    [site?.id, id],
  );
  if (!action) return <Screen back title="Corrective action"><Loading error={loadError} onRetry={reload} /></Screen>;

  const canClose = action.state === "open" || action.state === "overdue";

  async function addPhoto() {
    if (!site) return;
    setBusy("photo");
    setError("");
    try {
      const taken = await takeEvidencePhoto(site.id);
      if (taken) setPhotos([...photos, { id: taken.id, note: `${formatTime(taken.capturedAt)} · sealed` }]);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function close() {
    if (!site) return;
    setBusy("save");
    setError("");
    try {
      const body: CloseoutRequest = { note: note.trim() || null, evidence_ids: photos.map((p) => p.id), client_submitted_at: now() };
      await api<Action>(sitePath(site.id, `/actions/${id}/closeouts/${closeoutId}`), { method: "PUT", body });
      router.replace("/");
    } catch (e) {
      setError(message(e));
      setBusy("");
    }
  }

  return (
    <Screen
      back
      title="Corrective action"
      sub={`CA-${shortId(action.id)}`}
      footer={canClose ? <Button title="Close action" variant="primary" busy={busy === "save"} disabled={photos.length === 0 || busy !== ""} onPress={close} /> : undefined}
    >
      <View style={s.pills}>
        <Pill tone={action.severity}>{action.severity}</Pill>
        <Pill tone={action.state === "overdue" ? "bad" : action.state.startsWith("closed") ? "ok" : "warn"}>
          {action.state === "overdue" ? `Overdue since ${formatDateOnly(action.due_on)}` : action.state === "open" ? `Due ${formatDateOnly(action.due_on)}` : label(action.state)}
        </Pill>
      </View>
      <H style={s.title}>{action.title}</H>
      {action.description ? <T>{action.description}</T> : null}
      <Card style={s.pad}>
        <Muted>Raised from</Muted>
        <T>{action.source === "finding" ? "A failed inspection item" : `Incident INC-${shortId(action.incident_id ?? "")}`}, {formatWhen(action.source_at)}</T>
        {action.responsible_party ? <Muted>Work by: {action.responsible_party}</Muted> : null}
      </Card>

      {action.closeouts.map((c) => (
        <Notice key={c.id} tone={c.decision === "rejected" ? "bad" : c.decision === "verified" ? "ok" : "warn"}>
          Close-out sent {formatWhen(c.client_submitted_at)}: {c.decision ? label(c.decision) : "waiting for an owner or SHE officer to verify"}.
        </Notice>
      ))}

      {canClose && (
        <>
          <View style={s.gap}>
            <H>Proof it&apos;s fixed</H>
            <Muted>A photo is required to close. It must be taken after the problem was found, and can&apos;t be one of the original photos.</Muted>
            <PhotoStrip photos={photos} onAdd={photos.length < 20 ? addPhoto : undefined} busy={busy === "photo"} />
          </View>
          <Field label="What was done" value={note} onChangeText={setNote} multiline maxLength={2000} />
          <Muted>After you close it, an owner or SHE officer verifies the fix or sends it back.</Muted>
        </>
      )}
      {error ? <Notice tone="bad">{error}</Notice> : null}
    </Screen>
  );
}

const s = StyleSheet.create({
  gap: { gap: 8 },
  pills: { flexDirection: "row", flexWrap: "wrap", gap: 6 },
  title: { fontSize: 22, lineHeight: 28 },
  pad: { padding: 14, gap: 4 },
});
