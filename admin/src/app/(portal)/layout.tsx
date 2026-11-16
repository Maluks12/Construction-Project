import Link from "next/link";

import { Mark } from "@/components/ui";

import { Nav } from "./nav";

function Brand() {
  return (
    <span className="inline-flex items-center gap-2.5">
      <Mark />
      <span className="font-head text-lg font-bold text-side-ink">SiteProof</span>
      <span className="rounded-full border border-side-line px-2 py-0.5 text-[11px] tracking-[.08em] text-side-muted uppercase">
        Admin
      </span>
    </span>
  );
}

export default function PortalLayout({ children }: { children: React.ReactNode }) {
  const sidebar = (
    <>
      <div className="rounded-lg border border-side-line px-3 py-2.5">
        <div className="text-[11px] tracking-[.06em] text-side-muted uppercase">Environment</div>
        <div className="text-sm font-semibold text-side-ink">Preview · sample data</div>
        <div className="text-xs text-side-muted">Not connected to the API</div>
      </div>
      <Nav />
      <div className="mt-auto flex items-center justify-between gap-2 px-3 pt-2 text-xs text-side-muted">
        <span>Self Hawk Tech staff only</span>
        <Link href="/login" className="hover:text-side-ink">Sign out</Link>
      </div>
    </>
  );

  return (
    <div className="min-h-screen md:grid md:grid-cols-[240px_1fr]">
      <details className="bg-side md:hidden">
        <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3">
          <Brand />
          <span className="rounded-lg border border-side-line px-3 py-1.5 text-sm font-semibold text-side-ink">Menu</span>
        </summary>
        <div className="flex flex-col gap-4 px-3.5 pb-4">{sidebar}</div>
      </details>
      <aside className="hidden bg-side md:sticky md:top-0 md:flex md:h-screen md:flex-col md:gap-[18px] md:overflow-y-auto md:px-3.5 md:py-5">
        <div className="px-2 py-1"><Brand /></div>
        {sidebar}
      </aside>
      <main className="min-w-0 px-4 py-6 sm:px-8 sm:py-7">
        <div role="note" className="mb-5 rounded-lg border border-dashed border-line-strong bg-warn-soft px-3.5 py-2.5 text-[13px] text-warn">
          <strong className="font-semibold">Sample data.</strong> The admin portal is front end only for now: the API has
          no admin endpoints, so nothing is fetched and the buttons don&apos;t save.
        </div>
        {children}
      </main>
    </div>
  );
}
