"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, api } from "@/lib/api";
import type { SiteWork, SiteWorkStatus, SiteWorkVersionRequest } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) => String(form.get(name) ?? "").trim();

// Records planned construction work (version 1) or amends it (the next
// version, the whole record again with a reason). docs/api/site-work.md.
// The work id and version number come from the form, so submitting twice
// sends the same version twice, which the API answers 200.
export async function submitSiteWork(_: FormState, form: FormData): Promise<FormState> {
  const siteId = field(form, "site_id");
  const workId = field(form, "work_id");
  const version = Number(field(form, "version"));
  const facts = form.getAll("facts").map(String);
  const values = {
    description: field(form, "description"),
    starts_on: field(form, "starts_on"),
    ends_on: field(form, "ends_on"),
    status: field(form, "status") || "planned",
    amend_reason: field(form, "amend_reason"),
    facts: facts.join(","),
  };

  const body: SiteWorkVersionRequest = {
    description: values.description,
    starts_on: values.starts_on,
    ends_on: values.ends_on || null,
    facts,
    status: values.status as SiteWorkStatus,
    client_submitted_at: new Date().toISOString(),
    amend_reason: version > 1 ? values.amend_reason : null,
  };

  try {
    await api<SiteWork>(
      `/v1/construction/sites/${encodeURIComponent(siteId)}/site-work/${encodeURIComponent(workId)}/versions/${version}`,
      { method: "PUT", body },
    );
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
  revalidatePath("/permits");
  revalidatePath("/deadlines");
  redirect(`/permits?work=${encodeURIComponent(workId)}`);
}
