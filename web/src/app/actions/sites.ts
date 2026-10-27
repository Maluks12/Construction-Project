"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, api } from "@/lib/api";
import type { Location, Site } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) =>
  String(form.get(name) ?? "").trim();

const sitePath = (id: string) => `/v1/sites/${encodeURIComponent(id)}`;

// Runs an API call and turns ApiError into form state. Anything else
// (including Next.js redirects) is re-thrown.
async function attempt(
  fn: () => Promise<unknown>,
  values?: Record<string, string>,
): Promise<FormState> {
  try {
    await fn();
    return { ok: true };
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }
}

export async function createSite(_: FormState, form: FormData): Promise<FormState> {
  const name = field(form, "name");
  const address = field(form, "address");

  let site: Site;
  try {
    site = await api<Site>("/v1/sites", {
      method: "POST",
      body: { name, address: address || null },
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values: { name, address } };
    throw e;
  }
  revalidatePath("/sites");
  redirect(`/sites/${site.id}`);
}

export async function updateSite(_: FormState, form: FormData): Promise<FormState> {
  const id = field(form, "site_id");
  const name = field(form, "name");
  const address = field(form, "address");

  const result = await attempt(
    () =>
      api<Site>(sitePath(id), {
        method: "PATCH",
        // The API keeps the old value for any field that is left out, so a
        // blank address is sent as "" and stored as an empty address.
        body: { name, address },
      }),
    { name, address },
  );
  if (result?.ok) revalidatePath(`/sites/${id}`);
  return result;
}

export async function archiveSite(_: FormState, form: FormData): Promise<FormState> {
  const id = field(form, "site_id");
  const result = await attempt(() => api<void>(sitePath(id), { method: "DELETE" }));
  if (!result?.ok) return result;
  revalidatePath("/sites");
  redirect("/sites");
}

export async function createLocation(
  _: FormState,
  form: FormData,
): Promise<FormState> {
  const siteId = field(form, "site_id");
  const name = field(form, "name");
  const kind = field(form, "kind");
  const parentId = field(form, "parent_id");

  const result = await attempt(
    () =>
      api<Location>(`${sitePath(siteId)}/locations`, {
        method: "POST",
        body: { name, kind, parent_id: parentId || null },
      }),
    { name, kind, parent_id: parentId },
  );
  if (result?.ok) revalidatePath(`/sites/${siteId}`);
  return result;
}

export async function renameLocation(
  _: FormState,
  form: FormData,
): Promise<FormState> {
  const siteId = field(form, "site_id");
  const locationId = field(form, "location_id");
  const name = field(form, "name");

  const result = await attempt(
    () =>
      api<Location>(
        `${sitePath(siteId)}/locations/${encodeURIComponent(locationId)}`,
        { method: "PATCH", body: { name } },
      ),
    { name },
  );
  if (result?.ok) revalidatePath(`/sites/${siteId}`);
  return result;
}

export async function archiveLocation(
  _: FormState,
  form: FormData,
): Promise<FormState> {
  const siteId = field(form, "site_id");
  const locationId = field(form, "location_id");

  const result = await attempt(() =>
    api<void>(`${sitePath(siteId)}/locations/${encodeURIComponent(locationId)}`, {
      method: "DELETE",
    }),
  );
  if (result?.ok) revalidatePath(`/sites/${siteId}`);
  return result;
}

export async function removeSiteMember(
  _: FormState,
  form: FormData,
): Promise<FormState> {
  const siteId = field(form, "site_id");
  const userId = field(form, "user_id");

  const result = await attempt(() =>
    api<void>(`${sitePath(siteId)}/members/${encodeURIComponent(userId)}`, {
      method: "DELETE",
    }),
  );
  if (result?.ok) revalidatePath(`/sites/${siteId}`);
  return result;
}
