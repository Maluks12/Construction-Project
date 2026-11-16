"use server";

import { ApiError, api } from "@/lib/api";
import type { ViewURL } from "@/lib/types";

// Asks the API for a 60-second link to a photo. The API records the view in
// the evidence access log before it issues the link, so this is only called
// when someone clicks a photo, never to pre-load thumbnails. The link is
// handed to the browser for the <img> and not kept anywhere.
export async function viewEvidence(
  siteId: string,
  evidenceId: string,
): Promise<{ url?: string; error?: string }> {
  try {
    const view = await api<ViewURL>(
      `/v1/sites/${encodeURIComponent(siteId)}/evidence/${encodeURIComponent(evidenceId)}/view`,
      { method: "POST" },
    );
    return { url: view.url };
  } catch (e) {
    if (e instanceof ApiError) return { error: e.message };
    throw e;
  }
}
