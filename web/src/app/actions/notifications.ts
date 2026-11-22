"use server";

import { revalidatePath } from "next/cache";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ApiError, api } from "@/lib/api";
import { SITE_COOKIE } from "@/lib/site";
import type { Notification } from "@/lib/types";

import type { FormState } from "./state";

// Opens what a reminder is about: marks it read, points the sidebar at its
// site (records are per site), then goes to the record's page. `to` is a
// path of this dashboard, built by the notifications page.
export async function openNotification(form: FormData) {
  const id = String(form.get("notification_id") ?? "");
  const siteId = String(form.get("site_id") ?? "");
  const to = String(form.get("to") ?? "/notifications");
  try {
    await api<Notification>(`/v1/notifications/${encodeURIComponent(id)}/read`, { method: "POST" });
  } catch (e) {
    // Opening the record matters more than the read mark.
    if (!(e instanceof ApiError)) throw e;
  }
  if (siteId) {
    (await cookies()).set(SITE_COOKIE, siteId, {
      httpOnly: true,
      sameSite: "lax",
      secure: process.env.NODE_ENV === "production",
      path: "/",
      maxAge: 180 * 24 * 60 * 60,
    });
  }
  revalidatePath("/", "layout");
  redirect(to.startsWith("/") && !to.startsWith("//") ? to : "/notifications");
}

// Marks one reminder read (docs/api/notifications.md). Reading it again
// changes nothing, so a double click is harmless.
export async function markNotificationRead(_: FormState, form: FormData): Promise<FormState> {
  const id = String(form.get("notification_id") ?? "");
  try {
    await api<Notification>(`/v1/notifications/${encodeURIComponent(id)}/read`, { method: "POST" });
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
  // The sidebar's unread count is on every page.
  revalidatePath("/", "layout");
  return { ok: true };
}

// The page's "Mark all read": one call per reminder, as the API has no bulk
// endpoint. Sequential, so a long list doesn't open dozens of connections.
// One that fails (already gone, say) doesn't stop the rest.
export async function markAllNotificationsRead(form: FormData) {
  for (const id of form.getAll("notification_id").map(String)) {
    try {
      await api<Notification>(`/v1/notifications/${encodeURIComponent(id)}/read`, { method: "POST" });
    } catch (e) {
      if (!(e instanceof ApiError)) throw e;
    }
  }
  revalidatePath("/", "layout");
}
