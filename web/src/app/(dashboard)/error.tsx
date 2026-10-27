"use client";

import { buttonClass } from "@/components/ui";

export default function DashboardError({ retry }: { retry: () => void }) {
  return (
    <div className="rounded-lg border border-line bg-surface p-6">
      <h1 className="text-lg font-semibold">This page couldn&apos;t load</h1>
      <p className="mt-1 text-sm text-muted">
        The SiteProof API may not be running, or it returned an error. Check
        that the API is up, then try again.
      </p>
      <button type="button" onClick={() => retry()} className={buttonClass("secondary", "sm") + " mt-4"}>
        Try again
      </button>
    </div>
  );
}
