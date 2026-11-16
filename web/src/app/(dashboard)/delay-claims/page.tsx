import type { Metadata } from "next";

import { Badge, Card, Field, Input, PageHeader, SampleBanner, Select, Textarea, buttonClass } from "@/components/ui";
import { delayCauses, delayEvents } from "@/lib/sample";

export const metadata: Metadata = { title: "Delay claims" };

export default function DelayClaimsPage() {
  return (
    <>
      <PageHeader
        title="Delay claims"
        description="Log anything that delays the work as it happens. Contract notice periods are counted in working days, which skip weekends, public holidays and the builders' holiday."
      />
      <SampleBanner needs="the JBCC delay claims milestone" />
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[999_1_620px]">
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Event</th><th>Started</th><th>Cause</th><th>Days lost</th><th>Notice to principal agent</th><th>Evidence</th></tr></thead>
              <tbody>
                {delayEvents.map((e) => (
                  <tr key={e.ref}>
                    <td><strong className="font-semibold">{e.title}</strong><div className="text-xs text-muted">{e.ref}</div></td>
                    <td className="whitespace-nowrap">{e.start}</td>
                    <td>{e.cause}</td>
                    <td>{e.days}</td>
                    <td><Badge tone={e.tone}>{e.notice}</Badge><div className="mt-1 text-xs text-muted">{e.due}</div></td>
                    <td>{e.ev}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
        <Card className="flex-[1_1_340px]" title="New delay event">
          <div className="space-y-3">
            <Field label="What happened"><Input disabled defaultValue="Level 4 pour postponed" /></Field>
            <Field label="Started"><Input disabled defaultValue="Mon 16 Nov 2026" /></Field>
            <Field label="Cause"><Select disabled defaultValue="Supplier or plant">{delayCauses.map((c) => <option key={c}>{c}</option>)}</Select></Field>
            <Field label="Expected days lost"><Input disabled defaultValue="2" /></Field>
            <Field label="Details"><Textarea disabled defaultValue="Concrete supplier's pump broke down; next available slot Wednesday." /></Field>
            <p className="rounded-lg bg-bg px-3 py-2 text-[13px] text-muted">
              The notice deadline is worked out from the contract&apos;s rule in the regime data, shown with its clause.
              [CLAUSE AND NOTICE PERIOD TO BE QUOTED FROM THE JBCC SOURCE]
            </p>
            <div className="flex flex-wrap gap-2">
              <button type="button" disabled className={buttonClass("primary")}>Save event</button>
              <button type="button" disabled className={buttonClass("secondary")}>Link diary days and photos</button>
            </div>
          </div>
        </Card>
      </div>
    </>
  );
}
