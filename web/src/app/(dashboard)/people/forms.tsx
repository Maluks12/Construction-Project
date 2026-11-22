"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { recordAppointment, retractAppointment } from "@/app/actions/appointments";
import { addToRoster, removeFromRoster, retractSignIn } from "@/app/actions/attendance";
import { recordCompetency, recordMedical, retractWorkerRecord, saveWorker } from "@/app/actions/workers";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input, Select } from "@/components/ui";
import type { AppointmentType, CompetencyType, Worker } from "@/lib/types";

const ACCEPT = "application/pdf,image/jpeg,image/png";
const FILE_HINT = "PDF, JPEG or PNG, up to 20 MB. After an error, choose it again.";

export type Option = { id: string; label: string };

// ---- Workers ----

export function WorkerForm({ workerId, worker }: { workerId: string; worker?: Worker }) {
  const [state, action] = useActionState(saveWorker, null);
  if (state?.ok && !worker) {
    return (
      <Alert tone="ok">
        Added. <Link href={`/people?worker=${state.data?.worker_id ?? workerId}`} className="link">Open their record</Link> to add competencies.
      </Alert>
    );
  }
  const v = state?.values ?? {};
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="worker_id" value={workerId} />
      <Field label="Name" hint="As the person writes it. No ID numbers are kept.">
        <Input name="name" required maxLength={200} defaultValue={v.name ?? worker?.name} />
      </Field>
      <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(150px,1fr))]">
        <Field label="Firm (optional)"><Input name="firm" maxLength={200} defaultValue={v.firm ?? worker?.firm ?? ""} /></Field>
        <Field label="Clock number (optional)"><Input name="clock_number" maxLength={50} defaultValue={v.clock_number ?? worker?.clock_number ?? ""} /></Field>
        <Field label="Trade (optional)"><Input name="trade" maxLength={100} defaultValue={v.trade ?? worker?.trade ?? ""} /></Field>
      </div>
      {worker && (
        <Field label="On the register">
          <Select name="active" defaultValue={v.active ?? String(worker.active)}>
            <option value="true">Active</option>
            <option value="false">Not active (left, or not working for you now)</option>
          </Select>
        </Field>
      )}
      {state?.ok && <Alert tone="ok">Saved.</Alert>}
      <FormError state={state} />
      <SubmitButton pendingText="Saving…">{worker ? "Save changes" : "Add worker"}</SubmitButton>
    </form>
  );
}

export function CompetencyForm({
  id,
  workerId,
  regimeCode,
  types,
  renew,
}: {
  id: string;
  workerId: string;
  regimeCode: string;
  types: CompetencyType[];
  // A renewal: same type, superseding this record.
  renew?: { recordId: string; typeKey: string };
}) {
  const [state, action] = useActionState(recordCompetency, null);
  const v = state?.values ?? {};
  const [typeKey, setTypeKey] = useState(renew?.typeKey ?? v.type_key ?? types[0]?.key ?? "");
  if (state?.ok) return <Alert tone="ok">{renew ? "Renewal recorded." : "Recorded."}</Alert>;
  const type = types.find((t) => t.key === typeKey);
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="worker_id" value={workerId} />
      <input type="hidden" name="regime_code" value={regimeCode} />
      {renew && <input type="hidden" name="supersedes_id" value={renew.recordId} />}
      {renew ? (
        <input type="hidden" name="type_key" value={renew.typeKey} />
      ) : (
        <Field label="Type" hint={type?.source_ref}>
          <Select name="type_key" value={typeKey} onChange={(e) => setTypeKey(e.currentTarget.value)}>
            {types.map((t) => <option key={t.key} value={t.key}>{t.label}</option>)}
          </Select>
        </Field>
      )}
      <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(150px,1fr))]">
        <Field label="Number (optional)"><Input name="number" maxLength={100} defaultValue={v.number} /></Field>
        <Field label="Issuer (optional)"><Input name="issuer" maxLength={200} defaultValue={v.issuer} /></Field>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Issued on"><Input name="issued_on" type="date" required defaultValue={v.issued_on} /></Field>
        {type?.expiry !== "none" && (
          <Field label={type?.expiry === "required" ? "Expires on" : "Expires on (optional)"}>
            <Input name="expires_on" type="date" required={type?.expiry === "required"} defaultValue={v.expires_on} />
          </Field>
        )}
      </div>
      <Field label="Certificate" hint={FILE_HINT}>
        <Input name="file" type="file" required accept={ACCEPT} className="py-1.5" />
      </Field>
      <FormError state={state} />
      <SubmitButton pendingText="Uploading…">{renew ? "Record renewal" : "Record competency"}</SubmitButton>
    </form>
  );
}

