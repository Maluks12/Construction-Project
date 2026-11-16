import type { Metadata } from "next";

import { Badge, Card, Field, Input, PageHeader, Row, SampleBanner, Textarea, buttonClass } from "@/components/ui";
import { iod } from "@/lib/sample";

export const metadata: Metadata = { title: "Injury on duty" };

export default function InjuryOnDutyPage() {
  return (
    <>
      <PageHeader
        title={`Injury on duty · ${iod.worker}`}
        description={`COIDA claim from ${iod.incident}. Uses the incident's 7-day employer-report clock; it never starts a second one. Fund claim number: not yet issued.`}
      />
      <SampleBanner needs="the injury-on-duty milestone (IOD)" />
      <div className="flex flex-col gap-5">
        <div className="grid gap-2.5 [grid-template-columns:repeat(auto-fit,minmax(170px,1fr))]">
          {iod.stages.map((s, i) => (
            <div key={s.form} className={`rounded-[10px] border bg-surface px-3.5 py-3 ${i === 0 ? "border-accent" : "border-line"}`}>
              <div className="flex items-center justify-between gap-2">
                <strong className="font-head text-base font-semibold">{s.form}</strong>
                <Badge tone={s.tone}>{s.state}</Badge>
              </div>
              <div className="mt-1 text-[13px] text-muted">{s.what}</div>
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-start gap-5">
          <Card
            className="min-w-0 flex-[999_1_560px]"
            title="W.Cl.2 · Employer's report of an accident"
            description="Most fields are filled from the incident and the worker register. Check them, add what's missing, then mark it submitted. A PDF of the form comes later; for now this is the data to copy."
          >
            <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(220px,1fr))]">
              {iod.fields.map(([name, value]) => (
                <Field key={name} label={name}><Input defaultValue={value} disabled /></Field>
              ))}
            </div>
            <div className="mt-3.5 space-y-3.5">
              <Field label="How the accident happened"><Textarea defaultValue={iod.how} disabled /></Field>
              <Field label="Injuries"><Input defaultValue={iod.injuries} disabled /></Field>
            </div>
            <div className="mt-4 flex flex-wrap gap-2">
              <button type="button" disabled className={buttonClass("primary")}>Mark W.Cl.2 submitted</button>
              <button type="button" disabled className={buttonClass("secondary")}>Copy as text</button>
            </div>
          </Card>
          <div className="flex flex-[1_1_320px] flex-col gap-4">
            <Card title="Deadline">
              <p className="text-sm font-semibold">Employer report (W.Cl.2) by end of Mon 23 Nov (SAST)</p>
              <p className="mt-1 text-xs text-muted">COIDA s39(1) · 7 days from when the employer became aware</p>
              <div className="mt-2"><Badge tone="warn">7 days left</Badge></div>
            </Card>
            <Card title="Supporting documents">
              {iod.docs.map((d) => (
                <Row key={d.name} aside={<Badge tone={d.tone}>{d.state}</Badge>}>{d.name}</Row>
              ))}
            </Card>
          </div>
        </div>
      </div>
    </>
  );
}
