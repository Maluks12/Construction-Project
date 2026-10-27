"use client";

import { useActionState } from "react";

import { login } from "@/app/actions/auth";
import { FormError, SubmitButton } from "@/components/client";
import { Field, Input } from "@/components/ui";

export function LoginForm({ next }: { next: string }) {
  const [state, action] = useActionState(login, null);

  return (
    <form action={action} className="space-y-4">
      <input type="hidden" name="next" value={next} />
      <Field label="Email or mobile">
        <Input
          name="login"
          required
          autoComplete="username"
          placeholder="you@company.co.za or 082 123 4567"
          defaultValue={state?.values?.login}
        />
      </Field>
      <Field label="Password">
        <Input name="password" type="password" required autoComplete="current-password" />
      </Field>
      <FormError state={state} />
      <SubmitButton className="w-full" pendingText="Signing in…">
        Sign in
      </SubmitButton>
    </form>
  );
}
