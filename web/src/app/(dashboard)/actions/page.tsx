import { randomUUID } from "node:crypto";

import type { Metadata } from "next";
import Link from "next/link";

import { PhotoButton } from "@/components/photo";
import { Forbidden, NoSite } from "@/components/states";
import { Alert, Badge, Card, EmptyState, PageHeader, Row, Tabs, type Tone } from "@/components/ui";
import { getAction, getMe, listActions, listFindings } from "@/lib/data";
import { formatDateOnly, formatWhen, label, shortId } from "@/lib/format";
import { can, currentSite, sitePeople } from "@/lib/site";
import type { ActionRequestKind, ActionState, ActionSummary, Finding, Severity } from "@/lib/types";

import { ChangeForm, RaiseForm, RetractWaiverForm, WaiveForm, type OwnerOption } from "./forms";

export const metadata: Metadata = { title: "Findings & actions" };

const STATE_TONE: Record<ActionState, Tone> = {
  open: "warn",
  overdue: "bad",
  awaiting_verification: "neutral",
  closed: "ok",
  closed_late: "ok",
  cancelled: "neutral",
};

// Product defaults from docs/api/actions.md (days after the day raised).
const DEFAULT_DAYS: Record<Severity, number> = { low: 14, medium: 7, high: 1, critical: 0 };

const TABS = {
  open: { label: "Open", states: ["open", "awaiting_verification"] },
  overdue: { label: "Overdue", states: ["overdue"] },
  unactioned: { label: "No action yet", states: [] },
  waived: { label: "Waived", states: [] },
  closed: { label: "Closed", states: ["closed", "closed_late", "cancelled"] },
} satisfies Record<string, { label: string; states: ActionState[] }>;
// Tabs that list findings rather than actions.
const FINDING_TABS: string[] = ["unactioned", "waived"];
type TabKey = keyof typeof TABS;

const findingKey = (inspectionId: string, itemKey: string) => `${inspectionId}:${itemKey}`;

// What the action's state allows. Who may do each is the API's call.
function allowedKinds(a: ActionSummary): ActionRequestKind[] {
  if (a.state === "cancelled") return [];
  if (a.state === "closed" || a.state === "closed_late") return ["reopen"];
  if (a.state === "awaiting_verification") return ["verify", "reject", "reassign", "cancel"];
  return ["reassign", "move_due", "cancel"];
}

