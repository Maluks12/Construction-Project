// Shapes returned by the Go API (construction_db). Keep these in step with the
// json tags in internal/core/*/model.go.

export type TokenPair = {
  access_token: string;
  refresh_token: string;
  expires_in: number; // seconds
  token_type: "Bearer";
};

export type Me = {
  user_id: string;
  org_id: string;
  session_id: string;
};

export type Role =
  | "owner"
  | "she_officer"
  | "supervisor"
  | "claims_handler"
  | "contracts_manager"
  | "bookkeeper";

export type SiteStatus = "active" | "completed" | "archived";

export type Site = {
  id: string;
  org_id: string;
  name: string;
  address: string | null;
  status: SiteStatus;
  created_at: string;
};

export type LocationKind = "zone" | "floor";

export type Location = {
  id: string;
  site_id: string;
  parent_id: string | null;
  kind: LocationKind;
  name: string;
  created_at: string;
};

export type SiteMember = {
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
};

export type PendingInvite = {
  id: string;
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
  created_at: string;
  expires_at: string;
};

export type InviteResponse = {
  id: string;
  user_id: string;
  token: string;
  expires_at: string;
};

export type InvitePreview = {
  org_name: string;
  full_name: string;
  role: Role;
  has_password: boolean;
  expires_at: string;
};

export type Session = {
  id: string;
  device_name: string | null;
  user_agent: string | null;
  created_at: string;
  last_used_at: string;
  current: boolean;
};

// ---- docs/api/inspections.md ----
export type Severity = "low" | "medium" | "high" | "critical";

export type Cadence = "per_shift" | "daily" | "weekly" | "per_activity" | "ad_hoc";

export type ResponseType = "pass_fail_na";

export interface ChecklistCatalog {
  regime: RegimeRef;
  checklists: ChecklistTemplate[];
  severities: SeverityLevel[];
}

export interface RegimeRef {
  code: string;
  name: string;
  version: string;
}

export interface ChecklistTemplate {
  key: string;
  version: number;
  sha256: string;
  title: string;
  cadence: Cadence;
  source_ref: string;
  items: ChecklistItem[];
}

export interface ChecklistItem {
  key: string;
  prompt: string;
  response_type: ResponseType;
  source_ref: string;
}

export interface SeverityLevel {
  key: Severity;
  label: string;
  definition: string;
}

export type InspectionStatus = "in_progress" | "completed" | "abandoned";

export type Result = "pass" | "fail" | "na";

export interface StartInspectionRequest {
  template_key: string;
  template_version: number;
  template_sha256: string;
  client_started_at: string;
  device_id?: string | null;
  corrects_id?: string | null;
}

export interface Inspection {
  id: string;
  site_id: string;
  status: InspectionStatus;
  template_key: string;
  template_version: number;
  template_sha256: string;
  template_title: string;
  template_source_ref: string;
  item_count: number;
  started_by: string;
  client_started_at: string;
  started_at: string;
  device_id: string | null;
  corrects_id: string | null;
  client_completed_at: string | null;
  completed_at: string | null;
  abandoned_at: string | null;
  abandoned_by: string | null;
  abandon_reason: string | null;
  items: InspectionItem[];
  findings: InspectionFinding[];
}

export interface InspectionFinding {
  item_key: string;
  severity: Severity;
  raised_at: string;
}

export interface CompleteRequest {
  client_completed_at: string;
}

export interface AbandonRequest {
  reason: string;
}

export interface IncompleteInspection {
  error: string;
  item_keys: string[];
}

export interface Finding {
  inspection_id: string;
  site_id: string;
  item_key: string;
  severity: Severity;
  raised_at: string;
  template_key: string;
  template_version: number;
  template_title: string;
  prompt: string;
  corrected_by: string | null;
  coverage: Coverage;
  covered_by: string[];
  waiver_id: string | null;
}

export type Coverage = "unactioned" | "actioned" | "waived" | "superseded";

export interface FindingList {
  findings: Finding[];
}

export interface InspectionItem {
  key: string;
  position: number;
  prompt: string;
  source_ref: string | null;
  response_type: ResponseType;
  result: Result | null;
  severity: Severity | null;
  comment: string | null;
  client_answered_at: string | null;
  answered_at: string | null;
  evidence: AttachedEvidence[];
}

export interface AttachedEvidence {
  evidence_id: string;
  client_captured_at: string;
  captured_before_start: boolean;
  attached_at: string;
}

