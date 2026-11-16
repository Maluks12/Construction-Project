"use client";

import { useActionState, useState } from "react";

import { submitIncident } from "@/app/actions/incidents";
import { FormError, SubmitButton } from "@/components/client";
import { Alert, Card, Field, Input, Select, Textarea, buttonClass, cx } from "@/components/ui";
import type { IncidentTypeDef } from "@/lib/regime";
import type { PersonInput, Treatment } from "@/lib/types";

const BLANK: PersonInput = { employee_of_org: true, injured: false };

// The API refuses injury details for someone not injured, and an employer
// name for an employee, so fields that no longer apply are dropped.
function tidy(p: PersonInput): PersonInput {
  const text = (s?: string | null) => (s && s.trim() ? s.trim() : null);
  return {
    name: text(p.name),
    occupation: text(p.occupation),
    employee_of_org: p.employee_of_org,
    employer_name: p.employee_of_org ? null : text(p.employer_name),
    injured: p.injured,
    injury_description: p.injured ? text(p.injury_description) : null,
    body_part: p.injured ? text(p.body_part) : null,
    treatment: p.injured ? (p.treatment ?? null) : null,
    died_at: p.injured ? text(p.died_at) : null,
  };
}

export function IncidentForm({
  siteId,
  incidentId,
  version,
  types,
  initial,
}: {
  siteId: string;
  incidentId: string;
  version: number;
  types: IncidentTypeDef[];
  initial: {
    occurred_at: string;
    employer_aware_at: string;
    location: string;
    description: string;
    types: string[];
    people: PersonInput[];
    evidence_ids: string[];
  };
}) {
  const [state, action] = useActionState(submitIncident, null);
  const v = state?.values;
  const [checked, setChecked] = useState<string[]>(v?.types ? v.types.split(",").filter(Boolean) : initial.types);
  const [people, setPeople] = useState<PersonInput[]>(initial.people);
  const amending = version > 1;

  const setPerson = (i: number, patch: Partial<PersonInput>) =>
    setPeople(people.map((p, n) => (n === i ? { ...p, ...patch } : p)));
  const reportable = types.some((t) => t.reportable && checked.includes(t.key));

  if (types.length === 0) {
    return (
      <Alert tone="warn">
        This dashboard doesn&apos;t have the incident types for this site&apos;s regulations yet, so an incident can&apos;t
        be classified here. Report it from the phone.
      </Alert>
    );
  }

  return (
    <form action={action} className="flex flex-wrap items-start gap-5">
      <input type="hidden" name="site_id" value={siteId} />
      <input type="hidden" name="incident_id" value={incidentId} />
      <input type="hidden" name="version" value={version} />
      <input type="hidden" name="people" value={JSON.stringify(people.map(tidy))} />
      {initial.evidence_ids.map((id) => (
        <input key={id} type="hidden" name="evidence_ids" value={id} />
      ))}

      <div className="flex min-w-0 flex-[999_1_620px] flex-col gap-4">
        <Card>
          <div className="grid gap-3.5 [grid-template-columns:repeat(auto-fit,minmax(220px,1fr))]">
            <Field label="When it happened (SAST)">
              <Input name="occurred_at" type="datetime-local" required defaultValue={v?.occurred_at ?? initial.occurred_at} />
            </Field>
            <Field
              label="When the employer became aware"
              hint="Starts the COIDA clock. Needed when an employee is injured. Moving it later takes an owner or SHE officer."
            >
              <Input name="employer_aware_at" type="datetime-local" defaultValue={v?.employer_aware_at ?? initial.employer_aware_at} />
            </Field>
            <Field label="Where">
              <Input name="location" maxLength={500} defaultValue={v?.location ?? initial.location} />
            </Field>
          </div>
        </Card>

        <Card>
          <fieldset>
            <legend className="text-sm font-semibold">What happened? Tick every type that applies</legend>
            <p className="mt-1 mb-2.5 text-[13px] text-muted">
              Types come from the site&apos;s regime. Deadlines are the combined rules of every type ticked.
            </p>
            <div className="grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(240px,1fr))]">
              {types.map((t) => {
                const on = checked.includes(t.key);
                return (
                  <label
                    key={t.key}
                    className={cx(
                      "flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm",
                      on ? "border-accent bg-accent-soft" : "border-line-strong",
                    )}
                  >
                    <input
                      type="checkbox"
                      name="taxonomy_codes"
                      value={t.key}
                      checked={on}
                      onChange={() => setChecked(on ? checked.filter((k) => k !== t.key) : [...checked, t.key])}
                      className="mt-0.5 accent-[var(--accent)]"
                    />
                    <span>
                      {t.label}
                      {t.reportable && <span className="block text-xs text-muted">Reportable</span>}
                    </span>
                  </label>
                );
              })}
            </div>
          </fieldset>
        </Card>

        <Card
          title="People involved"
          description="Names are optional, so a near miss can stay no-blame. Injury details are health information."
          actions={
            <button type="button" className={buttonClass("secondary", "sm")} onClick={() => setPeople([...people, { ...BLANK }])}>
              Add person
            </button>
          }
        >
          {people.length === 0 && <p className="text-sm text-muted">Nobody listed.</p>}
          <div className="flex flex-col gap-3">
            {people.map((p, i) => (
              <div key={i} className="grid gap-3 rounded-lg border border-line-soft p-3.5 [grid-template-columns:repeat(auto-fit,minmax(180px,1fr))]">
                <Field label="Name (optional)">
                  <Input value={p.name ?? ""} maxLength={200} onChange={(e) => setPerson(i, { name: e.target.value })} />
                </Field>
                <Field label="Role">
                  <Input value={p.occupation ?? ""} maxLength={200} onChange={(e) => setPerson(i, { occupation: e.target.value })} />
                </Field>
                <Field label="Employee of this organisation?">
                  <Select value={p.employee_of_org ? "yes" : "no"} onChange={(e) => setPerson(i, { employee_of_org: e.target.value === "yes" })}>
                    <option value="yes">Yes</option>
                    <option value="no">No</option>
                  </Select>
                </Field>
                {!p.employee_of_org && (
                  <Field label="Their employer">
                    <Input value={p.employer_name ?? ""} maxLength={200} onChange={(e) => setPerson(i, { employer_name: e.target.value })} />
                  </Field>
                )}
                <Field label="Injured?">
                  <Select value={p.injured ? "yes" : "no"} onChange={(e) => setPerson(i, { injured: e.target.value === "yes" })}>
                    <option value="no">No</option>
                    <option value="yes">Yes</option>
                  </Select>
                </Field>
                {p.injured && (
                  <>
                    <Field label="Injury">
                      <Input value={p.injury_description ?? ""} maxLength={2000} onChange={(e) => setPerson(i, { injury_description: e.target.value })} />
                    </Field>
                    <Field label="Body part">
                      <Input value={p.body_part ?? ""} maxLength={200} onChange={(e) => setPerson(i, { body_part: e.target.value })} />
                    </Field>
                    <Field label="Treatment">
                      <Select value={p.treatment ?? ""} onChange={(e) => setPerson(i, { treatment: (e.target.value || null) as Treatment | null })}>
                        <option value="">Not recorded</option>
                        <option value="none">None</option>
                        <option value="first_aid">First aid on site</option>
                        <option value="medical_treatment">Medical treatment</option>
                        <option value="hospitalised">Hospitalised</option>
                      </Select>
                    </Field>
                    <Field label="Died (date and time)" hint="Leave empty unless the person died.">
                      <Input type="datetime-local" value={p.died_at ?? ""} onChange={(e) => setPerson(i, { died_at: e.target.value })} />
                    </Field>
                  </>
                )}
                <div className="flex items-end">
                  <button type="button" className={buttonClass("ghost", "sm")} onClick={() => setPeople(people.filter((_, n) => n !== i))}>
                    Remove
                  </button>
                </div>
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <div className="flex flex-col gap-3">
            <Field label="What happened, in order">
              <Textarea name="description" rows={4} maxLength={5000} defaultValue={v?.description ?? initial.description} />
            </Field>
            {amending && (
              <Field label="Why are you amending it? (required)">
                <Textarea name="amend_reason" rows={2} required maxLength={500} defaultValue={v?.amend_reason} />
              </Field>
            )}
            <p className="text-[13px] text-muted">
              Photos are taken and sealed on the phone.
              {initial.evidence_ids.length > 0 && ` The ${initial.evidence_ids.length} already attached stay attached.`}
            </p>
          </div>
        </Card>
      </div>

      <Card className="flex-[1_1_340px]" title="What submitting will do">
        <div className="flex flex-col gap-3">
          <p className="text-[13px] text-muted">
            The server works out the legal deadlines from the types ticked and the people listed, and shows them on the
            incident. Nothing changes until you submit.
          </p>
          {reportable && (
            <Alert tone="danger">
              Reportable. Submitting starts the legal clocks, which can include telling the provincial inspector now.
            </Alert>
          )}
          {amending && (
            <p className="text-[13px] text-muted">
              Earlier versions stay readable exactly as they were submitted. Withdrawing a report deadline or moving it
              later takes an owner or SHE officer.
            </p>
          )}
          <FormError state={state} />
          <SubmitButton pendingText="Submitting…">Submit version {version}</SubmitButton>
        </div>
      </Card>
    </form>
  );
}
