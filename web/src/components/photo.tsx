"use client";

import { useState } from "react";

import { viewEvidence } from "@/app/actions/evidence";

import { Thumb, buttonClass } from "./ui";

// A grey thumbnail that opens the real photo on click. Thumbnails stay grey
// on purpose: every time the API hands out a photo it writes an access-log
// row, so photos are only fetched when someone asks to see one.
export function PhotoButton({
  siteId,
  evidenceId,
  label,
}: {
  siteId: string;
  evidenceId: string;
  label: string;
}) {
  const [state, setState] = useState<{ url?: string; error?: string; busy?: boolean }>({});

  async function open() {
    setState({ busy: true });
    setState(await viewEvidence(siteId, evidenceId));
  }

  return (
    <>
      <button type="button" onClick={open} disabled={state.busy} aria-label={`View photo: ${label}`} className="cursor-pointer rounded-md disabled:opacity-60">
        <Thumb />
      </button>
      {state.error && <p className="mt-1 max-w-[160px] text-[11px] text-danger">{state.error}</p>}
      {state.url && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label={label}
          className="fixed inset-0 z-50 flex flex-col items-center justify-center gap-3 bg-black/80 p-4"
          onClick={() => setState({})}
        >
          {/* A short-lived signed link straight from storage; next/image can't optimise it. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={state.url} alt={label} className="max-h-[80vh] max-w-full rounded-lg" />
          <div className="flex items-center gap-3 text-sm text-white">
            <span>This view was recorded in the evidence access log.</span>
            <button type="button" className={buttonClass("secondary", "sm")} onClick={() => setState({})}>
              Close
            </button>
          </div>
        </div>
      )}
    </>
  );
}
