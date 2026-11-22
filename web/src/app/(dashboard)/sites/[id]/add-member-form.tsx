"use client";

import { useActionState } from "react";

import { addSiteMember } from "@/app/actions/sites";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Select } from "@/components/ui";

export type PersonOption = { user_id: string; label: string };

// Adds someone to this site. The list holds people the dashboard can name
// today: those on the organisation's other sites and anyone invited but not
// signed up yet. A full member list arrives with ONBOARD (GET /v1/members).
export function AddMemberForm({ siteId, people }: { siteId: string; people: PersonOption[] }) {
  const [state, action] = useActionState(addSiteMember, null);
  if (people.length === 0) {
    return (
      <p className="text-sm text-muted">
        Nobody else to add yet. Invite a supervisor under Team &amp; billing; they show here straight away, before they
        accept.
      </p>
    );
  }
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="site_id" value={siteId} />
      <Field label="Person">
        <Select name="user_id" required defaultValue="">
          <option value="" disabled>Choose…</option>
          {people.map((p) => <option key={p.user_id} value={p.user_id}>{p.label}</option>)}
        </Select>
      </Field>
      {state?.ok && <Alert tone="ok">Added to this site.</Alert>}
      <FormError state={state} />
      <SubmitButton variant="secondary" pendingText="Adding…">Add to site</SubmitButton>
    </form>
  );
}
