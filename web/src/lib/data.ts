// Reads used by Server Components. Each wraps one Go endpoint.

import "server-only";

import { ApiError, api } from "./api";
import type {
  InvitePreview,
  Location,
  Me,
  PendingInvite,
  Session,
  Site,
  SiteMember,
} from "./types";

export const getMe = () => api<Me>("/v1/me");

export async function listSites() {
  return (await api<{ sites: Site[] }>("/v1/sites")).sites;
}

// null when the site doesn't exist, is archived, or (for a supervisor)
// isn't one of theirs. The API answers 404 for all three on purpose.
export async function getSite(id: string) {
  try {
    return await api<Site>(`/v1/sites/${encodeURIComponent(id)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export async function listLocations(siteId: string) {
  const path = `/v1/sites/${encodeURIComponent(siteId)}/locations`;
  return (await api<{ locations: Location[] }>(path)).locations;
}

// Only owners and SHE officers can list members or invites. For anyone
// else these return "forbidden" so the page can say so instead of failing.
export async function listSiteMembers(siteId: string) {
  const path = `/v1/sites/${encodeURIComponent(siteId)}/members`;
  try {
    return (await api<{ members: SiteMember[] }>(path)).members;
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) return "forbidden" as const;
    throw e;
  }
}

export async function listPendingInvites() {
  try {
    return (await api<{ invites: PendingInvite[] }>("/v1/invites")).invites;
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) return "forbidden" as const;
    throw e;
  }
}

export async function listSessions() {
  return (await api<{ sessions: Session[] }>("/v1/auth/sessions")).sessions;
}

export async function previewInvite(token: string) {
  try {
    return await api<InvitePreview>("/v1/invites/preview", {
      method: "POST",
      body: { token },
      auth: false,
    });
  } catch (e) {
    if (e instanceof ApiError && e.status === 400) return null;
    throw e;
  }
}
