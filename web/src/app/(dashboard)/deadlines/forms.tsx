"use client";

import { useActionState } from "react";

import { recordSatisfaction, retractSatisfaction } from "@/app/actions/incidents";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input, Select, Textarea } from "@/components/ui";

const METHODS = [
  ["phone", "Phone"],
  ["email", "Email"],
  ["in_person", "In person"],
  ["online", "Online"],
  ["fax", "Fax"],
  ["post", "Post"],
  ["other", "Other"],
];

// A version of an H&S file document that can prove a site work notice.
export type ProofOption = { value: string; label: string };

export function SatisfyForm({
  id,
  siteId,
  deadlineId,
  now,
  proofs,
}: {
  id: string;
  siteId: string;
  deadlineId: string;
  now: string;
  // Site work deadlines only (REG4): the documents that can be its proof.
  proofs?: ProofOption[];
}) {
  const [state, action] = useActionState(recordSatisfaction, null);
  if (state?.ok) return <Alert tone="ok">Recorded. The alert for this deadline has stopped.</Alert>;
  const v = state?.values ?? {};
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="deadline_id" value={deadlineId} />
      <Field label="How">
        <Select name="method" defaultValue={v.method ?? "phone"}>
          {METHODS.map(([key, text]) => <option key={key} value={key}>{text}</option>)}
        </Select>
      </Field>
      <Field label="When it was done (SAST)" hint="When the call was made or the form sent, not when you're recording it.">
        <Input name="satisfied_at" type="datetime-local" required defaultValue={v.satisfied_at ?? now} />
      </Field>
      <Field label="Reference (optional)" hint="Who you spoke to, a reference number.">
        <Input name="reference" maxLength={200} defaultValue={v.reference} />
      </Field>
      <Field label="Note (optional)">
        <Textarea name="note" maxLength={2000} defaultValue={v.note} />
      </Field>
      {proofs && (
        <Field
          label="Proof from the H&S file"
          hint={
            proofs.length === 0
              ? "No document of the right type is in this site's H&S file yet. Upload the Annexure 2 (or the permit) under People & documents, then come back; or record the notice now and add proof later."
              : "The Annexure 2 for a Reg 4 notice, the permit itself for Reg 3."
          }
        >
          <Select name="proof" defaultValue={v.proof ?? ""} disabled={proofs.length === 0}>
            <option value="">No document</option>
            {proofs.map((p) => <option key={p.value} value={p.value}>{p.label}</option>)}
          </Select>
        </Field>
      )}
      <FormError state={state} />
      <SubmitButton pendingText="Saving…">Save record</SubmitButton>
    </form>
  );
}

export function RetractForm({ siteId, deadlineId, satisfactionId }: { siteId: string; deadlineId: string; satisfactionId: string }) {
  const [state, action] = useActionState(retractSatisfaction, null);
  if (state?.ok) return <Alert tone="ok">Retracted. The duty is open again.</Alert>;
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="deadline_id" value={deadlineId} />
      <input type="hidden" name="satisfaction_id" value={satisfactionId} />
      <Field label="Why it was recorded in error" hint="Both the record and the retraction stay on file.">
        <Textarea name="reason" required maxLength={500} rows={2} defaultValue={state?.values?.reason} />
      </Field>
      <FormError state={state} />
      <SubmitButton variant="danger" pendingText="Retracting…">Retract this record</SubmitButton>
    </form>
  );
}
