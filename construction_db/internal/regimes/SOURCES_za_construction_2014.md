# Sources: za_construction_2014

What each key in `data/za_construction_2014/` rests on, so the legal content can
be checked without rereading the regulations. All sources were read on
**28 Oct 2026**. Quotes are exact; where a site only allowed short quotes, the
pieces are joined in order.

Sources:

- **CR 2014**: Construction Regulations, 2014 (GN R84, GG 37305, 7 Feb 2014).
  Acts Online copy headed "7 February 2014 – to date":
  https://www.ecr.co.za/documents/62/SA_OSHACT.pdf (51 pp.).
- **OHSA**: Occupational Health and Safety Act 85 of 1993, s24:
  https://www.acts.co.za/occupational-health-/24__report_to_inspectors_regarding_certain_incidents
- **GAR**: General Administrative Regulations, 2003 (R929 of 2003). The
  acts.co.za URL slugs are mislabelled, so these are named by the regulation
  in the page body:
  - reg 8: https://www.acts.co.za/occupational-health-/gar_8_recording_and_investigation_of_incidents
  - reg 9: https://www.acts.co.za/occupational-health-/gar_9_witnesses_at_an_inquiry
- **COIDA**: Compensation for Occupational Injuries and Diseases Act 130 of
  1993, consolidated with Act 10 of 2022:
  https://www.acts.co.za/compensation/39__notice_of_accident_by_employer_to_commissioner

## Deadlines (`deadlines.json`)

| key | rests on |
|---|---|
| `ohsa_gar8_1a_written_notice` | GAR 8(1)(a): "within seven days of any incident referred to in section 24(1)(a) of the Act, give notice thereof to the provincial director in the form of WCL1 or WCL2" |
| `ohsa_gar8_1b_immediate_notice` | GAR 8(1)(b): "where a person, in consequence of such an incident, dies, becomes unconscious, suffers the loss of a limb or part of a limb, or is otherwise injured or becomes ill to such a degree that he or she is likely either to die or to suffer a permanent physical defect, such incident, including any other incident contemplated in section 24(1)(6) and (c) of the Act, shall forthwith also be reported to the provincial director by telephone, facsimile or similar means of communication." |
| `ohsa_gar8_2_death_after_notice` | GAR 8(2): "If an injured person dies after notice of the incident in which he or she was injured was given in terms of subregulation (1), the employer or user, as the case may be, shall forthwith notify the provincial director of his or her death." |
| `ohsa_gar9_2_investigation` | GAR 9(2): "An employer or user shall cause every incident, which must be recorded in terms of subregulation (1), to be investigated by the employer, a person appointed by him or her, by a health and safety representative or a member of a health and safety committee within 7 days from the date of the incident and finalised as soon as is reasonably practicable, or within the contracted period in the case of contracted workers." |
| `coida_s39_employer_report` | COIDA s39(1): "Subject to the provisions of this section an employer shall within seven days after having received notice of an accident or having learned in some other way that an employee has met with an accident, report the accident to the commissioner in the prescribed manner." No amendment note on (1). |
| `cr2014_reg4_notification` | CR 2014 reg 4(1): "must at least 7 days before that work is to be carried out notify the provincial director in writing in a form similar to Annexure 2 if the intended construction work will - (a) include excavation work; (b) include working at a height where there is risk of falling; (c) include the demolition of a structure; or (d) include the use of explosives to perform construction work." Reg 4(2) adds a single-storey dwelling for a client who will live in it. |
| `cr2014_reg3_permit_application` | CR 2014 reg 3(1): "A client who intends to have construction work carried out, must at least 30 days before that work is to be carried out apply to the provincial director in writing for a construction work permit". The triggers are (a) more than 180 days, (b) more than 1800 person-days, or (c) a works contract of R13m or more or CIDB grade 6. Rev 4 §3.2 says the R13m was raised by exemption notice to above R40m from 7 Feb 2017. That isn't modelled here; it belongs to the Reg 3/4 module. |

Readings we chose:

- **GAR 8(1)(b), "section 24(1)(6)":** we read "(6)" as a printing error for
  "(b)". Section 24(1) has no paragraph (6), and (b) is the major-incident
  paragraph. Major incidents therefore take the forthwith notice.
- **GAR 8(1)(a) covers s24(1)(a) only**, so major incidents and s24(1)(c)
  events get the forthwith notice but not the 7-day written notice.
- **COIDA routing:** the engine attaches `coida_s39_employer_report` when the
  injured person is an employee; it isn't attached to an incident type. Which
  insurer the report goes to (FEM or the Compensation Fund) comes from the
  organisation profile (Rev 4 §2.9).
- **GAR 8(2):** the engine attaches `ohsa_gar8_2_death_after_notice` when a
  reported person later dies. It isn't attached to an incident type.

## Incident taxonomy (`taxonomies.json`)

OHSA s24(1): "Each incident occurring at work or arising out of or in
connection with the activities of persons at work, or in connection with the use
of plant or machinery, in which, or in consequence of which—"

