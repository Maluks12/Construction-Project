"use client";

import { useActionState } from "react";

import { abandonInspection } from "@/app/actions/inspections";
import { FormError, SubmitButton } from "@/components/client";
import { Field, Textarea } from "@/components/ui";

export function AbandonForm({ siteId, inspectionId }: { siteId: string; inspectionId: string }) {
  const [state, action] = useActionState(abandonInspection, null);
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="inspection_id" value={inspectionId} />
      <Field label="Why is it being abandoned?" hint="Kept with the inspection. Its answers stay, and it raises no findings.">
        <Textarea name="reason" required maxLength={500} rows={2} defaultValue={state?.values?.reason} />
      </Field>
      <FormError state={state} />
      <SubmitButton variant="danger" pendingText="Abandoning…">Abandon inspection</SubmitButton>
    </form>
  );
}
