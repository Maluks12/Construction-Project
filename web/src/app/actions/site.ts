"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { SITE_COOKIE } from "@/lib/site";

// Remembers which site the sidebar picker points at. The value is only a
// site id; every page still asks the API, which refuses a site the person
// can't open.
export async function chooseSite(form: FormData) {
  const id = String(form.get("site_id") ?? "");
  const back = String(form.get("back") ?? "/overview");
  (await cookies()).set(SITE_COOKIE, id, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: 180 * 24 * 60 * 60,
  });
  // Only ever back to a page of this dashboard, without the old site's
  // record ids or filters.
  redirect(back.startsWith("/") && !back.startsWith("//") ? back : "/overview");
}
