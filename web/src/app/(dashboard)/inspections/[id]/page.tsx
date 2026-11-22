import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { PhotoButton } from "@/components/photo";
import { NoSite } from "@/components/states";
import { Badge, Card, Facts, PageHeader, Row, buttonClass, type Tone } from "@/components/ui";
import { getChecklists, getInspection, listInspections } from "@/lib/data";
import { formatWhen, label } from "@/lib/format";
import { currentSite, sitePeople } from "@/lib/site";
import type { Result } from "@/lib/types";

import { AbandonForm } from "./abandon-form";

export const metadata: Metadata = { title: "Inspection" };

const RESULT: Record<Result, { text: string; tone: Tone }> = {
  pass: { text: "Pass", tone: "ok" },
  fail: { text: "Fail", tone: "bad" },
  na: { text: "N/A", tone: "neutral" },
};

export default async function InspectionPage({ params }: PageProps<"/inspections/[id]">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Inspection" />;

  const { id } = await params;
  const [inspection, catalog, people, list] = await Promise.all([
    getInspection(site.id, id),
    getChecklists(site.id),
    sitePeople(site.id),
    listInspections(site.id),
  ]);
  // Also when the inspection is on another site than the one being viewed.
  if (!inspection) notFound();
  // The inspection that corrects this one, if any (at most one).
  const correction = list === "forbidden" ? undefined : list.find((i) => i.corrects_id === inspection.id);

  const prompts = new Map(inspection.items.map((it) => [it.key, it.prompt]));
  const severities = catalog === "forbidden" ? [] : [...catalog.severities].reverse();
  const tone: Tone = inspection.status === "completed" ? "ok" : inspection.status === "in_progress" ? "warn" : "neutral";

  return (
    <>
      <PageHeader
        eyebrow={<><Link href="/inspections" className="link">Inspections</Link> / {inspection.template_title}</>}
        title={inspection.template_title}
        badges={<Badge tone={tone}>{label(inspection.status)}</Badge>}
        description={`Template v${inspection.template_version} · ${inspection.template_source_ref} · hash ${inspection.template_sha256.slice(0, 8)}…${inspection.template_sha256.slice(-5)}`}
      >
        {inspection.findings.length > 0 && (
          <Link href="/actions?tab=unactioned" className={buttonClass("secondary")}>Raise actions</Link>
        )}
      </PageHeader>

      <div className="flex flex-col gap-5">
        <Card>
          <Facts
            items={[
              ["Inspector", people.who(inspection.started_by)],
              ["Started (device)", formatWhen(inspection.client_started_at)],
              ["Started (server)", formatWhen(inspection.started_at)],
              ["Completed (server)", inspection.completed_at ? formatWhen(inspection.completed_at) : "Not completed"],
              ["Device", inspection.device_id ?? "Not given"],
            ]}
          />
          {inspection.status === "abandoned" && (
            <p className="mt-3 rounded-lg bg-bg px-3 py-2 text-[13px]">
              <strong className="font-semibold">Abandoned</strong> by {people.who(inspection.abandoned_by)}
              {inspection.abandoned_at && `, ${formatWhen(inspection.abandoned_at)}`}: {inspection.abandon_reason}
            </p>
          )}
        </Card>

        <div className="flex flex-wrap items-start gap-5">
          <Card
            flush
            className="min-w-0 flex-[999_1_620px]"
            title={<span className="text-lg">Items</span>}
            description="The wording is the copy the inspector saw. Opening a photo is recorded in the evidence access log."
          >
            {inspection.items.map((it) => (
              <div key={it.key} className="flex gap-3.5 border-t border-line-soft px-[18px] py-3.5">
                <div className="w-5 pt-0.5 text-[13px] text-muted">{it.position}</div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{it.prompt}</div>
                  <div className="mt-0.5 text-xs text-muted">
                    {it.source_ref ?? "No regulation given"} ·{" "}
                    {it.answered_at ? `answered ${formatWhen(it.answered_at)}` : "not answered"}
                  </div>
                  {it.comment && <div className="mt-1.5 rounded-md bg-bg px-2.5 py-2 text-[13px]">“{it.comment}”</div>}
                  {it.evidence.length > 0 && (
                    <div className="mt-2.5 flex flex-wrap gap-2.5">
                      {it.evidence.map((p) => (
                        <div key={p.evidence_id}>
                          <PhotoButton siteId={site.id} evidenceId={p.evidence_id} label={it.prompt} />
                          <div className="mt-1 text-[11px] text-muted">Taken {formatWhen(p.client_captured_at)}</div>
                          {p.captured_before_start && <Badge tone="warn">Taken before start</Badge>}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
                <div className="flex flex-col items-end gap-1.5">
                  {it.result ? <Badge tone={RESULT[it.result].tone}>{RESULT[it.result].text}</Badge> : <Badge>Unanswered</Badge>}
                  {it.severity && <Badge tone={it.severity}>{it.severity}</Badge>}
                </div>
              </div>
            ))}
          </Card>

          <div className="flex flex-[1_1_320px] flex-col gap-4">
            <Card title="Findings" description="Raised when the inspection was completed, one per fail. They never change.">
              {inspection.findings.length === 0 ? (
                <p className="text-sm">{inspection.status === "completed" ? "No fails, so no findings." : "None: only a completed inspection raises findings."}</p>
              ) : (
                inspection.findings.map((f) => (
                  <Row key={f.item_key} aside={<Badge tone={f.severity}>{f.severity}</Badge>}>
                    {prompts.get(f.item_key) ?? f.item_key}
                  </Row>
                ))
              )}
            </Card>

            <Card
              title="Corrections"
              description="A completed inspection never changes. To correct it, the inspector opens Inspect on the phone and chooses it under “Correct a completed inspection”: a new inspection on the same checklist that links back here. One correction per inspection."
            >
              <div className="space-y-1.5 text-sm">
                <p>
                  {inspection.corrects_id ? (
                    <>This inspection corrects <Link href={`/inspections/${inspection.corrects_id}`} className="link">an earlier one</Link>.</>
                  ) : (
                    "This inspection doesn't correct another."
                  )}
                </p>
                {correction && (
                  <p>
                    Corrected by{" "}
                    <Link href={`/inspections/${correction.id}`} className="link">
                      {correction.template_title}, {formatWhen(correction.started_at)}
                    </Link>{" "}
                    ({label(correction.status).toLowerCase()}).
                  </p>
                )}
              </div>
            </Card>

            {inspection.status === "in_progress" && (
              <Card title="Abandon" description="For an inspection started by mistake, or whose inspector has left. It's final.">
                <AbandonForm siteId={site.id} inspectionId={inspection.id} />
              </Card>
            )}

            {severities.length > 0 && (
              <Card title="Severity scale">
                <div className="space-y-2 text-[13px] leading-relaxed">
                  {severities.map((s) => (
                    <div key={s.key}><Badge tone={s.key}>{s.key}</Badge> {s.definition}</div>
                  ))}
                </div>
              </Card>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
