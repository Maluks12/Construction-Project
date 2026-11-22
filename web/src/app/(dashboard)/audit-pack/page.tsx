import { randomUUID } from "node:crypto";

import type { Metadata } from "next";
import Link from "next/link";

import { downloadExport } from "@/app/actions/exports";
import { OpenFileButton } from "@/components/open-file";
import { NoSite } from "@/components/states";
import { Alert, Badge, Card, EmptyState, Facts, PageHeader, cx, type Tone } from "@/components/ui";
import { getExport, listExports } from "@/lib/data";
import { formatDateOnly, formatWhen, label, shortId, todaySAST } from "@/lib/format";
import { can, currentSite, sitePeople } from "@/lib/site";
import type { Export, ExportStatus } from "@/lib/types";

import { ExportForm, RefreshWhileBuilding } from "./forms";

export const metadata: Metadata = { title: "Audit pack" };

const TONE: Record<ExportStatus, Tone> = { queued: "neutral", building: "warn", ready: "ok", failed: "bad", expired: "neutral" };

function size(bytes: number | null) {
  if (bytes === null) return "—";
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

export default async function AuditPackPage({ searchParams }: PageProps<"/audit-pack">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Audit pack" />;

  const header = (
    <PageHeader
      title="Audit pack"
      description="One ZIP of this site's records over a period, for an inspector, a principal contractor or a court. Anyone can check a copy later against its SHA-256."
    />
  );
  // Owners and SHE officers only (exports.md); the API answers 403 to others.
  if (!(await can("export_audit_pack"))) {
    return (
      <>
        {header}
        <Alert tone="info">Only owners and SHE officers can export or download audit packs.</Alert>
      </>
    );
  }

  const sp = await searchParams;
  const selectedId = typeof sp.export === "string" ? sp.export : null;
  const [exports, people, selected] = await Promise.all([
    listExports(site.id),
    sitePeople(site.id),
    selectedId ? getExport(site.id, selectedId) : null,
  ]);
  const list = exports === "forbidden" ? [] : exports;
  const building = list.some((e) => e.status === "queued" || e.status === "building");
  const today = todaySAST();

  return (
    <>
      {header}
      <RefreshWhileBuilding active={building} />
      <div className="flex flex-wrap items-start gap-5">
        <div className="flex flex-[1_1_360px] flex-col gap-4">
          <Card title={`New export · ${site.name}`}>
            <ExportForm id={randomUUID()} siteId={site.id} from={`${today.slice(0, 8)}01`} to={today} />
          </Card>
          <Card title="What's in a pack">
            <ul className="list-disc space-y-1.5 pl-5 text-[13px] text-muted">
              <li>
                Statutory documents, appointments, the H&amp;S file, workers with competencies and medicals, toolbox
                talks, inspections (abandoned ones marked), actions and waivers, incidents with their deadlines,
                diaries, the sign-in register, notices before work, and the evidence chain with its head and length.
              </li>
              <li>What the server had received when you queued it. Anything a phone sends later isn&apos;t in it.</li>
              <li>
                <strong className="font-semibold text-ink">Medical certificates and injury details are included.</strong>{" "}
                Every download is logged against the person who downloads it, with every photo and file in the pack.
              </li>
              <li>Packs are kept for 7 days; the record and its SHA-256 stay for good.</li>
            </ul>
          </Card>
        </div>

        <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
          {selected && <ExportDetail e={selected} siteId={site.id} who={people.who} />}
          {selectedId && !selected && <Alert tone="info">That export isn&apos;t on this site.</Alert>}
          <Card flush title={<span className="text-lg">Exports of this site</span>}>
            {list.length === 0 ? (
              <div className="p-4"><EmptyState title="No exports yet" /></div>
            ) : (
              <div className="overflow-x-auto">
                <table className="tbl">
                  <thead><tr><th>Pack</th><th>Period</th><th>Asked by</th><th>Chain</th><th>Status</th></tr></thead>
                  <tbody>
                    {list.map((e) => (
                      <tr key={e.id} className={cx(e.id === selectedId && "bg-accent-soft")}>
                        <td>
                          <Link href={`/audit-pack?export=${e.id}`} className="link">AP-{shortId(e.id)}</Link>
                          <div className="text-xs text-muted">{formatWhen(e.as_of)}</div>
                        </td>
                        <td className="whitespace-nowrap">{formatDateOnly(e.from)} to {formatDateOnly(e.to)}</td>
                        <td>{people.who(e.requested_by)}</td>
                        <td>
                          {e.chain_length === null
                            ? "—"
                            : `${e.chain_intact ? "No break" : "Break found"} · ${e.chain_length.toLocaleString("en-ZA")} entries`}
                        </td>
                        <td>
                          <Badge tone={TONE[e.status]}>{label(e.status)}</Badge>
                          {e.integrity_alarm && <div className="mt-1"><Badge tone="critical">Integrity alarm</Badge></div>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
      </div>
    </>
  );
}

function ExportDetail({ e, siteId, who }: { e: Export; siteId: string; who: (id: string) => string }) {
  return (
    <Card
      title={`AP-${shortId(e.id)} · ${formatDateOnly(e.from)} to ${formatDateOnly(e.to)}`}
      actions={<Badge tone={TONE[e.status]}>{label(e.status)}</Badge>}
    >
      {e.status === "queued" && <p className="text-sm">Waiting for the builder. It starts at least two minutes after it was queued.</p>}
      {e.status === "building" && <p className="text-sm">Building now. A busy site-year can take a few minutes.</p>}
      {e.status === "failed" && (
        <Alert tone={e.integrity_alarm ? "danger" : "warn"}>
          {e.integrity_alarm
            ? `Integrity alarm: a stored original no longer matches its seal. ${e.last_error ?? ""} The pack was stopped for good; treat this as a security incident.`
            : `It failed${e.last_error ? `: ${e.last_error}` : ""}. Queue a new export.`}
        </Alert>
      )}
      {e.status === "expired" && <p className="text-sm">Built, but more than 7 days ago, so the pack is gone. Its SHA-256 below still checks a copy someone holds.</p>}
      <div className="mt-3">
        <Facts
          min={160}
          items={[
            ["Holds what the server had at", formatWhen(e.as_of)],
            ["Asked by", `${who(e.requested_by)} (${label(e.requester_role)})`],
            ["Size", size(e.size_bytes)],
            ["Photos · files", e.evidence_count === null ? "—" : `${e.evidence_count} · ${e.file_count ?? 0}`],
            ["Chain", e.chain_length === null ? "—" : `${e.chain_intact ? "No break found" : "Break found"}, ${e.chain_length.toLocaleString("en-ZA")} entries`],
            ["Health details", e.includes_health ? "Included" : "Left out"],
            ["Expires", e.expires_at ? formatWhen(e.expires_at) : "—"],
          ]}
        />
      </div>
      {e.sha256 && (
        <div className="mt-3">
          <div className="text-xs text-muted">SHA-256 of the ZIP</div>
          <div className="font-mono text-xs break-all">{e.sha256}</div>
        </div>
      )}
      {e.chain_head && (
        <div className="mt-2">
          <div className="text-xs text-muted">Chain head (keep it outside SiteProof)</div>
          <div className="font-mono text-xs break-all">{e.chain_head}</div>
        </div>
      )}
      {e.status === "ready" && (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <OpenFileButton open={downloadExport.bind(null, siteId, e.id)} label="Download" size="md" />
          <span className="text-xs text-muted">The link lasts 5 minutes. Every download is logged.</span>
        </div>
      )}
      <p className="mt-3 text-xs text-muted">
        To check a copy: <code className="font-mono">certutil -hashfile pack.zip SHA256</code> on Windows, or{" "}
        <code className="font-mono">sha256sum pack.zip</code>, and compare with the SHA-256 above.
      </p>
    </Card>
  );
}