export interface AnswerRequest {
  result: Result;
  severity?: Severity | null;
  comment?: string | null;
  evidence_ids: string[];
  client_answered_at: string;
}

export interface EvidenceProblem {
  error: string;
  evidence_ids: string[];
}

export interface StaleAnswer {
  error: string;
  stored: InspectionItem;
}

export interface InspectionSummary {
  id: string;
  site_id: string;
  status: InspectionStatus;
  template_key: string;
  template_version: number;
  template_sha256: string;
  template_title: string;
  template_source_ref: string;
  item_count: number;
  started_by: string;
  client_started_at: string;
  started_at: string;
  device_id: string | null;
  corrects_id: string | null;
  client_completed_at: string | null;
  completed_at: string | null;
  abandoned_at: string | null;
  abandoned_by: string | null;
  abandon_reason: string | null;
  answered_count: number;
  failed_count: number;
}

export interface InspectionList {
  inspections: InspectionSummary[];
}

// ---- docs/api/evidence.md ----
export type EvidenceStatus = "pending" | "verified" | "rejected";

export type RejectedReason =
  | "size_mismatch"
  | "hash_mismatch"
  | "not_an_image"
  | "wrong_type"
  | "too_large_dimensions";

export type ChainBreakReason =
  | "seq_gap"
  | "prev_mismatch"
  | "metadata_changed"
  | "chain_hash_mismatch"
  | "entry_without_verified_evidence"
  | "head_mismatch"
  | "verified_without_entry";

export interface DeclareRequest {
  content_sha256: string;
  size_bytes: number;
  content_type: "image/jpeg" | "image/png";
  client_captured_at: string;
  lat?: number | null;
  lng?: number | null;
  device_id?: string | null;
}

export interface Evidence {
  id: string;
  site_id: string;
  status: EvidenceStatus;
  rejected_reason: RejectedReason | null;
  content_sha256: string;
  size_bytes: number;
  content_type: "image/jpeg" | "image/png";
  client_captured_at: string;
  lat: number | null;
  lng: number | null;
  device_id: string | null;
  uploaded_by: string;
  created_at: string;
  chain: ChainEntry | null;
  upload?: Upload;
}

export interface ChainEntry {
  seq: number;
  server_received_at: string;
  prev_chain_hash: string;
  metadata_hash: string;
  chain_hash: string;
}

export interface Upload {
  method: "PUT";
  url: string;
  headers: Record<string, string>;
  expires_at: string;
}

export interface EvidenceList {
  evidence: Evidence[];
}

export interface EvidenceRejection {
  error: string;
  rejected_reason: RejectedReason;
}

export interface ChainVerification {
  site_id: string;
  intact: boolean;
  length: number;
  head_seq: number;
  head_hash: string;
  first_break: ChainBreak | null;
  checked_at: string;
}

export interface ChainBreak {
  seq: number | null;
  evidence_id: string;
  reason: ChainBreakReason;
}

export interface ViewURL {
  method: "GET";
  url: string;
  expires_at: string;
}

// ---- docs/api/incidents.md ----
// Named AttachedEvidence in docs/api/incidents.md; renamed here because
// inspections.md has a different AttachedEvidence.

export type Treatment = "none" | "first_aid" | "medical_treatment" | "hospitalised";

export type DeadlineState = "open" | "due_now" | "overdue" | "satisfied" | "satisfied_late" | "superseded" | "withdrawn";

export type SatisfactionMethod = "phone" | "email" | "fax" | "in_person" | "online" | "post" | "other";

export interface SubmitVersionRequest {
  taxonomy_codes: string[];
  occurred_at: string;
  employer_aware_at?: string | null;
  location?: string | null;
  description?: string | null;
  people: PersonInput[];
  evidence_ids: string[];
  client_submitted_at: string;
  device_id?: string | null;
  amend_reason?: string | null;
}

export interface PersonInput {
  name?: string | null;
  occupation?: string | null;
  employee_of_org: boolean;
  employer_name?: string | null;
  injured: boolean;
  injury_description?: string | null;
  body_part?: string | null;
  treatment?: Treatment | null;
  died_at?: string | null;
}

export interface Incident {
  id: string;
  site_id: string;
  reported_by: string;
  created_at: string;
  latest_version: number;
  reportable: boolean;
  version: Version;
  changes: DeadlineChanges;
  deadlines: Deadline[];
  witness_statements: WitnessStatement[];
}

