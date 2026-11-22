import type { Metadata } from "next";
import Link from "next/link";

import { chooseSite } from "@/app/actions/site";
import { Forbidden, NoSite } from "@/components/states";
import { Badge, Card, PageHeader, buttonClass } from "@/components/ui";
import { CATEGORY_LABELS, COLOUR_TONE, colourLabel } from "@/lib/compliance";
import { getRollup, listAllActions, listDeadlines } from "@/lib/data";
import { daysUntil, formatDay, formatWhen } from "@/lib/format";
import { can, currentSite } from "@/lib/site";
import type { CategoryKey, Deadline, SiteRank } from "@/lib/types";

export const metadata: Metadata = { title: "Overview" };

// The categories a site row lists when they aren't green, in this order.
const ROW_CATEGORIES: CategoryKey[] = [
  "incident_deadlines",
  "site_work_deadlines",
  "findings",
  "actions",
  "verifications",
  "sign_ins",
  "hs_file",
  "appointments",
  "worker_records",
];
// A call-now deadline makes incident_deadlines red, so only red sites are
// asked for them. Capped so a bad day can't fan out without limit.
const MAX_CALL_NOW_SITES = 12;

const sum = (sites: SiteRank[], key: CategoryKey) => sites.reduce((n, s) => n + (s.counts[key] ?? 0), 0);

