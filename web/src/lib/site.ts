// Which site the dashboard is looking at.
//
// Inspections, incidents, deadlines and evidence all belong to one site (the
// API paths are /v1/sites/:siteId/...), so the sidebar has a site picker.
// The choice is kept in a plain cookie: it holds a site id, not a secret,
// and the API still decides what the person may open.

import "server-only";

import { cache } from "react";
import { cookies } from "next/headers";

import { getChecklists, listSiteMembers, listSites } from "./data";
import type { Site } from "./types";

export const SITE_COOKIE = "sp_site";

// One sites call per request, shared by the layout and the page.
export const currentSite = cache(async (): Promise<{ sites: Site[]; site: Site | null }> => {
  const sites = await listSites();
  const chosen = (await cookies()).get(SITE_COOKIE)?.value;
  const site =
    sites.find((s) => s.id === chosen) ?? sites.find((s) => s.status === "active") ?? sites[0] ?? null;
  return { sites, site };
});

// Names for the user ids the API returns (started_by, owner_id...). Only
// owners and SHE officers may list a site's members, and the list holds the
// people added to the site, so an id can be missing: `who` then shows a
// short form of the id rather than guessing.
export const sitePeople = cache(async (siteId: string) => {
  const members = await listSiteMembers(siteId);
  const names = new Map<string, string>();
  if (members !== "forbidden") for (const m of members) names.set(m.user_id, m.full_name);
  return {
    members: members === "forbidden" ? [] : members,
    canManage: members !== "forbidden",
    who: (id: string | null | undefined) =>
      id ? (names.get(id) ?? `User ${id.slice(0, 8)}`) : "—",
  };
});

// The regime name shown under the site ("Construction Regulations 2014").
export async function regimeOf(siteId: string) {
  const catalog = await getChecklists(siteId);
  return catalog === "forbidden" ? null : catalog.regime;
}
