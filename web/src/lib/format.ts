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

// ---- Safety records ----

const TZ = "Africa/Johannesburg";

const whenFmt = new Intl.DateTimeFormat("en-ZA", {
  weekday: "short",
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
  timeZone: TZ,
});
const dayFmt = new Intl.DateTimeFormat("en-ZA", {
  weekday: "short",
  day: "numeric",
  month: "short",
  timeZone: TZ,
});
const timeFmt = new Intl.DateTimeFormat("en-ZA", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
  timeZone: TZ,
});
const isoDayFmt = new Intl.DateTimeFormat("en-CA", { timeZone: TZ });

const clean = (s: string) => s.replace(/,/g, "");

// "Mon 16 Nov 08:55", in SAST.
export const formatWhen = (iso: string) => clean(whenFmt.format(new Date(iso)));
// "Mon 16 Nov", in SAST.
export const formatDay = (iso: string) => clean(dayFmt.format(new Date(iso)));
export const formatTime = (iso: string) => timeFmt.format(new Date(iso));

// A date-only field (due_on, "2026-11-23") as "Mon 23 Nov". Parsed at noon
// UTC so the day can't slip in any time zone.
export const formatDateOnly = (ymd: string) => formatDay(`${ymd}T12:00:00Z`);

// Today's date in SAST as YYYY-MM-DD, for date inputs.
export const todaySAST = () => isoDayFmt.format(new Date());

// A <input type="datetime-local"> value is a wall-clock time with no zone.
// People type site times, so read it as SAST. South Africa has no daylight
// saving, so the offset is always +02:00.
export function sastToISO(local: string) {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(local)) return "";
  return new Date(`${local.slice(0, 16)}:00+02:00`).toISOString();
}

// The reverse, to pre-fill a datetime-local input.
export function isoToSASTInput(iso: string) {
  const d = new Date(new Date(iso).getTime() + 2 * 60 * 60 * 1000);
  return d.toISOString().slice(0, 16);
}

// Whole days from now until an instant (negative when past).
export function daysUntil(iso: string) {
  return Math.ceil((new Date(iso).getTime() - Date.now()) / 86_400_000);
}

export const shortId = (id: string) => id.slice(0, 8).toUpperCase();

const sentence = (s: string) => {
  const t = s.replace(/_/g, " ");
  return t.charAt(0).toUpperCase() + t.slice(1);
};

// "per_shift" -> "Per shift", "awaiting_verification" -> "Awaiting verification".
export const label = sentence;
