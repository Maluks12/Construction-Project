// Reads used by Server Components. Each wraps one Go endpoint.

import "server-only";

import { cache } from "react";

import { ApiError, api } from "./api";
import type {
  Action,
  ActionList,
  ActionSummary,
  AppointmentList,
  ChainVerification,
  ChecklistCatalog,
  Deadline,
  DeadlineDetail,
  DeadlineList,
  DeadlineSubjectType,
  Diary,
  DiaryList,
  DocumentDetail,
  DocumentRegister,
  Evidence,
  EvidenceList,
  Export,
  ExportList,
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
  NotificationList,
  PendingInvite,
  RegimeTypes,
  RevisionList,
  RosterList,
  Rollup,
  Session,
  SignInDay,
  Site,
  SiteMember,
  SiteSummary,
  SiteWork,
  SiteWorkCatalogue,
  SiteWorkList,
  Talk,
  TalkList,
  ToolboxTopicList,
  WorkerDetail,
  WorkerList,
} from "./types";

// Who is signed in: role, names and capabilities (M12). The API reads them
// from the membership on every call, so this is called per request, once.
export const getMe = cache(() => api<Me>("/v1/me"));

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

// subject_type (REG4): "incident" or "site_work"; left out, both.
export const listDeadlines = (siteId: string, state?: string, subjectType?: DeadlineSubjectType) =>
  guard<Deadline[]>(async () =>
    (await api<DeadlineList>(sitePath(siteId, `/deadlines${query({ state, subject_type: subjectType, limit: 200 })}`)))
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

// ---- M10 to REG4 (docs/api/*.md) ----

// The Construction edition's endpoints: the diary, the H&S file,
// appointments and site work. A site under another edition's regime answers
// 404 there, which the reads below turn into null.
const constructionPath = (siteId: string, rest: string) =>
  `/v1/construction/sites/${encodeURIComponent(siteId)}${rest}`;

// Compliance summary (M12). The rollup covers every site the caller can
// open, worst first; a site's summary has every item with its record ids.
export const getRollup = () => guard(() => api<Rollup>("/v1/compliance"));

export const getSiteCompliance = (siteId: string) =>
  guard(() => api<SiteSummary>(sitePath(siteId, "/compliance")));

// The caller's own inbox (M11). Every role has one.
export const listNotifications = async (unread = false, limit = 50) =>
  (await api<NotificationList>(`/v1/notifications${query({ unread: unread ? "true" : undefined, limit })}`))
    .notifications;

// The bell's count. Only a badge, so a failure here mustn't take the page down.
export const unreadCount = cache(async () => {
  try {
    return (await listNotifications(true, 200)).length;
  } catch (e) {
    if (e instanceof ApiError) return null;
    throw e;
  }
});

// A regime's competency, appointment and document types, medical rule and
// reminder lead times. The same for every caller, so shared per request.
export const getRegimeTypes = cache((code: string) =>
  guard(() => api<RegimeTypes>(`/v1/regimes/${encodeURIComponent(code)}/types`)),
);

// ---- Toolbox talks (docs/api/toolbox.md) ----
export const listToolboxTopics = (siteId: string) =>
  guard(async () => (await api<ToolboxTopicList>(sitePath(siteId, "/toolbox-topics"))).topics);

export const listTalks = (siteId: string, date?: string) =>
  guard(async () => (await api<TalkList>(sitePath(siteId, `/toolbox-talks${query({ date, limit: 100 })}`))).talks);

export const getTalk = (siteId: string, id: string) =>
  orNull(() => api<Talk>(sitePath(siteId, `/toolbox-talks/${encodeURIComponent(id)}`)));

// ---- Site diary (docs/api/diary.md) ----
export const listDiaries = (siteId: string, from?: string, to?: string) =>
  guard(() =>
    orNull(async () => (await api<DiaryList>(constructionPath(siteId, `/diary${query({ from, to, limit: 60 })}`))).diaries),
  );

// null when the day has no diary yet.
export const getDiary = (siteId: string, date: string) =>
  orNull(() => api<Diary>(constructionPath(siteId, `/diary/${encodeURIComponent(date)}`)));

export const listRevisions = (siteId: string, date: string) =>
  orNull(async () =>
    (await api<RevisionList>(constructionPath(siteId, `/diary/${encodeURIComponent(date)}/revisions`))).revisions,
  );

// ---- Worker register (docs/api/workers.md) ----
export const listWorkers = (filters: { active?: string; q?: string } = {}) =>
  guard(async () => (await api<WorkerList>(`/v1/workers${query({ ...filters, limit: 500 })}`)).workers);

export const getWorker = (id: string) =>
  orNull(() => api<WorkerDetail>(`/v1/workers/${encodeURIComponent(id)}`));

// ---- Site sign-in and roster (docs/api/attendance.md) ----
// date is YYYY-MM-DD; left out, today in the site's time zone.
export const getSignInDay = (siteId: string, date?: string) =>
  guard(() => api<SignInDay>(sitePath(siteId, `/sign-ins${query({ date })}`)));

export const listRoster = (siteId: string, all = false) =>
  guard(async () => (await api<RosterList>(sitePath(siteId, `/roster${query({ all: all ? "true" : undefined })}`))).roster);

// ---- H&S file and vault (docs/api/documents.md) ----
export const getSafetyFile = (siteId: string) =>
  guard(() => orNull(() => api<DocumentRegister>(constructionPath(siteId, "/safety-file"))));

export const getSafetyFileDocument = (siteId: string, id: string) =>
  orNull(() => api<DocumentDetail>(constructionPath(siteId, `/safety-file/${encodeURIComponent(id)}`)));

// Owners and SHE officers only; everyone else gets "forbidden".
export const getVault = (regimeCode?: string) =>
  guard(() => api<DocumentRegister>(`/v1/vault/documents${query({ regime_code: regimeCode })}`));

export const getVaultDocument = (id: string) =>
  orNull(() => api<DocumentDetail>(`/v1/vault/documents/${encodeURIComponent(id)}`));

// ---- Appointments (docs/api/appointments.md) ----
export const listAppointments = (siteId: string) =>
  guard(() => orNull(async () => (await api<AppointmentList>(constructionPath(siteId, "/appointments"))).appointments));

// ---- Audit packs (docs/api/exports.md): owners and SHE officers ----
export const listExports = (siteId: string) =>
  guard(async () => (await api<ExportList>(sitePath(siteId, "/exports"))).exports);

export const getExport = (siteId: string, id: string) =>
  orNull(() => api<Export>(sitePath(siteId, `/exports/${encodeURIComponent(id)}`)));

// ---- Site work (docs/api/site-work.md, REG4) ----
export const getSiteWorkFacts = (siteId: string) =>
  guard(() => orNull(() => api<SiteWorkCatalogue>(constructionPath(siteId, "/site-work-facts"))));

export const listSiteWork = (siteId: string, status?: string) =>
  guard(() =>
    orNull(async () => (await api<SiteWorkList>(constructionPath(siteId, `/site-work${query({ status, limit: 200 })}`))).site_work),
  );

export const getSiteWork = (siteId: string, id: string, version?: string) =>
  orNull(() => api<SiteWork>(constructionPath(siteId, `/site-work/${encodeURIComponent(id)}${query({ version })}`)));
