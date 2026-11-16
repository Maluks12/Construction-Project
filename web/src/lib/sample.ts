// Sample records for the screens that aren't connected to the API yet
// (their endpoints don't exist). Taken from the SiteProof prototype. Each
// page using this file shows a "Sample data" banner; when an endpoint
// ships, replace the page's import with a read in data.ts and delete the
// matching block here.
//
// Square-bracket values ([PLAN NAME], [DATE FROM RULE]...) are facts that
// haven't been decided or sourced yet. They are placeholders, not data.

import type { Tone } from "@/components/ui";

export const iod = {
  worker: "Bongani Zulu",
  incident: "INC-0042",
  stages: [
    { form: "W.Cl.2", what: "Employer's report of the accident", state: "Now", tone: "warn" as Tone },
    { form: "W.Cl.3", what: "Employee's notice of the accident and claim", state: "Collect", tone: "neutral" as Tone },
    { form: "W.Cl.4", what: "First medical report, from the doctor", state: "Awaiting", tone: "neutral" as Tone },
    { form: "W.Cl.5", what: "Progress or final medical report", state: "Later", tone: "neutral" as Tone },
    { form: "W.Cl.6", what: "Resumption of work report", state: "Later", tone: "neutral" as Tone },
  ],
  fields: [
    ["Employee", "Bongani Zulu"],
    ["ID or passport number", ""],
    ["Occupation", "General worker"],
    ["Date and time of accident", "Mon 16 Nov 2026, 08:55"],
    ["Employer became aware", "Mon 16 Nov 2026, 08:55"],
    ["Earnings at the time", ""],
    ["Compensation Fund registration number", ""],
    ["Treating doctor or clinic", "Rosebank clinic"],
  ],
  how: "Stepped onto an unsecured scaffold board, which tipped. Fell about 1.8 m to the platform below.",
  injuries: "Concussion; fractured left wrist",
  docs: [
    { name: "Copy of ID", state: "Missing", tone: "bad" as Tone },
    { name: "Incident report (INC-0042 v2)", state: "Linked", tone: "ok" as Tone },
    { name: "Witness statement", state: "Linked", tone: "ok" as Tone },
    { name: "First medical report (W.Cl.4)", state: "Awaiting doctor", tone: "warn" as Tone },
    { name: "Payslip for the month", state: "Missing", tone: "bad" as Tone },
  ],
};

export const delayEvents = [
  { title: "Scaffold Level 3 east closed", ref: "DE-014 · after INC-0042", start: "Mon 16 Nov", cause: "Safety stoppage", days: "1 (so far)", notice: "Not sent", tone: "warn" as Tone, due: "Due by [DATE FROM RULE]", ev: "Diary, 2 photos" },
  { title: "Thunderstorms, work stopped at 14:00", ref: "DE-013", start: "Mon 16 Nov", cause: "Weather", days: "0.5", notice: "Not sent", tone: "warn" as Tone, due: "Due by [DATE FROM RULE]", ev: "Diary" },
  { title: "Structural drawings for Level 5 late", ref: "DE-012", start: "Mon 2 Nov", cause: "Late information", days: "4", notice: "Sent Thu 5 Nov", tone: "ok" as Tone, due: "Acknowledged by principal agent", ev: "Email, RFI log" },
  { title: "Rebar delivery short", ref: "DE-011", start: "Tue 27 Oct", cause: "Supplier or plant", days: "1", notice: "Missed", tone: "bad" as Tone, due: "Notice window closed", ev: "Delivery note photo" },
];
export const delayCauses = ["Weather", "Late information", "Variation instruction", "Supplier or plant", "Safety stoppage", "Other"];

export const permits = {
  kinds: ["Excavation work", "Work at height", "Demolition", "Use of explosives", "Single-storey dwelling for the client"],
  items: [
    { title: "Construction work permit", ref: "Reg 3", sub: "Needed only above the regulation's thresholds", state: "Not required", tone: "neutral" as Tone },
    { title: "Notification: foundations phase", ref: "Reg 4", sub: "Sent Mon 3 Aug · acknowledgement attached", state: "Sent", tone: "ok" as Tone },
    { title: "Notification: roof and edge work", ref: "Reg 4", sub: "Planned start Mon 7 Dec", state: "Due", tone: "warn" as Tone },
  ],
};

