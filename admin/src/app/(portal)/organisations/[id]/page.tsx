import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { Table, Two } from "@/components/table";
import { Badge, Card, Facts, PageHeader, buttonClass } from "@/components/ui";
import { orgMembers, orgSites, orgs } from "@/lib/sample";

export const metadata: Metadata = { title: "Organisation" };

export function generateStaticParams() {
  return orgs.map((o) => ({ id: o.id }));
}

export default async function OrganisationPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const org = orgs.find((o) => o.id === id);
  if (!org) notFound();
  const sites = orgSites[org.id] ?? [];

  return (
    <>
      <PageHeader
        eyebrow={<><Link href="/organisations" className="link">Organisations</Link> / {org.name}</>}
        title={org.name}
        badges={<><Badge tone={org.tone}>{org.status}</Badge><Badge tone={org.paymentTone}>{org.payment}</Badge></>}
      >
        <button type="button" disabled className={buttonClass("secondary")}>Extend trial</button>
        <button type="button" disabled className={buttonClass("danger")}>Suspend</button>
      </PageHeader>
      <div className="flex flex-col gap-5">
        <Card>
          <Facts
            items={[
              ["Owner", `${org.owner} (${org.ownerLogin.toLowerCase()})`],
              ["Plan", org.plan],
              ["Seats", org.seats],
              ["Joined", org.joined],
              ["Last active", org.lastActive],
              ["Regime of its sites", org.regime],
            ]}
          />
        </Card>
        <div className="flex flex-wrap items-start gap-5">
          <Card flush className="min-w-0 flex-[999_1_520px]" title={<span className="text-lg">Sites</span>} description="Status and chain health only. Inspections, incidents and photos aren't shown to staff.">
            {sites.length === 0 ? (
              <p className="px-[18px] pb-4 text-sm text-muted">No sample sites for this organisation.</p>
            ) : (
              <Table
                head={["Site", "Status", "Evidence chain", "Last inspection"]}
                rows={sites.map((s) => [
                  <strong key="n" className="font-semibold">{s.name}</strong>,
                  <Badge key="s" tone={s.tone}>{s.status}</Badge>,
                  <Badge key="c" tone={s.chainTone}>{s.chain}</Badge>,
                  s.lastInspection,
                ])}
              />
            )}
          </Card>
          <Card flush className="flex-[1_1_360px]" title={<span className="text-lg">Members</span>}>
            <Table
              head={["Name", "Role", "Last seen"]}
              rows={orgMembers.map((m) => [<Two key="n" top={m.name} sub={`Signs in with ${m.login.toLowerCase()}`} />, m.role, m.last])}
            />
          </Card>
        </div>
      </div>
    </>
  );
}
