"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useActionState, useEffect } from "react";

import { queueExport } from "@/app/actions/exports";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Field, Input } from "@/components/ui";

export function ExportForm({ id, siteId, from, to }: { id: string; siteId: string; from: string; to: string }) {
  const [state, action] = useActionState(queueExport, null);
  if (state?.ok) {
    return (
      <Alert tone="ok">
        Queued. It starts building in about two minutes; this page checks on it by itself.{" "}
        <Link href={`/audit-pack?export=${state.data?.id ?? id}`} className="link">Follow it</Link>
      </Alert>
    );
  }
  const v = state?.values ?? {};
  return (
    <form action={action} className="space-y-3">
      <input type="hidden" name="id" value={id} />
      <input type="hidden" name="site_id" value={siteId} />
      <div className="grid grid-cols-2 gap-3">
        <Field label="From"><Input name="from" type="date" required defaultValue={v.from ?? from} /></Field>
        <Field label="To"><Input name="to" type="date" required defaultValue={v.to ?? to} /></Field>
      </div>
      <p className="text-xs text-muted">Site dates, at most 366 days. One pack per site.</p>
      <FormError state={state} />
      <SubmitButton pendingText="Queuing…">Build audit pack</SubmitButton>
    </form>
  );
}

// While a pack is queued or building, re-reads the page every few seconds so
// its status moves on without a reload (exports.md: poll GET …/:exportId).
export function RefreshWhileBuilding({ active }: { active: boolean }) {
  const router = useRouter();
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => router.refresh(), 5000);
    return () => clearInterval(t);
  }, [active, router]);
  return null;
}
