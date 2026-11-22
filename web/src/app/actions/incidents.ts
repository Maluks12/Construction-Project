"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, api } from "@/lib/api";
import { sastToISO } from "@/lib/format";
import type {
  DeadlineDetail,
  Incident,
  PersonInput,
  SatisfactionRequest,
  StatementRequest,
  SubmitVersionRequest,
  WitnessStatement,
} from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const orNull = (s: string) => (s === "" ? null : s);
const site = (id: string, rest: string) => `/v1/sites/${encodeURIComponent(id)}${rest}`;

function fail(e: unknown, values: Record<string, string>): FormState {
  if (e instanceof ApiError) return { error: e.message, values };
  throw e;
}

// Submit version 1 of a new incident, or the next version of an existing
// one. An amendment restates the whole account, so the form sends every
// field again, including the photos already attached (as hidden ids).
export async function submitIncident(_: FormState, form: FormData): Promise<FormState> {
  const siteId = field(form, "site_id");
  const id = field(form, "incident_id");
  const version = Number(field(form, "version"));
  const values = Object.fromEntries(
    ["occurred_at", "employer_aware_at", "location", "description", "amend_reason", "people", "types"].map((k) => [k, field(form, k)]),
  );
  values.types = form.getAll("taxonomy_codes").map(String).join(",");

  let people: PersonInput[];
  try {
    people = JSON.parse(values.people || "[]") as PersonInput[];
  } catch {
    return { error: "The people list couldn't be read. Reload the page and try again.", values };
  }

  const body: SubmitVersionRequest = {
    taxonomy_codes: form.getAll("taxonomy_codes").map(String),
    occurred_at: sastToISO(values.occurred_at),
    employer_aware_at: orNull(sastToISO(values.employer_aware_at)),
    location: orNull(values.location),
    description: orNull(values.description),
    // Times typed for a person (died at) are site times too.
    people: people.map((p) => ({ ...p, died_at: p.died_at ? sastToISO(p.died_at) : null })),
    evidence_ids: form.getAll("evidence_ids").map(String),
    client_submitted_at: new Date().toISOString(),
    amend_reason: version > 1 ? values.amend_reason : null,
  };

  try {
    await api<Incident>(site(siteId, `/incidents/${encodeURIComponent(id)}/versions/${version}`), {
      method: "PUT",
      body,
    });
  } catch (e) {
    return fail(e, values);
  }
  revalidatePath("/incidents");
  revalidatePath("/deadlines");
  redirect(`/incidents/${id}`);
}

export async function recordStatement(_: FormState, form: FormData): Promise<FormState> {
  const values = Object.fromEntries(
    ["witness_name", "witness_contact", "statement", "taken_at"].map((k) => [k, field(form, k)]),
  );
  const body: StatementRequest = {
    witness_name: values.witness_name,
    witness_contact: orNull(values.witness_contact),
    statement: values.statement,
    taken_at: sastToISO(values.taken_at),
  };
  const incidentId = field(form, "incident_id");
  try {
    await api<WitnessStatement>(
      site(field(form, "site_id"), `/incidents/${encodeURIComponent(incidentId)}/witness-statements/${encodeURIComponent(field(form, "id"))}`),
      { method: "PUT", body },
    );
  } catch (e) {
    return fail(e, values);
  }
  revalidatePath(`/incidents/${incidentId}`);
  return { ok: true };
}

// Record that a legal deadline was met: the call made, the form sent.
// `satisfied_at` is when that happened, not when it's being typed in.
// A site work notice (REG4) can name its proof: a version of an H&S file
// document, sent as "documentId:version" from the form's picker.
export async function recordSatisfaction(_: FormState, form: FormData): Promise<FormState> {
  const values = Object.fromEntries(["method", "satisfied_at", "reference", "note", "proof"].map((k) => [k, field(form, k)]));
  const [documentId, documentVersion] = values.proof ? values.proof.split(":") : [];
  const body: SatisfactionRequest = {
    satisfied_at: sastToISO(values.satisfied_at),
    method: values.method as SatisfactionRequest["method"],
    reference: orNull(values.reference),
    note: orNull(values.note),
    ...(documentId && documentVersion ? { document_id: documentId, document_version: Number(documentVersion) } : {}),
  };
  try {
    await api<DeadlineDetail>(
      site(field(form, "site_id"), `/deadlines/${encodeURIComponent(field(form, "deadline_id"))}/satisfactions/${encodeURIComponent(field(form, "id"))}`),
      { method: "PUT", body },
    );
  } catch (e) {
    return fail(e, values);
  }
  revalidatePath("/deadlines");
  revalidatePath("/permits");
  revalidatePath("/overview");
  return { ok: true };
}

// Owners and SHE officers only. The record stays, marked retracted, and the
// duty is open again.
export async function retractSatisfaction(_: FormState, form: FormData): Promise<FormState> {
  const reason = field(form, "reason");
  try {
    await api<DeadlineDetail>(
      site(
        field(form, "site_id"),
        `/deadlines/${encodeURIComponent(field(form, "deadline_id"))}/satisfactions/${encodeURIComponent(field(form, "satisfaction_id"))}/retract`,
      ),
      { method: "POST", body: { reason } },
    );
  } catch (e) {
    return fail(e, { reason });
  }
  revalidatePath("/deadlines");
  return { ok: true };
}
