import type { Metadata } from "next";
import Link from "next/link";

import { PhotoButton } from "@/components/photo";
import { Forbidden, NoSite } from "@/components/states";
import { Badge, Card, EmptyState, Facts, PageHeader, Tabs, Thumb, buttonClass, type Tone } from "@/components/ui";
import { listEvidence, verifyChain } from "@/lib/data";
import { formatWhen, label } from "@/lib/format";
import { currentSite, sitePeople } from "@/lib/site";
import type { EvidenceStatus } from "@/lib/types";

export const metadata: Metadata = { title: "Evidence" };

const TONE: Record<EvidenceStatus, Tone> = { verified: "ok", pending: "warn", rejected: "bad" };
const STATUSES: EvidenceStatus[] = ["verified", "pending", "rejected"];

export default async function EvidencePage({ searchParams }: PageProps<"/evidence">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Evidence" />;

  const sp = await searchParams;
  const status = STATUSES.find((s) => s === sp.status);
  const [evidence, people, chain] = await Promise.all([
    listEvidence(site.id, status),
    sitePeople(site.id),
    // The check walks the whole chain, so it only runs when asked for.
    sp.check ? verifyChain(site.id) : null,
  ]);
  if (evidence === "forbidden") return <Forbidden title="Evidence" />;
  const q = status ? `status=${status}&` : "";

  return (
    <>
      <PageHeader
        title="Evidence"
        description="Photos are hashed on the phone, re-checked by the server and sealed into this site's chain. Every view is logged, and so is opening this list."
      />
      <div className="flex flex-wrap items-start gap-5">
        <Card flush className="min-w-0 flex-[999_1_620px]">
          <Tabs
            tabs={[
              { href: "/evidence", label: "Newest", active: !status },
              ...STATUSES.map((s) => ({ href: `/evidence?status=${s}`, label: s === "verified" ? "Sealed" : label(s), active: status === s })),
            ]}
          />
          {evidence.length === 0 ? (
            <div className="p-4"><EmptyState title="No photos here" /></div>
          ) : (
            <div className="grid gap-4 p-[18px] [grid-template-columns:repeat(auto-fill,minmax(150px,1fr))]">
              {evidence.map((e) => (
                <div key={e.id} className="text-[13px]">
                  {e.status === "verified" ? (
                    <PhotoButton siteId={site.id} evidenceId={e.id} label={`Photo taken ${formatWhen(e.client_captured_at)}`} />
                  ) : (
                    <Thumb label="Photo not available" />
                  )}
                  <div className="mt-1.5">
                    <Badge tone={TONE[e.status]}>
                      {e.status === "verified" ? "Sealed" : e.status === "rejected" ? `Rejected: ${label(e.rejected_reason ?? "").toLowerCase()}` : "Pending"}
                    </Badge>
                  </div>
                  <div className="mt-1">{formatWhen(e.client_captured_at)} · {people.who(e.uploaded_by)}</div>
                  <div className="text-xs text-muted">
                    {e.chain ? `seq ${e.chain.seq.toLocaleString("en-ZA")}` : e.status === "pending" ? "not sealed yet" : "never sealed"}
                  </div>
                </div>
              ))}
            </div>
          )}
          <p className="border-t border-line-soft px-[18px] py-3 text-xs text-muted">
            Thumbnails load when you open a photo. Showing the newest {evidence.length}.
          </p>
        </Card>

        <div className="flex flex-[1_1_340px] flex-col gap-4">
          <Card title="Chain check">
            {chain === null ? (
              <p className="text-sm text-muted">Walks every entry of this site&apos;s chain and reports the first break, if any.</p>
            ) : chain === "forbidden" ? (
              <p className="text-sm text-muted">Only owners and SHE officers can run the chain check.</p>
            ) : (
              <>
                <div className="mb-3 flex flex-wrap items-center gap-2">
                  <Badge tone={chain.intact ? "ok" : "bad"}>{chain.intact ? "No break found" : "Break found"}</Badge>
                  <span className="text-[13px] text-muted">Checked {formatWhen(chain.checked_at)}</span>
                </div>
                <Facts
                  min={120}
                  items={[
                    ["Entries", chain.length.toLocaleString("en-ZA")],
                    ["Head (newest entry)", `seq ${chain.head_seq.toLocaleString("en-ZA")}`],
                    ["Head hash", <span key="h" className="font-mono text-xs break-all">{chain.head_hash}</span>],
                    ...(chain.first_break
                      ? ([["First break", `${label(chain.first_break.reason)}${chain.first_break.seq === null ? "" : ` at seq ${chain.first_break.seq}`}`]] as [string, string][])
                      : []),
                  ]}
                />
                <p className="mt-3 text-[13px] text-muted">
                  “No break found” is not “tamper-proof”. Keep the length and head hash somewhere outside SiteProof, so a
                  shortened chain would show.
                </p>
              </>
            )}
            <Link href={`/evidence?${q}check=1`} className={buttonClass("secondary", "sm") + " mt-3"}>
              {chain ? "Check again" : "Run the check"}
            </Link>
          </Card>
          <Card title="Access log" description="Append-only: who viewed, listed or exported which evidence, when and from where.">
            <p className="text-sm text-muted">
              The API records every view, list and export, but has no endpoint to read the log back yet, and audit
              packs don&apos;t include it. That needs a backend change.
            </p>
          </Card>
        </div>
      </div>
    </>
  );
}
