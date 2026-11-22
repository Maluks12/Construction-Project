import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { archiveSite, removeSiteMember } from "@/app/actions/sites";
import { InlineAction } from "@/components/client";
import { Alert, Badge, Card, EmptyState, PageHeader } from "@/components/ui";
import { getSite, listLocations, listPendingInvites, listSiteMembers, listSites } from "@/lib/data";
import { ROLE_LABELS, formatDate } from "@/lib/format";
import { can } from "@/lib/site";
import type { Location, SiteMember } from "@/lib/types";

import { AddMemberForm, type PersonOption } from "./add-member-form";
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

  // manage_sites (GET /v1/me): owners and SHE officers. The members list is
  // limited to them, so others don't ask for it.
  const canManage = await can("manage_sites");
  const [locations, members] = await Promise.all([
    listLocations(id),
    canManage ? listSiteMembers(id) : ("forbidden" as const),
  ]);
  const addable = canManage && members !== "forbidden" ? await peopleToAdd(id, members) : [];

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
            <Card title="Add someone to this site" description="Supervisors only see the sites they're added to.">
              <AddMemberForm siteId={id} people={addable} />
            </Card>
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

// Everyone the dashboard can name who isn't on this site yet: members of the
// organisation's other sites, and people invited who haven't accepted (an
// invitee already has the membership the API needs). Supervisors first.
async function peopleToAdd(siteId: string, members: SiteMember[]): Promise<PersonOption[]> {
  const here = new Set(members.map((m) => m.user_id));
  const [sites, invites] = await Promise.all([listSites(), listPendingInvites()]);
  const others = await Promise.all(
    sites.filter((s) => s.id !== siteId && s.status !== "archived").map((s) => listSiteMembers(s.id)),
  );
  const people = new Map<string, PersonOption & { supervisor: boolean }>();
  for (const list of others) {
    if (list === "forbidden") continue;
    for (const m of list) {
      if (here.has(m.user_id)) continue;
      people.set(m.user_id, { user_id: m.user_id, label: `${m.full_name} · ${ROLE_LABELS[m.role] ?? m.role}`, supervisor: m.role === "supervisor" });
    }
  }
  if (invites !== "forbidden") {
    for (const inv of invites) {
      if (here.has(inv.user_id) || people.has(inv.user_id)) continue;
      people.set(inv.user_id, {
        user_id: inv.user_id,
        label: `${inv.full_name} · ${ROLE_LABELS[inv.role] ?? inv.role} (invited)`,
        supervisor: inv.role === "supervisor",
      });
    }
  }
  return [...people.values()]
    .sort((a, b) => Number(b.supervisor) - Number(a.supervisor) || a.label.localeCompare(b.label))
    .map(({ user_id, label }) => ({ user_id, label }));
}
