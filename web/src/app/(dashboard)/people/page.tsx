import type { Metadata } from "next";

import { Badge, Card, PageHeader, SampleBanner, buttonClass } from "@/components/ui";
import { people } from "@/lib/sample";

export const metadata: Metadata = { title: "People & documents" };

export default function PeoplePage() {
  return (
    <>
      <PageHeader
        title="People & documents"
        description="The workers on site, their competencies and appointments, and the health and safety file."
      >
        <button type="button" disabled className={buttonClass("secondary")}>Upload document</button>
        <button type="button" disabled className={buttonClass("primary")}>Add worker</button>
      </PageHeader>
      <SampleBanner needs="people and documents (M11)" />
      <div className="flex flex-col gap-5">
        <Card
          flush
          title={<span className="text-lg">Worker register · 46 on site</span>}
          actions={<span className="text-[13px] text-muted">Reminders go out 30 and 7 days before expiry</span>}
        >
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Worker</th><th>Employer</th><th>Trade</th><th>Competencies</th><th>Appointments</th></tr></thead>
              <tbody>
                {people.workers.map((w) => (
                  <tr key={w.name}>
                    <td><strong className="font-semibold">{w.name}</strong></td>
                    <td>{w.employer}</td>
                    <td>{w.trade}</td>
                    <td><div className="flex flex-wrap gap-1.5">{w.comps.map((c) => <Badge key={c.label} tone={c.tone}>{c.label} · {c.when}</Badge>)}</div></td>
                    <td>{w.appt}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
        <Card flush title={<span className="text-lg">Health &amp; safety file</span>} description="Every upload is a new version; earlier versions stay downloadable.">
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Document</th><th>Version</th><th>Uploaded</th><th>Review by</th></tr></thead>
              <tbody>
                {people.docs.map((d) => (
                  <tr key={d.name}>
                    <td><strong className="font-semibold">{d.name}</strong></td>
                    <td>v{d.v}</td>
                    <td>{d.up}</td>
                    <td><Badge tone={d.tone}>{d.review}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>
    </>
  );
}
