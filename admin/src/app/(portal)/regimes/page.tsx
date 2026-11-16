import type { Metadata } from "next";

import { Table, Two } from "@/components/table";
import { Badge, Card, PageHeader } from "@/components/ui";
import { deadlineRules, regimes, templates } from "@/lib/sample";

export const metadata: Metadata = { title: "Regimes & checklists" };

export default function RegimesPage() {
  return (
    <>
      <PageHeader
        title="Regimes & checklists"
        description="The regulation data every site runs on: checklists, deadline rules, incident types. It ships with the API; wording only changes by publishing a new version."
      />
      <div className="flex flex-col gap-5">
        <Card flush title={<span className="text-lg">Regimes</span>}>
          <Table
            head={["Regime", "Version", "Sites using it", "State"]}
            rows={regimes.map((r) => [<Two key="n" top={r.name} sub={r.code} />, r.version, r.sites, <Badge key="s" tone={r.tone}>{r.state}</Badge>])}
          />
        </Card>
        <div className="flex flex-wrap items-start gap-5">
          <Card flush className="min-w-0 flex-[1_1_520px]" title={<span className="text-lg">Checklist templates</span>} description="Construction Regulations 2014. Latest version of each.">
            <Table
              head={["Template", "Version", "Items", "Cadence", "Inspections started"]}
              rows={templates.map((t) => [<Two key="n" top={t.title} sub={`${t.key} · ${t.ref}`} />, `v${t.v}`, t.items, t.cadence, t.used])}
            />
          </Card>
          <Card flush className="min-w-0 flex-[1_1_520px]" title={<span className="text-lg">Deadline rules</span>} description="Shown to customers as guidance, with their source. Each needs a practitioner's sign-off before launch.">
            <Table
              head={["Rule", "Period", "Review"]}
              rows={deadlineRules.map((d) => [<Two key="n" top={d.label} sub={d.ref} />, d.period, <Badge key="r" tone={d.tone}>{d.review}</Badge>])}
            />
          </Card>
        </div>
      </div>
    </>
  );
}