export function MedicalForm({
  id,
  workerId,
  regimeCode,
  validityMonths,
  supersedesId,
}: {
  id: string;
  workerId: string;
  regimeCode: string;
  validityMonths: number;
  supersedesId?: string;
}) {
  const [state, action] = useActionState(recordMedical, null);
  if (state?.ok) return <Alert tone="ok">Recorded.</Alert>;
  const v = state?.values ?? {};
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="worker_id" value={workerId} />
      <input type="hidden" name="regime_code" value={regimeCode} />
      {supersedesId && <input type="hidden" name="supersedes_id" value={supersedesId} />}
      <Field label="Outcome" hint="Only the outcome and dates are kept as data: no findings, diagnoses or restrictions.">
        <Select name="outcome" defaultValue={v.outcome ?? "fit"}>
          <option value="fit">Fit</option>
          <option value="fit_with_restrictions">Fit with restrictions</option>
          <option value="unfit">Unfit</option>
        </Select>
      </Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Examined on"><Input name="examined_on" type="date" required defaultValue={v.examined_on} /></Field>
        <Field label="Expires on" hint={`Usually ${validityMonths} months after; the date on the certificate wins.`}>
          <Input name="expires_on" type="date" required defaultValue={v.expires_on} />
        </Field>
      </div>
      <Field label="Practice number (optional)"><Input name="practice_number" maxLength={30} defaultValue={v.practice_number} /></Field>
      <Field label="Certificate of fitness" hint={`${FILE_HINT} Only owners and SHE officers can open it later.`}>
        <Input name="file" type="file" required accept={ACCEPT} className="py-1.5" />
      </Field>
      <FormError state={state} />
      <SubmitButton pendingText="Uploading…">{supersedesId ? "Record renewal" : "Record medical"}</SubmitButton>
    </form>
  );
}

export function RetractRecordForm({ workerId, kind, recordId }: { workerId: string; kind: "competencies" | "medicals"; recordId: string }) {
  const [state, action] = useActionState(retractWorkerRecord, null);
  if (state?.ok) return <Alert tone="ok">Retracted.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <input type="hidden" name="worker_id" value={workerId} />
      <input type="hidden" name="kind" value={kind} />
      <input type="hidden" name="record_id" value={recordId} />
      <div className="min-w-56 flex-1">
        <Field label="Why it was a mistake"><Input name="reason" required maxLength={500} defaultValue={state?.values?.reason} /></Field>
      </div>
      <SubmitButton variant="danger" size="sm" pendingText="Retracting…">Retract</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}

// ---- Sign-ins and roster ----

export function RosterAddForm({ siteId, workers }: { siteId: string; workers: Option[] }) {
  const [state, action] = useActionState(addToRoster, null);
  return (
    <form action={action} className="flex flex-wrap items-end gap-2">
      <input type="hidden" name="site_id" value={siteId} />
      <div className="min-w-56 flex-1">
        <Field label="Expect on this site">
          <Select name="worker_id" required defaultValue="">
            <option value="" disabled>Choose a worker…</option>
            {workers.map((w) => <option key={w.id} value={w.id}>{w.label}</option>)}
          </Select>
        </Field>
      </div>
      <SubmitButton variant="secondary" size="sm" pendingText="Adding…">Add to roster</SubmitButton>
      <div className="basis-full">
        {state?.ok && <Alert tone="ok">Added.</Alert>}
        <FormError state={state} />
      </div>
    </form>
  );
}

