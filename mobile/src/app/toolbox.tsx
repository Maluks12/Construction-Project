import * as Crypto from "expo-crypto";
import { useRouter } from "expo-router";
import { useState } from "react";
import { StyleSheet, View } from "react-native";

import { Button, Card, Chip, Field, H, Loading, Muted, Notice, Pill, Row, Screen, T } from "@/components/ui";
import { api, sitePath } from "@/lib/api";
import { formatTime, formatWhen, now, sastDay } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { Talk, TalkList, TalkRequest, ToolboxTopicList } from "@/lib/types";
import { F } from "@/theme";

// Today's toolbox talks on this site, and starting a new one
// (docs/api/toolbox.md). People sign on the talk's own screen.
export default function Toolbox() {
  const { site, me } = useSession();
  const router = useRouter();
  const [topicKey, setTopicKey] = useState<string | null>(null);
  const [ownTopic, setOwnTopic] = useState("");
  const [presenter, setPresenter] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Made up once per new talk, so pressing Start twice starts one talk.
  const [talkId, setTalkId] = useState(() => Crypto.randomUUID());

  const { data, error: loadError, reload } = useLoad(async () => {
    if (!site) return null;
    const today = sastDay(now());
    const [talks, recent, topics] = await Promise.all([
      api<TalkList>(sitePath(site.id, `/toolbox-talks?date=${today}`)),
      api<TalkList>(sitePath(site.id, "/toolbox-talks?limit=10")),
      api<ToolboxTopicList>(sitePath(site.id, "/toolbox-topics")),
    ]);
    return {
      today: talks.talks,
      earlier: recent.talks.filter((t) => t.talk_date !== today),
      topics: topics.topics,
    };
  }, [site?.id]);

  async function start() {
    if (!site) return;
    const name = (presenter || me?.full_name || "").trim();
    if (!topicKey && !ownTopic.trim()) return setError("Pick a topic, or write your own.");
    if (!name) return setError("Write who is giving the talk.");
    setBusy(true);
    setError("");
    try {
      const body: TalkRequest = {
        topic_key: topicKey,
        topic: topicKey ? null : ownTopic.trim(),
        presenter_name: name,
        started_at: now(),
      };
      await api<Talk>(sitePath(site.id, `/toolbox-talks/${talkId}`), { method: "PUT", body });
      const id = talkId;
      setTalkId(Crypto.randomUUID());
      setTopicKey(null);
      setOwnTopic("");
      router.push(`/talk/${id}`);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Screen tab="Toolbox" title="Toolbox talks" sub={site?.name}>
      {!site ? (
        <Muted>You haven&apos;t been added to a site yet.</Muted>
      ) : !data ? (
        <Loading error={loadError} onRetry={reload} />
      ) : (
        <>
          <Card title="Today">
            {data.today.length === 0 && <Row title="No talk yet today" sub="Start one below" />}
            {data.today.map((t) => (
              <Row
                key={t.id}
                title={t.topic}
                sub={`${formatTime(t.started_at)} · ${t.presenter_name} · ${t.signed_count} signed`}
                right={<Pill tone={t.ended_at ? "ok" : "warn"}>{t.ended_at ? "Closed" : "Signing"}</Pill>}
                href={`/talk/${t.id}`}
              />
            ))}
          </Card>

          <H>Start a talk</H>
          <View style={s.gap}>
            <T style={s.label}>Topic</T>
            <View style={s.chips}>
              {data.topics.map((t) => (
                <Chip key={t.key} title={t.title} on={topicKey === t.key} onPress={() => setTopicKey(topicKey === t.key ? null : t.key)} />
              ))}
            </View>
          </View>
          {!topicKey && <Field label="Or your own topic" value={ownTopic} onChangeText={setOwnTopic} maxLength={200} />}
          <Field
            label="Given by"
            value={presenter}
            onChangeText={setPresenter}
            placeholder={me?.full_name ?? "Name"}
            maxLength={200}
            hint="Left empty, it's you."
          />
          <Button title="Start the talk now" variant="primary" busy={busy} onPress={start} />
          {error ? <Notice tone="bad">{error}</Notice> : null}

          {data.earlier.length > 0 && (
            <Card title="Earlier">
              {data.earlier.map((t) => (
                <Row key={t.id} title={t.topic} sub={`${formatWhen(t.started_at)} · ${t.signed_count} signed`} href={`/talk/${t.id}`} />
              ))}
            </Card>
          )}
        </>
      )}
    </Screen>
  );
}

const s = StyleSheet.create({
  gap: { gap: 8 },
  label: { fontFamily: F.semi, fontSize: 14 },
  chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
});
