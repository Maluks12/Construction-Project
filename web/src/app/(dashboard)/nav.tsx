"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { chooseSite } from "@/app/actions/site";
import { cx } from "@/components/ui";

// The prototype's order. `match` lists other paths that light the link up.
const LINKS: { href: string; label: string; match?: string[] }[] = [
  { href: "/overview", label: "Overview" },
  { href: "/compliance", label: "Site compliance" },
  { href: "/inspections", label: "Inspections" },
  { href: "/actions", label: "Findings & actions" },
  { href: "/incidents", label: "Incidents" },
  { href: "/deadlines", label: "Deadlines" },
  { href: "/injury-on-duty", label: "Injury on duty" },
  { href: "/delay-claims", label: "Delay claims" },
  { href: "/permits", label: "Notifications & permits" },
  { href: "/toolbox", label: "Toolbox & diary" },
  { href: "/people", label: "People & documents" },
  { href: "/subcontractors", label: "Subcontractors" },
  { href: "/evidence", label: "Evidence" },
  { href: "/audit-pack", label: "Audit pack" },
  { href: "/locations", label: "Site locations" },
  { href: "/sync-conflicts", label: "Sync conflicts" },
  { href: "/reminders", label: "Reminders" },
  { href: "/organisation", label: "Organisation", match: ["/sites"] },
  { href: "/team", label: "Team & billing" },
];

export function Nav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Main" className="flex flex-col gap-0.5">
      {LINKS.map(({ href, label, match }) => {
        const active = [href, ...(match ?? [])].some(
          (p) => pathname === p || pathname.startsWith(p + "/"),
        );
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={cx(
              "rounded-lg px-3 py-[9px] text-sm transition-colors",
              active
                ? "bg-side-on font-semibold text-side-ink"
                : "text-side-text hover:bg-side-hover hover:text-side-ink",
            )}
          >
            {label}
          </Link>
        );
      })}
    </nav>
  );
}

// The "Viewing" box. Changing the site goes back to the top of the current
// section, since a record open on one site doesn't exist on another.
export function SitePicker({
  sites,
  siteId,
  regime,
}: {
  sites: { id: string; name: string }[];
  siteId: string | null;
  regime: string | null;
}) {
  const pathname = usePathname();
  const section = "/" + (pathname.split("/")[1] ?? "overview");
  const back = section === "/sites" ? "/organisation" : section;

  return (
    <form action={chooseSite} className="rounded-lg border border-side-line px-3 py-2.5">
      <input type="hidden" name="back" value={back} />
      <label htmlFor="site-picker" className="block text-[11px] tracking-[.06em] text-side-muted uppercase">
        Viewing
      </label>
      {sites.length === 0 ? (
        <p className="text-sm font-semibold text-side-ink">No sites yet</p>
      ) : (
        <select
          id="site-picker"
          name="site_id"
          defaultValue={siteId ?? undefined}
          onChange={(e) => e.currentTarget.form?.requestSubmit()}
          className="-ml-1 w-full cursor-pointer truncate rounded bg-side py-0.5 pl-1 text-sm font-semibold text-side-ink"
        >
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      )}
      {regime && <p className="truncate text-xs text-side-muted">{regime}</p>}
      <noscript>
        <button type="submit" className="mt-1 text-xs text-side-text underline">
          Switch site
        </button>
      </noscript>
    </form>
  );
}
