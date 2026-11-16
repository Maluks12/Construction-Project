"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import type { Inspection } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();

// Abandoning is final. The API decides who may: the inspector, an owner or a
// SHE officer, and only an owner or SHE officer once any item has failed.
export async function abandonInspection(_: FormState, form: FormData): Promise<FormState> {
  const siteId = field(form, "site_id");
  const id = field(form, "inspection_id");
  const reason = field(form, "reason");
  try {
    await api<Inspection>(
      `/v1/sites/${encodeURIComponent(siteId)}/inspections/${encodeURIComponent(id)}/abandon`,
      { method: "POST", body: { reason } },
    );
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values: { reason } };
    throw e;
  }
  revalidatePath("/inspections");
  return { ok: true };
}
