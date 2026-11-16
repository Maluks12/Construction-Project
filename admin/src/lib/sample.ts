// Sample records for the admin portal.
//
// The portal is front end only: the Go API has no admin endpoints yet, so
// nothing here is fetched and nothing is saved. Every page reads from this
// one file, so wiring a page later means replacing its import with a real
// read and deleting the matching block.
//
// The organisations and people are made up (the same ones the prototype
// uses). Square-bracket values ([AMOUNT], [PLAN NAME]) are facts that
// haven't been decided yet: placeholders, not data.

import type { Tone } from "@/components/ui";

export type Org = {
  id: string;
  name: string;
  owner: string;
  ownerLogin: string;
  plan: string;
  sites: number;
  seats: string;
  status: string;
  tone: Tone;
  joined: string;
  lastActive: string;
  regime: string;
  payment: string;
  paymentTone: Tone;
};

export const orgs: Org[] = [
  { id: "pillay-construction", name: "Pillay Construction", owner: "Ravi Pillay", ownerLogin: "Email", plan: "[PLAN NAME]", sites: 4, seats: "7 of 10", status: "Active", tone: "ok", joined: "3 Aug 2026", lastActive: "Today 11:30", regime: "Construction Regulations 2014", payment: "Mandate approved", paymentTone: "ok" },
  { id: "khumalo-electrical", name: "Khumalo Electrical", owner: "Ayanda Khumalo", ownerLogin: "Mobile number", plan: "[PLAN NAME]", sites: 2, seats: "6 of 10", status: "Active", tone: "ok", joined: "14 Sep 2026", lastActive: "Today 09:12", regime: "Construction Regulations 2014", payment: "Mandate approved", paymentTone: "ok" },
  { id: "botha-steel", name: "Botha Steel", owner: "Pieter Botha", ownerLogin: "Email", plan: "[PLAN NAME]", sites: 1, seats: "9 of 10", status: "Payment failed", tone: "bad", joined: "2 Oct 2026", lastActive: "Fri 13 Nov", regime: "Construction Regulations 2014", payment: "Debit failed 1 Nov", paymentTone: "bad" },
  { id: "moloi-plumbing", name: "Moloi Plumbing", owner: "Tebogo Moloi", ownerLogin: "Mobile number", plan: "Trial", sites: 1, seats: "4 of 5", status: "Trial · 6 days left", tone: "warn", joined: "9 Nov 2026", lastActive: "Sun 15 Nov", regime: "Construction Regulations 2014", payment: "No mandate yet", paymentTone: "neutral" },
  { id: "mahlangu-civils", name: "Mahlangu Civils", owner: "Thabo Mahlangu", ownerLogin: "Email", plan: "[PLAN NAME]", sites: 3, seats: "12 of 15", status: "Active", tone: "ok", joined: "21 Aug 2026", lastActive: "Today 07:02", regime: "Construction Regulations 2014", payment: "Mandate approved", paymentTone: "ok" },
  { id: "sky-scaffolding", name: "Sky Scaffolding", owner: "Zanele Mthembu", ownerLogin: "Email", plan: "Trial", sites: 0, seats: "1 of 5", status: "Signed up, no site", tone: "neutral", joined: "Sat 14 Nov 2026", lastActive: "Sat 14 Nov", regime: "—", payment: "No mandate yet", paymentTone: "neutral" },
];

export const orgSites: Record<string, { name: string; status: string; tone: Tone; chain: string; chainTone: Tone; lastInspection: string }[]> = {
  "pillay-construction": [
    { name: "Rosebank Block B", status: "Active", tone: "ok", chain: "No break found · 1,284 entries", chainTone: "ok", lastInspection: "Today 07:40" },
    { name: "Midrand Logistics Park", status: "Active", tone: "ok", chain: "No break found · 612 entries", chainTone: "ok", lastInspection: "Today 06:55" },
    { name: "Soweto Clinic Extension", status: "Completed", tone: "warn", chain: "Not checked in 7 days", chainTone: "warn", lastInspection: "Fri 13 Nov" },
    { name: "Sandton Office Refit", status: "Archived", tone: "neutral", chain: "No break found · 203 entries", chainTone: "ok", lastInspection: "Aug 2026" },
  ],
};

