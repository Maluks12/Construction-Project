import type { Metadata } from "next";
import Link from "next/link";

import { Forbidden, NoSite } from "@/components/states";
import { Badge, Card, EmptyState, PageHeader, Select, buttonClass } from "@/components/ui";
import { listIncidents } from "@/lib/data";
import { formatDay, formatWhen, shortId } from "@/lib/format";
import { incidentTypes } from "@/lib/regime";
import { currentSite, regimeOf, sitePeople } from "@/lib/site";

export const metadata: Metadata = { title: "Incidents" };

const one = (v: string | string[] | undefined) => (typeof v === "string" ? v : "");

export default async function IncidentsPage({ searchParams }: PageProps<"/incidents">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Incident register" />;

  const sp = await searchParams;
  const filters = { type: one(sp.type), reportable: one(sp.reportable), open_deadlines: one(sp.open_deadlines) };
  const [incidents, regime, people] = await Promise.all([
    listIncidents(site.id, filters),
    regimeOf(site.id),
    sitePeople(site.id),
  ]);
  if (incidents === "forbidden") return <Forbidden title="Incident register" />;
  const types = incidentTypes(regime?.code);

  return (
    <>
      <PageHeader
        title="Incident register"
        description="Classified from the site's regime. A submitted incident is only ever amended by a new version."
      >
        <Link href="/incidents/new" className={buttonClass("primary")}>Report an incident</Link>
      </PageHeader>

      <div className="flex flex-col gap-5">
        <form className="flex flex-wrap items-end gap-4 rounded-[10px] border border-line bg-surface px-[18px] py-3.5">
          <label className="block">
            <span className="mb-1 block text-xs font-semibold text-muted">Type</span>
            <Select name="type" defaultValue={filters.type} className="w-auto">
              <option value="">All types</option>
              {types.map((t) => <option key={t.key} value={t.key}>{t.label}</option>)}
            </Select>
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-semibold text-muted">Reportable</span>
            <Select name="reportable" defaultValue={filters.reportable} className="w-auto">
              <option value="">Any</option>
              <option value="true">Reportable only</option>
              <option value="false">Not reportable</option>
            </Select>
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-semibold text-muted">Deadlines</span>
            <Select name="open_deadlines" defaultValue={filters.open_deadlines} className="w-auto">
              <option value="">Any</option>
              <option value="true">Open deadlines</option>
              <option value="false">None open</option>
            </Select>
          </label>
          <button type="submit" className={buttonClass("secondary")}>Apply</button>
        </form>

        {incidents.length === 0 ? (
          <EmptyState title="No incidents match">Near misses count: reporting one starts no clocks.</EmptyState>
        ) : (
          <Card flush>
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead>
                  <tr><th>Ref</th><th>Occurred (SAST)</th><th>Where</th><th>Types</th><th>People</th><th>Next deadline</th><th>Version</th></tr>
                </thead>
                <tbody>
                  {incidents.map((i) => (
                    <tr key={i.id}>
                      <td><Link href={`/incidents/${i.id}`} className="link">INC-{shortId(i.id)}</Link></td>
                      <td className="whitespace-nowrap">{formatWhen(i.occurred_at)}</td>
                      <td className="min-w-[160px]">{i.location ?? "Not given"}</td>
                      <td>
                        <div className="flex flex-wrap gap-1.5">
                          {i.types.map((t) => <Badge key={t.key} tone={t.reportable ? "bad" : "neutral"}>{t.label}</Badge>)}
                        </div>
                      </td>
                      <td>
                        {i.injured_count ? `${i.injured_count} injured` : i.people_count ? `${i.people_count} involved` : "None"}
                        {i.death_count > 0 && <div className="text-xs text-danger">{i.death_count} died</div>}
                      </td>
                      <td>
                        {i.overdue_deadlines > 0 ? (
                          <Badge tone="bad">{i.overdue_deadlines} overdue</Badge>
                        ) : i.next_due_at ? (
                          <Badge tone="warn">{formatDay(i.next_due_at)}</Badge>
                        ) : (
                          <Badge>None open</Badge>
                        )}
                        {i.open_deadlines > 0 && <div className="mt-1 text-xs text-muted">{i.open_deadlines} open</div>}
                      </td>
                      <td>
                        v{i.latest_version}
                        <div className="text-xs text-muted">{people.who(i.reported_by)}</div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        )}
      </div>
    </>
  );
}
