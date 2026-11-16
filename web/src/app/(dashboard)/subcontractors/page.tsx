import type { Metadata } from "next";

import { Badge, Card, PageHeader, SampleBanner, buttonClass } from "@/components/ui";
import { subcontractors } from "@/lib/sample";

export const metadata: Metadata = { title: "Subcontractors" };

export default function SubcontractorsPage() {
  return (
    <>
      <PageHeader
        title="Subcontractors"
        description="Each subcontractor runs its own SiteProof organisation. Linked to this site, you see their compliance here, read-only. They never see yours."
      >
        <button type="button" disabled className={buttonClass("primary")}>Link a subcontractor</button>
      </PageHeader>
      <SampleBanner needs="the principal-contractor tier (PC)" />
      <div className="flex flex-col gap-5">
        <Card flush>
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Subcontractor</th><th>Workers on site</th><th>Competencies</th><th>Open findings</th><th>Incidents (90 days)</th><th>H&amp;S file</th><th>Link</th></tr></thead>
              <tbody>
                {subcontractors.map((s) => (
                  <tr key={s.name}>
                    <td><strong className="font-semibold">{s.name}</strong><div className="text-xs text-muted">{s.trade}</div></td>
                    <td>{s.workers}</td>
                    <td><Badge tone={s.compTone}>{s.comp}</Badge></td>
                    <td>{s.findings}</td>
                    <td>{s.inc}</td>
                    <td><Badge tone={s.fileTone}>{s.file}</Badge></td>
                    <td><Badge tone={s.linkTone}>{s.link}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
        <Card title="How linking works">
          <p className="max-w-3xl text-sm leading-relaxed text-muted">
            You send a link request for this site; the subcontractor&apos;s owner accepts it in their own account. From then
            on, their workers, competencies, findings and incidents for this site appear here. Either side can end the
            link; what you saw while linked stays in your audit pack.
          </p>
        </Card>
      </div>
    </>
  );
}
