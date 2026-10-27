"use client";

import { useActionState } from "react";

import { createSite } from "@/app/actions/sites";
import { FormError, SubmitButton } from "@/components/client";
import { Field, Input } from "@/components/ui";

export function NewSiteForm() {
  const [state, action] = useActionState(createSite, null);
  const v = state?.values ?? {};

  return (
    <form action={action} className="space-y-4">
      <Field label="Name">
        <Input name="name" required maxLength={200} placeholder="e.g. Rosebank Mall extension" defaultValue={v.name} />
      </Field>
      <Field label="Address" hint="Optional.">
        <Input name="address" maxLength={500} defaultValue={v.address} />
      </Field>
      <FormError state={state} />
      <SubmitButton pendingText="Adding…">Add site</SubmitButton>
    </form>
  );
}
