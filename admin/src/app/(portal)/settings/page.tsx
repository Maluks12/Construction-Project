import type { Metadata } from "next";

import { Table } from "@/components/table";
import { Badge, Card, PageHeader, Row, buttonClass } from "@/components/ui";
import { flags, staff } from "@/lib/sample";

export const metadata: Metadata = { title: "Settings" };

export default function SettingsPage() {
  return (
    <>
      <PageHeader title="Settings" description="Who on the team can use this portal, and which features are switched on.">
        <button type="button" disabled className={buttonClass("primary")}>Add a team member</button>
      </PageHeader>
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[1_1_460px]" title={<span className="text-lg">Team</span>}>
          <Table
            head={["Name", "Role", "Two-step sign-in", "Last seen"]}
            rows={staff.map((s) => [<strong key="n" className="font-semibold">{s.name}</strong>, s.role, <Badge key="t" tone="ok">{s.twoFactor}</Badge>, s.last])}
          />
        </Card>
        <Card className="flex-[1_1_460px]" title={<span className="text-lg">Feature switches</span>} description="Applies to every organisation.">
          {flags.map((f) => (
            <Row key={f.key} aside={<Badge tone={f.on ? "ok" : "neutral"}>{f.on ? "On" : "Off"}</Badge>}>
              <strong className="font-semibold">{f.label}</strong>
              <div className="text-xs text-muted">{f.detail}</div>
            </Row>
          ))}
        </Card>
      </div>
    </>
  );
}
