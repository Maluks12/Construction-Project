import type { Metadata } from "next";

import { logout, revokeSession } from "@/app/actions/auth";
import { InlineAction } from "@/components/client";
import { Badge, Card, PageHeader, buttonClass } from "@/components/ui";
import { listSessions } from "@/lib/data";
import { formatDateTime } from "@/lib/format";

export const metadata: Metadata = { title: "Account" };

export default async function AccountPage() {
  const sessions = await listSessions();

  return (
    <>
      <PageHeader title="Account">
        <form action={logout}>
          <button type="submit" className={buttonClass("secondary", "sm")}>
            Sign out
          </button>
        </form>
      </PageHeader>
      <Card
        title="Signed-in devices"
        description="Sign out any device you don't recognise. It loses access straight away."
      >
        <ul className="divide-y divide-line">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-3 py-3 first:pt-0 last:pb-0">
              <div className="min-w-0">
                <p className="flex items-center gap-2 text-sm font-medium">
                  <span className="truncate">{s.device_name || "Unnamed device"}</span>
                  {s.current && <Badge tone="ok">This device</Badge>}
                </p>
                <p className="truncate text-xs text-muted">
                  Last active {formatDateTime(s.last_used_at)} · signed in {formatDateTime(s.created_at)}
                </p>
              </div>
              {!s.current && (
                <InlineAction
                  action={revokeSession}
                  fields={{ session_id: s.id }}
                  label="Sign out"
                  confirmLabel="Sign out device"
                  variant="ghost"
                />
              )}
            </li>
          ))}
        </ul>
      </Card>
    </>
  );
}
