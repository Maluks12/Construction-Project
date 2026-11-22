// The site diary on the dashboard (docs/api/diary.md): read only. The diary
// is written on the phone; here a lead sees the current revision, the
// addenda, conflicts between phones and the headcount against the
// sign-ins.

import Link from "next/link";

import { PhotoButton } from "@/components/photo";
import { Alert, Badge, Card, Facts, Row } from "@/components/ui";
import { getDiary, listRevisions } from "@/lib/data";
import { formatDateOnly, formatTime, formatWhen, label, shortId } from "@/lib/format";
import type { Revision, Sections, Weather } from "@/lib/types";

const yesNo = (b: boolean | null) => (b === null ? "Not said" : b ? "Yes" : "No");
const timeOrDash = (iso: string | null) => (iso ? formatTime(iso) : "—");

export function weatherText(w: Weather) {
  const conditions = w.conditions.map((c) => label(c)).join(", ");
  return w.note ? `${conditions} (${w.note})` : conditions;
}

function Table({ head, rows }: { head: string[]; rows: React.ReactNode[][] }) {
  return (
    <div className="mt-1.5 overflow-x-auto rounded-lg border border-line-soft">
      <table className="tbl">
        <thead><tr>{head.map((h) => <th key={h}>{h}</th>)}</tr></thead>
        <tbody>{rows.map((r, i) => <tr key={i}>{r.map((c, j) => <td key={j}>{c}</td>)}</tr>)}</tbody>
      </table>
    </div>
  );
}

// Every optional section, as saved. Rows keep the ids the phone gave them.
export function SectionsView({ s, quiet }: { s: Sections; quiet?: boolean }) {
  const blocks: React.ReactNode[] = [];
  const h = (t: string) => <h4 className="mt-4 text-[13px] font-semibold">{t}</h4>;
  if (s.weather_times) {
    const w = s.weather_times;
    blocks.push(
      <div key="wt">
        {h("Weather times")}
        <div className="mt-1.5">
          <Facts
            min={130}
            items={[
              ["Rain started", timeOrDash(w.rain_started_at)],
              ["Rain stopped", timeOrDash(w.rain_stopped_at)],
              ["Work stopped", timeOrDash(w.work_stopped_at)],
              ["Work restarted", timeOrDash(w.work_restarted_at)],
              ["Rain gauge", w.rain_gauge_mm === null ? "—" : `${w.rain_gauge_mm} mm`],
              ["Hours lost", w.hours_lost === null ? "—" : String(w.hours_lost)],
              ["Critical path", yesNo(w.critical_path)],
              ["Delayed effect", yesNo(w.delayed_effect)],
            ]}
          />
          {w.activities_affected && <p className="mt-2 text-sm">Affected: {w.activities_affected}</p>}
        </div>
      </div>,
    );
  }
  if (s.labour?.length) {
    blocks.push(
      <div key="lab">
        {h("Labour by firm")}
        <Table
          head={["Firm", "Trade", "Headcount", "Named workers"]}
          rows={s.labour.map((r) => [
            <span key="f">{r.firm}{r.own_team && <Badge>Own team</Badge>}</span>,
            r.trade ?? "—",
            r.headcount,
            r.worker_ids?.length ?? 0,
          ])}
        />
      </div>,
    );
  }
  if (s.plant?.length) {
    blocks.push(
      <div key="plant">
        {h("Plant and equipment")}
        <Table
          head={["Plant", "Ref", "State", "Hours working", "Hours standing"]}
          rows={s.plant.map((r) => [
            r.description,
            r.ref ?? "—",
            <Badge key="s" tone={r.state === "broken_down" ? "bad" : r.state === "idle" ? "neutral" : "ok"}>{label(r.state)}</Badge>,
            r.hours_working ?? "—",
            r.hours_standing ?? "—",
          ])}
        />
      </div>,
    );
  }
  if (s.instructions?.length) {
    blocks.push(
      <div key="ins">
        {h("Instructions received")}
        <p className="text-xs text-muted">A record of what was said or written. It isn&apos;t a contract instruction by itself.</p>
        <Table
          head={["From", "When", "How", "What", "Reference"]}
          rows={s.instructions.map((r) => [
            `${r.from_name}${r.from_role ? ` (${r.from_role})` : ""}`,
            formatTime(r.received_at),
            <Badge key="f" tone={r.form === "verbal" ? "warn" : "neutral"}>{label(r.form)}</Badge>,
            r.description,
            r.reference ?? "—",
          ])}
        />
      </div>,
    );
  }
  if (s.delays?.length) {
    blocks.push(
      <div key="del">
        {h("Delays")}
        <Table
          head={["What", "From", "To", "Hours lost", "Cause"]}
          rows={s.delays.map((r) => [r.description, timeOrDash(r.started_at), timeOrDash(r.ended_at), r.hours_lost ?? "—", r.cause ?? "—"])}
        />
      </div>,
    );
  }
  if (s.visitors?.length) {
    blocks.push(
      <div key="vis">
        {h("Visitors")}
        <Table
          head={["Name", "Organisation", "Purpose", "Arrived", "Left"]}
          rows={s.visitors.map((r) => [r.name, r.organisation ?? "—", r.purpose ?? "—", timeOrDash(r.arrived_at), timeOrDash(r.left_at)])}
        />
      </div>,
    );
  }
  if (s.deliveries?.length) {
    blocks.push(
      <div key="dlv">
        {h("Deliveries")}
        <Table
          head={["What", "Supplier", "Quantity", "Status", "Received"]}
          rows={s.deliveries.map((r) => [
            r.description,
            r.supplier ?? "—",
            r.quantity ?? "—",
            <Badge key="s" tone={r.status === "received" ? "ok" : "warn"}>{label(r.status)}</Badge>,
            timeOrDash(r.received_at),
          ])}
        />
      </div>,
    );
  }
  if (blocks.length) return <>{blocks}</>;
  return quiet ? null : <p className="mt-2 text-sm text-muted">No optional sections filled in.</p>;
}

