import type { Metadata } from "next";
import Link from "next/link";

import { Forbidden, NoSite } from "@/components/states";
import { Badge, Card, EmptyState, PageHeader, Row, Tabs, type Tone } from "@/components/ui";
import { getChecklists, listInspections } from "@/lib/data";
import { formatTime, formatWhen, label } from "@/lib/format";
import { currentSite, sitePeople } from "@/lib/site";
import type { InspectionStatus } from "@/lib/types";

export const metadata: Metadata = { title: "Inspections" };

const STATUS_TONE: Record<InspectionStatus, Tone> = {
  in_progress: "warn",
  completed: "ok",
  abandoned: "neutral",
};

export default async function InspectionsPage({ searchParams }: PageProps<"/inspections">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Inspections" />;

  const { status } = await searchParams;
  const [all, catalog, people] = await Promise.all([
    listInspections(site.id),
    getChecklists(site.id),
    sitePeople(site.id),
  ]);
  if (all === "forbidden" || catalog === "forbidden") return <Forbidden title="Inspections" />;

  const titles = new Map(all.map((i) => [i.id, `${i.template_title}, ${formatWhen(i.started_at)}`]));
  const rows = typeof status === "string" ? all.filter((i) => i.status === status) : all;
  const tab = (key: InspectionStatus | undefined, text: string) => ({
    href: key ? `/inspections?status=${key}` : "/inspections",
    label: text,
    count: key ? all.filter((i) => i.status === key).length : all.length,
    active: status === key,
  });

  return (
    <>
      <PageHeader
        title="Inspections"
        description="Started and answered on the mobile app. The dashboard reads them; owners and SHE officers can abandon one."
      />
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[999_1_620px]">
          <Tabs
            tabs={[
              tab(undefined, "All"),
              tab("in_progress", "In progress"),
              tab("completed", "Completed"),
              tab("abandoned", "Abandoned"),
            ]}
          />
          {rows.length === 0 ? (
            <div className="p-4">
              <EmptyState title="No inspections here yet">They appear as soon as a phone starts one.</EmptyState>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Checklist</th>
                    <th>Inspector</th>
                    <th>Started</th>
                    <th>Progress</th>
                    <th>Fails</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((i) => (
                    <tr key={i.id}>
                      <td>
                        <Link href={`/inspections/${i.id}`} className="link">{i.template_title}</Link>
                        <div className="text-xs text-muted">v{i.template_version} · {i.template_source_ref}</div>
                        {i.corrects_id && (
                          <div className="mt-1 text-xs">
                            Corrects{" "}
                            <Link href={`/inspections/${i.corrects_id}`} className="link">
                              {titles.get(i.corrects_id) ?? "an earlier inspection"}
                            </Link>
                          </div>
                        )}
                      </td>
                      <td>{people.who(i.started_by)}</td>
                      <td>
                        {formatWhen(i.started_at)}
                        <div className="text-xs text-muted">Device {formatTime(i.client_started_at)}</div>
                      </td>
                      <td className="min-w-[140px]">
                        <div className="h-1.5 overflow-hidden rounded-full bg-line-soft">
                          <div className="h-1.5 bg-bar" style={{ width: `${i.item_count ? (i.answered_count / i.item_count) * 100 : 0}%` }} />
                        </div>
                        <div className="mt-1 text-xs text-muted">{i.answered_count} of {i.item_count} answered</div>
                      </td>
                      <td>{i.failed_count}</td>
                      <td>
                        <Badge tone={STATUS_TONE[i.status]}>{label(i.status)}</Badge>
                        {i.abandon_reason && (
                          <div className="mt-1 max-w-[180px] text-xs text-muted">
                            {i.abandon_reason} Abandoned by {people.who(i.abandoned_by)}.
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card
          className="flex-[1_1_320px]"
          title="Checklists for this site"
          description={`From the site's regime: ${catalog.regime.name}. Wording only changes by publishing a new version.`}
        >
          {catalog.checklists.map((t) => (
            <Row key={t.key} aside={<Badge>{label(t.cadence)}</Badge>}>
              <strong className="font-semibold">{t.title}</strong>
              <div className="mt-0.5 text-xs text-muted">{t.items.length} items · v{t.version} · {t.source_ref}</div>
            </Row>
          ))}
        </Card>
      </div>
    </>
  );
}
