"use client";

import { useActionState } from "react";

import { updateSite } from "@/app/actions/sites";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input } from "@/components/ui";
import type { Site } from "@/lib/types";

export function EditSiteForm({ site }: { site: Site }) {
  const [state, action] = useActionState(updateSite, null);
  const v = state?.values;

  return (
    <form action={action} className="space-y-4">
      <input type="hidden" name="site_id" value={site.id} />
      <Field label="Name">
        <Input name="name" required maxLength={200} defaultValue={v?.name ?? site.name} />
      </Field>
      <Field label="Address">
        <Input name="address" maxLength={500} defaultValue={v?.address ?? site.address ?? ""} />
      </Field>
      <FormError state={state} />
      {state?.ok && <Alert tone="ok">Saved.</Alert>}
      <SubmitButton variant="secondary">Save changes</SubmitButton>
    </form>
  );
}