// A button that switches the sidebar to a site and opens one of its pages.
function SiteLink({ site, to, children, className }: { site: { id: string }; to: string; children: React.ReactNode; className?: string }) {
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

  // The rollup (M12): every site the caller can open, worst first, in one call.
  const [rollup, overdue, awaiting, canExport] = await Promise.all([
    getRollup(),
    listAllActions({ state: "overdue" }),
    listAllActions({ state: "awaiting_verification" }),
    can("export_audit_pack"),
  ]);
  if (rollup === "forbidden" || overdue === "forbidden" || awaiting === "forbidden") {
    return <Forbidden title="Compliance overview" />;
  }

  const redSites = rollup.sites.filter((s) => s.statuses.incident_deadlines === "red").slice(0, MAX_CALL_NOW_SITES);
  const callNowLists = await Promise.all(redSites.map((s) => listDeadlines(s.site_id, "due_now")));
  const callNow = callNowLists.flatMap((list, i) =>
    list === "forbidden" ? [] : list.map((d: Deadline) => ({ d, site: redSites[i] })),
  );

  const oldest = overdue.reduce((min, a) => Math.min(min, daysUntil(a.due_at)), 0);
  const findingsRed = rollup.sites.filter((s) => s.statuses.findings === "red").length;
  const deadlineCount = sum(rollup.sites, "incident_deadlines") + sum(rollup.sites, "site_work_deadlines");
  const stalled = sum(rollup.sites, "verifications");
  const orgItems = rollup.org_items.filter((c) => c.count > 0);
  const pendingUploads = sum(rollup.sites, "pending_uploads") + (rollup.org_items.find((c) => c.key === "pending_uploads")?.count ?? 0);

  const kpis = [
    {
      label: "Findings with no action",
      value: sum(rollup.sites, "findings"),
      sub: findingsRed ? `High or critical on ${findingsRed} site${findingsRed === 1 ? "" : "s"}` : "None high or critical",
    },
    {
      label: "Overdue corrective actions",
      value: overdue.length,
      sub: overdue.length ? `Oldest ${Math.abs(oldest)} day${Math.abs(oldest) === 1 ? "" : "s"} over` : "None overdue",
    },
    { label: "Legal deadlines needing attention", value: deadlineCount, sub: `${callNow.length} call-now` },
    {
      label: "Close-outs to verify",
      value: awaiting.length,
      sub: stalled ? `${stalled} waiting more than 2 days` : "Waiting for an owner or SHE officer",
    },
  ];

  return (
    <>
      <PageHeader
        title="Compliance overview"
        description={`${formatDay(rollup.computed_at)} · ${rollup.sites.length} site${rollup.sites.length === 1 ? "" : "s"}, worst first · times in SAST`}
      >
        {canExport && (
          <Link href="/audit-pack" className={buttonClass("secondary")}>
            Export audit pack
          </Link>
        )}
      </PageHeader>

      <div className="flex flex-col gap-5">
        {callNow.map(({ d, site }) => (
          <div key={d.id} className="flex flex-wrap items-center gap-3.5 rounded-[10px] bg-critical px-[18px] py-3.5 text-critical-ink">
            <div className="min-w-0 flex-[1_1_320px]">
              <div className="font-semibold">Immediate notice not logged · {site.site_name}</div>
              <div className="text-[13px] opacity-90">
                {d.source_ref}: {d.label}. Running since {formatWhen(d.basis_at)}.
              </div>
            </div>
            <SiteLink site={{ id: site.site_id }} to={`/deadlines?deadline=${d.id}`} className={buttonClass("secondary") + " cursor-pointer"}>
              Log the call
            </SiteLink>
          </div>
        ))}

        {orgItems.length > 0 && (
          <Card title="For the organisation" description="Not tied to one site, so shown once.">
            {orgItems.map((c) => (
              <div key={c.key} className="flex flex-wrap items-center justify-between gap-2 border-t border-line-soft py-2.5 text-sm first:border-t-0">
                <span>
                  <strong className="font-semibold">{CATEGORY_LABELS[c.key]}</strong>
                  <span className="text-muted"> · {c.count}</span>
                </span>
                {c.status && <Badge tone={COLOUR_TONE[c.status]}>{colourLabel(c.status)}</Badge>}
              </div>
            ))}
            <p className="mt-2 text-xs text-muted">
              Statutory documents are under <Link href="/organisation" className="link">Organisation</Link>. Uploads
              not finished need whoever uploaded them to try again from their phone.
            </p>
          </Card>
        )}

        <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(200px,1fr))]">
          {kpis.map((k) => (
            <div key={k.label} className="rounded-[10px] border border-line bg-surface px-[18px] py-4">
              <div className="text-[13px] text-muted">{k.label}</div>
              <div className="mt-1 font-head text-[34px] leading-tight font-semibold">{k.value}</div>
              <div className="text-[13px] text-muted">{k.sub}</div>
            </div>
          ))}
        </div>

        <Card flush title={<span className="text-lg">Sites</span>} description="Red: something is already wrong. Amber: about to go wrong, or may not apply. Open a site for every item.">
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead>
                <tr>
                  <th>Site</th>
                  <th>Status</th>
                  <th>Red · amber</th>
                  <th>Needs attention</th>
                </tr>
              </thead>
              <tbody>
                {rollup.sites.map((s) => {
                  const flagged = ROW_CATEGORIES.filter((k) => s.statuses[k] && s.statuses[k] !== "green");
                  return (
                    <tr key={s.site_id}>
                      <td>
                        <SiteLink site={{ id: s.site_id }} to="/compliance">{s.site_name}</SiteLink>
                        {s.site_status !== "active" && <div className="text-xs text-muted">{s.site_status}</div>}
                      </td>
                      <td><Badge tone={COLOUR_TONE[s.status]}>{colourLabel(s.status)}</Badge></td>
                      <td className="whitespace-nowrap">{s.red_items} · {s.amber_items}</td>
                      <td>
                        <div className="flex flex-wrap gap-1.5">
                          {flagged.length === 0 && <Badge tone="ok">Nothing</Badge>}
                          {flagged.map((k) => (
                            <Badge key={k} tone={COLOUR_TONE[s.statuses[k]]}>
                              {CATEGORY_LABELS[k]} {s.counts[k] ?? 0}
                            </Badge>
                          ))}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>

        <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(320px,1fr))]">
          <Card title="Worker records expiring" description="Competencies and medicals of workers on a site's roster, within 30 days">
            <p className="text-sm">
              <strong className="font-semibold">{sum(rollup.sites, "worker_records")}</strong> across your sites.{" "}
              <Link href="/people" className="link">Open the register</Link>
            </p>
          </Card>
          <Card title="Uploads not finished" description="Photos and files a phone declared more than 24 hours ago and never completed">
            <p className="text-sm">
              <strong className="font-semibold">{pendingUploads}</strong>.{" "}
              <Link href="/evidence?status=pending" className="link">Pending photos</Link> stay pending until the phone
              tries again. Run a chain check from <Link href="/evidence" className="link">Evidence</Link>.
            </p>
          </Card>
        </div>
      </div>
    </>
  );
}
