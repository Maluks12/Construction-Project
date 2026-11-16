import type { Metadata } from "next";

import { Table, Two } from "@/components/table";
import { Badge, Card, PageHeader } from "@/components/ui";
import { chains, rejections } from "@/lib/sample";

export const metadata: Metadata = { title: "Evidence health" };

export default function EvidencePage() {
  return (
    <>
      <PageHeader
        title="Evidence health"
        description="Whether each site's evidence chain still checks out, and photos the server refused. Staff see hashes and counts, never the photos."
      />
      <div className="flex flex-col gap-5">
        <Card flush title={<span className="text-lg">Chain checks</span>} description="“No break found” is not “tamper-proof”: it means the last walk found nothing wrong.">
          <Table
            head={["Site", "Entries", "Last checked", "Result", "Pending photos"]}
            rows={chains.map((c) => [<Two key="n" top={c.site} sub={c.org} />, c.entries, c.checked, <Badge key="r" tone={c.tone}>{c.result}</Badge>, c.pending])}
          />
        </Card>
        <Card flush title={<span className="text-lg">Rejected uploads</span>} description="A rejected photo is never sealed; the phone retakes it under a new id.">
          <Table
            head={["When", "Site", "Reason", "Device"]}
            rows={rejections.map((r) => [r.when, <Two key="n" top={r.site} sub={r.org} />, <Badge key="r" tone="bad">{r.reason}</Badge>, r.device])}
          />
        </Card>
      </div>
    </>
  );
}
