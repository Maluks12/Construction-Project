"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import type { Download, Export, ExportRequest } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();
const exportsPath = (siteId: string, rest = "") => `/v1/sites/${encodeURIComponent(siteId)}/exports${rest}`;

// Queues an audit pack (docs/api/exports.md). The id was made up when the
// form was shown, so pressing the button twice queues one pack.
export async function queueExport(_: FormState, form: FormData): Promise<FormState> {
  const values = { from: field(form, "from"), to: field(form, "to") };
  const body: ExportRequest = { ...values, client_requested_at: new Date().toISOString() };
  try {
    await api<Export>(exportsPath(field(form, "site_id"), `/${encodeURIComponent(field(form, "id"))}`), {
      method: "PUT",
      body,
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
  revalidatePath("/audit-pack");
  return { ok: true, data: { id: field(form, "id") } };
}

// A 5-minute link to a ready pack. Every request is logged by the API, so
// this only runs when someone presses Download.
export async function downloadExport(siteId: string, exportId: string): Promise<{ url?: string; error?: string }> {
  try {
    const d = await api<Download>(exportsPath(siteId, `/${encodeURIComponent(exportId)}/download`), { method: "POST" });
    return { url: d.url };
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
}
