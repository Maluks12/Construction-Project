"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { cx } from "@/components/ui";

const LINKS = [
  { href: "/", label: "Overview" },
  { href: "/organisations", label: "Organisations" },
  { href: "/users", label: "Users & sessions" },
  { href: "/billing", label: "Billing" },
  { href: "/regimes", label: "Regimes & checklists" },
  { href: "/evidence", label: "Evidence health" },
  { href: "/jobs", label: "Jobs & reminders" },
  { href: "/support", label: "Support" },
  { href: "/audit-log", label: "Staff audit log" },
  { href: "/settings", label: "Settings" },
];

export function Nav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Main" className="flex flex-col gap-0.5">
      {LINKS.map(({ href, label }) => {
        const active = href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(href + "/");
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={cx(
              "rounded-lg px-3 py-[9px] text-sm transition-colors",
              active ? "bg-side-on font-semibold text-side-ink" : "text-side-text hover:bg-side-hover hover:text-side-ink",
            )}
          >
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
