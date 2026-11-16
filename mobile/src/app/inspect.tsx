import * as Crypto from "expo-crypto";
import { useRouter } from "expo-router";
import { useState } from "react";

import { Card, Loading, Muted, Notice, Pill, Row, Screen } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { formatTime, isToday, label, now } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { ChecklistCatalog, ChecklistTemplate, Inspection, InspectionList, Me, StartInspectionRequest } from "@/lib/types";

export default function Inspect() {
  const { site } = useSession();
  const router = useRouter();
  const [starting, setStarting] = useState("");
  const [error, setError] = useState("");

  const { data, error: loadError, reload } = useLoad(async () => {
    if (!site) return null;
    const [catalog, list, me] = await Promise.all([
      api<ChecklistCatalog>(sitePath(site.id, "/checklists")),
      api<InspectionList>(sitePath(site.id, "/inspections?limit=100")),
      api<Me>("/v1/me"),
    ]);
    return { catalog, inspections: list.inspections, me };
  }, [site?.id]);

  // Only the inspector who started an inspection can answer it.
  const open = (data?.inspections ?? []).filter((i) => i.status === "in_progress" && i.started_by === data?.me.user_id);
  const doneToday = (key: string) => (data?.inspections ?? []).find((i) => i.template_key === key && i.status === "completed" && isToday(i.started_at));
  const recurring = (t: ChecklistTemplate) => t.cadence === "per_shift" || t.cadence === "daily";
  const checklists = data?.catalog.checklists ?? [];
  const dueNow = checklists.filter((t) => recurring(t) && !doneToday(t.key) && !open.some((i) => i.template_key === t.key));

  async function start(t: ChecklistTemplate) {
    if (!site || starting) return;
    setStarting(t.key);
    setError("");
    try {
      // The id is made up here, so sending the start twice is harmless.
      const id = Crypto.randomUUID();
      await api<Inspection>(sitePath(site.id, `/inspections/${id}`), {
        method: "PUT",
        // The template exactly as it was shown: key, version and hash.
        body: { template_key: t.key, template_version: t.version, template_sha256: t.sha256, client_started_at: now() } satisfies StartInspectionRequest,
      });
      router.push(`/inspection/${id}`);
    } catch (e) {
      setError(message(e));
    } finally {
      setStarting("");
    }
  }

  const row = (t: ChecklistTemplate, extra?: string) => (
    <Row
      key={t.key}
      title={t.title}
      sub={[`${t.items.length} items`, t.source_ref, extra].filter(Boolean).join(" · ")}
      right={<Pill tone={starting === t.key ? "ok" : "neutral"}>{starting === t.key ? "Starting…" : label(t.cadence)}</Pill>}
      onPress={() => void start(t)}
    />
  );

  return (
    <Screen tab="Inspect" title="Start an inspection" sub={site && data ? `${site.name} · checklists from ${data.catalog.regime.name}` : site?.name}>
      {!data ? (
        <Loading error={loadError} onRetry={reload} />
      ) : (
        <>
          {error ? <Notice tone="bad">{error}</Notice> : null}
          {open.length > 0 && (
            <Card title="Carry on">
              {open.map((i) => (
                <Row key={i.id} title={i.template_title} sub={`${i.answered_count} of ${i.item_count} answered · started ${formatTime(i.started_at)}`} right={<Pill tone="warn">In progress</Pill>} href={`/inspection/${i.id}`} />
              ))}
            </Card>
          )}
          {dueNow.length > 0 && <Card title="Due now">{dueNow.map((t) => row(t, "not done today"))}</Card>}
          <Card title="All checklists">
            {checklists.map((t) => {
              const done = doneToday(t.key);
              return row(t, done?.completed_at ? `done ${formatTime(done.completed_at)}` : undefined);
            })}
          </Card>
          <Muted>
            Correcting a completed inspection? Starting the same checklist again is a new inspection; linking it as a
            correction arrives with the inspection history screen.
          </Muted>
        </>
      )}
    </Screen>
  );
}
