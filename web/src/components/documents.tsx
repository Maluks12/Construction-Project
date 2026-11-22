// A versioned document register (docs/api/documents.md): the organisation's
// statutory vault, or a site's H&S file. The same view serves both; `scope`
// picks the endpoints.

import { randomUUID } from "node:crypto";

import Link from "next/link";

import { viewDocumentVersion } from "@/app/actions/documents";
import { formatDateOnly, formatWhen, label } from "@/lib/format";
import type { DocumentDetail, DocumentRegister, DocumentScope, DocumentType, ReadinessStatus } from "@/lib/types";

import { AddVersionForm, CreateDocumentForm, RetractDocumentForm } from "./document-forms";
import { OpenFileButton } from "./open-file";
import { Alert, Badge, Card, EmptyState, cx, type Tone } from "./ui";

const READINESS: Record<ReadinessStatus, { text: string; tone: Tone }> = {
  present: { text: "Present", tone: "ok" },
  expired: { text: "Expired", tone: "bad" },
  missing: { text: "Missing", tone: "warn" },
};

function stateTone(state: string): Tone {
  if (state === "valid") return "ok";
  if (state === "expiring") return "warn";
  if (state === "expired" || state === "retracted") return "bad";
  return "neutral";
}

export function DocumentRegisterView({
  scope,
  siteId,
  register,
  types,
  selected,
  hrefFor,
  canWrite,
  who,
}: {
  scope: DocumentScope;
  siteId: string;
  register: DocumentRegister;
  // The regime's document types of this scope.
  types: DocumentType[];
  // The open document, or null.
  selected: DocumentDetail | null;
  hrefFor: (documentId: string) => string;
  canWrite: boolean;
  who: (id: string) => string;
}) {
  const typeLabel = new Map(types.map((t) => [t.key, t.label]));
  return (
    <div className="flex flex-wrap items-start gap-5">
      <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
        <Card title="Readiness" description="For each type the register expects: present (a current version not expired), expired, or missing.">
          {register.readiness.length === 0 && <p className="text-sm text-muted">This regime lists no document types here.</p>}
          <div className="grid gap-2 [grid-template-columns:repeat(auto-fill,minmax(240px,1fr))]">
            {register.readiness.map((r) => (
              <div key={r.type_key} className="flex items-start justify-between gap-2 rounded-lg border border-line-soft px-3 py-2 text-sm">
                <span>
                  {r.label}
                  {r.required && <span className="block text-xs text-muted">Required</span>}
                </span>
                <Badge tone={r.status === "missing" && !r.required ? "neutral" : READINESS[r.status].tone}>{READINESS[r.status].text}</Badge>
              </div>
            ))}
          </div>
        </Card>

        <Card flush title={<span className="text-lg">Documents</span>} description="Every upload is a new version; earlier versions stay readable.">
          {register.documents.length === 0 ? (
            <div className="p-4"><EmptyState title="Nothing uploaded yet" /></div>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead><tr><th>Document</th><th>Version</th><th>Expires</th><th>State</th></tr></thead>
                <tbody>
                  {register.documents.map((d) => (
                    <tr key={d.id} className={cx(d.retraction && "opacity-60", selected?.id === d.id && "bg-accent-soft")}>
                      <td>
                        <Link href={hrefFor(d.id)} className="link">{d.title}</Link>
                        <div className="text-xs text-muted">{typeLabel.get(d.type_key) ?? label(d.type_key)}</div>
                      </td>
                      <td>v{d.current.version}</td>
                      <td className="whitespace-nowrap">{d.current.expires_on ? formatDateOnly(d.current.expires_on) : "No expiry"}</td>
                      <td>
                        {d.retraction ? <Badge tone="bad">Retracted</Badge> : <Badge tone={stateTone(d.current.state)}>{label(d.current.state)}</Badge>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>

      <div className="flex min-w-0 flex-[1_1_380px] flex-col gap-4">
        {selected ? (
          <DocumentDetailView scope={scope} siteId={siteId} doc={selected} type={types.find((t) => t.key === selected.type_key)} canWrite={canWrite} who={who} />
        ) : canWrite ? (
          <Card title="Add a document" description="Uploaded, hashed and sealed. It can't be changed afterwards, only followed by a new version.">
            {types.length === 0 ? (
              <p className="text-sm text-muted">No document types for this register.</p>
            ) : (
              <CreateDocumentForm scope={scope} siteId={siteId} documentId={randomUUID()} regimeCode={register.regime_code} types={types} />
            )}
          </Card>
        ) : (
          <Alert tone="info">Owners and SHE officers add and retract documents.</Alert>
        )}
      </div>
    </div>
  );
}

function DocumentDetailView({
  scope,
  siteId,
  doc,
  type,
  canWrite,
  who,
}: {
  scope: DocumentScope;
  siteId: string;
  doc: DocumentDetail;
  type: DocumentType | undefined;
  canWrite: boolean;
  who: (id: string) => string;
}) {
  const standing = doc.versions.filter((v) => !v.retraction);
  return (
    <>
      <Card>
        <div className="text-xs text-muted">{type?.label ?? label(doc.type_key)}{type && ` · ${type.source_ref}`}</div>
        <h2 className="mt-0.5 font-head text-lg font-semibold">{doc.title}</h2>
        {doc.retraction && (
          <div className="mt-2">
            <Alert tone="warn">
              Retracted by {who(doc.retraction.retracted_by)}, {formatWhen(doc.retraction.retracted_at)}: “{doc.retraction.reason}”. It no
              longer counts for readiness or reminders.
            </Alert>
          </div>
        )}
        <h3 className="mt-4 text-sm font-semibold">Versions</h3>
        {[...doc.versions].reverse().map((v) => (
          <div key={v.version} className={cx("mt-2 rounded-lg border border-line-soft p-3 text-[13px]", v.retraction && "opacity-70")}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="flex items-center gap-2">
                <strong className="font-semibold">Version {v.version}</strong>
                <Badge tone={stateTone(v.state)}>{label(v.state)}</Badge>
              </span>
              <OpenFileButton open={viewDocumentVersion.bind(null, scope, siteId, doc.id, v.version)} label="Open file" />
            </div>
            <div className="mt-1 text-muted">
              {[v.issuer, v.reference].filter(Boolean).join(" · ") || "No issuer or reference"}
              {v.issued_on && ` · issued ${formatDateOnly(v.issued_on)}`}
              {v.expires_on ? ` · expires ${formatDateOnly(v.expires_on)}` : " · no expiry"}
            </div>
            <div className="text-muted">Uploaded by {who(v.uploaded_by)}, {formatWhen(v.received_at)}</div>
            {v.retraction && <div className="mt-1">Retracted: “{v.retraction.reason}”</div>}
          </div>
        ))}
        <p className="mt-3 text-xs text-muted">Opening a file is logged.</p>
      </Card>

      {canWrite && !doc.retraction && (
        <>
          <Card title="Upload a new version" description="A renewal or revision. It becomes the current version.">
            <AddVersionForm scope={scope} siteId={siteId} documentId={doc.id} version={doc.latest_version + 1} type={type} />
          </Card>
          <Card title="Uploaded by mistake?">
            <div className="space-y-4">
              {standing.length > 1 && (
                <details>
                  <summary className="link cursor-pointer text-sm">Retract version {doc.current.version} (the previous one becomes current)</summary>
                  <div className="mt-3">
                    <RetractDocumentForm scope={scope} siteId={siteId} documentId={doc.id} version={doc.current.version} button={`Retract version ${doc.current.version}`} />
                  </div>
                </details>
              )}
              <details>
                <summary className="link cursor-pointer text-sm">Retract the whole document</summary>
                <div className="mt-3">
                  <RetractDocumentForm scope={scope} siteId={siteId} documentId={doc.id} button="Retract document" />
                </div>
              </details>
            </div>
          </Card>
        </>
      )}
    </>
  );
}
