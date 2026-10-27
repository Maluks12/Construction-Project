"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { cx } from "@/components/ui";

const LINKS = [
  { href: "/sites", label: "Sites" },
  { href: "/team", label: "Team" },
  { href: "/account", label: "Account" },
] as const;

export function Nav() {
  const pathname = usePathname();
  return (
    <nav aria-label="Main" className="flex gap-1 overflow-x-auto px-3 pb-3 md:flex-col md:pb-0">
      {LINKS.map(({ href, label }) => {
        const active = pathname === href || pathname.startsWith(href + "/");
        return (
          <Link
            key={href}
            href={href}
            aria-current={active ? "page" : undefined}
            className={cx(
              "rounded-md px-3 py-2 text-sm font-medium transition-colors",
              active ? "bg-surface-2 text-ink" : "text-muted hover:bg-surface-2 hover:text-ink",
            )}
          >
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
