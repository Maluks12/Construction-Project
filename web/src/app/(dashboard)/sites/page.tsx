import type { Metadata } from "next";
import Link from "next/link";

import { Badge, Card, EmptyState, PageHeader } from "@/components/ui";
import { listSites } from "@/lib/data";
import { formatDate } from "@/lib/format";

import { NewSiteForm } from "./new-site-form";

export const metadata: Metadata = { title: "Sites" };

export default async function SitesPage() {
  const sites = await listSites();

  return (
    <>
      <PageHeader title="Sites" />
      <div className="grid items-start gap-6 lg:grid-cols-[1fr_320px]">
        <section aria-label="Your sites">
          {sites.length === 0 ? (
            <EmptyState title="No sites yet">
              Supervisors only see the sites they&apos;ve been added to.
            </EmptyState>
          ) : (
            <ul className="divide-y divide-line overflow-hidden rounded-lg border border-line bg-surface">
              {sites.map((site) => (
                <li key={site.id}>
                  <Link
                    href={`/sites/${site.id}`}
                    className="flex items-center justify-between gap-4 px-5 py-4 transition-colors hover:bg-surface-2"
                  >
                    <div className="min-w-0">
                      <p className="truncate font-medium">{site.name}</p>
                      <p className="truncate text-sm text-muted">
                        {site.address || "No address"}
                      </p>
                    </div>
                    <div className="flex shrink-0 flex-col items-end gap-1">
                      <Badge tone={site.status === "active" ? "ok" : "neutral"}>
                        {site.status}
                      </Badge>
                      <span className="text-xs text-muted">
                        Added {formatDate(site.created_at)}
                      </span>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </section>
        <Card title="New site" description="Owners and SHE officers can add sites.">
          <NewSiteForm />
        </Card>
      </div>
    </>
  );
}
