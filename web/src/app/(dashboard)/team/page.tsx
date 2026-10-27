import type { Metadata } from "next";

import { revokeInvite } from "@/app/actions/invites";
import { InlineAction } from "@/components/client";
import { Alert, Badge, Card, EmptyState, PageHeader } from "@/components/ui";
import { listPendingInvites } from "@/lib/data";
import { ROLE_LABELS, formatDate } from "@/lib/format";

import { InviteForm } from "./invite-form";

export const metadata: Metadata = { title: "Team" };

export default async function TeamPage() {
  const invites = await listPendingInvites();

  if (invites === "forbidden") {
    return (
      <>
        <PageHeader title="Team" />
        <Alert tone="info">Only owners and SHE officers can invite people or see pending invites.</Alert>
      </>
    );
  }

  return (
    <>
      <PageHeader title="Team" />
      <div className="grid items-start gap-6 lg:grid-cols-[1fr_360px]">
        <Card
          title="Pending invites"
          description="People who haven't accepted yet. Invites expire after 7 days."
        >
          {invites.length === 0 ? (
            <EmptyState title="No pending invites" />
          ) : (
            <ul className="divide-y divide-line">
              {invites.map((inv) => (
                <li key={inv.id} className="flex items-center justify-between gap-3 py-3 first:pt-0 last:pb-0">
                  <div className="min-w-0">
                    <p className="flex items-center gap-2 text-sm font-medium">
                      <span className="truncate">{inv.full_name}</span>
                      <Badge>{ROLE_LABELS[inv.role] ?? inv.role}</Badge>
                    </p>
                    <p className="truncate text-xs text-muted">
                      {[inv.email, inv.mobile].filter(Boolean).join(" · ")} · expires{" "}
                      {formatDate(inv.expires_at)}
                    </p>
                  </div>
                  <InlineAction
                    action={revokeInvite}
                    fields={{ invite_id: inv.id }}
                    label="Revoke"
                    confirmLabel="Revoke invite"
                    variant="ghost"
                  />
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card title="Invite someone" description="They'll get a link to set a password and join.">
          <InviteForm />
        </Card>
      </div>
    </>
  );
}
