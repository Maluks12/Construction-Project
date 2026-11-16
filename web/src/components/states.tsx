// Whole-page states the site-scoped pages share.

import Link from "next/link";

import { Alert, EmptyState, PageHeader, buttonClass } from "./ui";

export function NoSite({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <EmptyState title="No site to show">
        Add a site under Organisation first. Supervisors only see the sites they&apos;ve been added to.
      </EmptyState>
      <Link href="/organisation" className={buttonClass("secondary") + " mt-4"}>
        Go to Organisation
      </Link>
    </>
  );
}

export function Forbidden({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <Alert tone="info">
        Safety records are open to owners, SHE officers and supervisors. Your role doesn&apos;t include them.
      </Alert>
    </>
  );
}
