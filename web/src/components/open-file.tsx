"use client";

import { useState } from "react";

import { buttonClass } from "./ui";

// Opens a document file (a certificate, letter, H&S file version, audit
// pack) in a new tab. `open` is a Server Action, bound on the server to the
// record, that asks the API for a short-lived link. The API logs every link
// it hands out, so this only runs when someone clicks, never to pre-load.
export function OpenFileButton({
  open,
  label = "Open",
  size = "sm",
}: {
  open: () => Promise<{ url?: string; error?: string }>;
  label?: string;
  size?: "sm" | "md";
}) {
  const [state, setState] = useState<{ busy?: boolean; error?: string }>({});

  async function click() {
    // Opened now, while this is still the click, so pop-up blockers allow it;
    // pointed at the link once the API has issued it.
    const tab = window.open("", "_blank");
    if (tab) tab.opener = null;
    setState({ busy: true });
    const res = await open();
    if (res.url) {
      if (tab) tab.location.href = res.url;
      else window.location.assign(res.url);
      setState({});
    } else {
      tab?.close();
      setState({ error: res.error ?? "Couldn't open the file." });
    }
  }

  return (
    <span className="inline-flex flex-col items-start gap-1">
      <button type="button" onClick={click} disabled={state.busy} className={buttonClass("secondary", size)}>
        {state.busy ? "Opening…" : label}
      </button>
      {state.error && <span className="max-w-64 text-xs text-danger">{state.error}</span>}
    </span>
  );
}
