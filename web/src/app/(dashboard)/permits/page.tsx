import { randomUUID } from "node:crypto";

import type { Metadata } from "next";
import Link from "next/link";

import { DeadlineBody } from "@/components/deadline";
import { Forbidden, NoSite } from "@/components/states";
import { Alert, Badge, Card, EmptyState, Facts, PageHeader, Row, buttonClass, cx } from "@/components/ui";
import { getSiteWork, getSiteWorkFacts, listSiteWork } from "@/lib/data";
import { formatDateOnly, formatDay, formatWhen, todaySAST } from "@/lib/format";
import { currentSite, sitePeople } from "@/lib/site";
import type { Deadline, SiteWork, SiteWorkCatalogue } from "@/lib/types";

import { SiteWorkForm } from "./forms";

export const metadata: Metadata = { title: "Notifications & permits" };

const live = (d: Deadline) => d.state !== "superseded" && d.state !== "withdrawn";
const unmet = (d: Deadline) => d.state === "open" || d.state === "due_now" || d.state === "overdue";
const one = (v: string | string[] | undefined) => (typeof v === "string" ? v : undefined);

export default async function PermitsPage({ searchParams }: PageProps<"/permits">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Notifications & permits" />;

  const sp = await searchParams;
  const workId = one(sp.work);
  const [catalogue, list, people, work] = await Promise.all([
    getSiteWorkFacts(site.id),
    listSiteWork(site.id),
    sitePeople(site.id),
    workId ? getSiteWork(site.id, workId, one(sp.version)) : null,
  ]);
  if (catalogue === "forbidden" || list === "forbidden") return <Forbidden title="Notifications & permits" />;

  const header = (
    <PageHeader
      title="Notifications & permits"
      description="What the Department of Employment and Labour must hear before construction work starts: the Reg 4 notification (7 clear days before) and, for larger work, the Reg 3 permit application (30 clear days before). Guidance, not legal advice."
    >
      {catalogue && (
        <Link href="/permits?new=1" className={buttonClass("primary")}>Record planned work</Link>
      )}
    </PageHeader>
  );

  if (!catalogue || !list) {
    return (
      <>
        {header}
        <Alert tone="info">This site&apos;s regulations aren&apos;t the Construction Regulations, so it has no Reg 3 or Reg 4 notices.</Alert>
      </>
    );
  }

  const factLabel = new Map(catalogue.facts.map((f) => [f.key, f.label]));

  return (
    <>
      {header}
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[999_1_520px]" title={<span className="text-lg">Planned work on this site</span>} description="The earliest start first. Each record keeps every version.">
          {list.length === 0 ? (
            <div className="p-4"><EmptyState title="No work recorded yet">Record each phase before it starts, so its notices are worked out in time.</EmptyState></div>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead><tr><th>Work</th><th>Starts</th><th>Involves</th><th>Notices</th></tr></thead>
                <tbody>
                  {list.map((w) => (
                    <tr key={w.id} className={cx(w.status === "cancelled" && "opacity-60")}>
                      <td>
                        <Link href={`/permits?work=${w.id}`} className="link">{w.description}</Link>
                        <div className="text-xs text-muted">v{w.latest_version}{w.status === "cancelled" && " · cancelled"}</div>
                      </td>
                      <td className="whitespace-nowrap">{formatDateOnly(w.starts_on)}{w.ends_on && <div className="text-xs text-muted">to {formatDateOnly(w.ends_on)}</div>}</td>
                      <td className="min-w-[200px] text-[13px]">{w.facts.length === 0 ? "Nothing that needs notice" : w.facts.map((f) => factLabel.get(f) ?? f).join("; ")}</td>
                      <td>
                        {w.overdue_deadlines > 0 ? (
                          <Badge tone="bad">{w.overdue_deadlines} overdue</Badge>
                        ) : w.open_deadlines > 0 ? (
                          <Badge tone="warn">{w.open_deadlines} open</Badge>
                        ) : (
                          <Badge tone="ok">None open</Badge>
                        )}
                        {w.next_due_at && <div className="mt-1 text-xs text-muted">next by {formatDay(w.next_due_at)}</div>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <div className="flex min-w-0 flex-[1_1_420px] flex-col gap-4">
          {sp.new ? (
            <Card title="Record planned work" description="Version 1. Changing it later is a new version; every version stays readable.">
              <SiteWorkForm
                siteId={site.id}
                // Made up here, once, so submitting twice records one piece of work.
                workId={randomUUID()}
                version={1}
                catalogue={catalogue}
                initial={{ description: "", starts_on: todaySAST(), ends_on: "", facts: [], status: "planned" }}
              />
            </Card>
          ) : work ? (
            <WorkDetail work={work} catalogue={catalogue} siteId={site.id} who={people.who} amend={!!sp.amend} />
          ) : workId ? (
            <Alert tone="info">That work isn&apos;t on this site.</Alert>
          ) : (
            <Card title="The rules" description="From the site's regulations. Each notice is due by the end of the day shown, in site time.">
              {catalogue.rules.map((r) => (
                <Row key={r.rule_key}>
                  <strong className="font-semibold">{r.label}</strong>
                  <div className="text-xs text-muted">
                    At least {r.period} {r.unit.replace(/_/g, " ")} before the start · binds the {r.duty_holder} · {r.source_ref}
                  </div>
                  {r.caveat && <div className="mt-1 text-xs text-warn">{r.caveat}</div>}
                </Row>
              ))}
            </Card>
          )}
        </div>
      </div>
    </>
  );
}

function WorkDetail({
  work,
  catalogue,
  siteId,
  who,
  amend,
}: {
  work: SiteWork;
  catalogue: SiteWorkCatalogue;
  siteId: string;
  who: (id: string) => string;
  amend: boolean;
}) {
  const v = work.version;
  const latest = v.version === work.latest_version;
  // Live rows first; superseded and withdrawn ones stay as history.
  const rows = [...work.deadlines].sort((a, b) => Number(!live(a)) - Number(!live(b)));

  return (
    <>
      <Card>
        <div className="text-xs text-muted">Version {v.version} of {work.latest_version}</div>
        <h2 className="mt-0.5 font-head text-lg font-semibold">{v.description}</h2>
        <div className="mt-2 flex flex-wrap gap-1.5">
          <Badge tone={v.status === "cancelled" ? "neutral" : "ok"}>{v.status === "cancelled" ? "Cancelled" : "Planned"}</Badge>
          {v.late_when_recorded && <Badge tone="warn">Recorded too late for the notice period</Badge>}
        </div>
        <div className="mt-3">
          <Facts
            min={150}
            items={[
              ["Starts", formatDateOnly(v.starts_on)],
              ["Ends", v.ends_on ? formatDateOnly(v.ends_on) : "Not given"],
              ["Submitted", `${formatWhen(v.submitted_at)} by ${who(v.submitted_by)}`],
            ]}
          />
        </div>
        <h3 className="mt-4 text-sm font-semibold">Involves</h3>
        {v.facts.length === 0 ? (
          <p className="mt-1 text-sm text-muted">Nothing that needs a notice.</p>
        ) : (
          <ul className="mt-1 list-disc pl-5 text-sm">
            {v.facts.map((f) => <li key={f.key}>{f.label} <span className="text-xs text-muted">({f.source_ref})</span></li>)}
          </ul>
        )}
        {v.amend_reason && <p className="mt-3 rounded-lg bg-bg px-3 py-2 text-[13px]"><strong className="font-semibold">Why changed:</strong> {v.amend_reason}</p>}
        {v.late_when_recorded && (
          <p className="mt-3 text-[13px] text-warn">
            This was recorded after the notice was already due: the notice is overdue from the start, not missed while
            SiteProof watched.
          </p>
        )}
        <p className="mt-3 text-xs text-muted">
          This version: {work.changes.added.length} notices added, {work.changes.carried.length} carried,{" "}
          {work.changes.superseded.length} replaced, {work.changes.withdrawn.length} withdrawn.
        </p>
      </Card>

      <Card title="Notices" description="Worked out by the server from the facts and the start date.">
        {rows.length === 0 && <p className="text-sm">No notices: nothing ticked needs one.</p>}
        <div className="flex flex-col gap-3">
          {rows.map((d) => (
            <div key={d.id} className={cx("rounded-lg border border-line p-3", !live(d) && "opacity-70")}>
              <DeadlineBody d={d} />
              {unmet(d) && latest && (
                <Link href={`/deadlines?deadline=${d.id}`} className={buttonClass(d.state === "overdue" ? "primary" : "secondary", "sm") + " mt-2.5"}>
                  Record the notice
                </Link>
              )}
            </div>
          ))}
        </div>
        <p className="mt-3 text-xs text-muted">
          Upload the Annexure 2 (or the permit) to the H&amp;S file under People &amp; documents first, then link it when
          you record the notice.
        </p>
      </Card>

      <Card title="Versions">
        {Array.from({ length: work.latest_version }, (_, i) => work.latest_version - i).map((n) => (
          <Row key={n} aside={n === v.version ? <Badge tone="ok">Showing</Badge> : undefined}>
            <Link href={`/permits?work=${work.id}&version=${n}`} className="link">Version {n}</Link>
          </Row>
        ))}
      </Card>

      {latest && (
        <Card title="Change this work" description="A new version with the whole record again. Earlier versions stay readable.">
          {amend ? (
            <SiteWorkForm
              siteId={siteId}
              workId={work.id}
              version={work.latest_version + 1}
              catalogue={catalogue}
              initial={{
                description: v.description,
                starts_on: v.starts_on,
                ends_on: v.ends_on ?? "",
                facts: v.facts.map((f) => f.key),
                status: v.status,
              }}
            />
          ) : (
            <Link href={`/permits?work=${work.id}&amend=1`} className={buttonClass("secondary", "sm")}>
              Change the start, facts or status
            </Link>
          )}
        </Card>
      )}
    </>
  );
}
