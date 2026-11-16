import type { Metadata } from "next";

import { Badge, Card, Field, Input, PageHeader, Row, SampleBanner, buttonClass } from "@/components/ui";
import { permits } from "@/lib/sample";

export const metadata: Metadata = { title: "Notifications & permits" };

export default function PermitsPage() {
  return (
    <>
      <PageHeader
        title="Notifications & permits"
        description="What the Department of Employment and Labour must hear before work starts. The deadline counts back from the planned start date."
      />
      <SampleBanner needs="the Reg 4 notifications work (REG4)" />
      <div className="flex flex-wrap items-start gap-5">
        <Card
          className="min-w-0 flex-[999_1_560px]"
          title="Does this work need a notification?"
          description="Reg 4 doesn't cover all work: only the kinds listed in the regulation. Tick what applies to the next phase."
        >
          <div className="grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))]">
            {permits.kinds.map((k, i) => (
              <label key={k} className="flex items-center gap-2.5 rounded-lg border border-line-strong px-3 py-2.5 text-sm">
                <input type="checkbox" disabled defaultChecked={i < 2} /> {k}
              </label>
            ))}
          </div>
          <div className="mt-3.5 grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(220px,1fr))]">
            <Field label="Planned start of this work"><Input disabled defaultValue="Mon 7 Dec 2026" /></Field>
            <Field label="Principal contractor"><Input disabled defaultValue="Pillay Construction" /></Field>
          </div>
          <p className="mt-3.5 rounded-lg bg-warn-soft px-3 py-2 text-[13px] text-warn">
            Notify by [DATE FROM THE REG 4 RULE], before work starts. Whether the count is in clear days is still to be
            confirmed from the source.
          </p>
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" disabled className={buttonClass("primary")}>Record notification sent</button>
            <button type="button" disabled className={buttonClass("secondary")}>Attach acknowledgement</button>
          </div>
        </Card>
        <Card className="flex-[1_1_340px]" title="This site">
          {permits.items.map((i) => (
            <Row key={i.title} aside={<Badge tone={i.tone}>{i.state}</Badge>}>
              <strong className="font-semibold">{i.title}</strong>
              <div className="text-xs text-muted">{i.ref} · {i.sub}</div>
            </Row>
          ))}
        </Card>
      </div>
    </>
  );
}