export interface Version {
  version: number;
  types: IncidentType[];
  occurred_at: string;
  employer_aware_at: string | null;
  location: string | null;
  description: string | null;
  people: Person[];
  evidence: IncidentEvidence[];
  submitted_by: string;
  client_submitted_at: string;
  submitted_at: string;
  device_id: string | null;
  amend_reason: string | null;
}

export interface IncidentType {
  key: string;
  label: string;
  reportable: boolean;
}

export interface Person {
  position: number;
  name: string | null;
  occupation: string | null;
  employee_of_org: boolean;
  employer_name: string | null;
  injured: boolean;
  injury_description: string | null;
  body_part: string | null;
  treatment: Treatment | null;
  died_at: string | null;
}

export interface IncidentEvidence {
  evidence_id: string;
  client_captured_at: string;
}

export interface DeadlineChanges {
  added: string[];
  superseded: string[];
  withdrawn: string[];
  carried: string[];
}

export interface WitnessStatement {
  id: string;
  witness_name: string;
  witness_contact: string | null;
  statement: string;
  taken_by: string;
  taken_at: string;
  recorded_at: string;
  device_id: string | null;
  evidence: IncidentEvidence | null;
  supersedes_id: string | null;
  superseded_by: string | null;
}

export interface IncidentSummary {
  id: string;
  site_id: string;
  latest_version: number;
  types: IncidentType[];
  reportable: boolean;
  occurred_at: string;
  location: string | null;
  reported_by: string;
  created_at: string;
  people_count: number;
  injured_count: number;
  death_count: number;
  open_deadlines: number;
  overdue_deadlines: number;
  next_due_at: string | null;
}

export interface IncidentList {
  incidents: IncidentSummary[];
}

export interface StatementRequest {
  witness_name: string;
  witness_contact?: string | null;
  statement: string;
  taken_at: string;
  device_id?: string | null;
  evidence_id?: string | null;
  supersedes_id?: string | null;
}

export interface Deadline {
  id: string;
  duty_id: string;
  incident_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
  basis_at: string;
  time_zone: string;
  due_at: string;
  due_on: string | null;
  excluded_days: string[];
  legal_latest_on: string | null;
  legal_latest_at: string | null;
  legal_latest_source_ref: string | null;
  computed_in_version: number;
  computed_at: string;
  added_by_satisfaction_id: string | null;
  overdue_when_computed: boolean;
  state: DeadlineState;
  closed_as: string | null;
  superseded_by: string | null;
  closed_in_version: number | null;
  closed_by: string | null;
  closed_at: string | null;
  closed_reason: string | null;
  satisfaction: Satisfaction | null;
}

export interface Satisfaction {
  id: string;
  deadline_id: string;
  satisfied_at: string;
  method: SatisfactionMethod;
  reference: string | null;
  note: string | null;
  evidence_id: string | null;
  recorded_by: string;
  recorded_at: string;
  recorded_after_due: boolean;
  retraction: Retraction | null;
}

export interface Retraction {
  retracted_by: string;
  retracted_at: string;
  reason: string;
}

export interface DeadlineDetail {
  id: string;
  duty_id: string;
  incident_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
  basis_at: string;
  time_zone: string;
  due_at: string;
  due_on: string | null;
  excluded_days: string[];
  legal_latest_on: string | null;
  legal_latest_at: string | null;
  legal_latest_source_ref: string | null;
  computed_in_version: number;
  computed_at: string;
  added_by_satisfaction_id: string | null;
  overdue_when_computed: boolean;
  state: DeadlineState;
  closed_as: string | null;
  superseded_by: string | null;
  closed_in_version: number | null;
  closed_by: string | null;
  closed_at: string | null;
  closed_reason: string | null;
  satisfaction: Satisfaction | null;
  satisfactions: Satisfaction[];
}

export interface DeadlineList {
  deadlines: Deadline[];
}

export interface SatisfactionRequest {
  satisfied_at: string;
  method: SatisfactionMethod;
  reference?: string | null;
  note?: string | null;
  evidence_id?: string | null;
}

export interface RetractRequest {
  reason: string;
}

export interface UnknownTypes {
  error: string;
  taxonomy_codes: string[];
}

export interface NotNextVersion {
  error: string;
  latest_version: number;
}

// ---- docs/api/actions.md ----
export type ActionState = "open" | "overdue" | "awaiting_verification" | "closed" | "closed_late" | "cancelled";