export const toolbox = {
  talks: [
    { topic: "Scaffold boards: clip and check before you step", when: "Mon 16 Nov 12:30", by: "Thandi Mokoena", signed: 38, present: 41, state: "Signing", tone: "warn" as Tone },
    { topic: "Working near open excavations", when: "Mon 16 Nov 06:45", by: "Sipho Ndlovu", signed: 44, present: 44, state: "Complete", tone: "ok" as Tone },
    { topic: "Heat and hydration", when: "Fri 13 Nov 06:45", by: "Sipho Ndlovu", signed: 39, present: 40, state: "1 refused", tone: "neutral" as Tone },
    { topic: "Lightning: when to stop work", when: "Thu 12 Nov 06:45", by: "Lerato Dube", signed: 42, present: 42, state: "Complete", tone: "ok" as Tone },
  ],
  diary: {
    day: "Mon 16 Nov",
    facts: [["Headcount", "46 on site"], ["Subcontractors", "4 firms"], ["Weather", "Thunderstorms after 14:00"], ["Hours lost", "1.5 (rain)"]] as [string, string][],
    notes: "Level 4 slab pour moved to Wednesday (pump unavailable). Scaffold on Level 3 east closed after INC-0042 until inspected. Excavation north face bay stopped pending new trench sheets.",
    by: "Written by Sipho Ndlovu, 16:40. Days are closed at midnight; later notes are added as dated addenda.",
  },
};

export const people = {
  workers: [
    { name: "Lerato Dube", employer: "Pillay Construction", trade: "Scaffolder", comps: [{ label: "Working at heights", when: "expired 12 Nov", tone: "bad" as Tone }, { label: "Scaffold erector", when: "Mar 2027", tone: "ok" as Tone }], appt: "Scaffold inspector" },
    { name: "Johan van Wyk", employer: "Pillay Construction", trade: "Plant operator", comps: [{ label: "TLB operator", when: "24 Nov", tone: "warn" as Tone }, { label: "Medical fitness", when: "Jun 2027", tone: "ok" as Tone }], appt: "—" },
    { name: "Sipho Ndlovu", employer: "Pillay Construction", trade: "Site supervisor", comps: [{ label: "First aid level 2", when: "Aug 2027", tone: "ok" as Tone }], appt: "Construction supervisor" },
    { name: "Ayanda Khumalo", employer: "Khumalo Electrical", trade: "Electrician", comps: [{ label: "First aid level 1", when: "3 Dec", tone: "warn" as Tone }, { label: "Wireman's licence", when: "No expiry", tone: "neutral" as Tone }], appt: "—" },
    { name: "Bongani Zulu", employer: "Pillay Construction", trade: "General worker", comps: [{ label: "Site induction", when: "Feb 2027", tone: "ok" as Tone }], appt: "—" },
  ],
  docs: [
    { name: "Health and safety plan", v: 4, up: "Thu 5 Nov · Thandi Mokoena", review: "Feb 2027", tone: "ok" as Tone },
    { name: "Baseline risk assessment", v: 2, up: "Mon 2 Nov · Thandi Mokoena", review: "Feb 2027", tone: "ok" as Tone },
    { name: "Fall protection plan", v: 3, up: "Mon 16 Nov · Thandi Mokoena", review: "Review after INC-0042", tone: "warn" as Tone },
    { name: "Temporary works design, Level 4", v: 1, up: "Tue 10 Nov · [DESIGNER]", review: "Before pour", tone: "neutral" as Tone },
    { name: "Construction work permit", v: 1, up: "Mon 3 Aug · Thandi Mokoena", review: "No expiry", tone: "neutral" as Tone },
  ],
};

export const subcontractors = [
  { name: "Khumalo Electrical", trade: "Electrical", workers: 6, comp: "All valid", compTone: "ok" as Tone, findings: "0", inc: "0", file: "Complete", fileTone: "ok" as Tone, link: "Linked", linkTone: "ok" as Tone },
  { name: "Botha Steel", trade: "Structural steel", workers: 9, comp: "2 expiring", compTone: "warn" as Tone, findings: "1 high", inc: "1 first aid", file: "Fall plan missing", fileTone: "bad" as Tone, link: "Linked", linkTone: "ok" as Tone },
  { name: "Moloi Plumbing", trade: "Plumbing", workers: 4, comp: "Not shared yet", compTone: "neutral" as Tone, findings: "—", inc: "—", file: "—", fileTone: "neutral" as Tone, link: "Request sent", linkTone: "warn" as Tone },
  { name: "Sky Scaffolding", trade: "Scaffolding", workers: 0, comp: "Not on SiteProof", compTone: "neutral" as Tone, findings: "—", inc: "—", file: "—", fileTone: "neutral" as Tone, link: "Invite to join", linkTone: "neutral" as Tone },
];

