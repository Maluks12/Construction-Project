"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import type { RemoveRequest, RetractRequest, RosterEntry, SignIn } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const sitePath = (form: FormData, rest: string) => `/v1/sites/${encodeURIComponent(field(form, "site_id"))}${rest}`;

async function send<T>(path: string, body: unknown, values?: Record<string, string>): Promise<FormState> {
  try {
    await api<T>(path, body === undefined ? { method: "PUT" } : { method: "POST", body });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
  revalidatePath("/people");
  return { ok: true };
}

// Expects a register worker on this site (docs/api/attendance.md): they show
// as absent on days they don't sign in. No body; adding again answers 200.
export async function addToRoster(_: FormState, form: FormData): Promise<FormState> {
  const workerId = field(form, "worker_id");
  if (!workerId) return { error: "Choose a worker." };
  return send<RosterEntry>(sitePath(form, `/roster/${encodeURIComponent(workerId)}`), undefined);
}

export async function removeFromRoster(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: RemoveRequest = { reason };
  return send<RosterEntry>(sitePath(form, `/roster/${encodeURIComponent(field(form, "worker_id"))}/remove`), body, { reason });
}

// A sign-in recorded by mistake (the wrong person matched, say). It counts
// nowhere afterwards but stays on the day's register, marked.
export async function retractSignIn(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: RetractRequest = { reason };
  return send<SignIn>(sitePath(form, `/sign-ins/${encodeURIComponent(field(form, "sign_in_id"))}/retract`), body, { reason });
}
