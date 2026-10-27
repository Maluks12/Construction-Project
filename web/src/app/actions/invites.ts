"use server";

import { revalidatePath } from "next/cache";

import { ApiError, api } from "@/lib/api";
import { toE164 } from "@/lib/format";
import type { InviteResponse } from "@/lib/types";

import type { FormState } from "./state";

const field = (form: FormData, name: string) =>
  String(form.get(name) ?? "").trim();

const APP_URL = (process.env.APP_URL ?? "http://localhost:3000").replace(/\/$/, "");

export async function sendInvite(_: FormState, form: FormData): Promise<FormState> {
  const values = {
    full_name: field(form, "full_name"),
    email: field(form, "email"),
    mobile: field(form, "mobile"),
    role: field(form, "role"),
  };

  if (!values.email && !values.mobile) {
    return { error: "Add an email address or a mobile number.", values };
  }

  let invite: InviteResponse;
  try {
    invite = await api<InviteResponse>("/v1/invites", {
      method: "POST",
      body: {
        full_name: values.full_name,
        role: values.role,
        // The API rejects "" for these, so leave them out when blank.
        ...(values.email ? { email: values.email } : {}),
        ...(values.mobile ? { mobile: toE164(values.mobile) } : {}),
      },
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }

  revalidatePath("/team");
  return {
    ok: true,
    data: {
      name: values.full_name,
      link: `${APP_URL}/invite?token=${encodeURIComponent(invite.token)}`,
      expires_at: invite.expires_at,
    },
  };
}

export async function revokeInvite(_: FormState, form: FormData): Promise<FormState> {
  const id = field(form, "invite_id");
  try {
    await api<void>(`/v1/invites/${encodeURIComponent(id)}`, { method: "DELETE" });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
  revalidatePath("/team");
  return { ok: true };
}
