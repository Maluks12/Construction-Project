import Link from "next/link";

import { buttonClass } from "@/components/ui";

export default function SiteNotFound() {
  return (
    <div className="rounded-lg border border-line bg-surface p-6">
      <h1 className="text-lg font-semibold">Site not found</h1>
      <p className="mt-1 text-sm text-muted">
        It may have been archived, or you haven&apos;t been added to it.
      </p>
      <Link href="/sites" className={buttonClass("secondary", "sm") + " mt-4"}>
        Back to sites
      </Link>
    </div>
  );
}
