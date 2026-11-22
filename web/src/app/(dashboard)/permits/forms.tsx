"use client";

import { useActionState, useState } from "react";

import { submitSiteWork } from "@/app/actions/site-work";
import { FormError, SubmitButton } from "@/components/client";
import { Badge, Field, Input, Select, Textarea, cx } from "@/components/ui";
import type { SiteWorkCatalogue, SiteWorkRule, SiteWorkStatus } from "@/lib/types";

// The same reading of the rules as the server's RulesFor
// (internal/regimes/sitework.go): a rule applies when every fact of any one
// of its groups is ticked, unless one of its unless_facts is ticked or one of
// its unless_rules applies. Only a preview: the server works out the
// deadlines when the work is submitted.
function rulesFor(rules: SiteWorkRule[], facts: string[]): string[] {
  const have = new Set(facts);
  const memo = new Map<string, boolean>();
  const applies = (key: string): boolean => {
    const known = memo.get(key);
    if (known !== undefined) return known;
    const r = rules.find((x) => x.rule_key === key);
    let v = !!r && r.when_any.some((group) => group.length > 0 && group.every((f) => have.has(f)));
    if (v && r!.unless_facts.some((f) => have.has(f))) v = false;
    if (v && r!.unless_rules.some(applies)) v = false;
    memo.set(key, v);
    return v;
  };
  return rules.filter((r) => applies(r.rule_key)).map((r) => r.rule_key);
}

export function SiteWorkForm({
  siteId,
  workId,
  version,
  catalogue,
  initial,
}: {
  siteId: string;
  workId: string;
  // 1 for new work; latest + 1 to amend.
  version: number;
  catalogue: SiteWorkCatalogue;
  initial: { description: string; starts_on: string; ends_on: string; facts: string[]; status: SiteWorkStatus };
}) {
  const [state, action] = useActionState(submitSiteWork, null);
  const v = state?.values;
  const [facts, setFacts] = useState<string[]>(v?.facts !== undefined ? v.facts.split(",").filter(Boolean) : initial.facts);
  const amending = version > 1;
  const applying = new Set(rulesFor(catalogue.rules, facts));

  return (
    <form action={action} className="space-y-4">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="work_id" value={workId} />
      <input type="hidden" name="version" value={version} />

      <Field label="What the work is" hint="A phase or package, e.g. “Basement excavation and lateral support”.">
        <Input name="description" required maxLength={500} defaultValue={v?.description ?? initial.description} />
      </Field>
      <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(160px,1fr))]">
        <Field label="Planned start" hint="The notices count back from this day.">
          <Input name="starts_on" type="date" required defaultValue={v?.starts_on ?? initial.starts_on} />
        </Field>
        <Field label="Planned end (optional)">
          <Input name="ends_on" type="date" defaultValue={v?.ends_on ?? initial.ends_on} />
        </Field>
        {amending && (
          <Field label="Status">
            <Select name="status" defaultValue={v?.status ?? initial.status}>
              <option value="planned">Planned</option>
              <option value="cancelled">Cancelled (withdraws its notices)</option>
            </Select>
          </Field>
        )}
      </div>

      <fieldset>
        <legend className="text-sm font-semibold">What does the work involve? Tick everything that applies</legend>
        <p className="mt-1 mb-2.5 text-[13px] text-muted">From the site&apos;s regulations. Ticking nothing is fine: the work then needs no notice.</p>
        <div className="grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]">
          {catalogue.facts.map((f) => {
            const on = facts.includes(f.key);
            return (
              <label
                key={f.key}
                className={cx(
                  "flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm",
                  on ? "border-accent bg-accent-soft" : "border-line-strong",
                )}
              >
                <input
                  type="checkbox"
                  name="facts"
                  value={f.key}
                  checked={on}
                  onChange={() => setFacts(on ? facts.filter((k) => k !== f.key) : [...facts, f.key])}
                  className="mt-0.5 accent-[var(--accent)]"
                />
                <span>
                  {f.label}
                  <span className="block text-xs text-muted">{f.source_ref}</span>
                </span>
              </label>
            );
          })}
        </div>
      </fieldset>

      <div className="space-y-2 rounded-lg border border-line-soft p-3">
        <p className="text-[13px] font-semibold">What these facts start</p>
        {catalogue.rules.map((r) => (
          <div key={r.rule_key} className="text-[13px]">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={applying.has(r.rule_key) ? "warn" : "neutral"}>{applying.has(r.rule_key) ? "Due" : "Not due"}</Badge>
              <span className="font-medium">{r.label}</span>
            </div>
            <p className="mt-0.5 text-xs text-muted">
              At least {r.period} {r.unit.replace(/_/g, " ")} before the start · binds the {r.duty_holder.replace(/_/g, " ")} · {r.source_ref}
            </p>
            {r.caveat && <p className="mt-0.5 text-xs text-warn">{r.caveat}</p>}
          </div>
        ))}
        <p className="text-xs text-muted">
          A permit site needs no Reg 4 notice, and a single-storey dwelling never needs a permit. The server works out
          the dates when you submit.
        </p>
      </div>

      {amending && (
        <Field label="Why are you changing it? (required)" hint="A later start, removing a fact or cancelling takes an owner or SHE officer when it weakens an open notice.">
          <Textarea name="amend_reason" required maxLength={500} rows={2} defaultValue={v?.amend_reason} />
        </Field>
      )}
      <FormError state={state} />
      <SubmitButton pendingText="Submitting…">{amending ? `Submit version ${version}` : "Record the work"}</SubmitButton>
    </form>
  );
}
