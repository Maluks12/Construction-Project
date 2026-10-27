// What every form action hands back to useActionState.
export type FormState = {
  error?: string;
  ok?: boolean;
  // Echo typed values back so a failed submit doesn't empty the form.
  values?: Record<string, string>;
  // Action-specific extras, e.g. the invite link after sending an invite.
  data?: Record<string, string>;
} | null;
