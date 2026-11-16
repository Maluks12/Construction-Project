import { useLocalSearchParams, useRouter } from "expo-router";
import { useState } from "react";

import { Button, Card, Field, Loading, Notice, Pill, Row, Screen } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { now } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { Inspection } from "@/lib/types";

export default function Complete() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { site } = useSession();
  const router = useRouter();
  const [busy, setBusy] = useState<"" | "complete" | "abandon">("");
  const [error, setError] = useState("");
  const [abandoning, setAbandoning] = useState(false);
  const [reason, setReason] = useState("");

  const { data: inspection, error: loadError, reload } = useLoad(
    async () => (site ? api<Inspection>(sitePath(site.id, `/inspections/${id}`)) : null),
    [site?.id, id],
  );
  if (!inspection) return <Screen back title="Review & complete"><Loading error={loadError} onRetry={reload} /></Screen>;

  const items = inspection.items;
  const unanswered = items.filter((it) => it.result === null);
  const fails = items.filter((it) => it.result === "fail");
  const noPhoto = fails.filter((it) => it.evidence.length === 0);
  const done = inspection.status !== "in_progress";

  async function run(kind: "complete" | "abandon") {
    if (!site) return;
    setBusy(kind);
    setError("");
    try {
      await api<Inspection>(sitePath(site.id, `/inspections/${id}/${kind}`), {
        method: "POST",
        body: kind === "complete" ? { client_completed_at: now() } : { reason: reason.trim() },
      });
      router.replace("/");
    } catch (e) {
      setError(message(e));
      setBusy("");
    }
  }

  return (
    <Screen
      back
      title="Review & complete"
      sub={`${inspection.template_title} · ${items.length - unanswered.length} of ${items.length} answered`}
      footer={
        done ? undefined : (
          <>
            <Button title="Complete inspection" variant="primary" busy={busy === "complete"} disabled={unanswered.length > 0 || noPhoto.length > 0 || busy !== ""} onPress={() => void run("complete")} />
            <Button title={abandoning ? "Keep the inspection" : "Abandon this inspection"} variant={abandoning ? "secondary" : "danger"} onPress={() => setAbandoning(!abandoning)} />
          </>
        )
      }
    >
      {done && <Notice tone="ok">This inspection is {inspection.status}.</Notice>}
      {unanswered.length > 0 && (
        <Card title={`Not answered · ${unanswered.length}`}>
          {unanswered.map((it) => <Row key={it.key} title={it.prompt} right={<Pill tone="warn">Answer</Pill>} href={`/inspection/${id}?item=${encodeURIComponent(it.key)}`} />)}
        </Card>
      )}
      {noPhoto.length > 0 && (
        <Notice tone="bad">A failed item needs a photo. {noPhoto[0].prompt} has none yet. Add one, then complete.</Notice>
      )}
      <Card title={`Fails · ${fails.length}`}>
        {fails.length === 0 && <Row title="No fails" sub="Completing raises no findings" />}
        {fails.map((it) => (
          <Row
            key={it.key}
            title={it.prompt}
            sub={it.evidence.length === 0 ? "No photo yet" : `${it.evidence.length} photo${it.evidence.length === 1 ? "" : "s"}${it.evidence.some((e) => e.captured_before_start) ? " · 1 taken before start" : ""}`}
            right={it.severity ? <Pill tone={it.severity}>{it.severity}</Pill> : undefined}
            href={`/inspection/${id}?item=${encodeURIComponent(it.key)}`}
          />
        ))}
      </Card>
      {!done && (
        <Notice>
          Once completed, nothing in this inspection can change. Each fail becomes a finding. To fix a mistake later,
          start a correction on the same checklist.
        </Notice>
      )}
      {abandoning && !done && (
        <>
          <Field label="Why are you abandoning it?" value={reason} onChangeText={setReason} multiline maxLength={500} hint="Kept with the inspection. With a failed item, only an owner or SHE officer can abandon it." />
          <Button title="Abandon for good" variant="danger" busy={busy === "abandon"} disabled={!reason.trim() || busy !== ""} onPress={() => void run("abandon")} />
        </>
      )}
      {error ? <Notice tone="bad">{error}</Notice> : null}
    </Screen>
  );
}
