import * as Crypto from "expo-crypto";
import { useState } from "react";
import { StyleSheet, View } from "react-native";

import { Button, Card, Field, H, Loading, Muted, Notice, Pill, Row, Screen } from "@/components/ui";
import { ApiError, api, sitePath } from "@/lib/api";
import { formatTime, label, now, sastDay } from "@/lib/format";
import { useSession } from "@/lib/session";
import { message, useLoad } from "@/lib/use-load";
import type { RetractRequest, SignIn, SignInDay, SignInRequest, SignOutRequest, Worker, WorkerList } from "@/lib/types";

const who = (p: { name: string; firm: string | null; clock_number: string | null }) =>
  [p.name, p.firm, p.clock_number].filter(Boolean).join(" · ");

// The gate: sign people in and out of this site (docs/api/attendance.md).
// Every sign-in is a register worker; someone new is added on the spot.
// Warnings (no valid medical, an expired competency) are shown, never a
// block: the sign-in is recorded either way.
export default function SignIns() {
  const { site } = useSession();
  const [q, setQ] = useState("");
  const [results, setResults] = useState<Worker[] | null>(null);
  const [name, setName] = useState("");
  const [firm, setFirm] = useState("");
  const [clock, setClock] = useState("");
  const [last, setLast] = useState<SignIn | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const today = sastDay(now());
  const { data: day, error: loadError, reload } = useLoad(
    async () => (site ? api<SignInDay>(sitePath(site.id, `/sign-ins?date=${today}`)) : null),
    [site?.id, today],
  );
  if (!site) return <Screen back title="Site sign-in"><Muted>You haven&apos;t been added to a site yet.</Muted></Screen>;
  if (!day) return <Screen back title="Site sign-in"><Loading error={loadError} onRetry={reload} /></Screen>;

  const onSite = day.sign_ins.filter((s) => !s.retraction && !s.sign_out && !s.lapsed);
  const left = day.sign_ins.filter((s) => s.retraction || s.sign_out || s.lapsed);

  async function run(key: string, fn: () => Promise<void>) {
    setBusy(key);
    setError("");
    try {
      await fn();
      await reload();
    } catch (e) {
      // 409 AlreadySignedIn: another phone did it, or they never signed out.
      // Show it and drop this one; retrying would only repeat the 409.
      if (e instanceof ApiError && e.status === 409 && typeof e.body.sign_in_id === "string") {
        setError("Already signed in on this site. Check the list below.");
      } else {
        setError(message(e));
      }
    } finally {
      setBusy("");
    }
  }

  // A tap on a register worker, or someone new with an id made up here.
  function signIn(workerId: string, newWorker?: SignInRequest["new_worker"]) {
    return run(`in:${workerId}`, async () => {
      const body: SignInRequest = { worker_id: workerId, new_worker: newWorker ?? null, method: "tap", client_signed_in_at: now() };
      const s = await api<SignIn>(sitePath(site!.id, `/sign-ins/${Crypto.randomUUID()}`), { method: "PUT", body });
      setLast(s);
      setResults(null);
      setQ("");
    });
  }

  async function search() {
    if (!q.trim()) return setResults(null);
    setBusy("search");
    setError("");
    try {
      setResults((await api<WorkerList>(`/v1/workers?active=true&limit=50&q=${encodeURIComponent(q.trim())}`)).workers);
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy("");
    }
  }

  async function addNew() {
    if (!name.trim()) return setError("Write the person's name.");
    await signIn(Crypto.randomUUID(), { name: name.trim(), firm: firm.trim() || null, clock_number: clock.trim() || null });
    setName("");
    setFirm("");
    setClock("");
  }

  const signOut = (s: SignIn) =>
    run(`out:${s.id}`, async () => {
      const body: SignOutRequest = { method: "tap", client_signed_out_at: now() };
      await api<SignIn>(sitePath(site.id, `/sign-ins/${s.id}/sign-out`), { method: "POST", body });
    });

  // "Different person": the match was someone else on the register. Retract
  // it; sign them in again as new, with a clock number to tell them apart.
  const differentPerson = (s: SignIn) =>
    run(`retract:${s.id}`, async () => {
      const body: RetractRequest = { reason: "Different person" };
      await api<SignIn>(sitePath(site.id, `/sign-ins/${s.id}/retract`), { method: "POST", body });
      setLast(null);
      setName(s.name);
      setFirm(s.firm ?? "");
      setError("Signed out the wrong match. Add them below as new, with their clock number.");
    });

  return (
    <Screen back title="Site sign-in" sub={`${site.name} · ${day.headcount} today`}>
      {last && (
        <Card style={s.pad}>
          <H>{last.name} is signed in</H>
          <Muted>{who(last)} · {formatTime(last.client_signed_in_at)}</Muted>
          {last.warnings.map((w) => <Notice key={w} tone="bad">{label(w)}: recorded anyway. Tell the SHE officer.</Notice>)}
          {last.matched_existing && (
            <>
              <Notice tone="warn">Matched someone already on the register with the same name, firm and clock number.</Notice>
              <Button title="Different person" busy={busy === `retract:${last.id}`} disabled={busy !== ""} onPress={() => void differentPerson(last)} />
            </>
          )}
        </Card>
      )}
      {error ? <Notice tone="bad">{error}</Notice> : null}

      {day.absent.length > 0 && (
        <Card title="Expected, not signed in yet">
          {day.absent.map((a) => (
            <Row
              key={a.id}
              title={a.name}
              sub={[a.firm, a.clock_number].filter(Boolean).join(" · ") || undefined}
              right={<Pill tone="warn">{busy === `in:${a.worker_id}` ? "Signing in…" : "Tap to sign in"}</Pill>}
              onPress={busy ? undefined : () => void signIn(a.worker_id)}
            />
          ))}
        </Card>
      )}

      <View style={s.gap}>
        <H>Find on the register</H>
        <Field label="Name, firm or clock number" value={q} onChangeText={setQ} onSubmitEditing={() => void search()} returnKeyType="search" />
        <Button title="Search" busy={busy === "search"} disabled={busy !== ""} onPress={() => void search()} />
      </View>
      {results && (
        <Card title={results.length ? "Tap to sign in" : "Nobody found"}>
          {results.map((w) => (
            <Row key={w.id} title={w.name} sub={[w.firm, w.clock_number, w.trade].filter(Boolean).join(" · ") || undefined} onPress={busy ? undefined : () => void signIn(w.id)} />
          ))}
        </Card>
      )}

      <View style={s.gap}>
        <H>Someone new</H>
        <Field label="Name" value={name} onChangeText={setName} maxLength={200} hint="As they write it. No ID numbers." />
        <Field label="Firm (optional)" value={firm} onChangeText={setFirm} maxLength={200} />
        <Field label="Clock number (optional)" value={clock} onChangeText={setClock} maxLength={50} />
        <Button title="Add and sign in" variant="primary" busy={busy.startsWith("in:") && !!name} disabled={busy !== ""} onPress={() => void addNew()} />
      </View>

      <Card title={`On site · ${onSite.length}`}>
        {onSite.length === 0 && <Row title="Nobody signed in" />}
        {onSite.map((x) => (
          <Row
            key={x.id}
            title={x.name}
            sub={`In ${formatTime(x.client_signed_in_at)}${x.warnings.length ? ` · ${x.warnings.map(label).join(", ")}` : ""}`}
            right={<Pill>{busy === `out:${x.id}` ? "Signing out…" : "Sign out"}</Pill>}
            onPress={busy ? undefined : () => void signOut(x)}
          />
        ))}
      </Card>
      {left.length > 0 && (
        <Card title="Left or ended">
          {left.map((x) => (
            <Row
              key={x.id}
              title={x.name}
              sub={
                x.retraction
                  ? `Retracted: ${x.retraction.reason}`
                  : x.sign_out
                    ? `${formatTime(x.client_signed_in_at)} to ${formatTime(x.sign_out.client_signed_out_at)}`
                    : `In ${formatTime(x.client_signed_in_at)} · no sign-out`
              }
            />
          ))}
        </Card>
      )}
      <Muted>Sign-out is optional: a sign-in ends after 16 hours, or at the person&apos;s next sign-in.</Muted>
    </Screen>
  );
}

const s = StyleSheet.create({
  gap: { gap: 8 },
  pad: { padding: 14, gap: 8 },
});
