import type { Metadata } from "next";
import Link from "next/link";

import { DocumentRegisterView } from "@/components/documents";
import { Badge, Card, Field, Input, PageHeader, buttonClass } from "@/components/ui";
import { getMe, getRegimeTypes, getVault, getVaultDocument, listSites } from "@/lib/data";
import { company } from "@/lib/sample";
import { capabilities, regimeOf, sitePeople } from "@/lib/site";

import { NewSiteForm } from "../sites/new-site-form";

export const metadata: Metadata = { title: "Organisation" };

export default async function OrganisationPage({ searchParams }: PageProps<"/organisation">) {
  const [sites, me, caps] = await Promise.all([listSites(), getMe(), capabilities()]);
  const regimes = await Promise.all(sites.map((s) => regimeOf(s.id)));
  const { doc } = await searchParams;
  const docId = typeof doc === "string" ? doc : undefined;

  // The statutory vault (M11): owners and SHE officers only.
  const vault = caps.has("view_vault") ? await getVault() : null;
  const [vaultTypes, selected, people] =
    vault && vault !== "forbidden"
      ? await Promise.all([
          getRegimeTypes(vault.regime_code),
          docId ? getVaultDocument(docId) : null,
          sites[0] ? sitePeople(sites[0].id) : null,
        ])
      : [null, null, null];
  const who = people?.who ?? ((id: string) => (id === me.user_id ? me.full_name : `User ${id.slice(0, 8)}`));

  return (
    <>
      <PageHeader
        title="Organisation"
        description="Company details used on forms and the audit pack, the organisation's sites, and its statutory documents."
      />
      <div className="flex flex-col gap-5">
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
              description="The trading name comes from your account. The other fields are sample values: the API has no organisation details endpoint yet, so they can't be saved."
            >
              <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))]">
                {company.map(([name, value, hint], i) => (
                  <Field key={name} label={name} hint={hint}><Input disabled defaultValue={i === 0 ? me.org_name : value} /></Field>
                ))}
              </div>
              <button type="button" disabled className={buttonClass("primary") + " mt-4"}>Save details</button>
            </Card>
          </div>
          <Card className="flex-[1_1_320px]" title="New site" description="Owners and SHE officers can add sites.">
            <NewSiteForm />
          </Card>
        </div>

        {vault && vault !== "forbidden" && (
          <section>
            <h2 className="mb-1 font-head text-xl font-semibold">Statutory documents</h2>
            <p className="mb-4 max-w-3xl text-sm text-muted">
              The organisation&apos;s vault: Letter of Good Standing, COIDA registration, CIDB, tax and the other
              organisation documents of the regulations. Shown on every site&apos;s compliance summary when one expires.
            </p>
            <DocumentRegisterView
              scope="org"
              siteId=""
              register={vault}
              types={(vaultTypes && vaultTypes !== "forbidden" ? vaultTypes.document_types : []).filter((t) => t.scope === "org")}
              selected={selected}
              hrefFor={(id) => `/organisation?doc=${encodeURIComponent(id)}`}
              canWrite={caps.has("view_vault")}
              who={who}
            />
          </section>
        )}
      </div>
    </>
  );
}
