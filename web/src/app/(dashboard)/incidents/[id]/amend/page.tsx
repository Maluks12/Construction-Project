import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { NoSite } from "@/components/states";
import { PageHeader } from "@/components/ui";
import { getIncident } from "@/lib/data";
import { isoToSASTInput, shortId } from "@/lib/format";
import { incidentTypes } from "@/lib/regime";
import { currentSite, regimeOf } from "@/lib/site";

import { IncidentForm } from "../../incident-form";

export const metadata: Metadata = { title: "Amend incident" };

export default async function AmendIncidentPage({ params }: PageProps<"/incidents/[id]/amend">) {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Amend incident" />;

  const { id } = await params;
  const [incident, regime] = await Promise.all([getIncident(site.id, id), regimeOf(site.id)]);
  if (!incident) notFound();
  const v = incident.version;
  const next = incident.latest_version + 1;

  return (
    <>
      <PageHeader
        eyebrow={
          <>
            <Link href="/incidents" className="link">Incident register</Link> /{" "}
            <Link href={`/incidents/${id}`} className="link">INC-{shortId(id)}</Link> / Amend
          </>
        }
        title={`Amend INC-${shortId(id)} · new version ${next}`}
        description={`An amendment restates the whole account. Version ${incident.latest_version} stays readable exactly as it was submitted.`}
      />
      <IncidentForm
        siteId={site.id}
        incidentId={id}
        version={next}
        types={incidentTypes(regime?.code)}
        initial={{
          occurred_at: isoToSASTInput(v.occurred_at),
          employer_aware_at: v.employer_aware_at ? isoToSASTInput(v.employer_aware_at) : "",
          location: v.location ?? "",
          description: v.description ?? "",
          types: v.types.map((t) => t.key),
          people: v.people.map((p) => ({
            name: p.name,
            occupation: p.occupation,
            employee_of_org: p.employee_of_org,
            employer_name: p.employer_name,
            injured: p.injured,
            injury_description: p.injury_description,
            body_part: p.body_part,
            treatment: p.treatment,
            died_at: p.died_at ? isoToSASTInput(p.died_at) : null,
          })),
          evidence_ids: v.evidence.map((e) => e.evidence_id),
        }}
      />
    </>
  );
}