export type ActionStatus = "open" | "awaiting_verification" | "closed" | "cancelled";

export type ActionSource = "finding" | "incident";

export type ActionEventKind = "raised" | "reassigned" | "due_changed" | "cancelled" | "closeout_submitted" | "verified" | "rejected" | "reopened";

export type ActionRequestKind = "reassign" | "move_due" | "verify" | "reject" | "reopen" | "cancel";

export type ActionReview = "severity_raised" | "item_not_failed";

export interface FindingRef {
  inspection_id: string;
  item_key: string;
}

export interface IncidentRef {
  incident_id: string;
  version: number;
}

export interface RaiseRequest {
  finding?: FindingRef | null;
  incident?: IncidentRef | null;
  severity?: Severity | null;
  title: string;
  description?: string | null;
  owner_id: string;
  responsible_party?: string | null;
  due_on?: string | null;
  due_reason?: string | null;
  client_raised_at: string;
  device_id?: string | null;
}

export interface EventRequest {
  kind: ActionRequestKind;
  owner_id?: string | null;
  responsible_party?: string | null;
  due_on?: string | null;
  closeout_id?: string | null;
  reason?: string | null;
  client_at: string;
  device_id?: string | null;
}

export interface CloseoutRequest {
  note?: string | null;
  evidence_ids: string[];
  client_submitted_at: string;
  device_id?: string | null;
}

export interface ActionSummary {
  id: string;
  site_id: string;
  source: ActionSource;
  inspection_id: string | null;
  item_key: string | null;
  source_corrected_by: string | null;
  incident_id: string | null;
  incident_version: number | null;
  incident_latest_version: number | null;
  review: ActionReview | null;
  severity: Severity;
  title: string;
  description: string | null;
  raised_by: string;
  client_raised_at: string;
  raised_at: string;
  device_id: string | null;
  time_zone: string;
  source_at: string;
  default_due_on: string;
  status: ActionStatus;
  state: ActionState;
  owner_id: string;
  responsible_party: string | null;
  due_on: string;
  due_at: string;
  pending_closeout_id: string | null;
  awaiting_since: string | null;
  closed_at: string | null;
}

export interface Action {
  id: string;
  site_id: string;
  source: ActionSource;
  inspection_id: string | null;
  item_key: string | null;
  source_corrected_by: string | null;
  incident_id: string | null;
  incident_version: number | null;
  incident_latest_version: number | null;
  review: ActionReview | null;
  severity: Severity;
  title: string;
  description: string | null;
  raised_by: string;
  client_raised_at: string;
  raised_at: string;
  device_id: string | null;
  time_zone: string;
  source_at: string;
  default_due_on: string;
  status: ActionStatus;
  state: ActionState;
  owner_id: string;
  responsible_party: string | null;
  due_on: string;
  due_at: string;
  pending_closeout_id: string | null;
  awaiting_since: string | null;
  closed_at: string | null;
  events: ActionEvent[];
  closeouts: Closeout[];
}

export interface ActionEvent {
  id: string;
  seq: number;
  kind: ActionEventKind;
  actor_id: string;
  client_at: string;
  recorded_at: string;
  device_id: string | null;
  reason: string | null;
  status: ActionStatus;
  owner_id: string;
  responsible_party: string | null;
  due_on: string;
  closeout_id: string | null;
  self_verified: boolean | null;
}

export interface Closeout {
  id: string;
  note: string | null;
  submitted_by: string;
  client_submitted_at: string;
  submitted_at: string;
  device_id: string | null;
  evidence: CloseoutEvidence[];
  decision: "verified" | "rejected" | null;
  late: boolean;
  recorded_after_due: boolean;
}

export interface CloseoutEvidence {
  evidence_id: string;
  client_captured_at: string;
  captured_before_raised: boolean;
}

export interface ActionList {
  actions: ActionSummary[];
}

export interface WaiverRequest {
  inspection_id: string;
  item_key: string;
  reason: string;
  client_waived_at: string;
  device_id?: string | null;
}

export interface RetractWaiverRequest {
  reason: string;
}

export interface Waiver {
  id: string;
  site_id: string;
  inspection_id: string;
  item_key: string;
  severity: Severity;
  reason: string;
  waived_by: string;
  client_waived_at: string;
  waived_at: string;
  device_id: string | null;
  retracted_by: string | null;
  retracted_at: string | null;
  retract_reason: string | null;
}

export interface NotLatestVersion {
  error: string;
  latest_version: number;
}
