import type { Metadata } from "next";

import { Table, Two } from "@/components/table";
import { Badge, Card, Input, PageHeader } from "@/components/ui";
import { users } from "@/lib/sample";

export const metadata: Metadata = { title: "Users & sessions" };

export default function UsersPage() {
  return (
    <>
      <PageHeader title="Users & sessions" description="Everyone with a SiteProof login, across organisations. Useful when someone can't sign in or a session was revoked." />
      <div className="flex flex-col gap-5">
        <div className="rounded-[10px] border border-line bg-surface px-[18px] py-3.5">
          <label className="block max-w-md">
            <span className="mb-1 block text-xs font-semibold text-muted">Search</span>
            <Input disabled placeholder="Name, email or mobile number" />
          </label>
        </div>
        <Card flush>
          <Table
            head={["User", "Role", "Signs in with", "Devices", "Last seen", "State"]}
            rows={users.map((u) => [
              <Two key="n" top={u.name} sub={u.org} />,
              u.role,
              u.login,
              u.devices,
              u.last,
              <Badge key="s" tone={u.tone}>{u.state}</Badge>,
            ])}
          />
        </Card>
        <Card title="About revoked sessions">
          <p className="max-w-3xl text-sm leading-relaxed text-muted">
            A refresh token used twice revokes the whole session (reuse detection). It usually means two devices shared
            a login or an old token was replayed. The person signs in again; nothing is lost.
          </p>
        </Card>
      </div>
    </>
  );
}
