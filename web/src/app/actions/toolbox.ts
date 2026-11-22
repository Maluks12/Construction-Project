"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import type { Attendance, LinkRequest, SignatureLink, TalkRetractRequest, UnlinkRequest } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();

function attendancePath(form: FormData, rest = "") {
  return `/v1/sites/${encodeURIComponent(field(form, "site_id"))}/toolbox-talks/${encodeURIComponent(field(form, "talk_id"))}/attendance/${encodeURIComponent(field(form, "attendance_id"))}${rest}`;
}

async function send<T>(path: string, method: "PUT" | "POST", body: unknown, values?: Record<string, string>): Promise<FormState> {
  try {
    await api<T>(path, { method, body });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
  revalidatePath("/toolbox");
  return { ok: true };
}

// Links a signature to a register worker by hand (docs/api/workers.md).
// Owners and SHE officers only. The signature itself never changes.
export async function linkSignature(_: FormState, form: FormData): Promise<FormState> {
  const body: LinkRequest = { worker_id: field(form, "worker_id") };
  if (!body.worker_id) return { error: "Choose the worker this signature belongs to." };
  return send<SignatureLink>(attendancePath(form, "/worker"), "PUT", body);
}

// "Different person": takes the link off, with a reason.
export async function unlinkSignature(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: UnlinkRequest = { reason };
  return send<SignatureLink>(attendancePath(form, "/worker/unlink"), "POST", body, { reason });
}

// Withdraws a signature given by mistake (docs/api/toolbox.md). The person
// who recorded it, an owner or a SHE officer. It stays on the talk, marked.
export async function retractSignature(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: TalkRetractRequest = { reason, client_retracted_at: new Date().toISOString() };
  return send<Attendance>(attendancePath(form, "/retract"), "POST", body, { reason });
}
