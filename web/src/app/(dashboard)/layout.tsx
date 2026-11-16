import Link from "next/link";

import { logout } from "@/app/actions/auth";
import { Logo } from "@/components/ui";
import { currentSite, regimeOf } from "@/lib/site";

import { Nav, SitePicker } from "./nav";

export default async function DashboardLayout({ children }: LayoutProps<"/">) {
  const { sites, site } = await currentSite();
  const regime = site ? await regimeOf(site.id) : null;

  const sidebar = (
    <>
      <SitePicker
        sites={sites.map((s) => ({ id: s.id, name: s.name }))}
        siteId={site?.id ?? null}
        regime={regime?.name ?? null}
      />
      <Nav />
      <div className="mt-auto flex items-center justify-between gap-2 px-3 pt-2 text-xs text-side-muted">
        <Link href="/account" className="hover:text-side-ink">
          Account &amp; devices
        </Link>
        <form action={logout}>
          <button type="submit" className="hover:text-side-ink">
            Sign out
          </button>
        </form>
      </div>
    </>
  );

  return (
    <div className="min-h-screen md:grid md:grid-cols-[240px_1fr]">
      {/* Phones and narrow windows: the sidebar folds into a menu. */}
      <details className="group bg-side md:hidden">
        <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3">
          <Logo onDark />
          <span className="rounded-lg border border-side-line px-3 py-1.5 text-sm font-semibold text-side-ink">
            Menu
          </span>
        </summary>
        <div className="flex flex-col gap-4 px-3.5 pb-4">{sidebar}</div>
      </details>

      <aside className="hidden bg-side md:sticky md:top-0 md:flex md:h-screen md:flex-col md:gap-[18px] md:overflow-y-auto md:px-3.5 md:py-5">
        <div className="px-2 py-1">
          <Logo onDark />
        </div>
        {sidebar}
      </aside>

      <main className="min-w-0 px-4 py-6 sm:px-8 sm:py-7">{children}</main>
    </div>
  );
}
