import type { Metadata } from "next";
import Link from "next/link";

import { Badge, Card, PageHeader, Row } from "@/components/ui";
import { attention, kpis, services, signups } from "@/lib/sample";

export const metadata: Metadata = { title: "Overview" };

export default function OverviewPage() {
  return (
    <>
      <PageHeader title="Platform overview" description="Every organisation on SiteProof, and what needs someone on the team today." />
      <div className="flex flex-col gap-5">
        <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(200px,1fr))]">
          {kpis.map((k) => (
            <div key={k.label} className="rounded-[10px] border border-line bg-surface px-[18px] py-4">
              <div className="text-[13px] text-muted">{k.label}</div>
              <div className="mt-1 font-head text-[34px] leading-tight font-semibold">{k.value}</div>
              <div className="text-[13px] text-muted">{k.sub}</div>
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-start gap-5">
          <Card className="min-w-0 flex-[999_1_520px]" title={<span className="text-lg">Needs attention</span>}>
            {attention.map((a) => (
              <Row key={a.title} aside={<Badge tone={a.tone}>{a.state}</Badge>}>
                <Link href={a.href} className="link">{a.title}</Link>
                <div className="text-[13px] text-muted">{a.sub}</div>
              </Row>
            ))}
          </Card>
          <Card className="flex-[1_1_340px]" title="Services">
            {services.map((s) => (
              <Row key={s.name} aside={<Badge tone={s.tone}>{s.state}</Badge>}>
                <strong className="font-semibold">{s.name}</strong>
                <div className="text-xs text-muted">{s.detail}</div>
              </Row>
            ))}
          </Card>
        </div>
        <Card title="Recent sign-ups" description="New organisations, newest first.">
          {signups.map((s) => (
            <Row key={s.org} aside={<Badge tone={s.tone}>{s.state}</Badge>}>
              <strong className="font-semibold">{s.org}</strong> <span className="text-muted">· {s.who} · {s.when}</span>
            </Row>
          ))}
        </Card>
      </div>
    </>
  );
}
