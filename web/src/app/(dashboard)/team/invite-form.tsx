"use client";

import { useActionState } from "react";

import { sendInvite } from "@/app/actions/invites";
import { CopyField, FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input, Select } from "@/components/ui";
import { ROLE_LABELS } from "@/lib/format";

const ROLES = ["supervisor", "she_officer", "contracts_manager", "claims_handler", "bookkeeper", "owner"] as const;

export function InviteForm() {
  const [state, action] = useActionState(sendInvite, null);
  const v = state?.ok ? {} : (state?.values ?? {});

  return (
    <div className="space-y-5">
      {state?.ok && state.data && (
        <div className="space-y-2">
          <Alert tone="ok">Invite created for {state.data.name}.</Alert>
          <p className="text-xs text-muted">
            Sending by SMS and email isn&apos;t built yet. Share this link with them; it
            works once.
          </p>
          <CopyField label="Invite link" value={state.data.link} />
        </div>
      )}
      <form action={action} className="space-y-4">
        <Field label="Full name">
          <Input name="full_name" required maxLength={200} defaultValue={v.full_name} />
        </Field>
        <Field label="Role">
          <Select name="role" defaultValue={v.role ?? "supervisor"}>
            {ROLES.map((r) => (
              <option key={r} value={r}>
                {ROLE_LABELS[r]}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Mobile" hint="Supervisors usually sign in with their phone number.">
          <Input name="mobile" type="tel" placeholder="082 123 4567" defaultValue={v.mobile} />
        </Field>
        <Field label="Email" hint="Mobile, email, or both.">
          <Input name="email" type="email" defaultValue={v.email} />
        </Field>
        <FormError state={state} />
        <SubmitButton pendingText="Creating invite…">Create invite</SubmitButton>
      </form>
    </div>
  );
}
