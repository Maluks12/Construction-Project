import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { archiveSite, removeSiteMember } from "@/app/actions/sites";
import { InlineAction } from "@/components/client";
import { Alert, Badge, Card, EmptyState, PageHeader } from "@/components/ui";
import { getSite, listLocations, listSiteMembers } from "@/lib/data";
import { ROLE_LABELS, formatDate } from "@/lib/format";
import type { Location } from "@/lib/types";

import { EditSiteForm } from "./edit-site-form";
import { LocationForm } from "./location-form";
import { LocationRow } from "./location-row";

export async function generateMetadata({ params }: PageProps<"/sites/[id]">): Promise<Metadata> {
  const site = await getSite((await params).id);
  return { title: site?.name ?? "Site not found" };
}

export default async function SitePage({ params }: PageProps<"/sites/[id]">) {
  const { id } = await params;
  const site = await getSite(id);
  if (!site) notFound();

  const [locations, members] = await Promise.all([listLocations(id), listSiteMembers(id)]);
  // The members list is limited to owners and SHE officers, the same roles
  // that may change a site, so it doubles as the "can manage" check.
  const canManage = members !== "forbidden";

  return (
    <>
      <PageHeader
        eyebrow={
          <Link href="/sites" className="hover:text-ink">
            ← Sites
          </Link>
        }
        title={site.name}
      >
        <div className="flex items-center gap-2 text-sm text-muted">
          <Badge tone={site.status === "active" ? "ok" : "neutral"}>{site.status}</Badge>
          <span>Added {formatDate(site.created_at)}</span>
        </div>
      </PageHeader>

      <div className="grid items-start gap-6 lg:grid-cols-[1fr_340px]">
        <div className="space-y-6">
          <Card
            title="Locations"
            description="Zones and floors on this site. Incidents and inspections will be logged against these."
          >
            {locations.length === 0 ? (
              <EmptyState title="No locations yet">
                {canManage ? "Add the first zone or floor." : "An owner or SHE officer can add them."}
              </EmptyState>
            ) : (
              <LocationTree locations={locations} siteId={id} canManage={canManage} />
            )}
            {canManage && (
              <div className="mt-5 border-t border-line pt-5">
                <LocationForm siteId={id} locations={locations} />
              </div>
            )}
          </Card>

          <Card title="People on this site">
            {members === "forbidden" ? (
              <Alert tone="info">Only owners and SHE officers can see who is on a site.</Alert>
            ) : members.length === 0 ? (
              <EmptyState title="Nobody assigned yet">
                Owners and SHE officers can see every site. Supervisors need to be added to a site.
              </EmptyState>
            ) : (
              <ul className="divide-y divide-line">
                {members.map((m) => (
                  <li key={m.user_id} className="flex items-center justify-between gap-3 py-3 first:pt-0 last:pb-0">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{m.full_name}</p>
                      <p className="truncate text-xs text-muted">
                        {ROLE_LABELS[m.role] ?? m.role} · {m.email ?? m.mobile ?? "No contact"}
                      </p>
                    </div>
                    <InlineAction
                      action={removeSiteMember}
                      fields={{ site_id: id, user_id: m.user_id }}
                      label="Remove"
                      confirmLabel="Remove from site"
                      variant="ghost"
                    />
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </div>

        {canManage && (
          <div className="space-y-6">
            <Card title="Site details">
              <EditSiteForm site={site} />
            </Card>
            <Card title="Archive site" description="Hides the site everywhere. Its records are kept.">
              <InlineAction
                action={archiveSite}
                fields={{ site_id: id }}
                label="Archive this site"
                confirmLabel="Yes, archive it"
                variant="danger"
              />
            </Card>
          </div>
        )}
      </div>
    </>
  );
}

function LocationTree({
  locations,
  siteId,
  canManage,
}: {
  locations: Location[];
  siteId: string;
  canManage: boolean;
}) {
  const byParent = new Map<string | null, Location[]>();
  for (const loc of locations) {
    const key = loc.parent_id ?? null;
    byParent.set(key, [...(byParent.get(key) ?? []), loc]);
  }
  const ids = new Set(locations.map((l) => l.id));
  // A child whose parent isn't in the list is shown at the top level.
  const roots = locations.filter((l) => !l.parent_id || !ids.has(l.parent_id));

  const render = (items: Location[], depth: number) => (
    <ul className={depth ? "ml-5 border-l border-line pl-3" : "divide-y divide-line"}>
      {items
        .slice()
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((loc) => (
          <li key={loc.id} className="py-1.5">
            <LocationRow
              location={loc}
              siteId={siteId}
              canManage={canManage}
              hasChildren={(byParent.get(loc.id)?.length ?? 0) > 0}
            />
            {byParent.get(loc.id) && render(byParent.get(loc.id)!, depth + 1)}
          </li>
        ))}
    </ul>
  );

  return render(roots, 0);
}
