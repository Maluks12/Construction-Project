import type { Metadata } from "next";
import Link from "next/link";

import { chooseSite } from "@/app/actions/site";
import { Forbidden, NoSite } from "@/components/states";
import { Badge, Card, PageHeader, buttonClass } from "@/components/ui";
import { listAllActions, listDeadlines, listFindings, listInspections } from "@/lib/data";
import { daysUntil, formatDay, formatWhen } from "@/lib/format";
import { currentSite } from "@/lib/site";
import type { Deadline, Finding, Severity, Site } from "@/lib/types";

export const metadata: Metadata = { title: "Overview" };

const SEVERITIES: Severity[] = ["critical", "high", "medium", "low"];
// Each site costs three API calls, so the table stops at this many.
const MAX_SITES = 12;

const isOpen = (d: Deadline) => d.state === "open" || d.state === "due_now" || d.state === "overdue";

// A button that switches the sidebar to a site and opens one of its pages.
function SiteLink({ site, to, children, className }: { site: Site; to: string; children: React.ReactNode; className?: string }) {
  return (
    <form action={chooseSite} className="inline">
      <input type="hidden" name="site_id" value={site.id} />
      <input type="hidden" name="back" value={to} />
      <button type="submit" className={className ?? "link cursor-pointer text-left"}>
        {children}
      </button>
    </form>
  );
}

export default async function OverviewPage() {
  const { sites } = await currentSite();
  if (sites.length === 0) return <NoSite title="Compliance overview" />;

  const active = sites.filter((s) => s.status !== "archived").slice(0, MAX_SITES);
  const [overdue, awaiting, perSite] = await Promise.all([
    listAllActions({ state: "overdue" }),
    listAllActions({ state: "awaiting_verification" }),
    Promise.all(
      active.map(async (site) => {
        const [findings, deadlines, inspections] = await Promise.all([
          listFindings(site.id, "unactioned"),
          listDeadlines(site.id),
          listInspections(site.id),
        ]);
        return { site, findings, deadlines, inspections };
      }),
    ),
  ]);
  if (overdue === "forbidden" || awaiting === "forbidden") return <Forbidden title="Compliance overview" />;

  const rows = perSite.map((r) => ({
    site: r.site,
    findings: r.findings === "forbidden" ? [] : r.findings,
    deadlines: (r.deadlines === "forbidden" ? [] : r.deadlines).filter(isOpen),
    last: r.inspections === "forbidden" ? undefined : r.inspections[0],
    overdue: overdue.filter((a) => a.site_id === r.site.id).length,
  }));

  const findings: Finding[] = rows.flatMap((r) => r.findings);
  const deadlines = rows.flatMap((r) => r.deadlines.map((d) => ({ d, site: r.site })));
  const callNow = deadlines.filter(({ d }) => d.state === "due_now");
  const soon = deadlines.filter(({ d }) => d.state !== "open" || daysUntil(d.due_at) <= 2);
  const count = (sev: Severity) => findings.filter((f) => f.severity === sev).length;
  const oldest = overdue.reduce((min, a) => Math.min(min, daysUntil(a.due_at)), 0);

  const kpis = [
    { label: "Findings with no action", value: findings.length, sub: `${count("critical")} critical, ${count("high")} high` },
    { label: "Overdue corrective actions", value: overdue.length, sub: overdue.length ? `Oldest ${Math.abs(oldest)} day${Math.abs(oldest) === 1 ? "" : "s"} over` : "None overdue" },
    { label: "Legal deadlines in 48 h", value: soon.length, sub: `${callNow.length} call-now` },
    { label: "Close-outs to verify", value: awaiting.length, sub: "Waiting for an owner or SHE officer" },
  ];

  return (
    <>
      <PageHeader
        title="Compliance overview"
        description={`${formatDay(new Date().toISOString())} · ${active.length} active site${active.length === 1 ? "" : "s"} · times in SAST`}
      >
        <Link href="/audit-pack" className={buttonClass("secondary")}>
          Export audit pack
        </Link>
      </PageHeader>

      <div className="flex flex-col gap-5">
        {callNow.map(({ d, site }) => (
          <div key={d.id} className="flex flex-wrap items-center gap-3.5 rounded-[10px] bg-critical px-[18px] py-3.5 text-critical-ink">
            <div className="min-w-0 flex-[1_1_320px]">
              <div className="font-semibold">Immediate notice not logged · {site.name}</div>
              <div className="text-[13px] opacity-90">
                {d.source_ref}: {d.label}. Running since {formatWhen(d.basis_at)}.
              </div>
            </div>
            <SiteLink site={site} to="/deadlines" className={buttonClass("secondary") + " cursor-pointer"}>
              Log the call
            </SiteLink>
          </div>
        ))}

        <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(200px,1fr))]">
          {kpis.map((k) => (
            <div key={k.label} className="rounded-[10px] border border-line bg-surface px-[18px] py-4">
              <div className="text-[13px] text-muted">{k.label}</div>
              <div className="mt-1 font-head text-[34px] leading-tight font-semibold">{k.value}</div>
              <div className="text-[13px] text-muted">{k.sub}</div>
            </div>
          ))}
        </div>

        <Card
          flush
          title={<span className="text-lg">Sites</span>}
          actions={sites.length > active.length ? <span className="text-[13px] text-muted">Showing {active.length} of {sites.length} sites</span> : undefined}
        >
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Site</th>
                  <th>Findings with no action</th>
                  <th>Overdue actions</th>
                  <th>Next deadline</th>
                  <th>Last inspection</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const next = r.deadlines[0];
                  return (
                    <tr key={r.site.id}>
                      <td>
                        <SiteLink site={r.site} to="/inspections">{r.site.name}</SiteLink>
                        <div className="text-xs text-muted">{r.site.address || "No address"}</div>
                      </td>
                      <td>
                        <div className="flex flex-wrap gap-1.5">
                          {r.findings.length === 0 && <Badge tone="ok">None</Badge>}
                          {SEVERITIES.map((sev) => {
                            const n = r.findings.filter((f) => f.severity === sev).length;
                            return n > 0 ? <Badge key={sev} tone={sev}>{n} {sev}</Badge> : null;
                          })}
                        </div>
                      </td>
                      <td>
                        <Badge tone={r.overdue ? "bad" : "ok"}>{r.overdue ? `${r.overdue} overdue` : "None"}</Badge>
                      </td>
                      <td>
                        {!next ? "None open" : next.state === "due_now" ? "Now" : next.due_on ? formatDay(next.due_at) : formatWhen(next.due_at)}
                        {next && <div className="text-xs text-muted">{next.source_ref} · {next.label}</div>}
                      </td>
                      <td>{r.last ? formatWhen(r.last.started_at) : "None yet"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>

        <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(320px,1fr))]">
          <Card title="Competencies expiring in 30 days" description="From the worker register">
            <p className="text-sm text-muted">
              Not connected yet. The worker register arrives with People &amp; documents (M11).
            </p>
          </Card>
          <Card title="Evidence chain and pending photos" description="Checked per site, on request">
            <p className="text-sm text-muted">
              Listing evidence is recorded in the access log, so it isn&apos;t loaded here. Open{" "}
              <Link href="/evidence" className="link">Evidence</Link> to see pending photos and run a chain check.
            </p>
          </Card>
        </div>
      </div>
    </>
  );
}
