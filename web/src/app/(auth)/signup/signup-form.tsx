"use client";

import { useActionState } from "react";

import { signup } from "@/app/actions/auth";
import { FormError, SubmitButton } from "@/components/client";
import { Field, Input } from "@/components/ui";

export function SignupForm() {
  const [state, action] = useActionState(signup, null);
  const v = state?.values ?? {};

  return (
    <form action={action} className="space-y-4">
      <Field label="Company name">
        <Input name="company_name" required maxLength={200} defaultValue={v.company_name} />
      </Field>
      <Field label="Your full name">
        <Input name="full_name" required maxLength={200} autoComplete="name" defaultValue={v.full_name} />
      </Field>
      <Field label="Email">
        <Input name="email" type="email" required autoComplete="email" defaultValue={v.email} />
      </Field>
      <Field label="Password" hint="At least 10 characters.">
        <Input name="password" type="password" required minLength={10} maxLength={72} autoComplete="new-password" />
      </Field>
      <Field label="First site" hint="The project you'll set up first. You can add more later.">
        <Input name="site_name" required maxLength={200} placeholder="e.g. Sandton Tower, Phase 2" defaultValue={v.site_name} />
      </Field>
      <FormError state={state} />
      <SubmitButton className="w-full" pendingText="Creating…">
        Create account
      </SubmitButton>
    </form>
  );
}
