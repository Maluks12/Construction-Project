// The worker register (docs/api/workers.md): the organisation's workers,
// their competencies and medical certificates of fitness. Shared by every
// site, so this tab doesn't depend on the site being viewed.

import { randomUUID } from "node:crypto";

import Link from "next/link";

import { viewWorkerRecord } from "@/app/actions/workers";
import { OpenFileButton } from "@/components/open-file";
import { Alert, Badge, Card, EmptyState, Facts, Input, Select, buttonClass, cx, type Tone } from "@/components/ui";
import { getWorker, listWorkers } from "@/lib/data";
import { formatDateOnly, formatWhen, label } from "@/lib/format";
import type { Capability, RecordState, RegimeTypes } from "@/lib/types";

import { CompetencyForm, MedicalForm, RetractRecordForm, WorkerForm } from "./forms";

export const STATE_TONE: Record<RecordState, Tone> = {
  valid: "ok",
  expiring: "warn",
  expired: "bad",
  superseded: "neutral",
  retracted: "neutral",
};

export async function WorkersTab({
  q,
  active,
  workerId,
  types,
  caps,
  who,
}: {
  q?: string;
  active?: string;
  workerId?: string;
  types: RegimeTypes | null;
  caps: Set<Capability>;
  who: (id: string) => string;
}) {
  const [workers, worker] = await Promise.all([
    listWorkers({ q, active: active === "all" ? undefined : (active ?? "true") }),
    workerId ? getWorker(workerId) : null,
  ]);
  const list = workers === "forbidden" ? [] : workers;

  return (
    <div className="flex flex-wrap items-start gap-5">
      <Card flush className="min-w-0 flex-[999_1_520px]" title={<span className="text-lg">Worker register</span>} description="The organisation's, shared by every site. One person, one entry: name, firm and clock number together.">
        <form className="flex flex-wrap items-end gap-2 border-b border-line bg-surface-2 px-[18px] py-3">
          <input type="hidden" name="tab" value="workers" />
          <label className="block min-w-48 flex-1">
            <span className="mb-1 block text-xs font-semibold text-muted">Search</span>
            <Input name="q" defaultValue={q} placeholder="Name, firm or clock number" />
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-semibold text-muted">Show</span>
            <Select name="active" defaultValue={active ?? "true"} className="w-auto">
              <option value="true">Active</option>
              <option value="false">Not active</option>
              <option value="all">Everyone</option>
            </Select>
          </label>
          <button type="submit" className={buttonClass("secondary")}>Apply</button>
        </form>
        {list.length === 0 ? (
          <div className="p-4"><EmptyState title="Nobody here">Workers are added here or on the phone when they first sign in.</EmptyState></div>
        ) : (
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Worker</th><th>Firm</th><th>Clock number</th><th>Trade</th></tr></thead>
              <tbody>
                {list.map((w) => (
                  <tr key={w.id} className={cx(!w.active && "opacity-60", w.id === workerId && "bg-accent-soft")}>
                    <td><Link href={`/people?worker=${w.id}`} className="link">{w.name}</Link>{!w.active && <div className="text-xs text-muted">Not active</div>}</td>
                    <td>{w.firm ?? "—"}</td>
                    <td>{w.clock_number ?? "—"}</td>
                    <td>{w.trade ?? "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <div className="flex min-w-0 flex-[1_1_420px] flex-col gap-4">
        {worker ? (
          <WorkerDetailView worker={worker} types={types} caps={caps} who={who} />
        ) : workerId ? (
          <Alert tone="info">That worker isn&apos;t on the register.</Alert>
        ) : caps.has("capture_records") ? (
          <Card title="Add a worker" description="Most are added on the phone the first time they sign in.">
            <WorkerForm workerId={randomUUID()} />
          </Card>
        ) : null}
      </div>
    </div>
  );
}

async function WorkerDetailView({
  worker,
  types,
  caps,
  who,
}: {
  worker: NonNullable<Awaited<ReturnType<typeof getWorker>>>;
  types: RegimeTypes | null;
  caps: Set<Capability>;
  who: (id: string) => string;
}) {
  const typeLabel = new Map((types?.competency_types ?? []).map((t) => [t.key, t.label]));
  const manage = caps.has("manage_workers");
  const capture = caps.has("capture_records");
  const live = (s: RecordState) => s !== "superseded" && s !== "retracted";
  const liveMedical = worker.medicals.filter((m) => live(m.state)).at(-1);

  return (
    <>
      <Card>
        <h2 className="font-head text-lg font-semibold">{worker.name}</h2>
        <div className="mt-2">
          <Facts
            min={130}
            items={[
              ["Firm", worker.firm ?? "—"],
              ["Clock number", worker.clock_number ?? "—"],
              ["Trade", worker.trade ?? "—"],
              ["On the register", worker.active ? "Active" : "Not active"],
              ["Added", `${formatWhen(worker.created_at)} by ${who(worker.created_by)}`],
            ]}
          />
        </div>
        {manage && (
          <details className="mt-3 border-t border-line-soft pt-3">
            <summary className="link cursor-pointer text-sm">Edit details</summary>
            <div className="mt-3"><WorkerForm workerId={worker.id} worker={worker} /></div>
          </details>
        )}
      </Card>

      <Card title="Competencies" description="Certificates, licences, training and registrations. A renewal supersedes the old record; nothing is overwritten.">
        {worker.competencies.length === 0 && <p className="text-sm text-muted">None recorded.</p>}
        {[...worker.competencies].reverse().map((c) => (
          <div key={c.id} className={cx("mt-2 rounded-lg border border-line-soft p-3 text-[13px] first:mt-0", !live(c.state) && "opacity-70")}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className={cx("font-semibold", c.state === "retracted" && "line-through")}>{typeLabel.get(c.type_key) ?? label(c.type_key)}</span>
              <Badge tone={STATE_TONE[c.state]}>{label(c.state)}</Badge>
            </div>
            <div className="mt-1 text-muted">
              {[c.number, c.issuer].filter(Boolean).join(" · ") || "No number or issuer"} · issued {formatDateOnly(c.issued_on)}
              {c.expires_on ? ` · expires ${formatDateOnly(c.expires_on)}` : " · no expiry"}
            </div>
            {c.retraction && <div className="mt-1">Retracted by {who(c.retraction.retracted_by)}: “{c.retraction.reason}”</div>}
            <div className="mt-2 flex flex-wrap gap-2">
              <OpenFileButton open={viewWorkerRecord.bind(null, worker.id, "competencies", c.id)} label="Open certificate" />
            </div>
            {live(c.state) && capture && types && (
              <details className="mt-2">
                <summary className="link cursor-pointer text-xs">Renew</summary>
                <div className="mt-2">
                  <CompetencyForm id={randomUUID()} workerId={worker.id} regimeCode={types.regime_code} types={types.competency_types} renew={{ recordId: c.id, typeKey: c.type_key }} />
                </div>
              </details>
            )}
            {c.state !== "retracted" && manage && (
              <details className="mt-1">
                <summary className="link cursor-pointer text-xs">Recorded by mistake?</summary>
                <RetractRecordForm workerId={worker.id} kind="competencies" recordId={c.id} />
              </details>
            )}
          </div>
        ))}
        {capture && types && types.competency_types.length > 0 && (
          <details className="mt-3 border-t border-line-soft pt-3">
            <summary className="link cursor-pointer text-sm">Record a competency</summary>
            <div className="mt-3">
              <CompetencyForm id={randomUUID()} workerId={worker.id} regimeCode={types.regime_code} types={types.competency_types} />
            </div>
          </details>
        )}
      </Card>

      <Card
        title="Medical certificates of fitness"
        description="Outcome and dates only. The certificate opens for owners and SHE officers, and every view is logged."
      >
        {worker.medicals.length === 0 && <p className="text-sm text-muted">None recorded.</p>}
        {[...worker.medicals].reverse().map((m) => (
          <div key={m.id} className={cx("mt-2 rounded-lg border border-line-soft p-3 text-[13px] first:mt-0", !live(m.state) && "opacity-70")}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className={cx("font-semibold", m.state === "retracted" && "line-through")}>
                {label(m.outcome)}
              </span>
              <Badge tone={m.outcome === "unfit" && live(m.state) ? "bad" : STATE_TONE[m.state]}>{label(m.state)}</Badge>
            </div>
            <div className="mt-1 text-muted">
              Examined {formatDateOnly(m.examined_on)} · expires {formatDateOnly(m.expires_on)}
              {m.practice_number && ` · practice ${m.practice_number}`}
            </div>
            {m.retraction && <div className="mt-1">Retracted by {who(m.retraction.retracted_by)}: “{m.retraction.reason}”</div>}
            {caps.has("open_medical_certificates") && (
              <div className="mt-2">
                <OpenFileButton open={viewWorkerRecord.bind(null, worker.id, "medicals", m.id)} label="Open certificate" />
              </div>
            )}
            {m.state !== "retracted" && manage && (
              <details className="mt-1">
                <summary className="link cursor-pointer text-xs">Recorded by mistake?</summary>
                <RetractRecordForm workerId={worker.id} kind="medicals" recordId={m.id} />
              </details>
            )}
          </div>
        ))}
        {capture && types && (
          <details className="mt-3 border-t border-line-soft pt-3">
            <summary className="link cursor-pointer text-sm">{liveMedical ? "Record a new certificate (renewal)" : "Record a certificate"}</summary>
            <div className="mt-3">
              <MedicalForm
                id={randomUUID()}
                workerId={worker.id}
                regimeCode={types.regime_code}
                validityMonths={types.medical.default_validity_months}
                supersedesId={liveMedical?.id}
              />
            </div>
          </details>
        )}
      </Card>
    </>
  );
}
