// Reads used by Server Components. Each wraps one Go endpoint.

import "server-only";

import { cache } from "react";

import { ApiError, api } from "./api";
import type {
  Action,
  ActionList,
  ActionSummary,
  ChainVerification,
  ChecklistCatalog,
  Deadline,
  DeadlineDetail,
  DeadlineList,
  Evidence,
  EvidenceList,
  Finding,
  FindingList,
  Incident,
  IncidentList,
  IncidentSummary,
  Inspection,
  InspectionList,
  InspectionSummary,
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

// ---- Safety records (docs/api/*.md) ----
//
// Every read below is limited to owners, SHE officers and supervisors. The
// other roles get 403, which `guard` turns into "forbidden" so a page can
// say so instead of failing.


export type Forbidden = "forbidden";

async function guard<T>(run: () => Promise<T>): Promise<T | Forbidden> {
  try {
    return await run();
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) return "forbidden";
    throw e;
  }
}

async function orNull<T>(run: () => Promise<T>): Promise<T | null> {
  try {
    return await run();
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

const sitePath = (siteId: string, rest: string) =>
  `/v1/sites/${encodeURIComponent(siteId)}${rest}`;

// Builds "?a=1&b=2", leaving out empty values.
function query(params: Record<string, string | number | undefined>) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

// The site's checklists and severity scale. Shared by the layout and pages
// within one request.
export const getChecklists = cache((siteId: string) =>
  guard(() => api<ChecklistCatalog>(sitePath(siteId, "/checklists"))),
);

export const listInspections = (siteId: string, status?: string) =>
  guard<InspectionSummary[]>(async () =>
    (await api<InspectionList>(sitePath(siteId, `/inspections${query({ status, limit: 200 })}`)))
      .inspections,
  );

export const getInspection = (siteId: string, id: string) =>
  orNull(() => api<Inspection>(sitePath(siteId, `/inspections/${encodeURIComponent(id)}`)));

export const listFindings = (siteId: string, coverage?: string) =>
  guard<Finding[]>(async () =>
    (await api<FindingList>(sitePath(siteId, `/findings${query({ coverage, limit: 200 })}`)))
      .findings,
  );

export const listIncidents = (
  siteId: string,
  filters: { type?: string; reportable?: string; open_deadlines?: string } = {},
) =>
  guard<IncidentSummary[]>(async () =>
    (await api<IncidentList>(sitePath(siteId, `/incidents${query({ ...filters, limit: 200 })}`)))
      .incidents,
  );

export const getIncident = (siteId: string, id: string, version?: string) =>
  orNull(() =>
    api<Incident>(sitePath(siteId, `/incidents/${encodeURIComponent(id)}${query({ version })}`)),
  );

export const listDeadlines = (siteId: string, state?: string) =>
  guard<Deadline[]>(async () =>
    (await api<DeadlineList>(sitePath(siteId, `/deadlines${query({ state, limit: 200 })}`)))
      .deadlines,
  );

export const getDeadline = (siteId: string, id: string) =>
  orNull(() => api<DeadlineDetail>(sitePath(siteId, `/deadlines/${encodeURIComponent(id)}`)));

type ActionFilters = { state?: string; owner?: string; source?: string };

export const listActions = (siteId: string, filters: ActionFilters = {}) =>
  guard<ActionSummary[]>(async () =>
    (await api<ActionList>(sitePath(siteId, `/actions${query({ ...filters, limit: 200 })}`)))
      .actions,
  );

// The register across every site the caller can open.
export const listAllActions = (filters: ActionFilters = {}) =>
  guard<ActionSummary[]>(async () =>
    (await api<ActionList>(`/v1/actions${query({ ...filters, limit: 200 })}`)).actions,
  );

export const getAction = (siteId: string, id: string) =>
  orNull(() => api<Action>(sitePath(siteId, `/actions/${encodeURIComponent(id)}`)));

// Each call is recorded in the evidence access log, so only the Evidence
// page lists evidence.
export const listEvidence = (siteId: string, status?: string) =>
  guard<Evidence[]>(async () =>
    (await api<EvidenceList>(sitePath(siteId, `/evidence${query({ status, limit: 100 })}`)))
      .evidence,
  );

// Owners and SHE officers only. Walks the whole chain, so pages call it on
// request rather than on every load.
export const verifyChain = (siteId: string) =>
  guard(() => api<ChainVerification>(sitePath(siteId, "/chain")));
