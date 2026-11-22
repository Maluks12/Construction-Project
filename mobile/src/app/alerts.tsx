import { useRouter, type Href } from "expo-router";
import { useState } from "react";

import { Card, Loading, Muted, Notice, Pill, Row, Screen } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { formatDateOnly, formatWhen, label } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { DeadlineList, Notification, NotificationList } from "@/lib/types";
import type { Tone } from "@/theme";

// What a reminder opens on the phone. Close-outs, expiring records and audit
// packs are dealt with on the dashboard.
function target(n: Notification): Href | null {
  if (n.subject_type === "deadline") return `/call/${n.subject_id}`;
  if (n.subject_type === "action") return `/action/${n.subject_id}`;
  return null;
}

function tone(n: Notification): Tone {
  if (n.kind === "integrity_alarm") return "critical";
  if (n.subject_state === "superseded") return "neutral";
  if (n.threshold === "overdue" || n.threshold === "forthwith" || n.threshold.startsWith("escalation")) return "bad";
  return "warn";
}

// Live legal deadlines on this site first (they can't be muted), then the
// inbox the server keeps for this person (docs/api/notifications.md): the
// same reminders that go by email, read or not.
export default function Alerts() {
  const { site, sites, chooseSite } = useSession();
  const router = useRouter();
  const [error, setError] = useState("");
  const { data, error: loadError, reload } = useLoad(async () => {
    const [deadlines, inbox] = await Promise.all([
      site ? api<DeadlineList>(sitePath(site.id, "/deadlines")) : null,
      api<NotificationList>("/v1/notifications?limit=50"),
    ]);
    return {
      deadlines: (deadlines?.deadlines ?? []).filter((d) => d.state === "due_now" || d.state === "overdue" || d.state === "open"),
      inbox: inbox.notifications,
    };
  }, [site?.id]);

  async function open(n: Notification) {
    setError("");
    try {
      if (!n.read_at) await api<Notification>(`/v1/notifications/${n.id}/read`, { method: "POST" });
      const to = target(n);
      if (!to) return void reload();
      // Records are per site: switch to the reminder's site first.
      if (n.site_id && n.site_id !== site?.id && sites.some((s) => s.id === n.site_id)) await chooseSite(n.site_id);
      router.push(to);
    } catch (e) {
      setError(message(e));
    }
  }

  const unread = data?.inbox.filter((n) => !n.read_at) ?? [];
  const read = data?.inbox.filter((n) => n.read_at) ?? [];

  return (
    <Screen back title="Notifications" sub={site?.name}>
      {!data ? (
        <Loading error={loadError} onRetry={reload} />
      ) : (
        <>
          {error ? <Notice tone="bad">{error}</Notice> : null}
          {data.deadlines.length > 0 && (
            <Card title="Legal deadlines on this site">
              {data.deadlines.map((d) => (
                <Row
                  key={d.id}
                  title={d.state === "due_now" ? "Call the inspector now" : d.label}
                  sub={`${d.source_ref} · ${d.state === "due_now" ? "immediate notice not logged yet" : d.due_on ? `by the end of ${formatDateOnly(d.due_on)}` : `before ${formatWhen(d.due_at)}`}`}
                  right={<Pill tone={d.state === "due_now" ? "now" : d.state === "overdue" ? "bad" : "warn"}>{d.state === "due_now" ? "Now" : d.state === "overdue" ? "Overdue" : "Open"}</Pill>}
                  href={`/call/${d.id}`}
                />
              ))}
            </Card>
          )}
          {data.inbox.some((n) => n.kind === "integrity_alarm" && !n.read_at) && (
            <Notice tone="bad">
              Integrity alarm: a stored photo or file no longer matches the seal taken when it arrived. Tell an owner or
              SHE officer now.
            </Notice>
          )}
          <Card title={`Unread · ${unread.length}`}>
            {unread.length === 0 && <Row title="Nothing new" />}
            {unread.map((n) => (
              <Row
                key={n.id}
                title={n.title}
                sub={`${label(n.threshold)}${n.due_on ? ` · due ${formatDateOnly(n.due_on)}` : ""} · ${formatWhen(n.created_at)}${target(n) ? "" : " · open on the dashboard"}`}
                right={<Pill tone={tone(n)}>{n.subject_state === "superseded" ? "Done" : "New"}</Pill>}
                onPress={() => void open(n)}
              />
            ))}
          </Card>
          {read.length > 0 && (
            <Card title="Earlier">
              {read.map((n) => (
                <Row
                  key={n.id}
                  title={n.title}
                  sub={`${label(n.threshold)} · ${formatWhen(n.created_at)}`}
                  right={n.subject_state === "superseded" ? <Pill>Done</Pill> : undefined}
                  onPress={target(n) ? () => void open(n) : undefined}
                />
              ))}
            </Card>
          )}
          <Muted>Reminders also go by email. Call-now and legal deadline alerts can&apos;t be muted.</Muted>
        </>
      )}
    </Screen>
  );
}
