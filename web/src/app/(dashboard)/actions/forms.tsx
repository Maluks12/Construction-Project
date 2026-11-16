"use client";

import { useActionState, useState } from "react";

import { changeAction, raiseAction, waiveFinding } from "@/app/actions/corrective";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input, Select, Textarea } from "@/components/ui";
import type { ActionRequestKind, Severity } from "@/lib/types";

export type OwnerOption = { id: string; name: string };

function Owners({ owners, value }: { owners: OwnerOption[]; value?: string }) {
  return (
    <Select name="owner_id" required defaultValue={value}>
      {owners.map((o) => (
        <option key={o.id} value={o.id}>{o.name}</option>
      ))}
    </Select>
  );
}

export function RaiseForm({
  id,
  siteId,
  source,
  owners,
  defaultTitle,
  defaultDays,
}: {
  id: string;
  siteId: string;
  // A finding takes its own severity; an incident's action needs one chosen.
  source:
    | { inspection_id: string; item_key: string }
    | { incident_id: string; version: number };
  owners: OwnerOption[];
  defaultTitle?: string;
  defaultDays?: number;
}) {
  const [state, action] = useActionState(raiseAction, null);
  if (state?.ok) return <Alert tone="ok">Action raised. It&apos;s in the Open tab now.</Alert>;
  const v = state?.values ?? {};
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      {Object.entries(source).map(([k, val]) => (
        <input key={k} type="hidden" name={k} value={String(val)} />
      ))}
      {"incident_id" in source && (
        <Field label="Severity">
          <Select name="severity" required defaultValue={v.severity ?? "medium"}>
            {(["low", "medium", "high", "critical"] as Severity[]).map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </Select>
        </Field>
      )}
      <Field label="What needs doing">
        <Input name="title" required maxLength={200} defaultValue={v.title ?? defaultTitle} />
      </Field>
      <Field label="Details (optional)">
        <Textarea name="description" maxLength={2000} rows={2} defaultValue={v.description} />
      </Field>
      <Field label="Owner" hint="Accountable for it: an owner, SHE officer or a supervisor on this site.">
        <Owners owners={owners} value={v.owner_id} />
      </Field>
      <Field label="Who does the work (optional)" hint="Free text, e.g. a subcontractor and contact.">
        <Input name="responsible_party" maxLength={200} defaultValue={v.responsible_party} />
      </Field>
      <Field
        label="Due (optional)"
        hint={
          defaultDays === undefined
            ? "Leave empty for the severity's default."
            : `Leave empty for the default: ${defaultDays === 0 ? "today" : `${defaultDays} day${defaultDays === 1 ? "" : "s"} after today`}. A later date needs an owner or SHE officer and a reason.`
        }
      >
        <Input name="due_on" type="date" defaultValue={v.due_on} />
      </Field>
      <Field label="Reason for a later date">
        <Input name="due_reason" maxLength={500} defaultValue={v.due_reason} />
      </Field>
      <FormError state={state} />
      <SubmitButton pendingText="Raising…">Raise action</SubmitButton>
    </form>
  );
}

export function WaiveForm({ id, siteId, inspectionId, itemKey }: { id: string; siteId: string; inspectionId: string; itemKey: string }) {
  const [state, action] = useActionState(waiveFinding, null);
  if (state?.ok) return <Alert tone="ok">Finding waived.</Alert>;
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="inspection_id" value={inspectionId} />
      <input type="hidden" name="item_key" value={itemKey} />
      <Field label="Why no action is needed">
        <Textarea name="reason" required maxLength={500} rows={2} defaultValue={state?.values?.reason} />
      </Field>
      <FormError state={state} />
      <SubmitButton variant="secondary" pendingText="Saving…">No action needed</SubmitButton>
    </form>
  );
}

const KIND_LABELS: Record<ActionRequestKind, string> = {
  verify: "Verify the close-out (closes the action)",
  reject: "Reject the close-out (opens it again)",
  reassign: "Reassign",
  move_due: "Move the due date",
  cancel: "Cancel the action",
  reopen: "Reopen",
};

export function ChangeForm({
  id,
  siteId,
  actionId,
  kinds,
  closeoutId,
  owners,
  ownerId,
  responsibleParty,
  dueOn,
}: {
  id: string;
  siteId: string;
  actionId: string;
  // What this action's state allows; the API checks who may.
  kinds: ActionRequestKind[];
  closeoutId: string | null;
  owners: OwnerOption[];
  ownerId: string;
  responsibleParty: string | null;
  dueOn: string;
}) {
  const [state, action] = useActionState(changeAction, null);
  const [kind, setKind] = useState<ActionRequestKind>(kinds[0]);
  if (state?.ok) return <Alert tone="ok">Saved.</Alert>;
  const reasonRequired = kind === "reject" || kind === "reopen" || kind === "cancel";

  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="action_id" value={actionId} />
      {closeoutId && <input type="hidden" name="closeout_id" value={closeoutId} />}
      <Field label="Change">
        <Select name="kind" value={kind} onChange={(e) => setKind(e.currentTarget.value as ActionRequestKind)}>
          {kinds.map((k) => (
            <option key={k} value={k}>{KIND_LABELS[k]}</option>
          ))}
        </Select>
      </Field>
      {kind === "reassign" && (
        <>
          <Field label="Owner"><Owners owners={owners} value={ownerId} /></Field>
          <Field label="Who does the work (optional)">
            <Input name="responsible_party" maxLength={200} defaultValue={responsibleParty ?? ""} />
          </Field>
        </>
      )}
      {kind === "move_due" && (
        <Field label="New due date" hint="Later needs an owner or SHE officer and a reason.">
          <Input name="due_on" type="date" required defaultValue={dueOn} />
        </Field>
      )}
      {kind !== "verify" && kind !== "reassign" && (
        <Field label={reasonRequired ? "Reason" : "Reason (needed to move it later)"}>
          <Textarea name="reason" required={reasonRequired} maxLength={500} rows={2} defaultValue={state?.values?.reason} />
        </Field>
      )}
      <FormError state={state} />
      <SubmitButton variant={kind === "cancel" || kind === "reject" ? "danger" : "primary"} pendingText="Saving…">
        Save
      </SubmitButton>
    </form>
  );
}
