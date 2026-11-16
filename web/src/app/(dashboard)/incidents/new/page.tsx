import { randomUUID } from "node:crypto";

import type { Metadata } from "next";
import Link from "next/link";

import { NoSite } from "@/components/states";
import { PageHeader } from "@/components/ui";
import { isoToSASTInput } from "@/lib/format";
import { incidentTypes } from "@/lib/regime";
import { currentSite, regimeOf } from "@/lib/site";

import { IncidentForm } from "../incident-form";

export const metadata: Metadata = { title: "Report an incident" };

export default async function NewIncidentPage() {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Report an incident" />;
  const regime = await regimeOf(site.id);
  const now = isoToSASTInput(new Date().toISOString());

  return (
    <>
      <PageHeader
        eyebrow={<><Link href="/incidents" className="link">Incident register</Link> / Report</>}
        title={`Report an incident · ${site.name}`}
        description="Version 1. It can be amended later; every version is kept as it was submitted."
      />
      <IncidentForm
        siteId={site.id}
        // Made up here, once, so submitting twice can't create two incidents.
        incidentId={randomUUID()}
        version={1}
        types={incidentTypes(regime?.code)}
        initial={{ occurred_at: now, employer_aware_at: now, location: "", description: "", types: [], people: [], evidence_ids: [] }}
      />
    </>
  );
}
