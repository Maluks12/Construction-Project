import { Button, Card, Muted, Notice, Pill, Row, SampleNote, Screen } from "@/components/ui";
import { outbox } from "@/lib/sample";

// What the outbox will look like once the app works offline. Today every
// screen sends straight to the server and tells you at once if it couldn't,
// so nothing waits here.
export default function Outbox() {
  return (
    <Screen back title="Outbox" sub={outbox.status} footer={<Button title="Try sending now" disabled />}>
      <SampleNote needs="offline sync, SYNC" />
      <Card title="Sends first: records">
        {outbox.records.map((r) => <Row key={r.what} title={r.what} sub={r.sub} right={<Pill tone={r.tone}>{r.state}</Pill>} />)}
      </Card>
      <Card title="Then: photos">
        {outbox.photos.map((p) => <Row key={p.what} title={p.what} sub={p.sub} right={<Pill tone={p.tone}>{p.state}</Pill>} />)}
      </Card>
      <Card title="Needs you">
        <Row title={outbox.conflict.title} sub={outbox.conflict.body} right={<Pill tone="bad">Conflict</Pill>} />
      </Card>
      <Notice>Nothing is lost if the app closes. Everything stays on the phone until the server confirms it.</Notice>
      <Muted>Until sync is built, the app needs signal to save.</Muted>
    </Screen>
  );
}
