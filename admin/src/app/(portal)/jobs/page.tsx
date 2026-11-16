import type { Metadata } from "next";

import { Table, Two } from "@/components/table";
import { Badge, Card, PageHeader, buttonClass } from "@/components/ui";
import { failedJobs, queues } from "@/lib/sample";

export const metadata: Metadata = { title: "Jobs & reminders" };

export default function JobsPage() {
  return (
    <>
      <PageHeader title="Jobs & reminders" description="Background work the worker runs: reminders, escalations, audit packs and message delivery." />
      <div className="flex flex-col gap-5">
        <Card flush title={<span className="text-lg">Queues</span>}>
          <Table
            head={["Queue", "Waiting", "Running", "Failed", "Oldest waiting", "State"]}
            rows={queues.map((q) => [
              <strong key="n" className="font-semibold">{q.name}</strong>,
              q.waiting,
              q.running,
              q.failed,
              q.oldest,
              <Badge key="s" tone={q.tone}>{q.failed ? "Check failures" : "Healthy"}</Badge>,
            ])}
          />
        </Card>
        <Card flush title={<span className="text-lg">Failed jobs</span>}>
          <Table
            head={["Job", "Queue", "Tries", "Error", "Last tried", ""]}
            rows={failedJobs.map((j) => [
              <Two key="n" top={j.what} sub={j.id} />,
              j.queue,
              j.tries,
              j.error,
              j.when,
              <button key="b" type="button" disabled className={buttonClass("secondary", "sm")}>Retry</button>,
            ])}
          />
        </Card>
      </div>
    </>
  );
}
