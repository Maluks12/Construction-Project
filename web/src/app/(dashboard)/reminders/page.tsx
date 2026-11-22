import type { Metadata } from "next";
import Link from "next/link";

import { NoSite } from "@/components/states";
import { Alert, Badge, Card, Facts, PageHeader } from "@/components/ui";
import { getRegimeTypes, listNotifications } from "@/lib/data";
import { formatWhen, label } from "@/lib/format";
import { currentSite, regimeOf } from "@/lib/site";

export const metadata: Metadata = { title: "Reminders" };

const days = (list: number[]) =>
  list.length === 0 ? "—" : list.map((d) => (d === 0 ? "on the day" : `${d} day${d === 1 ? "" : "s"}`)).join(", ");

// Who the server reminds, and when (docs/api/notifications.md). The rules
// are the regime's, not settings: legal deadline reminders can't be turned
// off, and the lead times below come from GET /v1/regimes/:code/types.
export default async function RemindersPage() {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Reminders" />;

  const regime = await regimeOf(site.id);
  const [types, recent] = await Promise.all([
    regime ? getRegimeTypes(regime.code) : null,
    listNotifications(false, 20),
  ]);
  const r = types && types !== "forbidden" ? types.reminders : null;

  const rules: [string, string, string][] = [
    ["An incident deadline", "Whoever submitted the incident, owners, SHE officers, the site's supervisors", r ? `Forthwith at once; others ${days(r.deadline_days_before)} before, then when overdue` : "—"],
    ["A notice before work starts (Reg 3, Reg 4)", "Whoever recorded the work, owners, SHE officers, the site's supervisors", r ? `${days(r.notification_days_before)} before, then when overdue` : "—"],
    ["A corrective action overdue", "The owner and raiser, then SHE officers, then owners", r ? `Escalates after ${days(r.action_escalation_days)} overdue` : "—"],
    ["A close-out waiting for verification", "Owners and SHE officers", r ? `After ${r.verification_stalled_days} days` : "—"],
    ["A competency, medical or appointment expiring", "Owners, SHE officers, the supervisors of sites where the worker is on the roster", r ? `${days(r.worker_expiry_days)} before` : "—"],
    ["A statutory document expiring", "Owners and SHE officers (H&S file documents: the site's supervisors too)", r ? `Vault: ${days(r.company_document_days)} before; H&S file as competencies` : "—"],
    ["An audit pack integrity alarm", "Owners and SHE officers", "At once"],
  ];

  return (
    <>
      <PageHeader
        title="Reminders"
        description="Who gets told what, and when. The rules come from the site's regulations: legal deadline reminders can't be switched off."
      />
      <div className="flex flex-col gap-5">
        <Alert tone="info">
          Reminders arrive in each person&apos;s <Link href="/notifications" className="link">Notifications</Link> and by
          email to anyone with an email address. Push, SMS and WhatsApp aren&apos;t sent yet, and the people and lead
          times can&apos;t be changed per organisation yet.
        </Alert>

        <Card flush title={<span className="text-lg">Rules</span>} description={r ? `Lead times from ${regime?.name} (${r.source_ref}).` : undefined}>
          <div className="overflow-x-auto">
            <table className="tbl">
              <thead><tr><th>When</th><th>Who</th><th>Timing</th></tr></thead>
              <tbody>
                {rules.map(([when, who, timing]) => (
                  <tr key={when}>
                    <td><strong className="font-semibold">{when}</strong></td>
                    <td>{who}</td>
                    <td>{timing}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="px-[18px] py-3 text-xs text-muted">
            Each reminder comes once per record and lead time. A renewal, a met deadline or a closed action stops the rest.
          </p>
        </Card>

        {r && (
          <Card title="Lead times" description="As the regulations set them for this site.">
            <Facts
              min={200}
              items={[
                ["Deadline reminders", days(r.deadline_days_before)],
                ["Notices before work", days(r.notification_days_before)],
                ["Worker records", days(r.worker_expiry_days)],
                ["Company documents", days(r.company_document_days)],
                ["Action escalation", days(r.action_escalation_days)],
                ["Close-out waiting", `${r.verification_stalled_days} days`],
                ["Roster lapses after", `${r.roster_lapse_days} days without a sign-in`],
              ]}
            />
          </Card>
        )}

        <Card flush title={<span className="text-lg">Your recent reminders</span>} actions={<Link href="/notifications?show=all" className="link text-sm">All notifications</Link>}>
          {recent.length === 0 ? (
            <p className="px-[18px] pb-4 text-sm text-muted">None yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead><tr><th>Sent</th><th>Reminder</th><th>When</th><th>Read</th></tr></thead>
                <tbody>
                  {recent.map((n) => (
                    <tr key={n.id}>
                      <td className="whitespace-nowrap">{formatWhen(n.created_at)}</td>
                      <td>{n.title}</td>
                      <td>{label(n.threshold)}</td>
                      <td>{n.read_at ? <Badge tone="ok">Read</Badge> : <Badge tone="warn">Unread</Badge>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>
    </>
  );
}
