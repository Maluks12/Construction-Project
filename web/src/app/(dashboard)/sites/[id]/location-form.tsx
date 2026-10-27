"use client";

import { useActionState } from "react";

import { createLocation } from "@/app/actions/sites";
import { FormError, SubmitButton } from "@/components/client";
import { Field, Input, Select } from "@/components/ui";
import type { Location } from "@/lib/types";

export function LocationForm({ siteId, locations }: { siteId: string; locations: Location[] }) {
  const [state, action] = useActionState(createLocation, null);
  const v = state?.ok ? {} : (state?.values ?? {});

  return (
    <form action={action} className="space-y-4">
      <input type="hidden" name="site_id" value={siteId} />
      <h3 className="text-sm font-semibold">Add a location</h3>
      <div className="grid gap-4 sm:grid-cols-[140px_1fr]">
        <Field label="Type">
          <Select name="kind" defaultValue={v.kind ?? "zone"}>
            <option value="zone">Zone</option>
            <option value="floor">Floor</option>
          </Select>
        </Field>
        <Field label="Name">
          <Input name="name" required maxLength={100} placeholder="e.g. Basement 2, Tower crane area" defaultValue={v.name} />
        </Field>
      </div>
      <Field label="Inside" hint="Optional. Put a floor inside a zone, for example.">
        <Select name="parent_id" defaultValue={v.parent_id ?? ""}>
          <option value="">Top level of the site</option>
          {locations
            .slice()
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((l) => (
              <option key={l.id} value={l.id}>
                {l.name} ({l.kind})
              </option>
            ))}
        </Select>
      </Field>
      <FormError state={state} />
      <SubmitButton variant="secondary" pendingText="Adding…">
        Add location
      </SubmitButton>
    </form>
  );
}
