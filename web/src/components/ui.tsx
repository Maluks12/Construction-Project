// Small presentational pieces. No hooks, so Server Components can use them.
// The look follows the SiteProof prototype: white cards on warm paper,
// Space Grotesk headings, pill-shaped status labels, a rust primary button.

import type { ComponentProps, ReactNode } from "react";

export function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

type Variant = "primary" | "secondary" | "danger" | "ghost" | "now";

export function buttonClass(variant: Variant = "primary", size: "md" | "sm" = "md") {
  return cx(
    "inline-flex items-center justify-center gap-2 rounded-lg font-semibold whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-60",
    size === "md" ? "min-h-10 px-4 text-sm" : "min-h-8 px-3 text-[13px]",
    variant === "primary" &&
      "border border-primary bg-primary text-primary-ink hover:border-primary-hover hover:bg-primary-hover",
    variant === "secondary" &&
      "border border-line-strong bg-surface text-ink hover:bg-surface-2",
    variant === "danger" &&
      "border border-danger/40 bg-surface text-danger hover:bg-danger-soft",
    variant === "now" && "border border-critical bg-critical text-critical-ink hover:opacity-90",
    variant === "ghost" && "text-muted hover:bg-surface-2 hover:text-ink",
  );
}

export function Card({
  title,
  description,
  actions,
  children,
  className,
  flush,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
  // No padding around the body, for tables and row lists that run edge to edge.
  flush?: boolean;
}) {
  return (
    <section className={cx("rounded-[10px] border border-line bg-surface", flush && "overflow-hidden", className)}>
      {(title || actions) && (
        <header className="flex flex-wrap items-start justify-between gap-3 px-[18px] pt-4 pb-3">
          <div className="min-w-0">
            {title && <h2 className="font-head text-base font-semibold">{title}</h2>}
            {description && <p className="mt-1 text-[13px] text-muted">{description}</p>}
          </div>
          {actions}
        </header>
      )}
      {children !== undefined && (
        <div className={flush ? "" : cx("px-[18px] pb-4", !(title || actions) && "pt-4")}>{children}</div>
      )}
    </section>
  );
}

export function PageHeader({
  eyebrow,
  title,
  description,
  badges,
  children,
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  badges?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="mb-5">
      {eyebrow && <div className="mb-3 text-[13px] text-muted">{eyebrow}</div>}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="font-head text-[28px] leading-tight font-semibold break-words">{title}</h1>
          {badges && <div className="mt-2 flex flex-wrap gap-1.5">{badges}</div>}
          {description && <p className="mt-1 max-w-3xl text-sm text-muted">{description}</p>}
        </div>
        {children && <div className="flex flex-wrap gap-2">{children}</div>}
      </div>
    </div>
  );
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[13px] font-semibold">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-muted">{hint}</span>}
    </label>
  );
}

const inputBase =
  "min-h-10 w-full rounded-lg border border-line-strong bg-surface px-2.5 text-sm text-ink placeholder:text-muted/70 focus:border-accent focus:outline-none disabled:bg-surface-2 disabled:text-muted";

export function Input(props: ComponentProps<"input">) {
  return <input {...props} className={cx(inputBase, props.className)} />;
}

export function Select(props: ComponentProps<"select">) {
  return <select {...props} className={cx(inputBase, "pr-8", props.className)} />;
}

export function Textarea(props: ComponentProps<"textarea">) {
  return <textarea rows={3} {...props} className={cx(inputBase, "py-2", props.className)} />;
}

export type Tone =
  | "neutral"
  | "ok"
  | "warn"
  | "bad"
  | "now"
  | "low"
  | "medium"
  | "high"
  | "critical";

const TONES: Record<Tone, string> = {
  neutral: "bg-neutral-soft text-neutral",
  low: "bg-neutral-soft text-neutral",
  ok: "bg-ok-soft text-ok",
  warn: "bg-warn-soft text-warn",
  medium: "bg-warn-soft text-warn",
  bad: "bg-danger-soft text-danger",
  high: "bg-high-soft text-high",
  now: "bg-critical text-critical-ink",
  critical: "bg-critical text-critical-ink",
};

// The prototype's pill. Severity keys (low...critical) are tones too, so a
// finding's severity can be passed straight in.
export function Badge({ children, tone = "neutral" }: { children: ReactNode; tone?: Tone }) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-[3px] text-xs font-semibold whitespace-nowrap",
        TONES[tone],
      )}
    >
      {children}
    </span>
  );
}

