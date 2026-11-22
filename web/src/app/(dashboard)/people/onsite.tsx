// A site's day register and roster (docs/api/attendance.md). People sign in
// on the phone at the gate; here a lead sees who was on site, who was
// expected and didn't come, overlaps with other sites, and keeps the roster.

import Link from "next/link";

import { PhotoButton } from "@/components/photo";
import { Badge, Card, Input, Row, buttonClass, cx } from "@/components/ui";
import { getMe, getSignInDay, listRoster, listWorkers } from "@/lib/data";
import { formatDateOnly, formatTime, formatWhen, label, todaySAST } from "@/lib/format";
import type { Capability, Site } from "@/lib/types";

import { RetractSignInForm, RosterAddForm, RosterRemoveForm } from "./forms";

export async function OnSiteTab({
  siteId,
  date,
  sites,
  caps,
  who,
}: {
  siteId: string;
  date?: string;
  sites: Site[];
  caps: Set<Capability>;
  who: (id: string) => string;
}) {
  const day = date ?? todaySAST();
  const capture = caps.has("capture_records");
  const [register, roster, me, workers] = await Promise.all([
    getSignInDay(siteId, day),
    listRoster(siteId),
    getMe(),
    capture ? listWorkers({ active: "true" }) : [],
  ]);
  if (register === "forbidden" || roster === "forbidden") return null;
  const siteName = new Map(sites.map((s) => [s.id, s.name]));
  const overlaps = new Map(register.overlaps.map((o) => [o.sign_in_id, o]));
  const onRoster = new Set(roster.map((r) => r.worker_id));
  const addable = (workers === "forbidden" ? [] : workers)
    .filter((w) => !onRoster.has(w.id))
    .map((w) => ({ id: w.id, label: [w.name, w.firm, w.clock_number].filter(Boolean).join(" · ") }));

  return (
    <div className="flex flex-wrap items-start gap-5">
      <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
        <Card
          flush
          title={<span className="text-lg">Signed in · {formatDateOnly(register.date)}</span>}
          description={`Headcount ${register.headcount}: distinct people who signed in, plus anyone who only signed a toolbox talk.`}
          actions={
            <form className="flex items-end gap-2">
              <input type="hidden" name="tab" value="onsite" />
              <Input name="date" type="date" defaultValue={day} className="w-auto" aria-label="Day" />
              <button type="submit" className={buttonClass("secondary", "sm")}>Show</button>
            </form>
          }
        >
          {register.sign_ins.length === 0 ? (
            <p className="px-[18px] pb-4 text-sm text-muted">Nobody signed in on this day.</p>
          ) : (
            register.sign_ins.map((s) => {
              const overlap = overlaps.get(s.id);
              const canRetract = !s.retraction && (caps.has("manage_workers") || s.recorded_by === me.user_id);
              return (
                <div key={s.id} className={cx("border-t border-line-soft px-[18px] py-3 text-sm", s.retraction && "opacity-60")}>
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="min-w-0">
                      <Link href={`/people?worker=${s.worker_id}`} className={cx("link", s.retraction && "line-through")}>{s.name}</Link>
                      <span className="text-muted">{[s.firm, s.clock_number].filter(Boolean).map((x) => ` · ${x}`).join("")}</span>
                      <div className="text-xs text-muted">
                        In {formatTime(s.client_signed_in_at)}
                        {s.sign_out
                          ? ` · out ${formatTime(s.sign_out.client_signed_out_at)}`
                          : s.lapsed
                            ? ` · no sign-out (lapsed ${formatWhen(s.ends_at)})`
                            : " · on site"}
                        {` · ${s.method === "paper" ? "paper sheet" : "tap"} · recorded by ${who(s.recorded_by)}`}
                      </div>
                    </div>
                    <div className="flex flex-wrap justify-end gap-1">
                      {s.warnings.map((w) => <Badge key={w} tone="bad">{label(w)}</Badge>)}
                      {s.matched_existing && <Badge>Matched an existing worker</Badge>}
                      {s.received_after_close && <Badge tone="warn">Arrived after the day closed</Badge>}
                    </div>
                  </div>
                  {overlap && (
                    <p className="mt-1 text-xs text-warn">
                      Also signed in on {siteName.get(overlap.other_site_id) ?? "another site"} at the same time.
                    </p>
                  )}
                  {s.retraction && <p className="mt-1 text-xs text-muted">Retracted by {who(s.retraction.retracted_by)}: “{s.retraction.reason}”</p>}
                  {canRetract && (
                    <details className="mt-1">
                      <summary className="link cursor-pointer text-xs">Wrong person or a mistake?</summary>
                      <RetractSignInForm siteId={siteId} signInId={s.id} />
                    </details>
                  )}
                </div>
              );
            })
          )}
        </Card>

        <Card title={`Expected and didn't sign in · ${register.absent.length}`} description="The roster as it stood that day. Shown, not counted against the site.">
          {register.absent.length === 0 ? (
            <p className="text-sm text-muted">Nobody.</p>
          ) : (
            register.absent.map((a) => (
              <Row key={a.id}>
                <Link href={`/people?worker=${a.worker_id}`} className="link">{a.name}</Link>
                <span className="text-muted">{[a.firm, a.clock_number].filter(Boolean).map((x) => ` · ${x}`).join("")}</span>
              </Row>
            ))
          )}
        </Card>

        {register.sheets.length > 0 && (
          <Card title="Paper sign-in sheets">
            <div className="flex flex-wrap gap-2.5">
              {register.sheets.map((s) => <PhotoButton key={s.evidence_id} siteId={siteId} evidenceId={s.evidence_id} label="Signed sign-in sheet" />)}
            </div>
          </Card>
        )}
      </div>

      <Card className="min-w-0 flex-[1_1_380px]" title="Roster" description="Who is expected on this site. A sign-in adds someone; an entry added that way lapses after 14 days without one. Entries added by hand stay until removed.">
        {roster.length === 0 && <p className="text-sm text-muted">Nobody on the roster yet.</p>}
        {roster.map((r) => (
          <Row key={r.id} aside={<Badge tone={r.expected ? "ok" : "neutral"}>{r.expected ? "Expected" : "Lapsed"}</Badge>}>
            <Link href={`/people?worker=${r.worker_id}`} className="link">{r.name}</Link>
            <div className="text-xs text-muted">
              {r.added_via === "manual" ? "Added by hand" : "Added by a sign-in"}
              {r.last_sign_in_on ? ` · last signed in ${formatDateOnly(r.last_sign_in_on)}` : " · never signed in here"}
            </div>
            {capture && (
              <details className="mt-1">
                <summary className="link cursor-pointer text-xs">Remove from the roster</summary>
                <RosterRemoveForm siteId={siteId} workerId={r.worker_id} />
              </details>
            )}
          </Row>
        ))}
        {capture && (
          <div className="mt-3 border-t border-line-soft pt-3">
            <RosterAddForm siteId={siteId} workers={addable} />
          </div>
        )}
      </Card>
    </div>
  );
}
