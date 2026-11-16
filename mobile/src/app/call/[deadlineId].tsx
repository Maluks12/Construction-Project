import * as Crypto from "expo-crypto";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useState } from "react";
import { StyleSheet, View } from "react-native";

import { Button, Chip, Field, H, Loading, Muted, Notice, Screen, T } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { formatWhen, isoToSASTInput, label, now, sastInputToISO, shortId } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { DeadlineDetail, SatisfactionMethod, SatisfactionRequest } from "@/lib/types";
import { F } from "@/theme";

const METHODS: SatisfactionMethod[] = ["phone", "email", "in_person"];

export default function CallNow() {
  const { deadlineId } = useLocalSearchParams<{ deadlineId: string }>();
  const { site } = useSession();
  const router = useRouter();
  const [satisfactionId] = useState(() => Crypto.randomUUID());
  const [method, setMethod] = useState<SatisfactionMethod>("phone");
  const [when, setWhen] = useState(() => isoToSASTInput(now()));
  const [reference, setReference] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const { data: d, error: loadError, reload } = useLoad(
    async () => (site ? api<DeadlineDetail>(sitePath(site.id, `/deadlines/${deadlineId}`)) : null),
    [site?.id, deadlineId],
  );
  if (!d) return <Screen back title="Legal deadline"><Loading error={loadError} onRetry={reload} /></Screen>;

  const unmet = d.state === "due_now" || d.state === "open" || d.state === "overdue";

  async function save() {
    if (!site) return;
    const at = sastInputToISO(when);
    if (!at) return setError("Write when as YYYY-MM-DD HH:MM, for example 2026-11-16 09:10.");
    setBusy(true);
    setError("");
    try {
      // When the call was made, not when it's being typed in.
      const body: SatisfactionRequest = { satisfied_at: at, method, reference: reference.trim() || null, note: note.trim() || null };
      await api<DeadlineDetail>(sitePath(site.id, `/deadlines/${deadlineId}/satisfactions/${satisfactionId}`), { method: "PUT", body });
      router.replace("/");
    } catch (e) {
      setError(message(e));
      setBusy(false);
    }
  }

  return (
    <Screen
      back
      title={d.incident_id ? `INC-${shortId(d.incident_id)}` : "Legal deadline"}
      sub={site?.name}
      footer={unmet ? <Button title="Save call log" variant="primary" busy={busy} onPress={save} /> : undefined}
    >
      <View style={s.gap}>
        <H style={s.title}>{d.unit === "immediately" ? "Tell the provincial inspector now" : d.label}</H>
        <T>{d.label}. {d.unit === "immediately" ? "The law asks for notice forthwith, before the written report." : ""}</T>
        <Muted>{d.source_ref} · running since {formatWhen(d.basis_at)}</Muted>
      </View>

      {!unmet ? (
        <Notice tone="ok">
          {d.satisfaction ? `Recorded: ${label(d.satisfaction.method)}, ${formatWhen(d.satisfaction.satisfied_at)}.` : `This deadline is ${label(d.state).toLowerCase()}.`}
        </Notice>
      ) : (
        <>
          <Notice tone="warn">
            The provincial office&apos;s number isn&apos;t in SiteProof yet. Use your site&apos;s emergency contact list, make the
            call, then log it here.
          </Notice>
          <H>Log the call</H>
          <View style={s.gap}>
            <T style={s.label}>How</T>
            <View style={s.chips}>
              {METHODS.map((m) => <Chip key={m} title={label(m)} on={method === m} onPress={() => setMethod(m)} />)}
            </View>
          </View>
          <Field label="When" value={when} onChangeText={setWhen} hint="Site time, as YYYY-MM-DD HH:MM" keyboardType="numbers-and-punctuation" />
          <Field label="Who you spoke to, reference" value={reference} onChangeText={setReference} maxLength={200} />
          <Field label="Note" value={note} onChangeText={setNote} multiline maxLength={2000} />
        </>
      )}
      {d.caveat ? <Notice>{d.caveat}</Notice> : null}
      <Muted>Deadlines are guidance worked out from the regulations, not legal advice.</Muted>
      {error ? <Notice tone="bad">{error}</Notice> : null}
    </Screen>
  );
}

const s = StyleSheet.create({
  gap: { gap: 8 },
  title: { fontSize: 22, lineHeight: 28 },
  label: { fontFamily: F.semi, fontSize: 14 },
  chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
});