function RevisionCard({ r, who, siteId, title }: { r: Revision; who: (id: string) => string; siteId: string; title: string }) {
  return (
    <div className="rounded-lg border border-line-soft p-3">
      <div className="flex flex-wrap items-center gap-2">
        <strong className="text-sm font-semibold">{title}</strong>
        {r.saved_after_close && <Badge tone="warn">Late diary</Badge>}
        {r.received_after_close && <Badge tone="warn">Saved on time, arrived late</Badge>}
      </div>
      <p className="mt-1 text-xs text-muted">
        {who(r.saved_by)} · phone {formatWhen(r.client_saved_at)} · server {formatWhen(r.server_received_at)}
      </p>
      <div className="mt-2.5">
        <Facts
          min={130}
          items={[
            ["Weather", weatherText(r.weather)],
            ["Headcount", String(r.headcount)],
            ["Sign-ins said", String(r.headcount_suggested)],
          ]}
        />
      </div>
      <p className="mt-2.5 text-sm whitespace-pre-line">{r.work_done}</p>
      {r.late_reason && <p className="mt-2 text-[13px]"><strong className="font-semibold">Why late:</strong> {r.late_reason}</p>}
      <SectionsView s={r.sections} />
      {r.evidence_ids.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-2.5">
          {r.evidence_ids.map((id) => <PhotoButton key={id} siteId={siteId} evidenceId={id} label={`Diary photo ${shortId(id)}`} />)}
        </div>
      )}
    </div>
  );
}

export async function DiaryDetail({ siteId, date, who }: { siteId: string; date: string; who: (id: string) => string }) {
  const diary = await getDiary(siteId, date);
  if (!diary) {
    return <Alert tone="info">No diary for {formatDateOnly(date)}. The supervisor writes it on the phone.</Alert>;
  }
  const conflict = diary.conflict_revision_ids.length > 0;
  const revisions = conflict ? ((await listRevisions(siteId, date)) ?? []) : [];
  const others = revisions.filter((r) => diary.conflict_revision_ids.includes(r.id) && r.id !== diary.current.id);
  const gap = diary.current.headcount !== diary.headcount_suggested_now;

  return (
    <>
      <Card
        title={`Site diary · ${formatDateOnly(diary.diary_date)}`}
        actions={
          <div className="flex flex-wrap gap-1.5">
            <Badge tone={diary.closed ? "neutral" : "ok"}>{diary.closed ? "Closed" : `Open until ${formatTime(diary.closes_at)}`}</Badge>
            {diary.late && <Badge tone="warn">Late diary</Badge>}
            {conflict && <Badge tone="bad">Two phones disagree</Badge>}
          </div>
        }
      >
        {gap && (
          <div className="mb-3">
            <Alert tone="warn">
              Headcount entered: {diary.current.headcount}. Sign-ins and toolbox talks now count{" "}
              {diary.headcount_suggested_now} for this day.
            </Alert>
          </div>
        )}
        <RevisionCard r={diary.current} who={who} siteId={siteId} title={`Current (revision ${diary.revision_count})`} />
        {(diary.toolbox_talk_ids.length > 0 || diary.incident_ids.length > 0) && (
          <div className="mt-3 text-sm">
            <span className="font-semibold">Linked:</span>{" "}
            {diary.toolbox_talk_ids.map((id) => (
              <Link key={id} href={`/toolbox?talk=${id}`} className="link mr-2">Talk {shortId(id)}</Link>
            ))}
            {diary.incident_ids.map((id) => (
              <Link key={id} href={`/incidents/${id}`} className="link mr-2">INC-{shortId(id)}</Link>
            ))}
          </div>
        )}
      </Card>

      {conflict && (
        <Card
          title="The other side of the conflict"
          description="Saved by a phone that hadn't seen the current revision. Both are kept. It clears when the supervisor saves a revision based on the current one."
        >
          <div className="flex flex-col gap-3">
            {others.map((r) => <RevisionCard key={r.id} r={r} who={who} siteId={siteId} title={`Revision ${r.seq}`} />)}
            {others.length === 0 && <p className="text-sm text-muted">The conflicting revision couldn&apos;t be loaded.</p>}
          </div>
        </Card>
      )}

      <Card title="Addenda" description="Added after the diary closed. The original never changes.">
        {diary.addenda.length === 0 && <p className="text-sm text-muted">None.</p>}
        {diary.addenda.map((a) => (
          <Row key={a.id}>
            <div className="text-sm">
              <strong className="font-semibold">{a.reason}</strong>
              <div className="text-xs text-muted">
                {who(a.added_by)} · phone {formatWhen(a.client_added_at)} · server {formatWhen(a.server_received_at)}
              </div>
              {a.note && <p className="mt-1 whitespace-pre-line">{a.note}</p>}
              <SectionsView s={a.sections} quiet />
              {a.evidence_ids.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-2.5">
                  {a.evidence_ids.map((id) => <PhotoButton key={id} siteId={siteId} evidenceId={id} label={`Addendum photo ${shortId(id)}`} />)}
                </div>
              )}
            </div>
          </Row>
        ))}
      </Card>
    </>
  );
}