export function RosterRemoveForm({ siteId, workerId }: { siteId: string; workerId: string }) {
  const [state, action] = useActionState(removeFromRoster, null);
  if (state?.ok) return <Alert tone="ok">Removed from the roster.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="worker_id" value={workerId} />
      <div className="min-w-56 flex-1">
        <Field label="Why"><Input name="reason" required maxLength={500} defaultValue={state?.values?.reason ?? "No longer on this site"} /></Field>
      </div>
      <SubmitButton variant="secondary" size="sm" pendingText="Removing…">Remove</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}

export function RetractSignInForm({ siteId, signInId }: { siteId: string; signInId: string }) {
  const [state, action] = useActionState(retractSignIn, null);
  if (state?.ok) return <Alert tone="ok">Retracted. It no longer counts anywhere.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="sign_in_id" value={signInId} />
      <div className="min-w-56 flex-1">
        <Field label="Why"><Input name="reason" required maxLength={500} defaultValue={state?.values?.reason ?? "Different person"} /></Field>
      </div>
      <SubmitButton variant="danger" size="sm" pendingText="Retracting…">Retract sign-in</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}

// ---- Appointments ----

export function AppointmentForm({
  id,
  siteId,
  types,
  workers,
  live,
}: {
  id: string;
  siteId: string;
  types: AppointmentType[];
  workers: Option[];
  // Current appointees, so a new one can replace one of them.
  live: { id: string; typeKey: string; label: string }[];
}) {
  const [state, action] = useActionState(recordAppointment, null);
  const v = state?.values ?? {};
  const [typeKey, setTypeKey] = useState(v.type_key ?? types[0]?.key ?? "");
  if (state?.ok) return <Alert tone="ok">Appointment recorded.</Alert>;
  const type = types.find((t) => t.key === typeKey);
  const current = live.filter((a) => a.typeKey === typeKey);
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      <Field label="Appointment" hint={type?.source_ref}>
        <Select name="type_key" value={typeKey} onChange={(e) => setTypeKey(e.currentTarget.value)}>
          {types.map((t) => <option key={t.key} value={t.key}>{t.label}</option>)}
        </Select>
      </Field>
      <Field label="Who" hint="A worker on the register. Add them under Workers first if they aren't.">
        <Select name="worker_id" required defaultValue={v.worker_id ?? ""}>
          <option value="" disabled>Choose…</option>
          {workers.map((w) => <option key={w.id} value={w.id}>{w.label}</option>)}
        </Select>
      </Field>
      {current.length > 0 && (
        <Field label="Replaces" hint="A new appointee or a renewal replaces the current one. Leave it for an extra appointee.">
          <Select name="supersedes_id" defaultValue="" key={typeKey}>
            <option value="">Nobody: an additional appointee</option>
            {current.map((a) => <option key={a.id} value={a.id}>{a.label}</option>)}
          </Select>
        </Field>
      )}
      <div className="grid grid-cols-2 gap-3">
        <Field label="Appointed on"><Input name="appointed_on" type="date" required defaultValue={v.appointed_on} /></Field>
        {type?.expiry !== "none" && (
          <Field label={type?.expiry === "required" ? "Expires on" : "Expires on (optional)"}>
            <Input name="expires_on" type="date" required={type?.expiry === "required"} defaultValue={v.expires_on} />
          </Field>
        )}
      </div>
      <Field label="Signed appointment letter" hint={FILE_HINT}>
        <Input name="file" type="file" required accept={ACCEPT} className="py-1.5" />
      </Field>
      <FormError state={state} />
      <SubmitButton pendingText="Uploading…">Record appointment</SubmitButton>
    </form>
  );
}

export function RetractAppointmentForm({ siteId, appointmentId }: { siteId: string; appointmentId: string }) {
  const [state, action] = useActionState(retractAppointment, null);
  if (state?.ok) return <Alert tone="ok">Retracted.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="appointment_id" value={appointmentId} />
      <div className="min-w-56 flex-1">
        <Field label="Why it was a mistake"><Input name="reason" required maxLength={500} defaultValue={state?.values?.reason} /></Field>
      </div>
      <SubmitButton variant="danger" size="sm" pendingText="Retracting…">Retract</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}
