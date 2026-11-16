import type { Metadata } from "next";

import { Table } from "@/components/table";
import { Card, PageHeader } from "@/components/ui";
import { auditLog } from "@/lib/sample";

export const metadata: Metadata = { title: "Staff audit log" };

export default function AuditLogPage() {
  return (
    <>
      <PageHeader title="Staff audit log" description="Everything a member of the team does in this portal, append-only. Customers should be able to trust that staff access is recorded." />
      <Card flush>
        <Table
          head={["When", "Who", "What", "Target", "From"]}
          rows={auditLog.map((a) => [a.when, <strong key="w" className="font-semibold">{a.who}</strong>, a.what, a.target, a.from])}
        />
      </Card>
    </>
  );
}
