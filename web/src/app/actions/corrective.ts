"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import type { Action, ActionRequestKind, EventRequest, RaiseRequest, Waiver, WaiverRequest } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const orNull = (s: string) => (s === "" ? null : s);
const site = (id: string, rest: string) => `/v1/sites/${encodeURIComponent(id)}${rest}`;

// Every write is a PUT under an id made up when the form was shown (a
// hidden field), so pressing the button twice can't raise two actions.
async function put<T>(path: string, body: unknown, values: Record<string, string>): Promise<FormState> {
  try {
    await api<T>(path, { method: "PUT", body });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
  revalidatePath("/actions");
  revalidatePath("/overview");
  return { ok: true };
}

// Raise an action from a finding. An incident's action is raised from the
// incident page with `incident_id` and `version` instead.
export async function raiseAction(_: FormState, form: FormData): Promise<FormState> {
  const values = Object.fromEntries(
    ["title", "description", "owner_id", "responsible_party", "due_on", "due_reason", "severity"].map((k) => [k, field(form, k)]),
  );
  const incidentId = field(form, "incident_id");
  const body: RaiseRequest = {
    ...(incidentId
      ? {
          incident: { incident_id: incidentId, version: Number(field(form, "version")) },
          severity: (values.severity || null) as RaiseRequest["severity"],
        }
      : { finding: { inspection_id: field(form, "inspection_id"), item_key: field(form, "item_key") } }),
    title: values.title,
    description: orNull(values.description),
    owner_id: values.owner_id,
    responsible_party: orNull(values.responsible_party),
    // Left empty, the API uses the default date for the severity.
    due_on: orNull(values.due_on),
    due_reason: orNull(values.due_reason),
    client_raised_at: new Date().toISOString(),
  };
  return put<Action>(site(field(form, "site_id"), `/actions/${encodeURIComponent(field(form, "id"))}`), body, values);
}

const KINDS: ActionRequestKind[] = ["reassign", "move_due", "verify", "reject", "reopen", "cancel"];

// One form covers every change to an action. Each kind takes only its own
// fields (the API refuses extras), so the body is built per kind.
export async function changeAction(_: FormState, form: FormData): Promise<FormState> {
  const kind = field(form, "kind") as ActionRequestKind;
  const values = Object.fromEntries(
    ["kind", "owner_id", "responsible_party", "due_on", "reason"].map((k) => [k, field(form, k)]),
  );
  if (!KINDS.includes(kind)) return { error: "Choose what to change.", values };

  const body: EventRequest = { kind, client_at: new Date().toISOString() };
  if (kind === "reassign") {
    body.owner_id = values.owner_id;
    body.responsible_party = orNull(values.responsible_party);
  } else if (kind === "move_due") {
    body.due_on = values.due_on;
    if (values.reason) body.reason = values.reason;
  } else if (kind === "verify") {
    body.closeout_id = field(form, "closeout_id");
  } else if (kind === "reject") {
    body.closeout_id = field(form, "closeout_id");
    body.reason = values.reason;
  } else {
    body.reason = values.reason;
  }

  const path = site(
    field(form, "site_id"),
    `/actions/${encodeURIComponent(field(form, "action_id"))}/events/${encodeURIComponent(field(form, "id"))}`,
  );
  return put<Action>(path, body, values);
}

// "No action needed" on a low or medium finding. Owners and SHE officers only.
export async function waiveFinding(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: WaiverRequest = {
    inspection_id: field(form, "inspection_id"),
    item_key: field(form, "item_key"),
    reason,
    client_waived_at: new Date().toISOString(),
  };
  return put<Waiver>(site(field(form, "site_id"), `/waivers/${encodeURIComponent(field(form, "id"))}`), body, { reason });
}
