"use client";

import { useActionState } from "react";

import { acceptInvite } from "@/app/actions/auth";
import { FormError, SubmitButton } from "@/components/client";
import { Field, Input } from "@/components/ui";

export function AcceptForm({ token, hasPassword }: { token: string; hasPassword: boolean }) {
  const [state, action] = useActionState(acceptInvite, null);

  return (
    <form action={action} className="space-y-4">
      <input type="hidden" name="token" value={token} />
      {hasPassword ? (
        <Field
          label="Your SiteProof password"
          hint="You already have an account, so this company is added to it."
        >
          <Input name="password" type="password" required autoComplete="current-password" />
        </Field>
      ) : (
        <>
          <Field label="Choose a password" hint="At least 10 characters.">
            <Input name="password" type="password" required minLength={10} maxLength={72} autoComplete="new-password" />
          </Field>
          <Field label="Confirm password">
            <Input name="confirm" type="password" required minLength={10} maxLength={72} autoComplete="new-password" />
          </Field>
        </>
      )}
      <FormError state={state} />
      <SubmitButton className="w-full" pendingText="Joining…">
        Accept and continue
      </SubmitButton>
    </form>
  );
}
