"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import { uploadFile } from "@/lib/upload";
import type {
  Competency,
  CompetencyRequest,
  Medical,
  MedicalOutcome,
  MedicalRequest,
  RetractRequest,
  ViewURL,
  Worker,
  WorkerRequest,
} from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const orNull = (s: string) => (s === "" ? null : s);
const workerPath = (id: string, rest = "") => `/v1/workers/${encodeURIComponent(id)}${rest}`;

type RecordKind = "competencies" | "medicals";
const kindOf = (s: string): RecordKind => (s === "medicals" ? "medicals" : "competencies");

function fail(e: unknown, values?: Record<string, string>): FormState {
  if (e instanceof ApiError) return { error: e.message, values };
  throw e;
}

// Adds a worker under the id made up when the form was shown, or edits one
// (owners and SHE officers). docs/api/workers.md.
export async function saveWorker(_: FormState, form: FormData): Promise<FormState> {
  const values = Object.fromEntries(["name", "firm", "clock_number", "trade", "active"].map((k) => [k, field(form, k)]));
  const body: WorkerRequest = {
    name: values.name,
    firm: orNull(values.firm),
    clock_number: orNull(values.clock_number),
    trade: orNull(values.trade),
    active: values.active !== "false",
  };
  const id = field(form, "worker_id");
  try {
    await api<Worker>(workerPath(id), { method: "PUT", body });
  } catch (e) {
    return fail(e, values);
  }
  revalidatePath("/people");
  return { ok: true, data: { worker_id: id } };
}

// A certificate, licence, training record or registration: upload the file,
// then the record naming it. A renewal sets supersedes_id.
export async function recordCompetency(_: FormState, form: FormData): Promise<FormState> {
  const values = Object.fromEntries(
    ["type_key", "number", "issuer", "issued_on", "expires_on"].map((k) => [k, field(form, k)]),
  );
  const workerId = field(form, "worker_id");
  const recordId = field(form, "id");
  try {
    const fileId = await uploadFile(form.get("file"), { recordId, purpose: "competency" });
    const body: CompetencyRequest = {
      regime_code: field(form, "regime_code"),
      type_key: values.type_key,
      number: orNull(values.number),
      issuer: orNull(values.issuer),
      issued_on: values.issued_on,
      expires_on: orNull(values.expires_on),
      file_id: fileId,
      supersedes_id: orNull(field(form, "supersedes_id")),
      client_recorded_at: new Date().toISOString(),
    };
    await api<Competency>(workerPath(workerId, `/competencies/${encodeURIComponent(recordId)}`), { method: "PUT", body });
  } catch (e) {
    return fail(e, values);
  }
  revalidatePath("/people");
  return { ok: true };
}

// A medical certificate of fitness: only the outcome and dates are stored as
// data; the certificate opens for owners and SHE officers only.
export async function recordMedical(_: FormState, form: FormData): Promise<FormState> {
  const values = Object.fromEntries(
    ["outcome", "examined_on", "expires_on", "practice_number"].map((k) => [k, field(form, k)]),
  );
  const workerId = field(form, "worker_id");
  const recordId = field(form, "id");
  try {
    const fileId = await uploadFile(form.get("file"), { recordId, purpose: "medical" });
    const body: MedicalRequest = {
      regime_code: field(form, "regime_code"),
      outcome: values.outcome as MedicalOutcome,
      examined_on: values.examined_on,
      expires_on: values.expires_on,
      practice_number: orNull(values.practice_number),
      file_id: fileId,
      supersedes_id: orNull(field(form, "supersedes_id")),
      client_recorded_at: new Date().toISOString(),
    };
    await api<Medical>(workerPath(workerId, `/medicals/${encodeURIComponent(recordId)}`), { method: "PUT", body });
  } catch (e) {
    return fail(e, values);
  }
  revalidatePath("/people");
  return { ok: true };
}

// A record made in error stays, marked retracted. Owners and SHE officers.
export async function retractWorkerRecord(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  const body: RetractRequest = { reason };
  const path = workerPath(field(form, "worker_id"), `/${kindOf(field(form, "kind"))}/${encodeURIComponent(field(form, "record_id"))}/retract`);
  try {
    await api<Competency | Medical>(path, { method: "POST", body });
  } catch (e) {
    return fail(e, { reason });
  }
  revalidatePath("/people");
  return { ok: true };
}

// A 60-second link to the certificate; the API logs the view first.
export async function viewWorkerRecord(workerId: string, kind: RecordKind, recordId: string): Promise<{ url?: string; error?: string }> {
  try {
    const v = await api<ViewURL>(workerPath(workerId, `/${kindOf(kind)}/${encodeURIComponent(recordId)}/view`), { method: "POST" });
    return { url: v.url };
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
}
