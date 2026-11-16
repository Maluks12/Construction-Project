import type { Metadata } from "next";

import { Table } from "@/components/table";
import { Badge, Card, PageHeader } from "@/components/ui";
import { debits, subscriptions } from "@/lib/sample";

export const metadata: Metadata = { title: "Billing" };

export default function BillingPage() {
  return (
    <>
      <PageHeader title="Billing" description="Subscriptions and DebiCheck collections. Plan names and amounts aren't decided yet, so they show as placeholders." />
      <div className="flex flex-col gap-5">
        <Card flush title={<span className="text-lg">Subscriptions</span>}>
          <Table
            head={["Organisation", "Plan", "Seats", "Amount", "Next debit", "Payment"]}
            rows={subscriptions.map((s) => [
              <strong key="o" className="font-semibold">{s.org}</strong>,
              s.plan,
              s.seats,
              s.amount,
              s.next,
              <Badge key="p" tone={s.tone}>{s.payment}</Badge>,
            ])}
          />
        </Card>
        <Card flush title={<span className="text-lg">Recent collections</span>} description="From the payment provider's webhooks.">
          <Table
            head={["When", "Organisation", "Amount", "Result", "Next step"]}
            rows={debits.map((d) => [d.when, d.org, d.amount, <Badge key="r" tone={d.tone}>{d.result}</Badge>, d.next])}
          />
        </Card>
      </div>
    </>
  );
}
