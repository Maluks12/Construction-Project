import type { Metadata } from "next";

import { markAllNotificationsRead, markNotificationRead, openNotification } from "@/app/actions/notifications";
import { InlineAction } from "@/components/client";
import { Badge, Card, EmptyState, PageHeader, Tabs, buttonClass, cx, type Tone } from "@/components/ui";
import { listNotifications } from "@/lib/data";
import { formatDateOnly, formatWhen, label } from "@/lib/format";
import { currentSite } from "@/lib/site";
import type { Notification, NotificationKind } from "@/lib/types";

export const metadata: Metadata = { title: "Notifications" };

const KIND_LABELS: Record<NotificationKind, string> = {
  incident_deadline: "Incident deadline",
  site_work_deadline: "Notice before work",
  action_overdue: "Overdue action",
  verification_stalled: "Close-out waiting",
  competency_expiry: "Competency expiry",
  medical_expiry: "Medical expiry",
  appointment_expiry: "Appointment expiry",
  document_expiry: "Document expiry",
  integrity_alarm: "Integrity alarm",
};

function tone(n: Notification): Tone {
  if (n.kind === "integrity_alarm") return "critical";
  if (n.subject_state === "superseded") return "neutral";
  if (n.threshold === "overdue" || n.threshold === "forthwith" || n.threshold.startsWith("escalation")) return "bad";
  return "warn";
}

// Where a reminder's record opens (notifications.md, subject_type). Reminders
// stored before REG4 name a deadline's duty, which no page opens by id.
function target(n: Notification): string | null {
  const id = encodeURIComponent(n.subject_id);
  switch (n.subject_type) {
    case "deadline":
      return `/deadlines?deadline=${id}`;
    case "action":
      return `/actions?action=${id}`;
    case "closeout":
      return "/actions?tab=open";
    case "competency":
    case "medical":
      return "/people";
    case "appointment":
      return "/people?tab=appointments";
    case "document":
      return n.site_id ? `/people?tab=file&doc=${id}` : `/organisation?doc=${id}`;
    case "export":
      return `/audit-pack?export=${id}`;
    case "deadline_duty":
      return null;
  }
}

export default async function NotificationsPage({ searchParams }: PageProps<"/notifications">) {
  const { show } = await searchParams;
  const all = show === "all";
  const [list, { sites }] = await Promise.all([listNotifications(!all, 200), currentSite()]);
  const siteName = new Map(sites.map((s) => [s.id, s.name]));
  const unread = list.filter((n) => !n.read_at);

  return (
    <>
      <PageHeader
        title="Notifications"
        description="Reminders the server raises about deadlines, overdue actions and expiring records. The same ones go by email to anyone with an email address."
      >
        {unread.length > 0 && (
          <form action={markAllNotificationsRead}>
            {unread.map((n) => <input key={n.id} type="hidden" name="notification_id" value={n.id} />)}
            <button type="submit" className={buttonClass("secondary")}>Mark all read</button>
          </form>
        )}
      </PageHeader>

      <Card flush>
        <Tabs
          tabs={[
            { href: "/notifications", label: "Unread", active: !all, count: all ? undefined : list.length },
            { href: "/notifications?show=all", label: "All", active: all },
          ]}
        />
        {list.length === 0 ? (
          <div className="p-4">
            <EmptyState title={all ? "No reminders yet" : "Nothing unread"}>
              Legal deadline reminders can&apos;t be switched off; they arrive here and by email.
            </EmptyState>
          </div>
        ) : (
          <ul>
            {list.map((n) => {
              const to = target(n);
              const superseded = n.subject_state === "superseded";
              return (
                <li
                  key={n.id}
                  className={cx(
                    "flex flex-wrap items-start gap-3 border-t border-line-soft px-[18px] py-3.5 first:border-t-0",
                    n.kind === "integrity_alarm" && "bg-danger-soft",
                    superseded && "opacity-60",
                  )}
                >
                  <div className="min-w-0 flex-[1_1_320px]">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge tone={tone(n)}>{KIND_LABELS[n.kind]}</Badge>
                      {!n.read_at && <Badge tone="now">New</Badge>}
                      {superseded && <Badge>Renewed, met or closed since</Badge>}
                    </div>
                    <p className="mt-1.5 text-sm font-semibold">{n.title}</p>
                    <p className="mt-0.5 text-xs text-muted">
                      {label(n.threshold)}
                      {n.due_on ? ` · due ${formatDateOnly(n.due_on)}` : n.due_at ? ` · due ${formatWhen(n.due_at)}` : ""}
                      {n.site_id ? ` · ${siteName.get(n.site_id) ?? "another site"}` : " · organisation"}
                      {` · ${formatWhen(n.created_at)}`}
                    </p>
                    {n.kind === "integrity_alarm" && (
                      <p className="mt-1.5 text-[13px] text-danger">
                        A stored photo or file no longer matches the seal taken when it arrived. The audit pack was
                        stopped for good. Treat this as a security incident.
                      </p>
                    )}
                  </div>
                  <div className="flex shrink-0 flex-wrap items-start gap-2">
                    {to && (
                      <form action={openNotification}>
                        <input type="hidden" name="notification_id" value={n.id} />
                        <input type="hidden" name="site_id" value={n.site_id ?? ""} />
                        <input type="hidden" name="to" value={to} />
                        <button type="submit" className={buttonClass("secondary", "sm")}>Open</button>
                      </form>
                    )}
                    {!n.read_at && (
                      <InlineAction
                        action={markNotificationRead}
                        fields={{ notification_id: n.id }}
                        label="Mark read"
                        variant="ghost"
                      />
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Card>
    </>
  );
}
