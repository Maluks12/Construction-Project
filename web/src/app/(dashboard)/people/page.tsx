import type { Metadata } from "next";

import { DocumentRegisterView } from "@/components/documents";
import { Forbidden, NoSite } from "@/components/states";
import { Alert, PageHeader, Tabs } from "@/components/ui";
import { getRegimeTypes, getSafetyFile, getSafetyFileDocument } from "@/lib/data";
import { capabilities, currentSite, regimeOf, sitePeople } from "@/lib/site";

import { AppointmentsTab } from "./appointments";
import { OnSiteTab } from "./onsite";
import { WorkersTab } from "./workers";

export const metadata: Metadata = { title: "People & documents" };

const TABS = {
  workers: "Workers",
  onsite: "On site",
  appointments: "Appointments",
  file: "H&S file",
} as const;
type Tab = keyof typeof TABS;

const one = (v: string | string[] | undefined) => (typeof v === "string" && v !== "" ? v : undefined);

export default async function PeoplePage({ searchParams }: PageProps<"/people">) {
  const { site, sites } = await currentSite();
  if (!site) return <NoSite title="People & documents" />;

  const sp = await searchParams;
  const workerId = one(sp.worker);
  // A worker link opens the register, whatever tab was asked for.
  const tab: Tab = workerId ? "workers" : typeof sp.tab === "string" && sp.tab in TABS ? (sp.tab as Tab) : "workers";

  const [caps, people, regime] = await Promise.all([capabilities(), sitePeople(site.id), regimeOf(site.id)]);
  if (!caps.has("capture_records")) return <Forbidden title="People & documents" />;
  const types = regime ? await getRegimeTypes(regime.code) : null;
  const regimeTypes = types === "forbidden" ? null : types;

  let body: React.ReactNode;
  if (tab === "workers") {
    body = <WorkersTab q={one(sp.q)} active={one(sp.active)} workerId={workerId} types={regimeTypes} caps={caps} who={people.who} />;
  } else if (tab === "onsite") {
    body = <OnSiteTab siteId={site.id} date={one(sp.date)} sites={sites} caps={caps} who={people.who} />;
  } else if (tab === "appointments") {
    body = <AppointmentsTab siteId={site.id} types={regimeTypes} caps={caps} who={people.who} />;
  } else {
    const docId = one(sp.doc);
    const [file, selected] = await Promise.all([
      getSafetyFile(site.id),
      docId ? getSafetyFileDocument(site.id, docId) : null,
    ]);
    body =
      file === "forbidden" ? (
        <Alert tone="info">Your role doesn&apos;t include the H&amp;S file.</Alert>
      ) : file === null ? (
        <Alert tone="info">This site&apos;s regulations aren&apos;t the Construction Regulations, so it has no H&amp;S file here.</Alert>
      ) : (
        <DocumentRegisterView
          scope="site"
          siteId={site.id}
          register={file}
          types={(regimeTypes?.document_types ?? []).filter((t) => t.scope === "site")}
          selected={selected}
          hrefFor={(id) => `/people?tab=file&doc=${encodeURIComponent(id)}`}
          canWrite={caps.has("write_safety_file")}
          who={people.who}
        />
      );
  }

  return (
    <>
      <PageHeader
        title="People & documents"
        description={`The worker register, who is on ${site.name}, its appointments and its health and safety file (CR 2014 reg 7(1)(b)).`}
      />
      <div className="mb-5 overflow-hidden rounded-[10px] border border-line">
        <Tabs
          tabs={(Object.keys(TABS) as Tab[]).map((t) => ({ href: `/people?tab=${t}`, label: TABS[t], active: tab === t }))}
        />
      </div>
      {body}
    </>
  );
}
