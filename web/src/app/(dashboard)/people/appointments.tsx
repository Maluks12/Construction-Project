// A site's written appointments (docs/api/appointments.md): construction
// manager, supervisors, health and safety officer and the competent persons
// of the regulations, each with the signed letter.

import { randomUUID } from "node:crypto";

import Link from "next/link";

import { viewAppointmentLetter } from "@/app/actions/appointments";
import { OpenFileButton } from "@/components/open-file";
import { Alert, Badge, Card, cx } from "@/components/ui";
import { listAppointments, listWorkers } from "@/lib/data";
import { formatDateOnly, formatWhen, label } from "@/lib/format";
import type { Appointment, Capability, RegimeTypes } from "@/lib/types";

import { AppointmentForm, RetractAppointmentForm } from "./forms";
import { STATE_TONE } from "./workers";

const isLive = (a: Appointment) => a.state !== "superseded" && a.state !== "retracted";

export async function AppointmentsTab({
  siteId,
  types,
  caps,
  who,
}: {
  siteId: string;
  types: RegimeTypes | null;
  caps: Set<Capability>;
  who: (id: string) => string;
}) {
  // Owners and SHE officers record and retract appointments, the same people
  // who write the H&S file; write_safety_file is that display hint.
  const canWrite = caps.has("write_safety_file");
  const [appointments, workers] = await Promise.all([
    listAppointments(siteId),
    canWrite ? listWorkers({ active: "true" }) : [],
  ]);
  if (appointments === "forbidden") return null;
  if (appointments === null || !types) {
    return <Alert tone="info">This site&apos;s regulations aren&apos;t the Construction Regulations, so it has no appointments register here.</Alert>;
  }

  const live = appointments.filter(isLive);
  const options = (workers === "forbidden" ? [] : workers).map((w) => ({
    id: w.id,
    label: [w.name, w.firm, w.clock_number].filter(Boolean).join(" · "),
  }));

  return (
    <div className="flex flex-wrap items-start gap-5">
      <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
        {types.appointment_types.map((t) => {
          const mine = appointments.filter((a) => a.type_key === t.key);
          const current = mine.filter(isLive);
          const history = mine.filter((a) => !isLive(a));
          const missing = t.required && !current.some((a) => a.state === "valid" || a.state === "expiring");
          if (mine.length === 0 && !t.required) return null;
          return (
            <Card
              key={t.key}
              title={t.label}
              description={t.source_ref}
              actions={missing ? <Badge tone="warn">No appointee</Badge> : current.length ? <Badge tone="ok">Appointed</Badge> : undefined}
            >
              {missing && <p className="mb-2 text-[13px] text-warn">{t.required_note}</p>}
              {current.length === 0 && <p className="text-sm text-muted">Nobody appointed.</p>}
              {[...current, ...history].map((a) => (
                <div key={a.id} className={cx("mt-2 rounded-lg border border-line-soft p-3 text-[13px] first:mt-0", !isLive(a) && "opacity-60")}>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <Link href={`/people?worker=${a.worker_id}`} className={cx("link", a.state === "retracted" && "line-through")}>{a.worker_name}</Link>
                    <Badge tone={STATE_TONE[a.state]}>{label(a.state)}</Badge>
                  </div>
                  <div className="mt-1 text-muted">
                    Appointed {formatDateOnly(a.appointed_on)}
                    {a.expires_on ? ` · expires ${formatDateOnly(a.expires_on)}` : ""} · recorded by {who(a.recorded_by)}, {formatWhen(a.recorded_at)}
                  </div>
                  {a.retraction && <div className="mt-1">Retracted: “{a.retraction.reason}”</div>}
                  <div className="mt-2">
                    <OpenFileButton open={viewAppointmentLetter.bind(null, siteId, a.id)} label="Open letter" />
                  </div>
                  {canWrite && a.state !== "retracted" && (
                    <details className="mt-1">
                      <summary className="link cursor-pointer text-xs">Recorded by mistake?</summary>
                      <RetractAppointmentForm siteId={siteId} appointmentId={a.id} />
                    </details>
                  )}
                </div>
              ))}
            </Card>
          );
        })}
      </div>

      {canWrite ? (
        <Card className="min-w-0 flex-[1_1_380px]" title="Record an appointment" description="With the signed letter. A new appointee for a type replaces the current one.">
          <AppointmentForm
            id={randomUUID()}
            siteId={siteId}
            types={types.appointment_types}
            workers={options}
            live={live.map((a) => ({ id: a.id, typeKey: a.type_key, label: `${a.worker_name}, appointed ${formatDateOnly(a.appointed_on)}` }))}
          />
        </Card>
      ) : (
        <div className="flex-[1_1_380px]"><Alert tone="info">Owners and SHE officers record appointments.</Alert></div>
      )}
    </div>
  );
}
