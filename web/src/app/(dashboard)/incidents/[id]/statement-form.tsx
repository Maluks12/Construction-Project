"use client";

import { useActionState } from "react";

import { recordStatement } from "@/app/actions/incidents";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input, Textarea } from "@/components/ui";

export function StatementForm({ id, siteId, incidentId, now }: { id: string; siteId: string; incidentId: string; now: string }) {
  const [state, action] = useActionState(recordStatement, null);
  if (state?.ok) return <Alert tone="ok">Statement saved.</Alert>;
  const v = state?.values ?? {};
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="incident_id" value={incidentId} />
      <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(180px,1fr))]">
        <Field label="Witness name"><Input name="witness_name" required maxLength={200} defaultValue={v.witness_name} /></Field>
        <Field label="Contact (optional)"><Input name="witness_contact" maxLength={200} defaultValue={v.witness_contact} /></Field>
        <Field label="When taken (SAST)"><Input name="taken_at" type="datetime-local" required defaultValue={v.taken_at ?? now} /></Field>
      </div>
      <Field label="What they saw, in their words" hint="A statement can't be edited once saved. To correct one, take a further statement on the phone; both stay on the incident.">
        <Textarea name="statement" required maxLength={20000} rows={4} defaultValue={v.statement} />
      </Field>
      <FormError state={state} />
      <SubmitButton variant="secondary" pendingText="Saving…">Save statement</SubmitButton>
    </form>
  );
}
