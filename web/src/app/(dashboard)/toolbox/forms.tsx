"use client";

import { useActionState } from "react";

import { linkSignature, retractSignature, unlinkSignature } from "@/app/actions/toolbox";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input, Select } from "@/components/ui";

type Ids = { siteId: string; talkId: string; attendanceId: string };

function Hidden({ siteId, talkId, attendanceId }: Ids) {
  return (
    <>
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="talk_id" value={talkId} />
      <input type="hidden" name="attendance_id" value={attendanceId} />
    </>
  );
}

// Owners and SHE officers: says which register worker signed.
export function LinkForm({ workers, ...ids }: Ids & { workers: { id: string; label: string }[] }) {
  const [state, action] = useActionState(linkSignature, null);
  if (state?.ok) return <Alert tone="ok">Linked.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <Hidden {...ids} />
      <div className="min-w-56 flex-1">
        <Field label="Register worker">
          <Select name="worker_id" required defaultValue="">
            <option value="" disabled>Choose…</option>
            {workers.map((w) => <option key={w.id} value={w.id}>{w.label}</option>)}
          </Select>
        </Field>
      </div>
      <SubmitButton variant="secondary" size="sm" pendingText="Linking…">Link</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}

// "Different person": the signature stays; only its link to the worker goes.
export function UnlinkForm(ids: Ids) {
  const [state, action] = useActionState(unlinkSignature, null);
  if (state?.ok) return <Alert tone="ok">Unlinked.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <Hidden {...ids} />
      <div className="min-w-56 flex-1">
        <Field label="Why it's someone else">
          <Input name="reason" required maxLength={500} defaultValue={state?.values?.reason ?? "Different person"} />
        </Field>
      </div>
      <SubmitButton variant="secondary" size="sm" pendingText="Unlinking…">Unlink</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}

// A signature given by mistake: it stays on the talk, struck through.
export function RetractSignatureForm(ids: Ids) {
  const [state, action] = useActionState(retractSignature, null);
  if (state?.ok) return <Alert tone="ok">Signature withdrawn.</Alert>;
  return (
    <form action={action} className="mt-2 flex flex-wrap items-end gap-2">
      <Hidden {...ids} />
      <div className="min-w-56 flex-1">
        <Field label="Why it was a mistake">
          <Input name="reason" required maxLength={500} defaultValue={state?.values?.reason} />
        </Field>
      </div>
      <SubmitButton variant="danger" size="sm" pendingText="Withdrawing…">Withdraw signature</SubmitButton>
      <div className="basis-full"><FormError state={state} /></div>
    </form>
  );
}