export const auditPack = {
  parts: [
    { label: "Compliance summary", sub: "Open findings, overdue actions, expiring competencies", on: true },
    { label: "Inspections and findings", sub: "Including abandoned inspections, marked, with who, when and why", on: true },
    { label: "Incidents, versions and deadlines", sub: "Every version as submitted, and what each deadline said and when", on: true },
    { label: "Corrective actions", sub: "With close-out photos", on: true },
    { label: "Evidence manifest", sub: "Every photo: hash, capture and receipt times, chain entry", on: true },
    { label: "Chain verification", sub: "Result, length and head hash per site, to keep outside SiteProof", on: true },
    { label: "Evidence access log", sub: "Who viewed what, when", on: false },
  ],
  packs: [
    { id: "AP-0008", when: "Mon 16 Nov 11:20", scope: "Rosebank Block B · Aug to Nov", who: "Thandi Mokoena", chain: "Building…", status: "Building", tone: "warn" as Tone },
    { id: "AP-0007", when: "Fri 13 Nov 17:12", scope: "All sites · Oct", who: "Ravi Pillay", chain: "No break · 3 sites, heads recorded", status: "Ready", tone: "ok" as Tone },
    { id: "AP-0006", when: "Mon 2 Nov 09:00", scope: "Midrand Logistics Park · Sep to Oct", who: "Thandi Mokoena", chain: "No break · 612 entries", status: "Ready", tone: "ok" as Tone },
  ],
};

export const syncConflicts = [
  { title: "Reversing alarm working?", sub: "Mobile plant pre-use · Johan van Wyk · 2 phones", kind: "Older answer", tone: "warn" as Tone },
  { title: "Inspection arrived after it was abandoned", sub: "Temporary works · Lerato Dube · 3 answers", kind: "Abandoned", tone: "neutral" as Tone },
  { title: "Photo arrived after completion", sub: "Housekeeping walk · Sipho Ndlovu", kind: "Completed", tone: "neutral" as Tone },
  { title: "Same evidence id, different photo", sub: "Rosebank Block B · Sipho Ndlovu", kind: "Resolved: new id", tone: "ok" as Tone },
];

export const reminders = {
  rules: [
    { when: "A call-now deadline starts", who: "Reporter, site supervisor, SHE officers", push: true, sms: true, email: true, timing: "Immediately, then every 30 min until logged", locked: true },
    { when: "A legal deadline is near", who: "SHE officers, owner", push: true, sms: true, email: true, timing: "48 h and 24 h before due", locked: true },
    { when: "A legal deadline is overdue", who: "SHE officers, owner", push: true, sms: true, email: true, timing: "Daily at 07:00", locked: true },
    { when: "A critical finding is raised", who: "Site supervisor, SHE officers", push: true, sms: true, email: false, timing: "Immediately", locked: false },
    { when: "A corrective action is due", who: "Action owner", push: true, sms: false, email: true, timing: "1 day before, then daily when overdue", locked: false },
    { when: "A competency or appointment expires", who: "SHE officers, the worker's supervisor", push: false, sms: false, email: true, timing: "30 and 7 days before", locked: false },
    { when: "An inspection is due and not started", who: "Site supervisor", push: true, sms: false, email: false, timing: "At 09:00 for daily checks; shift start for per-shift", locked: false },
  ],
  sent: [
    { when: "Mon 16 Nov 11:30", to: "Thandi Mokoena", msg: "INC-0042: call the inspector now (GAR 8(1)(b))", ch: "SMS", status: "Delivered", tone: "ok" as Tone },
    { when: "Mon 16 Nov 11:30", to: "Sipho Ndlovu", msg: "INC-0042: call the inspector now (GAR 8(1)(b))", ch: "Push", status: "Opened", tone: "ok" as Tone },
    { when: "Mon 16 Nov 08:23", to: "Sipho Ndlovu", msg: "Critical finding: shoring, Rosebank Block B", ch: "SMS", status: "Failed, sent by email", tone: "warn" as Tone },
    { when: "Mon 16 Nov 07:00", to: "Ravi Pillay", msg: "2 corrective actions overdue", ch: "Email", status: "Delivered", tone: "ok" as Tone },
    { when: "Sun 15 Nov 07:00", to: "Lerato Dube", msg: "Working at heights expired 12 Nov", ch: "Email", status: "Bounced", tone: "bad" as Tone },
  ],
};

export const subscription: [string, string][] = [
  ["Plan", "[PLAN NAME]"],
  ["Seats", "7 of 10 used"],
  ["Status", "Active"],
  ["Next debit", "[AMOUNT] on 1 Dec 2026"],
  ["Payment", "DebiCheck mandate · approved in banking app"],
  ["Account", "[BANK] ····4821"],
];

export const company: [string, string, string?][] = [
  ["Trading name", "Pillay Construction"],
  ["Registered name", "Pillay Construction (Pty) Ltd"],
  ["Company registration number", ""],
  ["Compensation Fund registration number", "", "Printed on the W.Cl.2 employer's report."],
  ["Physical address", "Rosebank, Johannesburg"],
  ["Office phone", ""],
];
