import { randomUUID } from "node:crypto";

import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { DeadlineBody } from "@/components/deadline";
import { PhotoButton } from "@/components/photo";
import { NoSite } from "@/components/states";
import { Badge, Card, Facts, PageHeader, Row, buttonClass } from "@/components/ui";
import { getIncident, getMe } from "@/lib/data";
import { formatWhen, isoToSASTInput, label, shortId } from "@/lib/format";
import { currentSite, sitePeople } from "@/lib/site";

import { RaiseForm } from "../../actions/forms";
import { StatementForm } from "./statement-form";

export const metadata: Metadata = { title: "Incident" };

export default async function IncidentPage({ params, searchParams }: PageProps<"/incidents/[id]">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Incident" />;

  const { id } = await params;
  const { version } = await searchParams;
  const [incident, people, me] = await Promise.all([
    getIncident(site.id, id, typeof version === "string" ? version : undefined),
    sitePeople(site.id),
    getMe(),
  ]);
  if (!incident) notFound();

  const v = incident.version;
  const latest = v.version === incident.latest_version;
  const ref = `INC-${shortId(incident.id)}`;
  // Live rows first; superseded and withdrawn ones stay visible as history.
  const closed = (s: string) => s === "superseded" || s === "withdrawn";
  const deadlines = [...incident.deadlines].sort((a, b) => Number(closed(a.state)) - Number(closed(b.state)));
  const owners = [
    { id: me.user_id, name: "Me" },
    ...people.members
      .filter((m) => m.user_id !== me.user_id && ["owner", "she_officer", "supervisor"].includes(m.role))
      .map((m) => ({ id: m.user_id, name: m.full_name })),
  ];

  return (
    <>
      <PageHeader
        eyebrow={<><Link href="/incidents" className="link">Incident register</Link> / {ref}</>}
        title={`${ref}${v.location ? ` · ${v.location}` : ""}`}
        badges={
          <>
            {v.types.map((t) => <Badge key={t.key} tone={t.reportable ? "bad" : "neutral"}>{t.label}</Badge>)}
            <Badge>Version {v.version} of {incident.latest_version}</Badge>
          </>
        }
      >
        <Link href="/injury-on-duty" className={buttonClass("secondary")}>Injury-on-duty claim</Link>
        <Link href={`/incidents/${incident.id}/amend`} className={buttonClass("primary")}>Amend (new version)</Link>
      </PageHeader>

      <div className="flex flex-wrap items-start gap-5">
        <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
          <Card title={`Account (version ${v.version})`}>
            <Facts
              min={180}
              items={[
                ["Occurred", formatWhen(v.occurred_at)],
                ["Employer became aware", v.employer_aware_at ? formatWhen(v.employer_aware_at) : "Not recorded"],
                ["Location", v.location ?? "Not given"],
                ["Submitted", `${formatWhen(v.submitted_at)} by ${people.who(v.submitted_by)}`],
              ]}
            />
            {v.description && <p className="mt-3.5 text-sm leading-relaxed whitespace-pre-line">{v.description}</p>}
            {v.amend_reason && (
              <div className="mt-3.5 rounded-lg bg-bg px-3 py-2.5 text-[13px]">
                <strong className="font-semibold">Why amended:</strong> {v.amend_reason}
              </div>
            )}
            {!latest && (
              <p className="mt-3 text-[13px] text-muted">
                This is an earlier version. <Link href={`/incidents/${incident.id}`} className="link">Open the latest</Link>.
              </p>
            )}
          </Card>

          <Card title="People" description="Injury details are health information. Who may see them is still to be decided before launch.">
            {v.people.length === 0 && <p className="text-sm">Nobody listed.</p>}
            <div className="flex flex-col gap-3">
              {v.people.map((p) => (
                <div key={p.position} className="rounded-lg border border-line-soft p-3">
                  <Facts
                    min={150}
                    items={[
                      ["Name", p.name ?? "Not named"],
                      ["Role", p.occupation ?? "Not given"],
                      ["Employee of this organisation", p.employee_of_org ? "Yes" : `No${p.employer_name ? ` (${p.employer_name})` : ""}`],
                      ["Injury", p.injured ? [p.injury_description, p.body_part].filter(Boolean).join("; ") || "Injured" : "Not injured"],
                      ["Treatment", p.treatment ? label(p.treatment) : "Not recorded"],
                      ...(p.died_at ? ([["Died", formatWhen(p.died_at)]] as [string, string][]) : []),
                    ]}
                  />
                </div>
              ))}
            </div>
          </Card>

          {v.evidence.length > 0 && (
            <Card title="Photos" description="Opening a photo is recorded in the evidence access log.">
              <div className="flex flex-wrap gap-2.5">
                {v.evidence.map((p) => (
                  <div key={p.evidence_id}>
                    <PhotoButton siteId={site.id} evidenceId={p.evidence_id} label={`Photo for ${ref}`} />
                    <div className="mt-1 text-[11px] text-muted">Taken {formatWhen(p.client_captured_at)}</div>
                  </div>
                ))}
              </div>
            </Card>
          )}

          <Card title="Witness statements">
            {incident.witness_statements.length === 0 && <p className="mb-3 text-sm">None taken yet.</p>}
            {incident.witness_statements.map((s) => (
              <Row key={s.id} aside={s.superseded_by ? <Badge>Corrected by a later statement</Badge> : undefined}>
                <strong className="font-semibold">{s.witness_name}</strong>{" "}
                <span className="text-muted">
                  · taken by {people.who(s.taken_by)}, {formatWhen(s.taken_at)}
                  {s.evidence && " · signed statement photo attached"}
                </span>
                <div className="mt-1 whitespace-pre-line">“{s.statement}”</div>
              </Row>
            ))}
            <details className="mt-3 border-t border-line-soft pt-3">
              <summary className="link cursor-pointer text-sm">Add a witness statement</summary>
              <div className="mt-3">
                <StatementForm
                  key={incident.witness_statements.length}
                  id={randomUUID()}
                  siteId={site.id}
                  incidentId={incident.id}
                  now={isoToSASTInput(new Date().toISOString())}
                />
              </div>
            </details>
          </Card>

          <Card title="Versions" description="Every version stays readable as it was submitted.">
            {Array.from({ length: incident.latest_version }, (_, i) => incident.latest_version - i).map((n) => (
              <Row key={n} aside={n === v.version ? <Badge tone="ok">Showing</Badge> : undefined}>
                <Link href={`/incidents/${incident.id}?version=${n}`} className="link">Version {n}</Link>
                {n === v.version && (
                  <span className="text-muted">
                    {" "}· deadlines added {incident.changes.added.length}, carried {incident.changes.carried.length},
                    superseded {incident.changes.superseded.length}, withdrawn {incident.changes.withdrawn.length}
                  </span>
                )}
              </Row>
            ))}
          </Card>

          {latest && (
            <Card title="Corrective action" description="Raised against this version. Later versions never change it.">
              <details>
                <summary className="link cursor-pointer text-sm">Raise an action from this incident</summary>
                <div className="mt-3">
                  <RaiseForm
                    id={randomUUID()}
                    siteId={site.id}
                    source={{ incident_id: incident.id, version: incident.latest_version }}
                    owners={owners}
                  />
                </div>
              </details>
            </Card>
          )}
        </div>

        <Card
          className="flex-[1_1_380px]"
          title="Legal deadlines"
          description="Computed by the server from the regime's rules. Days are counted from the day after the incident, to the end of the last day (SAST). Guidance, not legal advice."
        >
          {deadlines.length === 0 && <p className="text-sm">No clocks: nothing about this incident starts one.</p>}
          <div className="flex flex-col gap-3">
            {deadlines.map((d) => (
              <div key={d.id} className={`rounded-lg border border-line p-3 ${closed(d.state) ? "opacity-70" : ""}`}>
                <DeadlineBody d={d} />
                {(d.state === "due_now" || d.state === "open" || d.state === "overdue") && (
                  <Link href={`/deadlines?deadline=${d.id}`} className={buttonClass(d.state === "due_now" ? "now" : "secondary", "sm") + " mt-2.5"}>
                    {d.state === "due_now" ? "Log the call" : "Record it as met"}
                  </Link>
                )}
              </div>
            ))}
          </div>
        </Card>
      </div>
    </>
  );
}
