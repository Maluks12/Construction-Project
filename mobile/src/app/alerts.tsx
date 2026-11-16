import { Card, Loading, Muted, Pill, Row, Screen } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { formatDateOnly, formatWhen } from "@/lib/format";
import { useSession } from "@/lib/session";
import { useLoad } from "@/lib/use-load";
import type { ActionList, DeadlineList } from "@/lib/types";

// Built from what is open on the site right now: legal deadlines not met
// and this person's overdue actions. Push notifications and a read/unread
// history arrive with reminders (M11).
export default function Alerts() {
  const { site } = useSession();
  const { data, error, reload } = useLoad(async () => {
    if (!site) return null;
    const [deadlines, actions] = await Promise.all([
      api<DeadlineList>(sitePath(site.id, "/deadlines")),
      api<ActionList>(sitePath(site.id, "/actions?owner=me&state=overdue")),
    ]);
    return { deadlines: deadlines.deadlines.filter((d) => d.state === "due_now" || d.state === "overdue" || d.state === "open"), actions: actions.actions };
  }, [site?.id]);

  return (
    <Screen back title="Notifications" sub={site?.name}>
      {!data ? (
        <Loading error={error} onRetry={reload} />
      ) : (
        <>
          {data.deadlines.length + data.actions.length === 0 && <Muted>Nothing needs you right now.</Muted>}
          {data.deadlines.length > 0 && (
            <Card title="Legal deadlines">
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
          {data.actions.length > 0 && (
            <Card title="Your overdue actions">
              {data.actions.map((a) => (
                <Row key={a.id} title={a.title} sub={`Was due ${formatDateOnly(a.due_on)}`} right={<Pill tone="bad">Overdue</Pill>} href={`/action/${a.id}`} />
              ))}
            </Card>
          )}
          <Muted>Call-now and legal deadline alerts can&apos;t be muted.</Muted>
        </>
      )}
    </Screen>
  );
}
