// How a legal deadline is described, shared by the incident and deadlines
// pages. Always shown with its source and caveat: deadlines are guidance,
// not legal advice.

import { daysUntil, formatDateOnly, formatWhen, label } from "@/lib/format";
import type { Deadline } from "@/lib/types";

import { Badge, type Tone } from "./ui";

export function deadlineState(d: Deadline): { text: string; tone: Tone } {
  switch (d.state) {
    case "due_now":
      return { text: "Call now", tone: "now" };
    case "overdue":
      return { text: "Overdue", tone: "bad" };
    case "open": {
      const days = daysUntil(d.due_at);
      return { text: days <= 1 ? "Due within a day" : `${days} days left`, tone: "warn" };
    }
    case "satisfied":
      return { text: "Met in time", tone: "ok" };
    case "satisfied_late":
      return { text: "Met after due", tone: "warn" };
    default:
      return { text: label(d.state), tone: "neutral" };
  }
}

export function deadlineDue(d: Deadline) {
  if (d.unit === "immediately") return "Due now: call, then log it";
  return d.due_on ? `By the end of ${formatDateOnly(d.due_on)} (SAST)` : `Before ${formatWhen(d.due_at)}`;
}

export function DeadlineBody({ d }: { d: Deadline }) {
  const state = deadlineState(d);
  return (
    <>
      <div className="flex items-start justify-between gap-2">
        <strong className="text-sm font-semibold">{d.label}</strong>
        <Badge tone={state.tone}>{state.text}</Badge>
      </div>
      <div className="mt-0.5 text-xs text-muted">
        {d.source_ref} · from {formatWhen(d.basis_at)} · computed in v{d.computed_in_version}
      </div>
      <div className="mt-2 text-sm">{deadlineDue(d)}</div>
      {d.legal_latest_on && d.legal_latest_on !== d.due_on && (
        <div className="mt-0.5 text-xs text-muted">
          The law allows at least until {formatDateOnly(d.legal_latest_on)} ({d.legal_latest_source_ref}).
        </div>
      )}
      {d.satisfaction && (
        <div className="mt-1 text-xs text-muted">
          Recorded: {label(d.satisfaction.method)}, {formatWhen(d.satisfaction.satisfied_at)}
          {d.satisfaction.recorded_after_due && `, recorded ${formatWhen(d.satisfaction.recorded_at)}`}
          {d.satisfaction.reference && ` · ${d.satisfaction.reference}`}
        </div>
      )}
      {d.closed_reason && <div className="mt-1 text-xs text-muted">{label(d.state)}: “{d.closed_reason}”</div>}
      {d.caveat && <div className="mt-2 rounded-md bg-bg px-2 py-1.5 text-xs">{d.caveat}</div>}
    </>
  );
}
