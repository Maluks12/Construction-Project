import { Logo } from "@/components/ui";

const POINTS = [
  "Inspections from the Construction Regulations, on the phone, offline",
  "Incident reports with the legal deadlines worked out for you",
  "Photos sealed with when and where they were taken",
  "An audit pack ready for an inspector or a claim",
];

// Two panels, as in the prototype: what SiteProof is on the dark side, the
// form on the light side. On a phone the dark panel shrinks to a header.
export default function AuthLayout({ children }: LayoutProps<"/">) {
  return (
    <main className="min-h-screen lg:grid lg:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]">
      <section className="flex flex-col gap-8 bg-side px-6 py-6 text-side-ink lg:px-12 lg:py-12">
        <Logo onDark />
        <div className="hidden max-w-md lg:my-auto lg:block">
          <h1 className="font-head text-[34px] leading-tight font-semibold">
            Safety records that hold up when someone checks.
          </h1>
          <ul className="mt-6 space-y-3 text-[15px] text-side-text">
            {POINTS.map((p) => (
              <li key={p} className="flex gap-3">
                <span aria-hidden className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-primary" />
                {p}
              </li>
            ))}
          </ul>
        </div>
      </section>
      <section className="flex flex-col items-center px-4 py-10 lg:justify-center">
        <div className="w-full max-w-sm rounded-[10px] border border-line bg-surface p-6">{children}</div>
      </section>
    </main>
  );
}
