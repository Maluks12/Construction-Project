import type { Role } from "./types";

export const ROLE_LABELS: Record<Role, string> = {
  owner: "Owner",
  she_officer: "SHE officer",
  supervisor: "Supervisor",
  claims_handler: "Claims handler",
  contracts_manager: "Contracts manager",
  bookkeeper: "Bookkeeper",
};

const dateFmt = new Intl.DateTimeFormat("en-ZA", {
  day: "numeric",
  month: "short",
  year: "numeric",
  timeZone: "Africa/Johannesburg",
});

const dateTimeFmt = new Intl.DateTimeFormat("en-ZA", {
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  timeZone: "Africa/Johannesburg",
});

export const formatDate = (iso: string) => dateFmt.format(new Date(iso));
export const formatDateTime = (iso: string) => dateTimeFmt.format(new Date(iso));

// SA numbers are often typed as 082 123 4567; the API wants +27821234567.
export function toE164(input: string) {
  const digits = input.replace(/[\s()-]/g, "");
  if (/^0\d{9}$/.test(digits)) return "+27" + digits.slice(1);
  if (/^27\d{9}$/.test(digits)) return "+" + digits;
  return digits;
}
