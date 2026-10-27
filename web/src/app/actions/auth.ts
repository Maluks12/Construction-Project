"use server";

import { cookies } from "next/headers";
import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, api } from "@/lib/api";
import { toE164 } from "@/lib/format";
import { clearTokens, writeTokens } from "@/lib/session";
import type { TokenPair } from "@/lib/types";

import type { FormState } from "./state";

const DEVICE_NAME = "Web dashboard";

const field = (form: FormData, name: string) =>
  String(form.get(name) ?? "").trim();

// Only follow ?next= to a page on this site, never to another domain.
function safeNext(value: string) {
  return value.startsWith("/") && !value.startsWith("//") ? value : "/sites";
}

export async function login(_: FormState, form: FormData): Promise<FormState> {
  const loginId = field(form, "login");
  const password = String(form.get("password") ?? "");
  const isEmail = loginId.includes("@");

  let pair: TokenPair;
  try {
    pair = await api<TokenPair>("/v1/auth/login", {
      method: "POST",
      auth: false,
      body: {
        ...(isEmail ? { email: loginId } : { mobile: toE164(loginId) }),
        password,
        device_name: DEVICE_NAME,
      },
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values: { login: loginId } };
    throw e;
  }

  writeTokens(await cookies(), pair);
  redirect(safeNext(field(form, "next")));
}

export async function signup(_: FormState, form: FormData): Promise<FormState> {
  const values = {
    company_name: field(form, "company_name"),
    full_name: field(form, "full_name"),
    email: field(form, "email"),
    site_name: field(form, "site_name"),
  };

  let pair: TokenPair;
  try {
    pair = await api<TokenPair>("/v1/auth/signup", {
      method: "POST",
      auth: false,
      body: {
        ...values,
        password: String(form.get("password") ?? ""),
        device_name: DEVICE_NAME,
      },
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message, values };
    throw e;
  }

  writeTokens(await cookies(), pair);
  redirect("/sites");
}

export async function acceptInvite(
  _: FormState,
  form: FormData,
): Promise<FormState> {
  const password = String(form.get("password") ?? "");
  const confirm = form.get("confirm");
  if (confirm !== null && confirm !== password) {
    return { error: "The two passwords don't match." };
  }

  let pair: TokenPair;
  try {
    pair = await api<TokenPair>("/v1/invites/accept", {
      method: "POST",
      auth: false,
      body: {
        token: field(form, "token"),
        password,
        device_name: DEVICE_NAME,
      },
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }

  writeTokens(await cookies(), pair);
  redirect("/sites");
}

export async function logout() {
  try {
    await api<void>("/v1/auth/logout", { method: "POST" });
  } catch (e) {
    // Signing out locally still matters if the API call fails.
    if (!(e instanceof ApiError)) throw e;
  }
  clearTokens(await cookies());
  redirect("/login");
}

export async function revokeSession(_: FormState, form: FormData): Promise<FormState> {
  const id = field(form, "session_id");
  try {
    await api<void>(`/v1/auth/sessions/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
  revalidatePath("/account");
  return { ok: true };
}
