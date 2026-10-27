import { logout } from "@/app/actions/auth";
import { Badge, Logo, buttonClass } from "@/components/ui";

import { Nav } from "./nav";

export default function DashboardLayout({ children }: LayoutProps<"/">) {
  return (
    <div className="min-h-screen md:grid md:grid-cols-[232px_1fr]">
      <aside className="border-b border-line bg-surface md:sticky md:top-0 md:flex md:h-screen md:flex-col md:border-r md:border-b-0">
        <div className="flex items-center justify-between gap-3 px-4 py-4 md:px-5 md:py-5">
          <Logo />
          <Badge tone="warn">Construction</Badge>
        </div>
        <Nav />
        <form action={logout} className="hidden px-3 pb-4 md:mt-auto md:block">
          <button type="submit" className={buttonClass("ghost", "sm") + " w-full justify-start"}>
            Sign out
          </button>
        </form>
      </aside>
      <main className="min-w-0 px-4 py-6 sm:px-8 sm:py-8">
        <div className="mx-auto max-w-5xl">{children}</div>
      </main>
    </div>
  );
}
