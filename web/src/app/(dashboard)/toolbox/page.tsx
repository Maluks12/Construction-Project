import type { Metadata } from "next";
import Link from "next/link";

import { PhotoButton } from "@/components/photo";
import { Forbidden, NoSite } from "@/components/states";
import { Alert, Badge, Card, EmptyState, Facts, PageHeader, Row, Tabs, cx } from "@/components/ui";
import { getMe, getTalk, listDiaries, listTalks, listToolboxTopics, listWorkers } from "@/lib/data";
import { formatDateOnly, formatTime, formatWhen } from "@/lib/format";
import { can, currentSite, sitePeople } from "@/lib/site";
import type { Talk, Worker } from "@/lib/types";

import { DiaryDetail, weatherText } from "./diary";
import { LinkForm, RetractSignatureForm, UnlinkForm } from "./forms";

export const metadata: Metadata = { title: "Toolbox & diary" };

const one = (v: string | string[] | undefined) => (typeof v === "string" ? v : undefined);
const workerLabel = (w: Worker) => [w.name, w.firm, w.clock_number].filter(Boolean).join(" · ");

export default async function ToolboxPage({ searchParams }: PageProps<"/toolbox">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Toolbox talks & site diary" />;

  const sp = await searchParams;
  const view = sp.view === "diary" ? "diary" : "talks";
  const talkId = one(sp.talk);
  const date = one(sp.date);
  const people = await sitePeople(site.id);

  const header = (
    <>
      <PageHeader
        title="Toolbox talks & site diary"
        description="Talks are given and signed on the phone; the diary is written there too, one per day. Here you read them, and owners and SHE officers tidy up signatures."
      />
      <div className="mb-5 overflow-hidden rounded-[10px] border border-line">
        <Tabs
          tabs={[
            { href: "/toolbox", label: "Toolbox talks", active: view === "talks" },
            { href: "/toolbox?view=diary", label: "Site diary", active: view === "diary" },
          ]}
        />
      </div>
    </>
  );

  if (view === "diary") {
    const diaries = await listDiaries(site.id);
    if (diaries === "forbidden") return <Forbidden title="Site diary" />;
    return (
      <>
        {header}
        {diaries === null ? (
          <Alert tone="info">This site&apos;s regulations aren&apos;t the Construction Regulations, so it has no site diary.</Alert>
        ) : (
          <div className="flex flex-wrap items-start gap-5">
            <Card flush className="min-w-0 flex-[1_1_420px]" title={<span className="text-lg">Diary register</span>} description="The last 60 days written, newest first.">
              {diaries.length === 0 ? (
                <div className="p-4"><EmptyState title="No diary written yet" /></div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="tbl">
                    <thead><tr><th>Day</th><th>Weather</th><th>Headcount</th><th /></tr></thead>
                    <tbody>
                      {diaries.map((d) => (
                        <tr key={d.diary_date} className={cx(d.diary_date === date && "bg-accent-soft")}>
                          <td className="whitespace-nowrap">
                            <Link href={`/toolbox?view=diary&date=${d.diary_date}`} className="link">{formatDateOnly(d.diary_date)}</Link>
                          </td>
                          <td className="text-[13px]">{weatherText(d.weather)}</td>
                          <td>
                            {d.headcount}
                            {d.headcount !== d.headcount_suggested && <div className="text-xs text-warn">sign-ins said {d.headcount_suggested}</div>}
                          </td>
                          <td>
                            <div className="flex flex-wrap gap-1">
                              {d.conflict_revision_ids.length > 0 && <Badge tone="bad">Conflict</Badge>}
                              {d.late && <Badge tone="warn">Late</Badge>}
                              {d.addendum_count > 0 && <Badge>{d.addendum_count} addend{d.addendum_count === 1 ? "um" : "a"}</Badge>}
                              {!d.closed && <Badge tone="ok">Open</Badge>}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
            <div className="flex min-w-0 flex-[1_1_520px] flex-col gap-4">
              {date ? (
                <DiaryDetail siteId={site.id} date={date} who={people.who} />
              ) : (
                <Card title="Pick a day">
                  <p className="text-sm text-muted">Open a day to see what was written, every addendum, and any conflict between phones.</p>
                </Card>
              )}
            </div>
          </div>
        )}
      </>
    );
  }

  const [talks, topics, talk] = await Promise.all([
    listTalks(site.id),
    listToolboxTopics(site.id),
    talkId ? getTalk(site.id, talkId) : null,
  ]);
  if (talks === "forbidden") return <Forbidden title="Toolbox talks" />;

  return (
    <>
      {header}
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[1_1_440px]" title={<span className="text-lg">Talks</span>} description="Newest first. Signatures and photos are added on the phone.">
          {talks.length === 0 ? (
            <div className="p-4"><EmptyState title="No talks yet">They appear as soon as a phone starts one.</EmptyState></div>
          ) : (
            talks.map((t) => (
              <Link
                key={t.id}
                href={`/toolbox?talk=${t.id}`}
                className={cx("block border-t border-line-soft px-[18px] py-3 first:border-t-0 hover:bg-surface-2", t.id === talkId && "bg-accent-soft")}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <strong className="text-sm font-semibold">{t.topic}</strong>
                    <div className="text-xs text-muted">{formatWhen(t.started_at)} · given by {t.presenter_name}</div>
                  </div>
                  <Badge tone={t.ended_at ? "ok" : "warn"}>{t.ended_at ? "Closed" : "Open"}</Badge>
                </div>
                <div className="mt-1 text-xs text-muted">
                  {t.signed_count} signed · {t.photo_count} photo{t.photo_count === 1 ? "" : "s"}
                </div>
              </Link>
            ))
          )}
        </Card>
        <div className="flex min-w-0 flex-[1_1_520px] flex-col gap-4">
          {talk ? (
            <TalkDetail
              talk={talk}
              siteId={site.id}
              who={people.who}
              topicRef={topics === "forbidden" ? undefined : topics.find((x) => x.key === talk.topic_key)?.source_ref}
            />
          ) : talkId ? (
            <Alert tone="info">That talk isn&apos;t on this site.</Alert>
          ) : (
            <Card title="Pick a talk">
              <p className="text-sm text-muted">Open a talk to see who signed, who was on site and didn&apos;t, and its photos.</p>
            </Card>
          )}
        </div>
      </div>
    </>
  );
}

async function TalkDetail({ talk, siteId, who, topicRef }: { talk: Talk; siteId: string; who: (id: string) => string; topicRef?: string }) {
  const [me, canLink] = await Promise.all([getMe(), can("manage_workers")]);
  const workers = canLink ? await listWorkers({ active: "true" }) : [];
  const register = workers === "forbidden" ? [] : workers;
  const byId = new Map(register.map((w) => [w.id, w]));
  const options = register.map((w) => ({ id: w.id, label: workerLabel(w) }));
  const ids = { siteId, talkId: talk.id };

  return (
    <>
      <Card>
        <div className="text-xs text-muted">{formatDateOnly(talk.talk_date)}{topicRef && ` · ${topicRef}`}</div>
        <h2 className="mt-0.5 font-head text-lg font-semibold">{talk.topic}</h2>
        <div className="mt-3">
          <Facts
            min={150}
            items={[
              ["Given by", talk.presenter_name],
              ["Started", formatWhen(talk.started_at)],
              ["Ended", talk.ended_at ? formatTime(talk.ended_at) : "Still open for signatures"],
              ["Recorded by", who(talk.recorded_by)],
              ["Signed", String(talk.signed_count)],
            ]}
          />
        </div>
        {talk.notes && <p className="mt-3 text-sm whitespace-pre-line">{talk.notes}</p>}
        {talk.photos.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-2.5">
            {talk.photos.map((p) => (
              <div key={p.evidence_id}>
                <PhotoButton siteId={siteId} evidenceId={p.evidence_id} label={p.kind === "paper_register" ? "Signed paper register" : talk.topic} />
                <div className="mt-1 text-[11px] text-muted">{p.kind === "paper_register" ? "Paper register" : "Photo"}</div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card title="Signatures" description="In signing order. A withdrawn signature stays, struck through, with the reason.">
        {talk.attendance.length === 0 && <p className="text-sm text-muted">Nobody has signed yet.</p>}
        {talk.attendance.map((a) => {
          const linked = a.worker_id ? byId.get(a.worker_id) : undefined;
          const canRetract = !a.retraction && (canLink || a.recorded_by === me.user_id);
          return (
            <Row
              key={a.id}
              aside={<span className="text-xs text-muted">{formatTime(a.client_signed_at)} · {a.method === "paper" ? "paper" : "tap"}</span>}
            >
              <div className={cx(a.retraction && "line-through opacity-70")}>
                <strong className="font-semibold">{a.name}</strong>
                <span className="text-muted">{[a.firm, a.clock_number].filter(Boolean).map((x) => ` · ${x}`).join("")}</span>
              </div>
              {a.retraction ? (
                <div className="text-xs text-muted">
                  Withdrawn by {who(a.retraction.retracted_by)}: “{a.retraction.reason}”
                </div>
              ) : (
                <div className="text-xs text-muted">
                  {a.worker_id ? `Register: ${linked ? workerLabel(linked) : "a worker"}` : "Not matched to the register"} · recorded by {who(a.recorded_by)}
                </div>
              )}
              {canLink && !a.retraction && (
                <details className="mt-1">
                  <summary className="link cursor-pointer text-xs">{a.worker_id ? "Different person?" : "Link to a register worker"}</summary>
                  {a.worker_id ? (
                    <UnlinkForm {...ids} attendanceId={a.id} />
                  ) : (
                    <LinkForm {...ids} attendanceId={a.id} workers={options} />
                  )}
                </details>
              )}
              {canRetract && (
                <details className="mt-1">
                  <summary className="link cursor-pointer text-xs">Signed by mistake?</summary>
                  <RetractSignatureForm {...ids} attendanceId={a.id} />
                </details>
              )}
            </Row>
          );
        })}
      </Card>

      <Card title="On site but didn't sign" description="Signed in when the talk started (or later that day) and not on the list above: absent from the talk or refused. Nothing more is inferred.">
        {talk.not_signed.length === 0 ? (
          <p className="text-sm text-muted">Nobody.</p>
        ) : (
          talk.not_signed.map((n) => (
            <Row key={n.sign_in_id}>
              <strong className="font-semibold">{n.name}</strong>
              <span className="text-muted">{[n.firm, n.clock_number].filter(Boolean).map((x) => ` · ${x}`).join("")}</span>
            </Row>
          ))
        )}
      </Card>
    </>
  );
}
