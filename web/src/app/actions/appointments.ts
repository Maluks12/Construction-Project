"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import { uploadFile } from "@/lib/upload";
import type { Appointment, AppointmentRequest, RetractRequest, ViewURL } from "@/lib/types";

import type { FormState } from "./state";

// A site's written appointments (docs/api/appointments.md, Construction
// edition). Owners and SHE officers record and retract.

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const orNull = (s: string) => (s === "" ? null : s);
const appointmentsPath = (siteId: string, rest = "") =>
  `/v1/construction/sites/${encodeURIComponent(siteId)}/appointments${rest}`;

// Uploads the signed letter, then records the appointment. A new appointee
// for the same type supersedes the current one (supersedes_id).
export async function recordAppointment(_: FormState, form: FormData): Promise<FormState> {
  const siteId = field(form, "site_id");
  const id = field(form, "id");
  const values = Object.fromEntries(["worker_id", "type_key", "appointed_on", "expires_on"].map((k) => [k, field(form, k)]));
  try {
    const fileId = await uploadFile(form.get("file"), { recordId: id, purpose: "appointment", siteId });
    const body: AppointmentRequest = {
      worker_id: values.worker_id,
      type_key: values.type_key,
      appointed_on: values.appointed_on,
      expires_on: orNull(values.expires_on),
      file_id: fileId,
      supersedes_id: orNull(field(form, "supersedes_id")),
      client_recorded_at: new Date().toISOString(),
    };
    await api<Appointment>(appointmentsPath(siteId, `/${encodeURIComponent(id)}`), { method: "PUT", body });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
  revalidatePath("/people");
  return { ok: true };
}

export async function retractAppointment(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: RetractRequest = { reason };
  try {
    await api<Appointment>(
      appointmentsPath(field(form, "site_id"), `/${encodeURIComponent(field(form, "appointment_id"))}/retract`),
      { method: "POST", body },
    );
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values: { reason } };
    throw e;
  }
  revalidatePath("/people");
  return { ok: true };
}

// A 60-second link to the appointment letter; the API logs the view first.
export async function viewAppointmentLetter(siteId: string, appointmentId: string): Promise<{ url?: string; error?: string }> {
  try {
    const v = await api<ViewURL>(appointmentsPath(siteId, `/${encodeURIComponent(appointmentId)}/view`), { method: "POST" });
    return { url: v.url };
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
}