export const orgMembers = [
  { name: "Ravi Pillay", role: "Owner", login: "Email", last: "Today 08:30" },
  { name: "Thandi Mokoena", role: "SHE officer", login: "Email", last: "Today 11:30" },
  { name: "Sipho Ndlovu", role: "Supervisor", login: "Mobile number", last: "Today 09:12" },
  { name: "Johan van Wyk", role: "Supervisor", login: "Mobile number", last: "Today 06:58" },
  { name: "Priya Naidoo", role: "Bookkeeper", login: "Email", last: "Mon 9 Nov" },
];

export const kpis = [
  { label: "Organisations", value: "6", sub: "2 on trial" },
  { label: "Active sites", value: "9", sub: "11 including completed and archived" },
  { label: "Seats in use", value: "39", sub: "of 55 paid or on trial" },
  { label: "Needs attention", value: "4", sub: "1 failed debit, 1 chain check, 2 jobs" },
];

export const attention = [
  { title: "Debit failed: Botha Steel", sub: "DebiCheck collection on 1 Nov was returned. Retry is scheduled.", href: "/billing", state: "Billing", tone: "bad" as Tone },
  { title: "Chain not checked in 7 days", sub: "Pillay Construction · Soweto Clinic Extension", href: "/evidence", state: "Evidence", tone: "warn" as Tone },
  { title: "Audit pack job retried 3 times", sub: "AP-0008 · Pillay Construction", href: "/jobs", state: "Jobs", tone: "warn" as Tone },
  { title: "Support request waiting 2 days", sub: "Moloi Plumbing · invite SMS not arriving", href: "/support", state: "Support", tone: "warn" as Tone },
];

export const signups = [
  { org: "Sky Scaffolding", who: "Zanele Mthembu", when: "Sat 14 Nov", state: "No site yet", tone: "neutral" as Tone },
  { org: "Moloi Plumbing", who: "Tebogo Moloi", when: "Mon 9 Nov", state: "Trial", tone: "warn" as Tone },
  { org: "Botha Steel", who: "Pieter Botha", when: "Fri 2 Oct", state: "Paying", tone: "ok" as Tone },
];

export const services = [
  { name: "API", detail: "construction_db · /healthz", state: "Up", tone: "ok" as Tone },
  { name: "Database", detail: "PostgreSQL 16 · migrations at 000019", state: "Up", tone: "ok" as Tone },
  { name: "Evidence storage", detail: "S3 af-south-1 · Object Lock", state: "Up", tone: "ok" as Tone },
  { name: "Worker", detail: "Reminders, audit packs", state: "2 failing jobs", tone: "warn" as Tone },
  { name: "SMS provider", detail: "Invites and reminders", state: "Degraded", tone: "warn" as Tone },
];

export const users = [
  { name: "Ravi Pillay", org: "Pillay Construction", role: "Owner", login: "Email", devices: 2, last: "Today 08:30", state: "Active", tone: "ok" as Tone },
  { name: "Thandi Mokoena", org: "Pillay Construction", role: "SHE officer", login: "Email", devices: 1, last: "Today 11:30", state: "Active", tone: "ok" as Tone },
  { name: "Sipho Ndlovu", org: "Pillay Construction", role: "Supervisor", login: "Mobile number", devices: 2, last: "Today 09:12", state: "Active", tone: "ok" as Tone },
  { name: "Ayanda Khumalo", org: "Khumalo Electrical", role: "Owner", login: "Mobile number", devices: 1, last: "Today 09:12", state: "Active", tone: "ok" as Tone },
  { name: "Pieter Botha", org: "Botha Steel", role: "Owner", login: "Email", devices: 1, last: "Fri 13 Nov", state: "Active", tone: "ok" as Tone },
  { name: "Tebogo Moloi", org: "Moloi Plumbing", role: "Owner", login: "Mobile number", devices: 1, last: "Sun 15 Nov", state: "Active", tone: "ok" as Tone },
  { name: "Kagiso Molefe", org: "Khumalo Electrical", role: "Supervisor", login: "Mobile number", devices: 0, last: "Never", state: "Invite pending", tone: "warn" as Tone },
  { name: "Lerato Dube", org: "Pillay Construction", role: "Supervisor", login: "Mobile number", devices: 1, last: "Sun 15 Nov", state: "Session revoked (token reuse)", tone: "bad" as Tone },
];

