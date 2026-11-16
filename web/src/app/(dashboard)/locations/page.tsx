import { redirect } from "next/navigation";

import { NoSite } from "@/components/states";
import { currentSite } from "@/lib/site";

// Zones and floors are edited on the site's own page; this is the sidebar's
// shortcut to them for the site being viewed.
export default async function LocationsPage() {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Site locations" />;
  redirect(`/sites/${site.id}`);
}
