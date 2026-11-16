import type { Metadata } from "next";

import { Table, Two } from "@/components/table";
import { Badge, Card, PageHeader } from "@/components/ui";
import { tickets } from "@/lib/sample";

export const metadata: Metadata = { title: "Support" };

export default function SupportPage() {
  return (
    <>
      <PageHeader title="Support" description="Requests from customers. Staff help from here without opening a customer's safety records." />
      <Card flush>
        <Table
          head={["Request", "From", "Opened", "State"]}
          rows={tickets.map((t) => [<Two key="n" top={t.subject} sub={t.id} />, <Two key="f" top={t.from} sub={t.org} />, t.opened, <Badge key="s" tone={t.tone}>{t.state}</Badge>])}
        />
      </Card>
    </>
  );
}
