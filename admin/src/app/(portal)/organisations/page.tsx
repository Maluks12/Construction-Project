import type { Metadata } from "next";
import Link from "next/link";

import { Table, Two } from "@/components/table";
import { Badge, Card, Input, PageHeader, Select } from "@/components/ui";
import { orgs } from "@/lib/sample";

export const metadata: Metadata = { title: "Organisations" };

export default function OrganisationsPage() {
  return (
    <>
      <PageHeader title="Organisations" description="Each customer company. Their safety records stay theirs: this portal shows counts and status, not record contents." />
      <div className="flex flex-col gap-5">
        <div className="flex flex-wrap items-end gap-4 rounded-[10px] border border-line bg-surface px-[18px] py-3.5">
          <label className="block min-w-[220px] flex-1">
            <span className="mb-1 block text-xs font-semibold text-muted">Search</span>
            <Input disabled placeholder="Company or owner" />
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-semibold text-muted">Status</span>
            <Select disabled className="w-auto"><option>Any</option><option>Active</option><option>Trial</option><option>Payment failed</option></Select>
          </label>
        </div>
        <Card flush>
          <Table
            head={["Organisation", "Plan", "Sites", "Seats", "Payment", "Status", "Last active"]}
            rows={orgs.map((o) => [
              <Two key="n" top={<Link href={`/organisations/${o.id}`} className="link">{o.name}</Link>} sub={`${o.owner} · joined ${o.joined}`} />,
              o.plan,
              o.sites,
              o.seats,
              <Badge key="p" tone={o.paymentTone}>{o.payment}</Badge>,
              <Badge key="s" tone={o.tone}>{o.status}</Badge>,
              o.lastActive,
            ])}
          />
        </Card>
      </div>
    </>
  );
}
