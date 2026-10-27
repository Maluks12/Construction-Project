import type { Metadata } from "next";
import Link from "next/link";

import { Alert } from "@/components/ui";
import { previewInvite } from "@/lib/data";
import { ROLE_LABELS, formatDate } from "@/lib/format";

import { AcceptForm } from "./accept-form";

export const metadata: Metadata = { title: "Accept invite" };

export default async function InvitePage({ searchParams }: PageProps<"/invite">) {
  const { token } = await searchParams;
  const preview = typeof token === "string" && token ? await previewInvite(token) : null;

  if (!preview || typeof token !== "string") {
    return (
      <>
        <h1 className="text-2xl font-semibold tracking-tight">Invite not valid</h1>
        <div className="mt-4">
          <Alert>
            This invite link is invalid, has expired, or was already used. Ask
            whoever invited you to send a new one.
          </Alert>
        </div>
        <p className="mt-6 text-sm text-muted">
          Already set up?{" "}
          <Link href="/login" className="font-medium text-ink underline underline-offset-4">
            Sign in
          </Link>
        </p>
      </>
    );
  }

  return (
    <>
      <p className="text-sm text-muted">You&apos;ve been invited to join</p>
      <h1 className="text-2xl font-semibold tracking-tight">{preview.org_name}</h1>
      <dl className="mt-4 mb-6 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-md border border-line bg-surface px-4 py-3 text-sm">
        <dt className="text-muted">Name</dt>
        <dd>{preview.full_name}</dd>
        <dt className="text-muted">Role</dt>
        <dd>{ROLE_LABELS[preview.role] ?? preview.role}</dd>
        <dt className="text-muted">Expires</dt>
        <dd>{formatDate(preview.expires_at)}</dd>
      </dl>
      <AcceptForm token={token} hasPassword={preview.has_password} />
    </>
  );
}
