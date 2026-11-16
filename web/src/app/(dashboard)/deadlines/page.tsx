import { randomUUID } from "node:crypto";

import type { Metadata } from "next";
import Link from "next/link";

import { DeadlineBody, deadlineDue, deadlineState } from "@/components/deadline";
import { Forbidden, NoSite } from "@/components/states";
import { Alert, Badge, Card, EmptyState, PageHeader, Row } from "@/components/ui";
import { getDeadline, listDeadlines } from "@/lib/data";
import { formatWhen, isoToSASTInput, label, shortId } from "@/lib/format";
import { currentSite, sitePeople } from "@/lib/site";
import type { DeadlineState } from "@/lib/types";

import { RetractForm, SatisfyForm } from "./forms";

export const metadata: Metadata = { title: "Deadlines" };

const GROUPS: { title: string; states: DeadlineState[] }[] = [
  { title: "Call now", states: ["due_now"] },
  { title: "Overdue", states: ["overdue"] },
  { title: "Open", states: ["open"] },
  { title: "Met", states: ["satisfied", "satisfied_late"] },
];

export default async function DeadlinesPage({ searchParams }: PageProps<"/deadlines">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Deadlines" />;

  const sp = await searchParams;
  const selectedId = typeof sp.deadline === "string" ? sp.deadline : null;
  const [deadlines, people, selected] = await Promise.all([
    listDeadlines(site.id),
    sitePeople(site.id),
    selectedId ? getDeadline(site.id, selectedId) : null,
  ]);
  if (deadlines === "forbidden") return <Forbidden title="Deadlines" />;

  const unmet = selected && ["open", "due_now", "overdue"].includes(selected.state);

  return (
    <>
      <PageHeader
        title="Deadlines"
        description={'Legal clocks for this site. "Due" is the conservative date alerts use; the law may allow longer when a last day falls on a Sunday or public holiday. Guidance, not legal advice.'}
      />
      <div className="flex flex-wrap items-start gap-5">
        <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-[18px]">
          {deadlines.length === 0 && (
            <EmptyState title="No live deadlines">Withdrawn and replaced deadlines stay on their incident.</EmptyState>
          )}
          {GROUPS.map((g) => {
            const items = deadlines.filter((d) => g.states.includes(d.state));
            if (items.length === 0) return null;
            return (
              <section key={g.title}>
                <h2 className="mb-2.5 font-head text-base font-semibold">
                  {g.title} <span className="font-medium text-muted">· {items.length}</span>
                </h2>
                <Card flush>
                  {items.map((d) => {
                    const state = deadlineState(d);
                    return (
                      <div key={d.id} className="flex flex-wrap items-start gap-3.5 border-t border-line-soft px-[18px] py-3.5 first:border-t-0">
                        <div className="flex-[1_1_300px]">
                          <Link href={`/deadlines?deadline=${d.id}`} className="link text-sm">{d.label}</Link>
                          <div className="mt-0.5 text-xs text-muted">
                            {d.source_ref}
                            {d.incident_id && (
                              <> · <Link href={`/incidents/${d.incident_id}`} className="link">INC-{shortId(d.incident_id)}</Link></>
                            )}
                          </div>
                          {d.caveat && <div className="mt-1.5 text-xs text-muted">{d.caveat}</div>}
                        </div>
                        <div className="text-right text-sm">
                          {d.satisfaction ? `${label(d.satisfaction.method)} ${formatWhen(d.satisfaction.satisfied_at)}` : deadlineDue(d)}
                          <div className="mt-1"><Badge tone={state.tone}>{state.text}</Badge></div>
                        </div>
                      </div>
                    );
                  })}
                </Card>
              </section>
            );
          })}
        </div>

        <div className="flex flex-[1_1_360px] flex-col gap-4">
          {selected ? (
            <>
              <Card><DeadlineBody d={selected} /></Card>
              {unmet && (
                <Card
                  title="Record the notice"
                  description="Recording it stops the alert. An owner or SHE officer can retract a wrong record with a reason; both stay on file."
                >
                  <SatisfyForm id={randomUUID()} siteId={site.id} deadlineId={selected.id} now={isoToSASTInput(new Date().toISOString())} />
                </Card>
              )}
              {selected.satisfactions.length > 0 && (
                <Card title="Records">
                  {selected.satisfactions.map((s) => (
                    <Row key={s.id} aside={s.retraction ? <Badge tone="bad">Retracted</Badge> : <Badge tone="ok">Stands</Badge>}>
                      <span className="font-medium">{label(s.method)}</span>, {formatWhen(s.satisfied_at)}
                      <div className="text-[13px] text-muted">
                        Recorded by {people.who(s.recorded_by)}, {formatWhen(s.recorded_at)}
                        {s.reference && ` · ${s.reference}`}
                      </div>
                      {s.note && <div className="text-[13px]">“{s.note}”</div>}
                      {s.retraction && (
                        <div className="text-[13px] text-muted">
                          Retracted by {people.who(s.retraction.retracted_by)}: “{s.retraction.reason}”
                        </div>
                      )}
                    </Row>
                  ))}
                  {selected.satisfaction && people.canManage && (
                    <details className="mt-3 border-t border-line-soft pt-3">
                      <summary className="link cursor-pointer text-sm">Recorded in error?</summary>
                      <div className="mt-3">
                        <RetractForm siteId={site.id} deadlineId={selected.id} satisfactionId={selected.satisfaction.id} />
                      </div>
                    </details>
                  )}
                </Card>
              )}
            </>
          ) : selectedId ? (
            <Alert tone="info">That deadline isn&apos;t on this site.</Alert>
          ) : (
            <Card title="Pick a deadline">
              <p className="text-sm text-muted">Open one to see how its date was worked out, or to record that it was met.</p>
            </Card>
          )}
        </div>
      </div>
    </>
  );
}
