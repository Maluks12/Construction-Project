// How the compliance summary (docs/api/compliance.md) is shown: category
// names, colours as badge tones, and which dashboard page opens each item.

import type { Tone } from "@/components/ui";

import type { CategoryKey, ComplianceItem, Colour } from "./types";

export const CATEGORY_LABELS: Record<CategoryKey, string> = {
  hs_file: "H&S file",
  vault: "Statutory documents",
  appointments: "Appointments",
  findings: "Findings with no action",
  actions: "Overdue actions",
  verifications: "Close-outs waiting",
  sign_ins: "Sign-in warnings",
  incident_deadlines: "Incident deadlines",
  site_work_deadlines: "Notices before work starts",
  worker_records: "Worker records expiring",
  absent_workers: "Absent today",
  pending_uploads: "Uploads not finished",
};

export const COLOUR_TONE: Record<Colour, Tone> = { red: "bad", amber: "warn", green: "ok" };

export const colourLabel = (c: Colour) => (c === "red" ? "Red" : c === "amber" ? "Amber" : "Green");

// The page that opens an item's record (the table under "Linking an item to
// its record"). Every page reads the site from the sidebar, so this is only
// the path; null when the dashboard has no page for it.
export function itemHref(category: CategoryKey, item: ComplianceItem): string | null {
  const q = (s: string) => encodeURIComponent(s);
  switch (item.subject_type) {
    case "document":
      if (!item.subject_id) return null;
      return category === "vault" ? `/organisation?doc=${q(item.subject_id)}` : `/people?tab=file&doc=${q(item.subject_id)}`;
    case "document_type":
      return category === "vault" ? "/organisation" : "/people?tab=file";
    case "appointment":
    case "appointment_type":
      return "/people?tab=appointments";
    case "finding":
      return item.inspection_id && item.item_key
        ? `/actions?tab=unactioned&finding=${q(`${item.inspection_id}:${item.item_key}`)}`
        : "/actions?tab=unactioned";
    case "action":
      return item.subject_id ? `/actions?action=${q(item.subject_id)}` : "/actions";
    case "closeout":
      return item.action_id ? `/actions?action=${q(item.action_id)}` : "/actions";
    case "deadline":
      return item.subject_id ? `/deadlines?deadline=${q(item.subject_id)}` : "/deadlines";
    case "sign_in":
    case "competency":
    case "medical":
    case "roster_entry":
      return item.worker_id ? `/people?worker=${q(item.worker_id)}` : "/people?tab=onsite";
    case "evidence":
      return "/evidence?status=pending";
    case "file":
      return null;
  }
}
