// Regime facts the API documents but doesn't serve yet.
//
// docs/api/incidents.md lists a regime's incident types as a table (there is
// no endpoint for them), so that table is copied here (and in web/src/lib/regime.ts), keyed by the regime
// code from GET /v1/sites/:siteId/checklists. A new regime or a changed type
// list needs this file updated to match the doc.

export type IncidentTypeDef = { key: string; label: string; reportable: boolean };

const ZA_CONSTRUCTION_2014: IncidentTypeDef[] = [
  { key: "fatality", label: "Death", reportable: true },
  { key: "unconsciousness", label: "Person became unconscious", reportable: true },
  { key: "loss_of_limb", label: "Loss of a limb or part of a limb", reportable: true },
  { key: "injury_likely_fatal_or_permanent", label: "Injury or illness likely to cause death or a permanent physical defect", reportable: true },
  { key: "injury_14_days_unable_to_work", label: "Injury or illness likely to keep the person off work for at least 14 days", reportable: true },
  { key: "major_incident", label: "Major incident", reportable: true },
  { key: "dangerous_substance_spill", label: "Dangerous substance spilled, endangering health or safety", reportable: true },
  { key: "pressure_release", label: "Uncontrolled release of a substance under pressure, endangering health or safety", reportable: true },
  { key: "machinery_failure_flying_objects", label: "Machinery fractured or failed, causing flying, falling or uncontrolled moving objects", reportable: true },
  { key: "machinery_out_of_control", label: "Machinery ran out of control", reportable: true },
  { key: "medical_treatment", label: "Injury needing medical treatment beyond first aid", reportable: false },
  { key: "first_aid", label: "First aid only", reportable: false },
  { key: "near_miss", label: "Near miss", reportable: false },
  { key: "property_damage", label: "Property or equipment damage only", reportable: false },
];

const BY_REGIME: Record<string, IncidentTypeDef[]> = {
  za_construction_2014: ZA_CONSTRUCTION_2014,
};

// [] for a regime this dashboard doesn't know: the form then says so instead
// of offering another regime's types.
export function incidentTypes(regimeCode: string | undefined): IncidentTypeDef[] {
  return (regimeCode && BY_REGIME[regimeCode]) || [];
}
