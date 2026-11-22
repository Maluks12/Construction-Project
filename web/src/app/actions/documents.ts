"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import { uploadFile } from "@/lib/upload";
import type {
  DocumentCreateRequest,
  DocumentDetail,
  DocumentScope,
  DocumentVersionRequest,
  RetractRequest,
  ViewURL,
} from "@/lib/types";

import type { FormState } from "./state";

// The vault (the organisation's statutory documents) and a site's H&S file
// are the same register behind two base paths (docs/api/documents.md).
// Every form sends `scope` ("org" or "site") and, for the H&S file, site_id.

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const orNull = (s: string) => (s === "" ? null : s);

function base(scope: DocumentScope, siteId: string) {
  return scope === "org"
    ? "/v1/vault/documents"
    : `/v1/construction/sites/${encodeURIComponent(siteId)}/safety-file`;
}

const scopeOf = (form: FormData): DocumentScope => (field(form, "scope") === "org" ? "org" : "site");

function fail(e: unknown, values?: Record<string, string>): FormState {
  if (e instanceof ApiError) return { error: e.message, values };
  throw e;
}

function refresh() {
  revalidatePath("/people");
  revalidatePath("/organisation");
  // Site work notices name H&S file documents as their proof.
  revalidatePath("/permits");
  revalidatePath("/deadlines");
}

const VERSION_FIELDS = ["issuer", "reference", "issued_on", "expires_on"];

async function versionBody(form: FormData, recordId: string, scope: DocumentScope, siteId: string): Promise<DocumentVersionRequest> {
  const fileId = await uploadFile(form.get("file"), {
    recordId,
    purpose: scope === "org" ? "org_document" : "site_document",
    siteId,
  });
  return {
    file_id: fileId,
    issuer: orNull(field(form, "issuer")),
    reference: orNull(field(form, "reference")),
    issued_on: orNull(field(form, "issued_on")),
    expires_on: orNull(field(form, "expires_on")),
    client_uploaded_at: new Date().toISOString(),
  };
}

// A new document with its version 1. Owners and SHE officers.
export async function createDocument(_: FormState, form: FormData): Promise<FormState> {
  const scope = scopeOf(form);
  const siteId = field(form, "site_id");
  const documentId = field(form, "document_id");
  const values = Object.fromEntries(["type_key", "title", ...VERSION_FIELDS].map((k) => [k, field(form, k)]));
  try {
    const body: DocumentCreateRequest = {
      // The vault names its regime; an H&S file document takes its site's.
      ...(scope === "org" ? { regime_code: field(form, "regime_code") } : {}),
      type_key: values.type_key,
      title: values.title,
      version: await versionBody(form, `${documentId}:1`, scope, siteId),
    };
    await api<DocumentDetail>(`${base(scope, siteId)}/${encodeURIComponent(documentId)}`, { method: "PUT", body });
  } catch (e) {
    return fail(e, values);
  }
  refresh();
  return { ok: true, data: { document_id: documentId } };
}

// A renewal or revision: the next version number, with a file of its own.
export async function addDocumentVersion(_: FormState, form: FormData): Promise<FormState> {
  const scope = scopeOf(form);
  const siteId = field(form, "site_id");
  const documentId = field(form, "document_id");
  const version = Number(field(form, "version"));
  const values = Object.fromEntries(VERSION_FIELDS.map((k) => [k, field(form, k)]));
  try {
    const body = await versionBody(form, `${documentId}:${version}`, scope, siteId);
    await api<DocumentDetail>(`${base(scope, siteId)}/${encodeURIComponent(documentId)}/versions/${version}`, {
      method: "PUT",
      body,
    });
  } catch (e) {
    return fail(e, values);
  }
  refresh();
  return { ok: true };
}

// Retracts the whole document, or only its current version (`version` set):
// the previous version is current again. Nothing is deleted.
export async function retractDocument(_: FormState, form: FormData): Promise<FormState> {
  const scope = scopeOf(form);
  const reason = field(form, "reason");
  const version = field(form, "version");
  const body: RetractRequest = { reason };
  const path = `${base(scope, field(form, "site_id"))}/${encodeURIComponent(field(form, "document_id"))}${version ? `/versions/${encodeURIComponent(version)}` : ""}/retract`;
  try {
    await api<DocumentDetail>(path, { method: "POST", body });
  } catch (e) {
    return fail(e, { reason });
  }
  refresh();
  return { ok: true };
}

// A 60-second link to one version's file; the API logs the view first.
export async function viewDocumentVersion(
  scope: DocumentScope,
  siteId: string,
  documentId: string,
  version: number,
): Promise<{ url?: string; error?: string }> {
  try {
    const v = await api<ViewURL>(`${base(scope, siteId)}/${encodeURIComponent(documentId)}/versions/${version}/view`, {
      method: "POST",
    });
    return { url: v.url };
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
}
