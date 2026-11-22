import type { Metadata } from "next";
import Link from "next/link";

import { Forbidden, NoSite } from "@/components/states";
import { Badge, Card, Facts, PageHeader, cx } from "@/components/ui";
import { CATEGORY_LABELS, COLOUR_TONE, colourLabel, itemHref } from "@/lib/compliance";
import { getSiteCompliance } from "@/lib/data";
import { formatDateOnly, formatWhen, label } from "@/lib/format";
import { currentSite } from "@/lib/site";
import type { ComplianceCategory, ComplianceItem } from "@/lib/types";

export const metadata: Metadata = { title: "Site compliance" };

const ESCALATION: Record<number, string> = {
  1: "told the owner and raiser",
  2: "escalated to SHE officers",
  3: "escalated to owners",
};

function ItemRow({ category, item }: { category: ComplianceCategory; item: ComplianceItem }) {
  const href = itemHref(category.key, item);
  const when = item.due_on ? `due ${formatDateOnly(item.due_on)}` : item.since ? `since ${formatWhen(item.since)}` : null;
  return (
    <div className="border-t border-line-soft py-2.5 text-sm first:border-t-0">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          {href ? <Link href={href} className="link">{item.label}</Link> : <span className="font-medium">{item.label}</span>}
          <div className="mt-0.5 text-xs text-muted">
            {label(item.state)}
            {when && ` · ${when}`}
            {item.severity && ` · ${item.severity}`}
            {item.escalation_step && ` · ${ESCALATION[item.escalation_step] ?? `escalation ${item.escalation_step}`}`}
          </div>
          {item.warnings.length > 0 && (
            <div className="mt-1 flex flex-wrap gap-1">
              {item.warnings.map((w) => <Badge key={w} tone="bad">{label(w)}</Badge>)}
            </div>
          )}
          {item.note && <p className="mt-1 text-xs text-warn">{item.note}</p>}
        </div>
        {item.colour && <Badge tone={COLOUR_TONE[item.colour]}>{colourLabel(item.colour)}</Badge>}
      </div>
    </div>
  );
}

export default async function CompliancePage() {
  const { site } = await currentSite();
  if (!site) return <NoSite title="Site compliance" />;

  const summary = await getSiteCompliance(site.id);
  if (summary === "forbidden") return <Forbidden title="Site compliance" />;

  // Counted categories with something in them first, then the rest.
  const order = (c: ComplianceCategory) => (c.status === "red" ? 0 : c.status === "amber" ? 1 : c.counted ? 3 : 2);
  const categories = [...summary.categories].sort((a, b) => order(a) - order(b));

  return (
    <>
      <PageHeader
        title={`Compliance · ${summary.site_name}`}
        badges={<Badge tone={COLOUR_TONE[summary.status]}>{colourLabel(summary.status)}</Badge>}
        description="What needs attention on this site now. It counts things and gives no score: red means something is already wrong, amber that it's about to go wrong or may not apply."
      />
      <div className="flex flex-col gap-5">
        <Card>
          <Facts
            items={[
              ["Red items", String(summary.red_items)],
              ["Amber items", String(summary.amber_items)],
              ["Today, for this summary", `${formatDateOnly(summary.date)} (${summary.time_zone})`],
              ["Worked out", formatWhen(summary.computed_at)],
            ]}
          />
        </Card>
        <div className="grid items-start gap-4 [grid-template-columns:repeat(auto-fill,minmax(340px,1fr))]">
          {categories.map((c) => (
            <Card
              key={c.key}
              className={cx(c.status === "red" && "border-danger/40")}
              title={
                <span className="flex items-center gap-2">
                  {CATEGORY_LABELS[c.key]}
                  <span className="text-sm font-normal text-muted">{c.count}</span>
                </span>
              }
              description={c.counted ? undefined : "Shown, not counted"}
              actions={c.status ? <Badge tone={COLOUR_TONE[c.status]}>{colourLabel(c.status)}</Badge> : <Badge>Info</Badge>}
            >
              {c.items.length === 0 ? (
                <p className="text-sm text-muted">Nothing here.</p>
              ) : (
                c.items.map((item, i) => <ItemRow key={`${item.subject_type}:${item.subject_id ?? item.type_key ?? i}`} category={c} item={item} />)
              )}
            </Card>
          ))}
        </div>
      </div>
    </>
  );
}
