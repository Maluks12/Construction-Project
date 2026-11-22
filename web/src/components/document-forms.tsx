"use client";

import { useActionState, useState } from "react";

import { addDocumentVersion, createDocument, retractDocument } from "@/app/actions/documents";
import type { DocumentScope, DocumentType } from "@/lib/types";

import { FormError, SubmitButton } from "./client";
import { Alert, Field, Input, Select } from "./ui";

// Forms for the vault and the H&S file (docs/api/documents.md). Each sends
// `scope` so one set of Server Actions serves both registers.

const ACCEPT = "application/pdf,image/jpeg,image/png";

function Scope({ scope, siteId }: { scope: DocumentScope; siteId: string }) {
  return (
    <>
      <input type="hidden" name="scope" value={scope} />
      <input type="hidden" name="site_id" value={siteId} />
    </>
  );
}

// Issuer, reference, dates and the file: what every version carries. The
// type decides whether an expiry is required, optional or not allowed, and
// whether the issuer is a fixed list (a Letter of Good Standing's).
function VersionFields({ type, values }: { type: DocumentType | undefined; values: Record<string, string> }) {
  return (
    <>
      {type && type.issuers.length > 0 ? (
        <Field label="Issued by">
          <Select name="issuer" required defaultValue={values.issuer ?? ""}>
            <option value="" disabled>Choose…</option>
            {type.issuers.map((i) => <option key={i} value={i}>{i}</option>)}
          </Select>
        </Field>
      ) : (
        <Field label="Issued by (optional)"><Input name="issuer" maxLength={200} defaultValue={values.issuer} /></Field>
      )}
      <Field label="Reference (optional)" hint="A certificate or letter number."><Input name="reference" maxLength={200} defaultValue={values.reference} /></Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Issued on (optional)"><Input name="issued_on" type="date" defaultValue={values.issued_on} /></Field>
        {type?.expiry !== "none" && (
          <Field label={type?.expiry === "required" ? "Expires on" : "Expires on (optional)"}>
            <Input name="expires_on" type="date" required={type?.expiry === "required"} defaultValue={values.expires_on} />
          </Field>
        )}
      </div>
      <Field label="File" hint="PDF, JPEG or PNG, up to 20 MB. After an error, choose it again.">
        <Input name="file" type="file" required accept={ACCEPT} className="py-1.5" />
      </Field>
    </>
  );
}

export function CreateDocumentForm({
  scope,
  siteId,
  documentId,
  regimeCode,
  types,
}: {
  scope: DocumentScope;
  siteId: string;
  documentId: string;
  // The vault names its regime; the H&S file takes its site's.
  regimeCode: string;
  types: DocumentType[];
}) {
  const [state, action] = useActionState(createDocument, null);
  const v = state?.values ?? {};
  const [typeKey, setTypeKey] = useState(v.type_key ?? types[0]?.key ?? "");
  if (state?.ok) return <Alert tone="ok">Uploaded and sealed. It&apos;s in the register now.</Alert>;
  const type = types.find((t) => t.key === typeKey);
  return (
    <form action={action} className="space-y-3">
      <Scope scope={scope} siteId={siteId} />
      <input type="hidden" name="document_id" value={documentId} />
      <input type="hidden" name="regime_code" value={regimeCode} />
      <Field label="Type" hint={type?.source_ref}>
        <Select name="type_key" value={typeKey} onChange={(e) => setTypeKey(e.currentTarget.value)}>
          {types.map((t) => <option key={t.key} value={t.key}>{t.label}{t.required ? " (required)" : ""}</option>)}
        </Select>
      </Field>
      <Field label="Title" hint="Several of one type are fine, e.g. two risk assessments.">
        <Input name="title" required maxLength={200} defaultValue={v.title ?? type?.label} key={typeKey} />
      </Field>
      <VersionFields type={type} values={v} />
      <FormError state={state} />
      <SubmitButton pendingText="Uploading…">Upload document</SubmitButton>
    </form>
  );
}

export function AddVersionForm({
  scope,
  siteId,
  documentId,
  version,
  type,
}: {
  scope: DocumentScope;
  siteId: string;
  documentId: string;
  // The next number: latest_version + 1.
  version: number;
  type: DocumentType | undefined;
}) {
  const [state, action] = useActionState(addDocumentVersion, null);
  if (state?.ok) return <Alert tone="ok">Version {version} uploaded. It&apos;s the current version now.</Alert>;
  return (
    <form action={action} className="space-y-3">
      <Scope scope={scope} siteId={siteId} />
      <input type="hidden" name="document_id" value={documentId} />
      <input type="hidden" name="version" value={version} />
      <VersionFields type={type} values={state?.values ?? {}} />
      <FormError state={state} />
      <SubmitButton pendingText="Uploading…">Upload version {version}</SubmitButton>
    </form>
  );
}

// Retracts the whole document, or (with `version`) only its current
// version. Both stay on record with who, when and why.
export function RetractDocumentForm({
  scope,
  siteId,
  documentId,
  version,
  button,
}: {
  scope: DocumentScope;
  siteId: string;
  documentId: string;
  version?: number;
  button: string;
}) {
  const [state, action] = useActionState(retractDocument, null);
  if (state?.ok) return <Alert tone="ok">Retracted.</Alert>;
  return (
    <form action={action} className="space-y-3">
      <Scope scope={scope} siteId={siteId} />
      <input type="hidden" name="document_id" value={documentId} />
      {version !== undefined && <input type="hidden" name="version" value={version} />}
      <Field label="Why it was a mistake">
        <Input name="reason" required maxLength={500} defaultValue={state?.values?.reason} />
      </Field>
      <FormError state={state} />
      <SubmitButton variant="danger" pendingText="Retracting…">{button}</SubmitButton>
    </form>
  );
}