export default async function ActionsPage({ searchParams }: PageProps<"/actions">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Findings & corrective actions" />;

  const sp = await searchParams;
  const tab: TabKey = typeof sp.tab === "string" && sp.tab in TABS ? (sp.tab as TabKey) : "open";
  const actionId = typeof sp.action === "string" ? sp.action : null;
  const findingParam = typeof sp.finding === "string" ? sp.finding : null;

  const [actions, findings, people, me, selected] = await Promise.all([
    listActions(site.id),
    listFindings(site.id),
    sitePeople(site.id),
    getMe(),
    actionId ? getAction(site.id, actionId) : null,
  ]);
  if (actions === "forbidden" || findings === "forbidden") return <Forbidden title="Findings & corrective actions" />;

  const byKey = new Map(findings.map((f) => [findingKey(f.inspection_id, f.item_key), f]));
  const unactioned = findings.filter((f) => f.coverage === "unactioned");
  const waived = findings.filter((f) => f.coverage === "waived" && f.waiver_id);
  const canWaive = await can("waive_findings");
  const inTab = (key: TabKey) => actions.filter((a) => (TABS[key].states as ActionState[]).includes(a.state));
  const rows = FINDING_TABS.includes(tab) ? [] : inTab(tab);
  const selectedFinding = findingParam ? byKey.get(findingParam) : undefined;

  // People an action can be given to. A supervisor can't list the site's
  // members, so they can only pick themselves.
  const owners: OwnerOption[] = [
    { id: me.user_id, name: "Me" },
    ...people.members
      .filter((m) => m.user_id !== me.user_id && ["owner", "she_officer", "supervisor"].includes(m.role))
      .map((m) => ({ id: m.user_id, name: m.full_name })),
  ];

  const sourceOf = (a: ActionSummary) => {
    const f = a.inspection_id && a.item_key ? byKey.get(findingKey(a.inspection_id, a.item_key)) : undefined;
    if (f) return { title: f.prompt, sub: `${f.template_title} · ${formatWhen(f.raised_at)}`, href: `/inspections/${f.inspection_id}` };
    if (a.incident_id) return { title: `Incident ${shortId(a.incident_id)}`, sub: `Version ${a.incident_version}`, href: `/incidents/${a.incident_id}` };
    return { title: "Inspection finding", sub: "", href: a.inspection_id ? `/inspections/${a.inspection_id}` : "/inspections" };
  };

  return (
    <>
      <PageHeader
        title="Findings & corrective actions"
        description="Every failed inspection item raises a finding. Actions come from findings and incidents; closing one needs a photo."
      />
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[999_1_620px]">
          <Tabs
            tabs={(Object.keys(TABS) as TabKey[]).map((key) => ({
              href: `/actions?tab=${key}`,
              label: TABS[key].label,
              count: key === "unactioned" ? unactioned.length : key === "waived" ? waived.length : inTab(key).length,
              active: tab === key,
            }))}
          />
          {tab === "unactioned" ? (
            unactioned.length === 0 ? (
              <div className="p-4"><EmptyState title="Every finding has an action or a waiver" /></div>
            ) : (
              <div className="overflow-x-auto">
                <table className="tbl">
                  <thead><tr><th>Finding</th><th>Severity</th><th>Raised</th><th /></tr></thead>
                  <tbody>
                    {unactioned.map((f: Finding) => (
                      <tr key={findingKey(f.inspection_id, f.item_key)}>
                        <td>
                          <strong className="font-semibold">{f.prompt}</strong>
                          <div className="text-xs text-muted">
                            <Link href={`/inspections/${f.inspection_id}`} className="link">{f.template_title}</Link>
                          </div>
                        </td>
                        <td><Badge tone={f.severity}>{f.severity}</Badge></td>
                        <td>{formatWhen(f.raised_at)}</td>
                        <td>
                          <Link href={`/actions?tab=unactioned&finding=${encodeURIComponent(findingKey(f.inspection_id, f.item_key))}`} className="link">
                            Raise action
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          ) : tab === "waived" ? (
            waived.length === 0 ? (
              <div className="p-4"><EmptyState title="No findings waived" /></div>
            ) : (
              <div className="overflow-x-auto">
                <table className="tbl">
                  <thead><tr><th>Finding</th><th>Severity</th><th>Raised</th><th /></tr></thead>
                  <tbody>
                    {waived.map((f) => (
                      <tr key={findingKey(f.inspection_id, f.item_key)}>
                        <td>
                          <strong className="font-semibold">{f.prompt}</strong>
                          <div className="text-xs text-muted">
                            <Link href={`/inspections/${f.inspection_id}`} className="link">{f.template_title}</Link>
                          </div>
                        </td>
                        <td><Badge tone={f.severity}>{f.severity}</Badge></td>
                        <td>{formatWhen(f.raised_at)}</td>
                        <td className="min-w-[220px]">
                          {canWaive ? (
                            <details>
                              <summary className="link cursor-pointer text-sm">Take the waiver back</summary>
                              <RetractWaiverForm siteId={site.id} waiverId={f.waiver_id!} />
                            </details>
                          ) : (
                            <Badge>No action needed</Badge>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
          ) : rows.length === 0 ? (
            <div className="p-4"><EmptyState title={`Nothing ${TABS[tab].label.toLowerCase()}`} /></div>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead><tr><th>From</th><th>Severity</th><th>Action</th><th>Owner</th><th>Due</th><th>Status</th></tr></thead>
                <tbody>
                  {rows.map((a) => {
                    const src = sourceOf(a);
                    return (
                      <tr key={a.id}>
                        <td>
                          <strong className="font-semibold">{src.title}</strong>
                          <div className="text-xs text-muted">{src.sub}</div>
                          {a.source_corrected_by && <div className="mt-1.5"><Badge>Inspection since corrected</Badge></div>}
                          {a.review && <div className="mt-1.5"><Badge tone="warn">Review: {label(a.review)}</Badge></div>}
                        </td>
                        <td><Badge tone={a.severity}>{a.severity}</Badge></td>
                        <td><Link href={`/actions?tab=${tab}&action=${a.id}`} className="link">{a.title}</Link></td>
                        <td>
                          {people.who(a.owner_id)}
                          {a.responsible_party && <div className="text-xs text-muted">{a.responsible_party}</div>}
                        </td>
                        <td className="whitespace-nowrap">{formatDateOnly(a.due_on)}</td>
                        <td><Badge tone={STATE_TONE[a.state]}>{label(a.state)}</Badge></td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <div className="flex flex-[1_1_340px] flex-col gap-4">
          {selected ? (
            <Card>
              <div className="text-xs text-muted">Action {shortId(selected.id)}</div>
              <h2 className="mt-0.5 font-head text-lg font-semibold">{selected.title}</h2>
              <div className="mt-2 flex flex-wrap gap-1.5">
                <Badge tone={selected.severity}>{selected.severity}</Badge>
                <Badge tone={STATE_TONE[selected.state]}>{label(selected.state)}</Badge>
              </div>
              <p className="mt-3 text-[13px] leading-relaxed text-muted">
                From: <Link href={sourceOf(selected).href} className="link">{sourceOf(selected).title}</Link>. Owner{" "}
                {people.who(selected.owner_id)}, due by the end of {formatDateOnly(selected.due_on)}.
                {selected.description && ` ${selected.description}`}
              </p>

              <h3 className="mt-4 border-t border-line-soft pt-3.5 text-sm font-semibold">Close-outs</h3>
              <p className="mt-1 text-[13px] text-muted">
                Submitted from the phone with at least one photo of the fix, then verified here.
              </p>
              {selected.closeouts.length === 0 && <p className="mt-2 text-sm">None yet.</p>}
              {selected.closeouts.map((c) => (
                <div key={c.id} className="mt-3 rounded-lg border border-line-soft p-3 text-[13px]">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge tone={c.decision === "verified" ? "ok" : c.decision === "rejected" ? "bad" : "warn"}>
                      {c.decision ? label(c.decision) : "Awaiting verification"}
                    </Badge>
                    {c.late && <Badge tone="warn">Late</Badge>}
                    <span className="text-muted">{people.who(c.submitted_by)}, {formatWhen(c.client_submitted_at)}</span>
                  </div>
                  {c.note && <p className="mt-2">“{c.note}”</p>}
                  <div className="mt-2 flex flex-wrap gap-2.5">
                    {c.evidence.map((p) => (
                      <div key={p.evidence_id}>
                        <PhotoButton siteId={site.id} evidenceId={p.evidence_id} label={`Close-out photo, ${selected.title}`} />
                        <div className="mt-1 text-[11px] text-muted">Taken {formatWhen(p.client_captured_at)}</div>
                        {p.captured_before_raised && <Badge>Before it was raised</Badge>}
                      </div>
                    ))}
                  </div>
                </div>
              ))}

              {allowedKinds(selected).length > 0 && (
                <div className="mt-4 border-t border-line-soft pt-3.5">
                  <ChangeForm
                    key={selected.events.length}
                    id={randomUUID()}
                    siteId={site.id}
                    actionId={selected.id}
                    kinds={allowedKinds(selected)}
                    closeoutId={selected.pending_closeout_id}
                    owners={owners}
                    ownerId={selected.owner_id}
                    responsibleParty={selected.responsible_party}
                    dueOn={selected.due_on}
                  />
                </div>
              )}

              <h3 className="mt-4 border-t border-line-soft pt-3.5 text-sm font-semibold">History</h3>
              {selected.events.map((e) => (
                <Row key={e.id} aside={<span className="text-xs text-muted">{formatWhen(e.recorded_at)}</span>}>
                  <span className="font-medium">{label(e.kind)}</span>{" "}
                  <span className="text-muted">by {people.who(e.actor_id)}{e.self_verified ? " (own close-out)" : ""}</span>
                  {e.reason && <div className="text-[13px] text-muted">“{e.reason}”</div>}
                </Row>
              ))}
            </Card>
          ) : actionId ? (
            <Alert tone="info">That action isn&apos;t on this site.</Alert>
          ) : selectedFinding ? (
            <>
              <Card
                title="Raise an action"
                description={<>{selectedFinding.prompt} · <Badge tone={selectedFinding.severity}>{selectedFinding.severity}</Badge></>}
              >
                <RaiseForm
                  id={randomUUID()}
                  siteId={site.id}
                  source={{ inspection_id: selectedFinding.inspection_id, item_key: selectedFinding.item_key }}
                  owners={owners}
                  defaultDays={DEFAULT_DAYS[selectedFinding.severity]}
                />
              </Card>
              {(selectedFinding.severity === "low" || selectedFinding.severity === "medium") && canWaive && (
                <Card title="Or: no action needed" description="Only low and medium findings can be waived, by an owner or SHE officer.">
                  <WaiveForm id={randomUUID()} siteId={site.id} inspectionId={selectedFinding.inspection_id} itemKey={selectedFinding.item_key} />
                </Card>
              )}
            </>
          ) : (
            <Card title="Pick a row">
              <p className="text-sm text-muted">
                Open an action to verify a close-out, reassign it or move its due date. Under “No action yet”, raise an
                action for a finding or waive a low or medium one.
              </p>
            </Card>
          )}
        </div>
      </div>
    </>
  );
}
