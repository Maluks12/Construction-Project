import type { Metadata } from "next";

import { Badge, Card, Facts, PageHeader, SampleBanner, Thumb, buttonClass } from "@/components/ui";
import { toolbox } from "@/lib/sample";

export const metadata: Metadata = { title: "Toolbox & diary" };

export default function ToolboxPage() {
  const { talks, diary } = toolbox;
  return (
    <>
      <PageHeader title="Toolbox talks & site diary" description="Talks are given and signed on the phone. The diary is one entry per day." />
      <SampleBanner needs="toolbox talks and the site diary (M10)" />
      <div className="flex flex-wrap items-start gap-5">
        <Card
          flush
          className="min-w-0 flex-[1_1_480px]"
          title={<span className="text-lg">Toolbox talks</span>}
          actions={<button type="button" disabled className={buttonClass("secondary", "sm")}>Schedule a talk</button>}
        >
          {talks.map((t) => (
            <div key={t.topic} className="border-t border-line-soft px-[18px] py-3.5">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <strong className="text-sm font-semibold">{t.topic}</strong>
                  <div className="text-xs text-muted">{t.when} · given by {t.by}</div>
                </div>
                <Badge tone={t.tone}>{t.state}</Badge>
              </div>
              <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-line-soft">
                <div className="h-1.5 bg-bar" style={{ width: `${Math.round((t.signed / t.present) * 100)}%` }} />
              </div>
              <div className="mt-1 text-xs text-muted">{t.signed} of {t.present} signed</div>
            </div>
          ))}
        </Card>
        <Card className="min-w-0 flex-[1_1_480px]" title={<span className="text-lg">Site diary · {diary.day}</span>}>
          <Facts items={diary.facts} min={130} />
          <h3 className="mt-4 text-[13px] font-semibold">Notes</h3>
          <p className="mt-1 text-sm leading-relaxed">{diary.notes}</p>
          <h3 className="mt-4 mb-1.5 text-[13px] font-semibold">Photos</h3>
          <div className="flex flex-wrap gap-2.5">{[1, 2, 3, 4].map((n) => <Thumb key={n} />)}</div>
          <p className="mt-4 text-xs text-muted">{diary.by}</p>
        </Card>
      </div>
    </>
  );
}
