"use client";

import { useActionState, useState } from "react";
import { useFormStatus } from "react-dom";

import type { FormState } from "@/app/actions/state";

import { Alert, buttonClass, cx } from "./ui";

type Action = (state: FormState, form: FormData) => Promise<FormState>;

export function SubmitButton({
  children,
  pendingText,
  variant = "primary",
  size = "md",
  className,
}: {
  children: React.ReactNode;
  pendingText?: string;
  variant?: "primary" | "secondary" | "danger" | "ghost";
  size?: "md" | "sm";
  className?: string;
}) {
  const { pending } = useFormStatus();
  return (
    <button
      type="submit"
      disabled={pending}
      className={cx(buttonClass(variant, size), className)}
    >
      {pending ? (pendingText ?? "Saving…") : children}
    </button>
  );
}

// A single-button form for destructive or one-off actions. The first click
// asks for confirmation in place; the second one submits.
export function InlineAction({
  action,
  fields,
  label,
  confirmLabel,
  variant = "secondary",
}: {
  action: Action;
  fields: Record<string, string>;
  label: string;
  confirmLabel?: string;
  variant?: "secondary" | "danger" | "ghost";
}) {
  const [state, formAction] = useActionState(action, null);
  const [armed, setArmed] = useState(false);

  return (
    <form action={formAction} className="inline-flex flex-col items-end gap-1">
      {Object.entries(fields).map(([name, value]) => (
        <input key={name} type="hidden" name={name} value={value} />
      ))}
      {confirmLabel && !armed ? (
        <button
          type="button"
          className={buttonClass(variant, "sm")}
          onClick={() => setArmed(true)}
        >
          {label}
        </button>
      ) : (
        <span className="inline-flex gap-1">
          {confirmLabel && (
            <button
              type="button"
              className={buttonClass("ghost", "sm")}
              onClick={() => setArmed(false)}
            >
              Cancel
            </button>
          )}
          <SubmitButton variant={confirmLabel ? "danger" : variant} size="sm" pendingText="Working…">
            {confirmLabel ?? label}
          </SubmitButton>
        </span>
      )}
      {state?.error && <span className="max-w-64 text-right text-xs text-danger">{state.error}</span>}
    </form>
  );
}

export function FormError({ state }: { state: FormState }) {
  return state?.error ? <Alert>{state.error}</Alert> : null;
}

export function CopyField({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex gap-2">
      <input
        readOnly
        aria-label={label}
        value={value}
        onFocus={(e) => e.currentTarget.select()}
        className="h-9 min-w-0 flex-1 rounded-md border border-line bg-surface-2 px-3 font-mono text-xs"
      />
      <button
        type="button"
        className={buttonClass("secondary", "sm")}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          } catch {
            // Clipboard blocked (e.g. plain http on another device); the
            // field is selectable so the link can still be copied by hand.
          }
        }}
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}