| key | provision | text |
|---|---|---|
| `fatality`, `unconsciousness`, `loss_of_limb`, `injury_likely_fatal_or_permanent` | s24(1)(a) | "any person dies, becomes unconscious, suffers the loss of a limb or part of a limb or is otherwise injured or becomes ill to such a degree that he is likely either to die or to suffer a permanent physical defect" |
| `injury_14_days_unable_to_work` | s24(1)(a) | "or likely to be unable for a period of at least 14 days either to work or to continue with the activity for which he was employed or is usually employed;" |
| `major_incident` | s24(1)(b) | "a major incident occurred; or" |
| `dangerous_substance_spill` | s24(1)(c)(i) | "the health or safety of any person was endangered and where— dangerous substance was spilled;" |
| `pressure_release` | s24(1)(c)(ii) | "the uncontrolled release of any substance under pressure took place;" |
| `machinery_failure_flying_objects` | s24(1)(c)(iii) | "machinery or any part thereof fractured or failed resulting in flying, falling or uncontrolled moving objects; or" |
| `machinery_out_of_control` | s24(1)(c)(iv) | "machinery ran out of control," |
| `medical_treatment` | GAR 9(1) | "any other incident which resulted in the person concerned having had to receive medical treatment other than first aid". It is recorded and investigated (GAR 9(2)), not reported. |
| `first_aid`, `near_miss`, `property_damage` | none | Product categories with no statutory clock (Rev 3 §2.4: near misses get a "no-blame entry"). |

Not in the taxonomy: GAR 8(3) (incidents to people other than employees) and
OHSA s24(3) (exclusions such as "a traffic accident on a public road"). Both
depend on who was involved and where, so they are engine logic for M8.

## Checklists (`checklists.json`)

All items are `pass_fail_na`. Each prompt paraphrases the paragraph in the
item's `source_ref`, and each template's `source_ref` is the inspection duty.

| key | cadence | inspection duty (CR 2014) |
|---|---|---|
| `excavation_inspection` | per_shift | reg 13(2)(h): inspected "(i) daily, prior to the commencement of each shift; (ii) after every blasting operation; (iii) after an unexpected fall of ground; (iv) after damage to supports; and (v) after rain" |
| `mobile_plant_pre_use` | daily | reg 23(1)(k): "inspected by the authorised operator or driver on a daily basis using a relevant checklist prior to use" |
| `temporary_works_inspection` | daily | reg 12(3)(f): "inspected by a competent person immediately before, during and after the placement of concrete, after inclement weather or any other imposed load and at least on a daily basis" |
| `temporary_electrical_weekly` | weekly | reg 24(d): "inspected at least once a week by a competent person" |
| `housekeeping_walk` | daily | reg 27: "suitable housekeeping is continuously implemented on each construction site". The regulation sets no frequency; daily is the product default (Rev 3 §2.3.3). |

Event-triggered inspections (after blasting, after rain, before and after a
pour) start the same templates ad hoc. M7 handles scheduling.

## Vocabulary (`vocabulary.json`)

From Rev 4 §3.11: "Site diary (not daily log) · contract instruction (not
change order) · principal agent · principal contractor · employer (JBCC) /
client (Construction Regulations) · practical completion · snag list ·
preliminaries and generals · bill of quantities." Keys are the generic term and
labels are the construction term. `client` and `employer` are the same party
in the two documents, so the UI picks one by context. Vocabulary keys aren't
locked.

## Gaps (known and deliberate)

- **Scaffolding:** CR 2014 reg 16(2) says "A contractor using access
  scaffolding must ensure that such scaffolding, when in use, complies with the
  safety standards incorporated for this purpose into these Regulations under
  section 44 of the Act." The regulation has no inspection items of its own, so
  there's no scaffolding checklist until we have the incorporated standard.
- **Not yet modelled:** W.Cl.5 monthly reports, the 14-day occupational disease
  notice and the JBCC notice clocks. They come with the IOD and JBCC milestones.

## Where Rev 4 is wrong

Rev 4 is `docs/spec/SiteProof_Rev4_Construction_and_Mining.pdf`.

1. **Rev 4 §3.2:** "Regulation 4 — for all other work, notify the provincial
   director at least 7 days before work begins (Regulation 4)."
   **CR 2014 reg 4(1)** only applies "if the intended construction work will -
   (a) include excavation work; (b) include working at a height where there is
   risk of falling; (c) include the demolition of a structure; or (d) include the
   use of explosives to perform construction work". Reg 4(2) adds the
   single-storey dwelling. Not all other work needs a notification.

2. **Rev 4 §3.5:** "OHSA section 24 — death, unconsciousness or disablement
   reportable to the Department of Employment and Labour on Annexure 1
   (MAKROSAFE)." (Rev 3 §2.4: "on the prescribed Annexure 1 form".)
   **GAR 8(1)(a):** notice is given "in the form of WCL1 or WCL2".
   **GAR 9(1):** Annexure 1 is "a record" kept "at a workplace … for a period of
   at least three years". Also, "death, unconsciousness or disablement" is
   narrower than s24(1). It misses the 14-day case, s24(1)(b) and s24(1)(c).

3. **Rev 4 §3.5:** "COIDA — report within 7 days on W.Cl.2; failure is an
   offence (Bowmans)." (Rev 3 §2.4: "Failure is an offence under section
   39(6).")
   **COIDA s39(6), as substituted by s19(a) of Act 10 of 2022, effective 1 April
   2026 (Proclamation Notice 306, GG53990):** "An employer, excluding an employer
   referred to in section 84(1)(a)(i), (ii) and (iii), who fails to comply with
   subsection (1) shall be liable to a penalty of 10 per cent of the actual or
   estimated annual earnings of that particular year."
   **s39(8):** "the Commissioner may impose a penalty equal to the full amount of
   compensation payable plus interest from the date of the accident."
   Caveat: that no other section still makes this an offence comes from law-firm
   summaries (Bowmans; Cliffe Dekker Hofmeyr citing s99), not the Act's text.
