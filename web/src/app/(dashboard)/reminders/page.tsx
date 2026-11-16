import type { Metadata } from "next";

import { Badge, Card, PageHeader, SampleBanner } from "@/components/ui";
import { reminders } from "@/lib/sample";

export const metadata: Metadata = { title: "Reminders" };

export default function RemindersPage() {
  return (
    <>
      <PageHeader
        title="Reminders"
        description="Who gets told what, how, and when. Legal deadlines can't be switched off; you only choose extra people and channels."
      />
      <SampleBanner needs="reminders and the worker (M11)" />
      <div className="flex flex-col gap-5">
        <Card flush title={<span className="text-lg">Rules</span>}>
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>When</th><th>Who</th><th>Channels</th><th>Timing</th></tr></thead>
              <tbody>
                {reminders.rules.map((r) => (
                  <tr key={r.when}>
                    <td>
                      <strong className="font-semibold">{r.when}</strong>
                      {r.locked && <div className="mt-1"><Badge>Always on (legal deadline)</Badge></div>}
                    </td>
                    <td>{r.who}</td>
                    <td>
                      <div className="flex gap-3.5">
                        {(["push", "sms", "email"] as const).map((ch) => (
                          <label key={ch} className="flex items-center gap-1.5 text-[13px]">
                            <input type="checkbox" disabled defaultChecked={r[ch]} /> {ch === "sms" ? "SMS" : ch === "push" ? "Push" : "Email"}
                          </label>
                        ))}
                      </div>
                    </td>
                    <td>{r.timing}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
        <Card flush title={<span className="text-lg">Recently sent</span>} actions={<span className="text-[13px] text-muted">A failed SMS is retried, then sent by email</span>}>
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>Sent</th><th>To</th><th>Message</th><th>Channel</th><th>Status</th></tr></thead>
              <tbody>
                {reminders.sent.map((s, i) => (
                  <tr key={i}>
                    <td className="whitespace-nowrap">{s.when}</td><td>{s.to}</td><td>{s.msg}</td><td>{s.ch}</td>
                    <td><Badge tone={s.tone}>{s.status}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>
    </>
  );
}
