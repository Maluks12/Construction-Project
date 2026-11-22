// Which site the dashboard is looking at.
//
// Inspections, incidents, deadlines and evidence all belong to one site (the
// API paths are /v1/sites/:siteId/...), so the sidebar has a site picker.
// The choice is kept in a plain cookie: it holds a site id, not a secret,
// and the API still decides what the person may open.

import "server-only";

import { cache } from "react";
import { cookies } from "next/headers";

import { getChecklists, getMe, listSiteMembers, listSites } from "./data";
import type { Capability, Site } from "./types";

export const SITE_COOKIE = "sp_site";

// One sites call per request, shared by the layout and the page.
export const currentSite = cache(async (): Promise<{ sites: Site[]; site: Site | null }> => {
  const sites = await listSites();
  const chosen = (await cookies()).get(SITE_COOKIE)?.value;
  const site =
    sites.find((s) => s.id === chosen) ?? sites.find((s) => s.status === "active") ?? sites[0] ?? null;
  return { sites, site };
});

// What the signed-in person's role lets them do (GET /v1/me, M12). Only for
// showing or hiding controls: the API still enforces every rule, and for a
// supervisor each capability means "on your own sites".
export const capabilities = cache(async () => new Set<Capability>((await getMe()).capabilities));

export async function can(capability: Capability) {
  return (await capabilities()).has(capability);
}

// Names for the user ids the API returns (started_by, owner_id...). Only
// owners and SHE officers may list a site's members (manage_sites), and the
// list holds the people added to the site, so an id can be missing: `who`
// then shows a short form of the id rather than guessing. The signed-in
// person is always known by name.
export const sitePeople = cache(async (siteId: string) => {
  const [me, canManage] = await Promise.all([getMe(), can("manage_sites")]);
  const members = canManage ? await listSiteMembers(siteId) : [];
  const list = members === "forbidden" ? [] : members;
  const names = new Map<string, string>(list.map((m) => [m.user_id, m.full_name]));
  names.set(me.user_id, me.full_name);
  return {
    members: list,
    canManage: canManage && members !== "forbidden",
    who: (id: string | null | undefined) =>
      id ? (names.get(id) ?? `User ${id.slice(0, 8)}`) : "—",
  };
});

// The regime name shown under the site ("Construction Regulations 2014").
// Only a label, so a failure here must not take every page down with it:
// it's logged and the label is left out. Pages that need the checklists
// call getChecklists themselves and show the real error.
export async function regimeOf(siteId: string) {
  try {
    const catalog = await getChecklists(siteId);
    return catalog === "forbidden" ? null : catalog.regime;
  } catch (e) {
    console.error("regime label: checklists request failed", e);
    return null;
  }
}