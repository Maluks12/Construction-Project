import type { Metadata } from "next";
import Link from "next/link";

import { Badge, Card, Field, Input, PageHeader, buttonClass } from "@/components/ui";
import { listSites } from "@/lib/data";
import { company } from "@/lib/sample";
import { regimeOf } from "@/lib/site";

import { NewSiteForm } from "../sites/new-site-form";

export const metadata: Metadata = { title: "Organisation" };

export default async function OrganisationPage() {
  const sites = await listSites();
  const regimes = await Promise.all(sites.map((s) => regimeOf(s.id)));

  return (
    <>
      <PageHeader
        title="Organisation"
        description="Company details used on forms and the audit pack, and the organisation's sites."
      />
      <div className="flex flex-wrap items-start gap-5">
        <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-5">
          <Card flush title={<span className="text-lg">Sites</span>} description="Each site has its own regulations (regime). Supervisors only see the sites they've been added to.">
            {sites.length === 0 ? (
              <p className="px-[18px] pb-4 text-sm text-muted">No sites yet.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="tbl">
                  <thead><tr><th>Site</th><th>Address</th><th>Regime</th><th>Status</th><th /></tr></thead>
                  <tbody>
                    {sites.map((s, i) => (
                      <tr key={s.id}>
                        <td><Link href={`/sites/${s.id}`} className="link">{s.name}</Link></td>
                        <td>{s.address || "No address"}</td>
                        <td>{regimes[i]?.name ?? "—"}</td>
                        <td><Badge tone={s.status === "active" ? "ok" : s.status === "completed" ? "warn" : "neutral"}>{s.status}</Badge></td>
                        <td><Link href={`/sites/${s.id}`} className="link">Locations · Edit</Link></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
          <Card
            title="Company details"
            description="Sample values: the API has no organisation details endpoint yet, so these can't be saved."
          >
            <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))]">
              {company.map(([name, value, hint]) => (
                <Field key={name} label={name} hint={hint}><Input disabled defaultValue={value} /></Field>
              ))}
            </div>
            <button type="button" disabled className={buttonClass("primary") + " mt-4"}>Save details</button>
          </Card>
        </div>
        <Card className="flex-[1_1_320px]" title="New site" description="Owners and SHE officers can add sites.">
          <NewSiteForm />
        </Card>
      </div>
    </>
  );
}
