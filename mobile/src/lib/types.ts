// Shapes returned by the Go API (construction_db). Copied from
// web/src/lib/types.ts, which mirrors docs/api/*.md and the json tags in
// internal/core/*/model.go. Keep the two files identical.

export type TokenPair = {
  access_token: string;
  refresh_token: string;
  expires_in: number; // seconds
  token_type: "Bearer";
};

// ---- docs/api/me.md (M12) ----
// Capabilities are display hints: hide a button the role would get 403 for.
// They decide nothing on the server, and for a supervisor each one means "on
// your own sites".
export type Capability =
  | "capture_records"
  | "read_safety_file"
  | "write_safety_file"
  | "manage_sites"
  | "invite_members"
  | "view_vault"
  | "open_medical_certificates"
  | "verify_chain"
  | "waive_findings"
  | "verify_actions"
  | "manage_workers"
  | "view_compliance"
  | "export_audit_pack";

export type Me = {
  user_id: string;
  org_id: string;
  session_id: string;
  role: Role;
  full_name: string;
  org_name: string;
  capabilities: Capability[];
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

// docs/api/deadlines.md (REG4): what started a deadline.
export type DeadlineSubjectType = "incident" | "site_work";

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
  subject_type: DeadlineSubjectType;
  incident_id: string | null;
  site_work_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
  proof_document_types: string[];
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
  document_id: string | null;
  document_version: number | null;
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
  subject_type: DeadlineSubjectType;
  incident_id: string | null;
  site_work_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
  proof_document_types: string[];
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
  // Site work: a version of an H&S file document whose type is in the
  // deadline's proof_document_types. Both or neither.
  document_id?: string | null;
  document_version?: number | null;
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

// ---- docs/api/toolbox.md (M10, M11) ----
// RetractRequest, Retraction and PhotoRequest are renamed Talk* here: the
// toolbox ones carry client_retracted_at, unlike the shared ones above.
export type AttendanceMethod = "tap" | "paper";

export type TalkPhotoKind = "photo" | "paper_register";

export interface ToolboxTopic {
  key: string;
  title: string;
  source_ref: string;
}

export interface ToolboxTopicList {
  topics: ToolboxTopic[];
}

export interface TalkRequest {
  topic_key?: string | null;
  topic?: string | null;
  presenter_name: string;
  notes?: string | null;
  started_at: string;
  ended_at?: string | null;
  device_id?: string | null;
}

export interface TalkPhotoRequest {
  kind: TalkPhotoKind;
}

export interface SignRequest {
  name: string;
  firm?: string | null;
  clock_number?: string | null;
  method: AttendanceMethod;
  client_signed_at: string;
  device_id?: string | null;
}

export interface TalkRetractRequest {
  reason: string;
  client_retracted_at: string;
}

export interface TalkSummary {
  id: string;
  site_id: string;
  topic_key: string | null;
  topic: string;
  presenter_name: string;
  notes: string | null;
  started_at: string;
  ended_at: string | null;
  talk_date: string;
  time_zone: string;
  recorded_by: string;
  device_id: string | null;
  created_at: string;
  updated_at: string | null;
  signed_count: number;
  photo_count: number;
}

export interface Talk {
  id: string;
  site_id: string;
  topic_key: string | null;
  topic: string;
  presenter_name: string;
  notes: string | null;
  started_at: string;
  ended_at: string | null;
  talk_date: string;
  time_zone: string;
  recorded_by: string;
  device_id: string | null;
  created_at: string;
  updated_at: string | null;
  signed_count: number;
  photos: TalkPhoto[];
  attendance: Attendance[];
  not_signed: NotSigned[];
}

export interface TalkList {
  talks: TalkSummary[];
}

export interface TalkPhoto {
  evidence_id: string;
  kind: TalkPhotoKind;
  added_by: string;
  added_at: string;
}

export interface Attendance {
  id: string;
  talk_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  method: AttendanceMethod;
  recorded_by: string;
  client_signed_at: string;
  signed_at: string;
  device_id: string | null;
  retraction: TalkRetraction | null;
  worker_id: string | null;
}

export interface TalkRetraction {
  reason: string;
  retracted_by: string;
  client_retracted_at: string;
  retracted_at: string;
}

export interface NotSigned {
  worker_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  sign_in_id: string;
}

export interface AlreadySigned {
  error: string;
  hint: string;
}

// ---- docs/api/diary.md (M10, Construction edition) ----
export type WeatherCondition = "fine" | "cloudy" | "rain" | "heavy_rain" | "thunderstorm" | "wind" | "hot" | "cold" | "mist";

export type PlantState = "working" | "idle" | "broken_down";

export type InstructionForm = "verbal" | "written";

export type DeliveryStatus = "received" | "expected_not_received";

export interface Weather {
  conditions: WeatherCondition[];
  note: string | null;
}

export interface Sections {
  weather_times?: WeatherTimes;
  labour?: LabourRow[];
  plant?: PlantRow[];
  instructions?: Instruction[];
  delays?: Delay[];
  visitors?: Visitor[];
  deliveries?: Delivery[];
}

export interface WeatherTimes {
  rain_started_at: string | null;
  rain_stopped_at: string | null;
  work_stopped_at: string | null;
  work_restarted_at: string | null;
  rain_gauge_mm: number | null;
  hours_lost: number | null;
  activities_affected: string | null;
  critical_path: boolean | null;
  delayed_effect: boolean | null;
}

export interface LabourRow {
  id: string;
  firm: string;
  own_team: boolean;
  trade: string | null;
  headcount: number;
  worker_ids?: string[];
}

export interface PlantRow {
  id: string;
  description: string;
  ref: string | null;
  state: PlantState;
  hours_working: number | null;
  hours_standing: number | null;
}

export interface Instruction {
  id: string;
  from_name: string;
  from_role: string | null;
  received_at: string;
  form: InstructionForm;
  description: string;
  reference: string | null;
}

export interface Delay {
  id: string;
  description: string;
  started_at: string | null;
  ended_at: string | null;
  hours_lost: number | null;
  cause: string | null;
}

export interface Visitor {
  id: string;
  name: string;
  organisation: string | null;
  purpose: string | null;
  arrived_at: string | null;
  left_at: string | null;
}

export interface Delivery {
  id: string;
  description: string;
  supplier: string | null;
  quantity: string | null;
  status: DeliveryStatus;
  received_at: string | null;
}

export interface RevisionRequest {
  base_revision_id?: string | null;
  weather: Weather;
  headcount: number;
  work_done: string;
  sections?: Sections;
  evidence_ids?: string[];
  client_saved_at: string;
  late_reason?: string | null;
  device_id?: string | null;
}

export interface AddendumRequest {
  reason: string;
  note?: string | null;
  sections?: Sections;
  evidence_ids?: string[];
  client_added_at: string;
  device_id?: string | null;
}

export interface Revision {
  id: string;
  site_id: string;
  diary_date: string;
  seq: number;
  base_revision_id: string | null;
  weather: Weather;
  headcount: number;
  headcount_suggested: number;
  work_done: string;
  sections: Sections;
  evidence_ids: string[];
  saved_by: string;
  client_saved_at: string;
  server_received_at: string;
  saved_after_close: boolean;
  received_after_close: boolean;
  late_reason: string | null;
  device_id: string | null;
}

export interface Addendum {
  id: string;
  site_id: string;
  diary_date: string;
  reason: string;
  note: string | null;
  sections: Sections;
  evidence_ids: string[];
  added_by: string;
  client_added_at: string;
  server_received_at: string;
  device_id: string | null;
}

export interface DiarySummary {
  site_id: string;
  diary_date: string;
  time_zone: string;
  opens_at: string;
  closes_at: string;
  grace_ends_at: string;
  closed: boolean;
  late: boolean;
  created_by: string;
  created_at: string;
  revision_count: number;
  conflict_revision_ids: string[];
  addendum_count: number;
  head_revision_id: string;
  weather: Weather;
  headcount: number;
  headcount_suggested: number;
}

export interface Diary {
  site_id: string;
  diary_date: string;
  time_zone: string;
  opens_at: string;
  closes_at: string;
  grace_ends_at: string;
  closed: boolean;
  late: boolean;
  created_by: string;
  created_at: string;
  revision_count: number;
  conflict_revision_ids: string[];
  addendum_count: number;
  current: Revision;
  headcount_suggested_now: number;
  addenda: Addendum[];
  toolbox_talk_ids: string[];
  incident_ids: string[];
}

export interface DiaryList {
  diaries: DiarySummary[];
}

export interface RevisionList {
  revisions: Revision[];
}

export interface FirmList {
  firms: string[];
}

export interface SectionProblem {
  error: string;
  section: string;
  row: number;
  field: string;
}

// ---- docs/api/files.md (M11) ----
// Renamed from DeclareRequest, Upload, File and Rejected: evidence.md has its
// own DeclareRequest and Upload, and File is the browser's own type. The
// view endpoints return the ViewURL above.
export type FilePurpose = "competency" | "medical" | "appointment" | "site_document" | "org_document";

export type FileContentType = "application/pdf" | "image/jpeg" | "image/png";

export type FileRejectedReason = "size_mismatch" | "hash_mismatch" | "wrong_type";

export interface FileDeclareRequest {
  purpose: FilePurpose;
  site_id?: string | null;
  content_sha256: string;
  size_bytes: number;
  content_type: FileContentType;
  file_name?: string | null;
  device_id?: string | null;
}

export interface FileUpload {
  method: string;
  url: string;
  headers: Record<string, string>;
  expires_at: string;
}

export interface StoredFile {
  id: string;
  purpose: FilePurpose;
  site_id: string | null;
  uploaded_by: string;
  content_sha256: string;
  size_bytes: number;
  content_type: FileContentType;
  file_name: string | null;
  device_id: string | null;
  status: "pending" | "verified" | "rejected";
  rejected_reason: FileRejectedReason | null;
  retain_until: string | null;
  created_at: string;
  upload?: FileUpload;
}

export interface FileRejection {
  error: string;
  rejected_reason: FileRejectedReason;
}

// ---- docs/api/workers.md (M11) ----
// Link is renamed SignatureLink (next/link exports a Link). RetractRequest
// and Retraction are the shared ones above.
export type RecordState = "valid" | "expiring" | "expired" | "superseded" | "retracted";

export type MedicalOutcome = "fit" | "fit_with_restrictions" | "unfit";

export type SignInWarning = "no_valid_medical" | "medical_unfit" | "competency_expired";

export interface WorkerRequest {
  name: string;
  firm?: string | null;
  clock_number?: string | null;
  trade?: string | null;
  active?: boolean | null;
}

export interface Worker {
  id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  trade: string | null;
  active: boolean;
  created_by: string;
  created_at: string;
  updated_by: string | null;
  updated_at: string | null;
}

export interface WorkerList {
  workers: Worker[];
}

export interface WorkerDetail {
  id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  trade: string | null;
  active: boolean;
  created_by: string;
  created_at: string;
  updated_by: string | null;
  updated_at: string | null;
  competencies: Competency[];
  medicals: Medical[];
}

export interface CompetencyRequest {
  regime_code: string;
  type_key: string;
  number?: string | null;
  issuer?: string | null;
  issued_on: string;
  expires_on?: string | null;
  file_id: string;
  supersedes_id?: string | null;
  client_recorded_at: string;
  device_id?: string | null;
}

export interface MedicalRequest {
  regime_code: string;
  outcome: MedicalOutcome;
  examined_on: string;
  expires_on: string;
  practice_number?: string | null;
  file_id: string;
  supersedes_id?: string | null;
  client_recorded_at: string;
  device_id?: string | null;
}

export interface Competency {
  id: string;
  worker_id: string;
  regime_code: string;
  file_id: string;
  supersedes_id: string | null;
  superseded_by: string | null;
  recorded_by: string;
  client_recorded_at: string;
  recorded_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  state: RecordState;
  type_key: string;
  number: string | null;
  issuer: string | null;
  issued_on: string;
  expires_on: string | null;
}

export interface Medical {
  id: string;
  worker_id: string;
  regime_code: string;
  file_id: string;
  supersedes_id: string | null;
  superseded_by: string | null;
  recorded_by: string;
  client_recorded_at: string;
  recorded_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  state: RecordState;
  outcome: MedicalOutcome;
  examined_on: string;
  expires_on: string;
  practice_number: string | null;
}

export interface LinkRequest {
  worker_id: string;
}

export interface UnlinkRequest {
  reason: string;
}

export interface SignatureLink {
  id: string;
  attendance_id: string;
  worker_id: string;
  method: "auto" | "manual";
  linked_by: string;
  linked_at: string;
  unlinked_by: string | null;
  unlinked_at: string | null;
  unlink_reason: string | null;
}

export interface RegimeSummary {
  code: string;
  name: string;
  edition: string;
  jurisdiction: string;
}

export interface RegimeList {
  regimes: RegimeSummary[];
}

export interface CompetencyType {
  key: string;
  label: string;
  kind: "certificate" | "licence" | "training" | "registration";
  expiry: "required" | "optional" | "none";
  source_ref: string;
}

export interface AppointmentType {
  key: string;
  label: string;
  expiry: "required" | "optional" | "none";
  required: boolean;
  required_note: string;
  source_ref: string;
}

export interface MedicalRule {
  default_validity_months: number;
  source_ref: string;
}

export interface DocumentType {
  key: string;
  label: string;
  scope: "org" | "site";
  required: boolean;
  expiry: "required" | "optional" | "none";
  issuers: string[];
  source_ref: string;
}

export interface Reminders {
  worker_expiry_days: number[];
  company_document_days: number[];
  deadline_days_before: number[];
  notification_days_before: number[];
  action_escalation_days: number[];
  verification_stalled_days: number;
  roster_lapse_days: number;
  source_ref: string;
}

export interface RegimeTypes {
  regime_code: string;
  competency_types: CompetencyType[];
  appointment_types: AppointmentType[];
  medical: MedicalRule;
  document_types: DocumentType[];
  reminders: Reminders;
}

export interface FieldProblem {
  error: string;
  field: string;
}

export interface IdentityClash {
  error: string;
  worker_id: string;
}

// ---- docs/api/attendance.md (M11) ----
// Day is renamed SignInDay. RetractRequest and Retraction are the shared ones.
export type SignInMethod = "tap" | "paper";

export interface SignInRequest {
  worker_id: string;
  new_worker?: WorkerRequest | null;
  method: SignInMethod;
  client_signed_in_at: string;
  device_id?: string | null;
}

export interface SignOutRequest {
  method: SignInMethod;
  client_signed_out_at: string;
}

export interface RemoveRequest {
  reason: string;
}

export interface SignIn {
  id: string;
  site_id: string;
  worker_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  method: SignInMethod;
  matched_existing: boolean;
  recorded_by: string;
  client_signed_in_at: string;
  received_at: string;
  sign_in_date: string;
  time_zone: string;
  received_after_close: boolean;
  device_id: string | null;
  sign_out: SignOut | null;
  retraction: Retraction | null;
  ends_at: string;
  lapsed: boolean;
  warnings: SignInWarning[];
}

export interface SignOut {
  method: SignInMethod;
  signed_out_by: string;
  client_signed_out_at: string;
  received_at: string;
}

export interface Sheet {
  site_id: string;
  sheet_date: string;
  evidence_id: string;
  added_by: string;
  added_at: string;
}

export interface Overlap {
  sign_in_id: string;
  other_site_id: string;
  other_sign_in_id: string;
}

export interface SignInDay {
  site_id: string;
  date: string;
  sign_ins: SignIn[];
  sheets: Sheet[];
  absent: RosterEntry[];
  overlaps: Overlap[];
  headcount: number;
}

export interface RosterEntry {
  id: string;
  worker_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  active: boolean;
  added_via: "sign_in" | "manual";
  added_by: string;
  added_at: string;
  removed_by: string | null;
  removed_at: string | null;
  remove_reason: string | null;
  last_sign_in_on: string | null;
  expected: boolean;
}

export interface RosterList {
  roster: RosterEntry[];
}

export interface AlreadySignedIn {
  error: string;
  sign_in_id: string;
}

// ---- docs/api/documents.md (M11: the vault and the H&S file) ----
// Renamed: CreateRequest, VersionRequest, Document (the browser's own type),
// Version (incidents have one) and Register.
export type DocumentScope = "org" | "site";

export type VersionState = "valid" | "expiring" | "expired";

export type ReadinessStatus = "present" | "missing" | "expired";

export interface DocumentCreateRequest {
  regime_code?: string | null;
  type_key: string;
  title: string;
  version: DocumentVersionRequest;
}

export interface DocumentVersionRequest {
  file_id: string;
  issuer?: string | null;
  reference?: string | null;
  issued_on?: string | null;
  expires_on?: string | null;
  client_uploaded_at: string;
  device_id?: string | null;
}

export interface DocumentSummary {
  id: string;
  scope: DocumentScope;
  site_id: string | null;
  regime_code: string;
  type_key: string;
  title: string;
  created_by: string;
  created_at: string;
  latest_version: number;
  current: DocumentVersion;
  retraction: Retraction | null;
}

export interface DocumentDetail {
  id: string;
  scope: DocumentScope;
  site_id: string | null;
  regime_code: string;
  type_key: string;
  title: string;
  created_by: string;
  created_at: string;
  latest_version: number;
  current: DocumentVersion;
  retraction: Retraction | null;
  versions: DocumentVersion[];
}

export interface DocumentVersion {
  version: number;
  file_id: string;
  issuer: string | null;
  reference: string | null;
  issued_on: string | null;
  expires_on: string | null;
  uploaded_by: string;
  client_uploaded_at: string;
  received_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  // VersionState for the current version; "superseded" or "retracted" for the others.
  state: string;
}

export interface Readiness {
  type_key: string;
  label: string;
  required: boolean;
  status: ReadinessStatus;
  document_ids: string[];
}

export interface DocumentRegister {
  regime_code: string;
  documents: DocumentSummary[];
  readiness: Readiness[];
}

// ---- docs/api/appointments.md (M11, Construction edition) ----
export interface AppointmentRequest {
  worker_id: string;
  type_key: string;
  appointed_on: string;
  expires_on?: string | null;
  file_id: string;
  supersedes_id?: string | null;
  client_recorded_at: string;
  device_id?: string | null;
}

export interface Appointment {
  id: string;
  worker_id: string;
  regime_code: string;
  file_id: string;
  supersedes_id: string | null;
  superseded_by: string | null;
  recorded_by: string;
  client_recorded_at: string;
  recorded_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  state: RecordState;
  site_id: string;
  type_key: string;
  worker_name: string;
  appointed_on: string;
  expires_on: string | null;
}

export interface AppointmentList {
  appointments: Appointment[];
}

// ---- docs/api/notifications.md (M11) ----
export type NotificationKind = "incident_deadline" | "site_work_deadline" | "action_overdue" | "verification_stalled" | "competency_expiry" | "medical_expiry" | "appointment_expiry" | "document_expiry" | "integrity_alarm";

export type NotificationSubject = "deadline" | "deadline_duty" | "action" | "closeout" | "competency" | "medical" | "appointment" | "document" | "export";

export type SubjectState = "current" | "superseded";

export interface Notification {
  id: string;
  kind: NotificationKind;
  subject_type: NotificationSubject;
  subject_id: string;
  threshold: string;
  site_id: string | null;
  title: string;
  due_on: string | null;
  due_at: string | null;
  created_at: string;
  read_at: string | null;
  subject_state: SubjectState;
}

export interface NotificationList {
  notifications: Notification[];
}

// ---- docs/api/compliance.md (M12) ----
export type Colour = "red" | "amber" | "green";

export type CategoryKey = "hs_file" | "vault" | "appointments" | "findings" | "actions" | "verifications" | "sign_ins" | "incident_deadlines" | "site_work_deadlines" | "worker_records" | "absent_workers" | "pending_uploads";

export type ComplianceSubject = "document" | "document_type" | "appointment" | "appointment_type" | "finding" | "action" | "closeout" | "sign_in" | "deadline" | "competency" | "medical" | "roster_entry" | "evidence" | "file";

export type ItemState = "missing" | "expired" | "expiring" | "unactioned" | "overdue" | "due_now" | "due_soon" | "stalled" | "warnings" | "absent" | "pending" | "no_appointee";

export interface ComplianceItem {
  subject_type: ComplianceSubject;
  subject_id: string | null;
  state: ItemState;
  colour: Colour | null;
  label: string;
  inspection_id: string | null;
  item_key: string | null;
  worker_id: string | null;
  incident_id: string | null;
  site_work_id: string | null;
  action_id: string | null;
  type_key: string | null;
  severity: string | null;
  escalation_step: number | null;
  due_on: string | null;
  since: string | null;
  warnings: string[];
  note: string | null;
}

export interface ComplianceCategory {
  key: CategoryKey;
  counted: boolean;
  status: Colour | null;
  count: number;
  items: ComplianceItem[];
}

export interface SiteSummary {
  site_id: string;
  site_name: string;
  site_status: string;
  regime_code: string;
  time_zone: string;
  date: string;
  computed_at: string;
  status: Colour;
  red_items: number;
  amber_items: number;
  categories: ComplianceCategory[];
}

export interface SiteRank {
  site_id: string;
  site_name: string;
  site_status: string;
  status: Colour;
  red_items: number;
  amber_items: number;
  counts: Record<string, number>;
  statuses: Record<string, Colour>;
}

export interface Rollup {
  computed_at: string;
  org_items: ComplianceCategory[];
  sites: SiteRank[];
}

// ---- docs/api/exports.md (M12) ----
export type ExportStatus = "queued" | "building" | "ready" | "failed" | "expired";

export interface ExportRequest {
  from: string;
  to: string;
  client_requested_at: string;
  device_id?: string | null;
}

export interface Export {
  id: string;
  site_id: string;
  from: string;
  to: string;
  time_zone: string;
  as_of: string;
  requested_by: string;
  requester_role: string;
  includes_health: boolean;
  pack_format: number;
  regime_code: string;
  regime_version: string;
  regime_data_sha256: string;
  client_requested_at: string;
  device_id: string | null;
  status: ExportStatus;
  attempts: number;
  failed_attempts: number;
  integrity_alarm: boolean;
  last_error: string | null;
  sha256: string | null;
  size_bytes: number | null;
  evidence_count: number | null;
  file_count: number | null;
  chain_length: number | null;
  chain_head: string | null;
  chain_intact: boolean | null;
  finished_at: string | null;
  expires_at: string | null;
}

export interface ExportList {
  exports: Export[];
}

export interface Download {
  method: string;
  url: string;
  expires_at: string;
  sha256: string;
  size_bytes: number;
}

// ---- docs/api/site-work.md (REG4, Construction edition) ----
// Renamed as the REG4 handover asks: VersionRequest -> SiteWorkVersionRequest,
// Catalogue -> SiteWorkCatalogue. NotNextVersion is the incidents one above.
export type SiteWorkStatus = "planned" | "cancelled";

export interface SiteWorkVersionRequest {
  description: string;
  starts_on: string;
  ends_on?: string | null;
  facts: string[];
  status: SiteWorkStatus;
  client_submitted_at: string;
  device_id?: string | null;
  amend_reason?: string | null;
}

export interface SiteWorkFact {
  key: string;
  label: string;
  source_ref: string;
}

export interface SiteWorkVersion {
  version: number;
  description: string;
  starts_on: string;
  ends_on: string | null;
  facts: SiteWorkFact[];
  status: SiteWorkStatus;
  submitted_by: string;
  client_submitted_at: string;
  submitted_at: string;
  device_id: string | null;
  amend_reason: string | null;
  late_when_recorded: boolean;
}

export interface SiteWorkChanges {
  added: string[];
  superseded: string[];
  withdrawn: string[];
  carried: string[];
}

export interface SiteWork {
  id: string;
  site_id: string;
  created_by: string;
  created_at: string;
  latest_version: number;
  version: SiteWorkVersion;
  changes: SiteWorkChanges;
  deadlines: Deadline[];
}

export interface SiteWorkSummary {
  id: string;
  site_id: string;
  latest_version: number;
  description: string;
  starts_on: string;
  ends_on: string | null;
  facts: string[];
  status: SiteWorkStatus;
  created_at: string;
  open_deadlines: number;
  overdue_deadlines: number;
  next_due_at: string | null;
}

export interface SiteWorkList {
  site_work: SiteWorkSummary[];
}

export interface SiteWorkRule {
  rule_key: string;
  label: string;
  period: number;
  unit: string;
  duty_holder: string;
  caveat: string | null;
  source_ref: string;
  when_any: string[][];
  unless_facts: string[];
  unless_rules: string[];
}

export interface SiteWorkCatalogue {
  facts: SiteWorkFact[];
  rules: SiteWorkRule[];
}

export interface UnknownFacts {
  error: string;
  facts: string[];
}
