import type { Metadata } from "next";

import { Badge, Card, PageHeader, SampleBanner, buttonClass } from "@/components/ui";
import { syncConflicts } from "@/lib/sample";

export const metadata: Metadata = { title: "Sync conflicts" };

function Side({ title, tag, tone, result, comment, meta }: { title: string; tag: string; tone: "ok" | "neutral"; result: React.ReactNode; comment: string; meta: string }) {
  return (
    <div className="flex-1 rounded-lg border border-line p-3.5">
      <div className="flex items-center justify-between gap-2">
        <strong className="text-sm font-semibold">{title}</strong>
        <Badge tone={tone}>{tag}</Badge>
      </div>
      <div className="mt-2.5 flex gap-1.5">{result}</div>
      <p className="mt-2 text-sm">{comment}</p>
      <p className="mt-1 text-xs text-muted">{meta}</p>
    </div>
  );
}

export default function SyncConflictsPage() {
  return (
    <>
      <PageHeader
        title="Sync conflicts"
        description="Records a phone sent that the server couldn't accept as they were. Nothing is lost: each one is kept until someone decides."
      />
      <SampleBanner needs="the offline sync API (SYNC)" />
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="flex-[1_1_340px]" title="Waiting for a decision · 3">
          {syncConflicts.map((c, i) => (
            <div key={c.title} className={`flex items-start justify-between gap-3 border-t border-line-soft px-[18px] py-3 ${i === 0 ? "bg-accent-soft" : ""}`}>
              <div><strong className="text-sm font-semibold">{c.title}</strong><div className="text-xs text-muted">{c.sub}</div></div>
              <Badge tone={c.tone}>{c.kind}</Badge>
            </div>
          ))}
        </Card>
        <Card className="min-w-0 flex-[999_1_560px]">
          <div className="text-xs text-muted">Mobile plant pre-use · Rosebank Block B · Johan van Wyk</div>
          <h2 className="mt-0.5 font-head text-lg font-semibold">Reversing alarm working?</h2>
          <p className="mt-1 text-[13px] text-muted">
            Johan answered on two phones. The server keeps the newest by the phone&apos;s clock, so the older answer was refused.
          </p>
          <div className="mt-3.5 flex flex-wrap gap-3.5">
            <Side title="Kept on the server" tag="Current" tone="ok" result={<><Badge tone="bad">Fail</Badge><Badge tone="high">high</Badge></>} comment="“Alarm not sounding on TLB 2.”" meta="Phone clock 09:30 · tablet SN-0388 · 1 photo" />
            <Side title="Refused (older)" tag="Not saved" tone="neutral" result={<Badge tone="ok">Pass</Badge>} comment="No comment." meta="Phone clock 09:12 · phone SN-0412 · no photo" />
          </div>
          <p className="mt-3.5 rounded-lg bg-bg px-3 py-2 text-[13px] text-muted">
            Only the inspector can change an answer, and only while the inspection is in progress. You can ask Johan to
            check, or dismiss this if the server&apos;s answer is right.
          </p>
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" disabled className={buttonClass("primary")}>Keep the server&apos;s answer</button>
            <button type="button" disabled className={buttonClass("secondary")}>Ask Johan to re-check</button>
          </div>
        </Card>
      </div>
    </>
  );
}
