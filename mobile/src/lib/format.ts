// Dates show in South African time whatever the phone is set to: the
// records are about a site in South Africa, and the dashboard shows SAST too.

const TZ = "Africa/Johannesburg";
const clean = (s: string) => s.replace(/,/g, "");

const whenFmt = new Intl.DateTimeFormat("en-ZA", { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12: false, timeZone: TZ });
const dayFmt = new Intl.DateTimeFormat("en-ZA", { weekday: "short", day: "numeric", month: "short", timeZone: TZ });
const timeFmt = new Intl.DateTimeFormat("en-ZA", { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: TZ });
const isoDayFmt = new Intl.DateTimeFormat("en-CA", { timeZone: TZ });

export const formatWhen = (iso: string) => clean(whenFmt.format(new Date(iso)));
export const formatDay = (iso: string) => clean(dayFmt.format(new Date(iso)));
export const formatTime = (iso: string) => timeFmt.format(new Date(iso));
export const formatDateOnly = (ymd: string) => formatDay(`${ymd}T12:00:00Z`);
export const sastDay = (iso: string) => isoDayFmt.format(new Date(iso));
export const isToday = (iso: string) => sastDay(iso) === sastDay(new Date().toISOString());

export const shortId = (id: string) => id.slice(0, 8).toUpperCase();

export function label(s: string) {
  const t = s.replace(/_/g, " ");
  return t.charAt(0).toUpperCase() + t.slice(1);
}

// The device's clock, as the API wants it (client_started_at and friends).
export const now = () => new Date().toISOString();

// "2026-11-16 08:55" typed on the phone, read as SAST (+02:00, no daylight
// saving). Returns "" when it isn't a date and time.
export function sastInputToISO(text: string) {
  const m = text.trim().match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2})$/);
  if (!m) return "";
  const d = new Date(`${m[1]}T${m[2]}:00+02:00`);
  return Number.isNaN(d.getTime()) ? "" : d.toISOString();
}

export function isoToSASTInput(iso: string) {
  return new Date(new Date(iso).getTime() + 2 * 3_600_000).toISOString().slice(0, 16).replace("T", " ");
}