export const subscriptions = orgs.map((o) => ({
  org: o.name,
  plan: o.plan,
  seats: o.seats,
  amount: o.plan === "Trial" ? "—" : "[AMOUNT]",
  next: o.plan === "Trial" ? "Trial ends 22 Nov" : "1 Dec 2026",
  payment: o.payment,
  tone: o.paymentTone,
}));

export const debits = [
  { when: "Sun 1 Nov 06:00", org: "Botha Steel", amount: "[AMOUNT]", result: "Returned: insufficient funds", tone: "bad" as Tone, next: "Retry Wed 18 Nov" },
  { when: "Sun 1 Nov 06:00", org: "Pillay Construction", amount: "[AMOUNT]", result: "Collected", tone: "ok" as Tone, next: "—" },
  { when: "Sun 1 Nov 06:00", org: "Mahlangu Civils", amount: "[AMOUNT]", result: "Collected", tone: "ok" as Tone, next: "—" },
  { when: "Sun 1 Nov 06:00", org: "Khumalo Electrical", amount: "[AMOUNT]", result: "Collected", tone: "ok" as Tone, next: "—" },
];

export const regimes = [
  { code: "za_construction_2014", name: "Construction Regulations 2014", version: "1", sites: 11, state: "Published", tone: "ok" as Tone },
  { code: "za_mining", name: "Mine Health and Safety (Mining edition)", version: "—", sites: 0, state: "Not built", tone: "neutral" as Tone },
];

export const templates = [
  { title: "Excavation inspection", key: "excavation", v: 1, items: 9, cadence: "Per shift", ref: "reg 13", used: 412 },
  { title: "Mobile plant pre-use", key: "mobile_plant", v: 1, items: 9, cadence: "Per shift", ref: "reg 23", used: 388 },
  { title: "Temporary works inspection", key: "temporary_works", v: 1, items: 11, cadence: "Per activity", ref: "reg 12", used: 64 },
  { title: "Temporary electrical weekly", key: "temporary_electrical", v: 1, items: 3, cadence: "Weekly", ref: "reg 24", used: 51 },
  { title: "Housekeeping walk", key: "housekeeping", v: 1, items: 10, cadence: "Daily", ref: "reg 27", used: 297 },
];

export const deadlineRules = [
  { label: "Immediate notice to the provincial inspector", ref: "GAR 8(1)(b)", period: "Forthwith", review: "Practitioner review pending", tone: "warn" as Tone },
  { label: "Written notice (WCL1/WCL2)", ref: "GAR 8(1)(a)", period: "7 days", review: "Practitioner review pending", tone: "warn" as Tone },
  { label: "Incident investigation", ref: "GAR 9(2)", period: "7 days", review: "Practitioner review pending", tone: "warn" as Tone },
  { label: "Employer report (W.Cl.2)", ref: "COIDA s39(1)", period: "7 days from employer aware", review: "Practitioner review pending", tone: "warn" as Tone },
];

export const chains = [
  { org: "Pillay Construction", site: "Rosebank Block B", entries: "1,284", checked: "Today 10:42", result: "No break found", tone: "ok" as Tone, pending: 2 },
  { org: "Pillay Construction", site: "Midrand Logistics Park", entries: "612", checked: "Today 06:00", result: "No break found", tone: "ok" as Tone, pending: 1 },
  { org: "Pillay Construction", site: "Soweto Clinic Extension", entries: "377", checked: "Mon 9 Nov", result: "Not checked in 7 days", tone: "warn" as Tone, pending: 0 },
  { org: "Mahlangu Civils", site: "N14 culvert repairs", entries: "958", checked: "Today 06:00", result: "No break found", tone: "ok" as Tone, pending: 0 },
  { org: "Khumalo Electrical", site: "Fourways substation", entries: "140", checked: "Today 06:00", result: "No break found", tone: "ok" as Tone, pending: 4 },
];

