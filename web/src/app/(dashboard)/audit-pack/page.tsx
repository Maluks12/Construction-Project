import type { Metadata } from "next";

import { Badge, Card, Field, Input, PageHeader, SampleBanner, Select, buttonClass } from "@/components/ui";
import { auditPack } from "@/lib/sample";

export const metadata: Metadata = { title: "Audit pack" };

export default function AuditPackPage() {
  return (
    <>
      <PageHeader
        title="Audit pack"
        description="One download for an inspector, a client or a claim. Building it can take a few minutes; you'll be notified when it's ready."
      />
      <SampleBanner needs="reports and the audit pack (M12)" />
      <div className="flex flex-wrap items-start gap-5">
        <Card className="flex-[1_1_360px]" title="New export">
          <div className="space-y-3">
            <Field label="Sites"><Select disabled><option>Rosebank Block B</option><option>All sites</option></Select></Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="From"><Input disabled defaultValue="1 Aug 2026" /></Field>
              <Field label="To"><Input disabled defaultValue="16 Nov 2026" /></Field>
            </div>
            <div className="text-[13px] font-semibold">Include</div>
            {auditPack.parts.map((p) => (
              <label key={p.label} className="flex items-start gap-2.5 text-sm">
                <input type="checkbox" disabled defaultChecked={p.on} className="mt-1" />
                <span>{p.label}<span className="block text-xs text-muted">{p.sub}</span></span>
              </label>
            ))}
            <p className="rounded-lg bg-bg px-3 py-2 text-[13px] text-muted">
              The export is recorded in the evidence access log, with who asked for it and which photos it contains.
            </p>
            <button type="button" disabled className={buttonClass("primary")}>Build audit pack</button>
          </div>
        </Card>
        <Card flush className="min-w-0 flex-[999_1_560px]" title={<span className="text-lg">Previous exports</span>}>
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Pack</th><th>Sites and period</th><th>Asked by</th><th>Chain at export</th><th>Status</th></tr></thead>
              <tbody>
                {auditPack.packs.map((k) => (
                  <tr key={k.id}>
                    <td><strong className="font-semibold">{k.id}</strong><div className="text-xs text-muted">{k.when}</div></td>
                    <td>{k.scope}</td>
                    <td>{k.who}</td>
                    <td>{k.chain}</td>
                    <td><Badge tone={k.tone}>{k.status}</Badge></td>
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
