import Link from "next/link";

import { logout } from "@/app/actions/auth";
import { Logo, cx } from "@/components/ui";
import { getMe, unreadCount } from "@/lib/data";
import { ROLE_LABELS } from "@/lib/format";
import { currentSite, regimeOf } from "@/lib/site";

import { Nav, SitePicker } from "./nav";

export default async function DashboardLayout({ children }: LayoutProps<"/">) {
  const [{ sites, site }, me, unread] = await Promise.all([currentSite(), getMe(), unreadCount()]);
  const regime = site ? await regimeOf(site.id) : null;

  const sidebar = (
    <>
      <SitePicker
        sites={sites.map((s) => ({ id: s.id, name: s.name }))}
        siteId={site?.id ?? null}
        regime={regime?.name ?? null}
      />
      <Link
        href="/notifications"
        className="flex items-center justify-between rounded-lg border border-side-line px-3 py-2 text-sm text-side-text hover:bg-side-hover hover:text-side-ink"
      >
        <span>Notifications</span>
        {unread !== null && (
          <span
            aria-label={`${unread} unread`}
            className={cx(
              "min-w-6 rounded-full px-2 py-0.5 text-center text-xs font-semibold",
              unread > 0 ? "bg-primary text-primary-ink" : "bg-side-on text-side-muted",
            )}
          >
            {unread}
          </span>
        )}
      </Link>
      <Nav />
      <div className="mt-auto px-3 pt-2">
        <p className="truncate text-sm font-semibold text-side-ink">{me.full_name}</p>
        <p className="truncate text-xs text-side-muted">
          {ROLE_LABELS[me.role] ?? me.role} · {me.org_name}
        </p>
      </div>
      <div className="flex items-center justify-between gap-2 px-3 text-xs text-side-muted">
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
            Menu{unread ? ` · ${unread}` : ""}
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