export const rejections = [
  { when: "Mon 16 Nov 09:30", org: "Pillay Construction", site: "Rosebank Block B", reason: "Hash mismatch", device: "tablet SN-0388" },
  { when: "Sat 14 Nov 15:40", org: "Khumalo Electrical", site: "Fourways substation", reason: "Too large (over 2048px)", device: "phone KE-0007" },
];

export const queues = [
  { name: "Reminders", waiting: 3, running: 1, failed: 0, oldest: "under a minute", tone: "ok" as Tone },
  { name: "Audit packs", waiting: 1, running: 1, failed: 1, oldest: "24 minutes", tone: "warn" as Tone },
  { name: "Action escalation", waiting: 0, running: 0, failed: 0, oldest: "—", tone: "ok" as Tone },
  { name: "SMS and email delivery", waiting: 12, running: 2, failed: 1, oldest: "6 minutes", tone: "warn" as Tone },
];

export const failedJobs = [
  { id: "job-8841", queue: "Audit packs", what: "Build AP-0008 · Pillay Construction", tries: "3 of 5", error: "Timed out reading 412 photos", when: "Today 11:44" },
  { id: "job-8790", queue: "SMS and email delivery", what: "Invite SMS · Moloi Plumbing", tries: "5 of 5", error: "Provider rejected the number format", when: "Sat 14 Nov 16:02" },
];

export const tickets = [
  { id: "SUP-031", org: "Moloi Plumbing", from: "Tebogo Moloi", subject: "Invite SMS not arriving for two supervisors", opened: "Sat 14 Nov", state: "Waiting on us", tone: "warn" as Tone },
  { id: "SUP-030", org: "Botha Steel", from: "Pieter Botha", subject: "Debit bounced, want to change the account", opened: "Mon 2 Nov", state: "Waiting on customer", tone: "neutral" as Tone },
  { id: "SUP-029", org: "Pillay Construction", from: "Thandi Mokoena", subject: "Photo rejected: hash mismatch on a tablet", opened: "Mon 16 Nov", state: "New", tone: "bad" as Tone },
  { id: "SUP-028", org: "Mahlangu Civils", from: "Thabo Mahlangu", subject: "How do we correct a completed inspection?", opened: "Thu 12 Nov", state: "Closed", tone: "ok" as Tone },
];

export const auditLog = [
  { when: "Mon 16 Nov 11:52", who: "[YOUR NAME]", what: "Viewed organisation", target: "Pillay Construction", from: "Admin portal" },
  { when: "Mon 16 Nov 11:47", who: "[YOUR NAME]", what: "Retried job", target: "job-8841 (audit pack AP-0008)", from: "Admin portal" },
  { when: "Sat 14 Nov 16:20", who: "[TEAMMATE]", what: "Extended trial by 7 days", target: "Moloi Plumbing", from: "Admin portal" },
  { when: "Fri 13 Nov 10:05", who: "[TEAMMATE]", what: "Published regime data", target: "za_construction_2014 v1", from: "Deploy" },
  { when: "Mon 2 Nov 14:31", who: "[YOUR NAME]", what: "Replied to support request", target: "SUP-030", from: "Admin portal" },
];

export const staff = [
  { name: "[YOUR NAME]", role: "Admin", twoFactor: "On", last: "Today 11:52" },
  { name: "[TEAMMATE]", role: "Admin", twoFactor: "On", last: "Sat 14 Nov" },
];

export const flags = [
  { key: "mining_edition", label: "Mining edition", detail: "Not built in this release", on: false },
  { key: "sms_invites", label: "Send invites by SMS", detail: "Otherwise the inviter copies the link", on: true },
  { key: "jbcc_delay_claims", label: "JBCC delay claims", detail: "Shown to contracts managers", on: false },
  { key: "principal_contractor", label: "Principal-contractor tier", detail: "Subcontractor links", on: false },
];
