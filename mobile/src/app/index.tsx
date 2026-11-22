import { useRouter } from "expo-router";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { Button, Card, HeaderButton, Icon, Loading, Muted, Pill, Row, Screen } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { formatDateOnly, formatDay, formatTime, isToday, label, now, sastDay } from "@/lib/format";
import { useSession } from "@/lib/session";
import { useLoad } from "@/lib/use-load";
import type { ActionList, ChecklistCatalog, DeadlineList, Diary, InspectionList, NotificationList, SignInDay, TalkList } from "@/lib/types";
import { C, F } from "@/theme";

// Extras on the Today screen: a failure here shouldn't hide the rest.
const quiet = <T,>(p: Promise<T>) => p.catch(() => null);

export default function Today() {
  const { site, sites, chooseSite, signOut } = useSession();
  const router = useRouter();
  const [picking, setPicking] = useState(false);

  const { data, error, loading, reload } = useLoad(async () => {
    if (!site) return null;
    const today = sastDay(now());
    const [catalog, inspections, actions, deadlines, talks, signIns, diary, unread] = await Promise.all([
      api<ChecklistCatalog>(sitePath(site.id, "/checklists")),
      api<InspectionList>(sitePath(site.id, "/inspections?limit=100")),
      api<ActionList>(sitePath(site.id, "/actions?owner=me")),
      api<DeadlineList>(sitePath(site.id, "/deadlines?state=due_now")),
      quiet(api<TalkList>(sitePath(site.id, `/toolbox-talks?date=${today}`))),
      quiet(api<SignInDay>(sitePath(site.id, `/sign-ins?date=${today}`))),
      // 404 is "no diary yet today".
      quiet(api<Diary>(`/v1/construction/sites/${encodeURIComponent(site.id)}/diary/${today}`)),
      quiet(api<NotificationList>("/v1/notifications?unread=true&limit=200")),
    ]);
    return {
      catalog,
      inspections: inspections.inspections,
      actions: actions.actions,
      callNow: deadlines.deadlines,
      talks: talks?.talks ?? null,
      headcount: signIns?.headcount ?? null,
      diary,
      unread: unread?.notifications.length ?? 0,
    };
  }, [site?.id]);

  const today = (data?.inspections ?? []).filter((i) => isToday(i.started_at) && i.status !== "abandoned");
  // Checks that come round every shift or every day.
  const due = (data?.catalog.checklists ?? []).filter((t) => t.cadence === "per_shift" || t.cadence === "daily");
  const mine = (data?.actions ?? []).filter((a) => a.state === "open" || a.state === "overdue");

  return (
    <Screen
      tab="Today"
      eyebrow={`${formatDay(now())} · Site`}
      title={site?.name ?? "No site"}
      headerRight={
        <>
          <HeaderButton icon="bell" label="Notifications" onPress={() => router.push("/alerts")} dot={(data?.callNow.length ?? 0) > 0 || (data?.unread ?? 0) > 0} />
          {sites.length > 1 && <HeaderButton icon="swap" label="Change site" onPress={() => setPicking(!picking)} />}
        </>
      }
    >
      {picking && (
        <Card title="Change site">
          {sites.map((s) => (
            <Row key={s.id} title={s.name} sub={s.address ?? undefined} right={s.id === site?.id ? <Pill tone="ok">Current</Pill> : undefined} onPress={() => { void chooseSite(s.id); setPicking(false); }} />
          ))}
        </Card>
      )}

      {!site ? (
        <Muted>You haven&apos;t been added to a site yet. Ask an owner or SHE officer to add you.</Muted>
      ) : !data ? (
        <Loading error={error} onRetry={reload} />
      ) : (
        <>
          {data.callNow.map((d) => (
            <Pressable key={d.id} accessibilityRole="button" onPress={() => router.push(`/call/${d.id}`)} style={s.callNow}>
              <Icon name="phone" color={C.white} />
              <View style={s.grow}>
                <Text style={s.callTitle}>Call the inspector now</Text>
                <Text style={s.callSub}>{d.source_ref} · immediate notice not logged</Text>
              </View>
              <Icon name="chevron" color={C.white} />
            </Pressable>
          ))}

          <Card title="Inspections due today">
            {due.length === 0 && <Row title="No daily or per-shift checks" sub="Start any checklist from Inspect" />}
            {due.map((t) => {
              const run = today.find((i) => i.template_key === t.key);
              const sub = `${label(t.cadence)} · ${run?.status === "in_progress" ? `${run.answered_count} of ${run.item_count} answered` : `${t.items.length} items`} · ${t.source_ref}`;
              return run?.status === "completed" ? (
                <Row key={t.key} title={t.title} sub={sub} right={<Pill tone="ok">Done {run.completed_at ? formatTime(run.completed_at) : ""}</Pill>} />
              ) : run ? (
                <Row key={t.key} title={t.title} sub={sub} right={<Pill tone="warn">In progress</Pill>} href={`/inspection/${run.id}`} />
              ) : (
                <Row key={t.key} title={t.title} sub={sub} right={<Pill>Not started</Pill>} href="/inspect" />
              );
            })}
          </Card>

          <Card title="My actions">
            {mine.length === 0 && <Row title="Nothing assigned to you" />}
            {mine.map((a) => (
              <Row
                key={a.id}
                title={a.title}
                sub={`${label(a.severity)} · from ${a.source === "finding" ? "an inspection" : "an incident"}`}
                right={<Pill tone={a.state === "overdue" ? "bad" : "warn"}>{a.state === "overdue" ? "Overdue" : `Due ${formatDateOnly(a.due_on)}`}</Pill>}
                href={`/action/${a.id}`}
              />
            ))}
          </Card>

          <Card>
            <Row
              title="Site sign-in"
              sub="Sign people in and out at the gate"
              right={data.headcount !== null ? <Pill>{data.headcount} today</Pill> : undefined}
              href="/sign-ins"
            />
            <Row
              title="Toolbox talk"
              sub="Sign the team in on this phone"
              right={data.talks ? <Pill tone={data.talks.length ? "ok" : "warn"}>{data.talks.length ? `${data.talks.length} today` : "None yet"}</Pill> : undefined}
              href="/toolbox"
            />
            <Row
              title="Site diary for today"
              sub="Headcount, weather, work done · closes at midnight"
              right={<Pill tone={data.diary ? "ok" : "warn"}>{data.diary ? `Saved ${formatTime(data.diary.current.client_saved_at)}` : "Not written"}</Pill>}
              href="/diary"
            />
            <Row title="Outbox" sub="What's waiting to send" right={<Icon name="outbox" color={C.muted} />} href="/outbox" />
          </Card>
          {loading && <Muted>Refreshing…</Muted>}
        </>
      )}
      <Button title="Sign out" onPress={() => void signOut()} />
    </Screen>
  );
}

const s = StyleSheet.create({
  grow: { flex: 1 },
  callNow: { flexDirection: "row", alignItems: "center", gap: 12, padding: 14, borderRadius: 12, backgroundColor: C.critical, minHeight: 64 },
  callTitle: { fontFamily: F.semi, fontSize: 15, color: C.white },
  callSub: { fontFamily: F.body, fontSize: 13, color: C.white, opacity: 0.9 },
});