export function Alert({
  children,
  tone = "danger",
}: {
  children: ReactNode;
  tone?: "danger" | "ok" | "info" | "warn";
}) {
  return (
    <div
      role={tone === "danger" ? "alert" : "status"}
      className={cx(
        "rounded-lg px-3 py-2 text-sm",
        tone === "danger" && "bg-danger-soft text-danger",
        tone === "ok" && "bg-ok-soft text-ok",
        tone === "warn" && "bg-warn-soft text-warn",
        tone === "info" && "bg-surface-2 text-muted",
      )}
    >
      {children}
    </div>
  );
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed border-line-strong px-4 py-8 text-center">
      <p className="text-sm font-semibold">{title}</p>
      {children && <p className="mt-1 text-sm text-muted">{children}</p>}
    </div>
  );
}

// Shield-with-tick mark from the prototype.
export function Mark({ size = 30 }: { size?: number }) {
  return (
    <span
      aria-hidden
      className="flex shrink-0 items-center justify-center rounded-[7px] bg-primary"
      style={{ width: size, height: size }}
    >
      <svg width={size * 0.6} height={size * 0.6} viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2">
        <path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" />
        <path d="M8.5 12l2.5 2.5 4.5-5" />
      </svg>
    </span>
  );
}

export function Logo({ onDark }: { onDark?: boolean }) {
  return (
    <span className="inline-flex items-center gap-2.5">
      <Mark />
      <span className={cx("font-head text-lg font-bold", onDark && "text-side-ink")}>SiteProof</span>
    </span>
  );
}

// Filter tabs that are plain links (?status=...), so they work without JS.
export function Tabs({
  tabs,
}: {
  tabs: { href: string; label: string; count?: number; active: boolean }[];
}) {
  return (
    <div className="flex flex-wrap gap-1.5 border-b border-line bg-surface-2 p-3">
      {tabs.map((t) => (
        <a
          key={t.href}
          href={t.href}
          aria-current={t.active ? "page" : undefined}
          className={cx(
            "inline-flex min-h-9 items-center gap-1.5 rounded-lg border px-3.5 text-sm",
            t.active ? "border-line-strong bg-surface font-semibold" : "border-transparent hover:bg-surface",
          )}
        >
          {t.label}
          {t.count !== undefined && <span className="font-normal text-muted">{t.count}</span>}
        </a>
      ))}
    </div>
  );
}

// Label/value grid used for record headers.
export function Facts({ items, min = 170 }: { items: [string, ReactNode][]; min?: number }) {
  return (
    <dl className="grid gap-3.5" style={{ gridTemplateColumns: `repeat(auto-fit,minmax(${min}px,1fr))` }}>
      {items.map(([label, value]) => (
        <div key={label}>
          <dt className="text-xs text-muted">{label}</dt>
          <dd className="mt-0.5 text-sm break-words">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

// A row in a card list: content on the left, a pill or button on the right.
export function Row({ children, aside }: { children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 border-t border-line-soft py-2.5 text-sm first:border-t-0">
      <div className="min-w-0">{children}</div>
      {aside && <div className="shrink-0 text-right">{aside}</div>}
    </div>
  );
}

// Shown on every screen that isn't connected to the API yet, so sample
// figures are never mistaken for the organisation's own records.
export function SampleBanner({ needs }: { needs: string }) {
  return (
    <div role="note" className="mb-5 rounded-lg border border-dashed border-line-strong bg-warn-soft px-3.5 py-2.5 text-[13px] text-warn">
      <strong className="font-semibold">Sample data.</strong> This screen shows the planned layout and isn&apos;t
      connected to the API yet ({needs}). Nothing here is saved.
    </div>
  );
}

export function Thumb({ label }: { label?: string }) {
  return (
    <span
      aria-label={label}
      className="flex h-16 w-[84px] items-center justify-center rounded-md bg-thumb text-muted"
    >
      <svg aria-hidden width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
        <rect x="3" y="5" width="18" height="14" rx="2" />
        <circle cx="9" cy="10" r="1.6" />
        <path d="M4 17l5-4 4 3 3-2 4 3" />
      </svg>
    </span>
  );
}
