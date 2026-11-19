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

Added for the deadline engine (M8), read on **15 Nov 2026**:

- **PHA**: Public Holidays Act 36 of 1994. Acts Online, consolidated text with
  no amendment notes:
  - s2: https://www.acts.co.za/public-holidays-act-1994/2_days_to_be_observed_as_public_holidays
  - Schedule 1: https://www.acts.co.za/public-holidays-act-1994/schedule_1
  - Section 2A (the President's power to declare a day) is not on Acts
    Online's section list; both proclamations below cite it. Its text has not
    been read.
- **Proclamations under PHA s2A** (Acts Online, "Notices and Proclamations"):
  - Proclamation Notice 157 of 2024, GG 50165:
    https://www.acts.co.za/public-holidays-act-1994/pn157_proclamation_notice_157_
  - Proclamation Notice 346 of 2026, GG 55352:
    https://www.acts.co.za/public-holidays-act-1994/pn346_proclamation_notice_346_
- **IA**: Interpretation Act 33 of 1957, as published in the Union Gazette
  Extraordinary of 24 May 1957 (gov.za copy):
  https://www.gov.za/sites/default/files/gcis_document/201505/act-33-1957.pdf.
  The Supreme Court of Appeal quotes s4 in the same words in *Road Accident
  Fund v Masindi* (586/2017) [2018] ZASCA 94, para [8]:
  https://www.supremecourtofappeal.org.za/index.php/judgements/send/3-judgments-2018/331-road-accident-fund-v-masindi-586-2017-2018-zasca-94-1-june-2018

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
- **COIDA routing:** `coida_s39_employer_report` has `applies_when:
  employee_injured`: the engine attaches it when a person in the incident is
  an employee of the organisation and injured or dead, whatever the incident
  type, first aid included (decided 15 Nov 2026). s39(1) doesn't say whether a
  first-aid-only injury is an accident to report; attaching it is the
  conservative reading. Its basis (`employer_notified_at`) is the incident's
  `employer_aware_at`, "having received notice of an accident or having
  learned in some other way". The IOD milestone reuses this clock; it must
  not start a second one. Which insurer the report goes to (FEM or the
  Compensation Fund) comes from the organisation profile (Rev 4 §2.9).
- **GAR 8(2):** `ohsa_gar8_2_death_after_notice` has `applies_when:
  person_died_after_notice` and `after_satisfied` = the two GAR 8(1) rules:
  "dies after notice of the incident ... was given in terms of subregulation
  (1)". It attaches when a person's death is later than a recorded GAR 8(1)
  notice. A death before any notice is covered by the GAR 8(1) notices
  themselves.
- **Reg 3 and reg 4** (`cr2014_reg3_permit_application`,
  `cr2014_reg4_notification`) have `applies_when: site_work`: they run from
  starting work on a site, not from an incident, so no incident type may list
  them. M8 creates no deadlines for them; the Reg 4 module will. Whether "at
  least 7 days before" means 7 clear days is not settled here; that module
  decides it.

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
depend on who was involved and where. **Known gap (decided 15 Nov 2026):**
neither text has been quoted here, so M8 does not model them. Every
reportable type starts its clocks whoever was hurt and wherever, which
over-reports rather than under-reports. The two GAR 8(1) rules carry a
`caveat` saying exclusions may apply. BUILD_PLAN has a task to quote and
model both.

An incident version may carry several types (decided 15 Nov 2026): one event
can fall under more than one paragraph of s24(1), and its clocks are the union
of the types' rules. Rev 4 §5.3 shows a single `taxonomy_code`.

## Counting days and the holiday calendar (M8)

The rules above say "within seven days of any incident" (GAR 8(1)(a)),
"within 7 days from the date of the incident" (GAR 9(2)) and "within seven
days after having received notice" (COIDA s39(1)). None says how to count.

**IA s4:** "When any particular number of days is prescribed for the doing of
any act, or for any other purpose, the same shall be reckoned exclusively of
the first and inclusively of the last day, unless the last day happens to fall
on a Sunday or on any public holiday, in which case the time shall be reckoned
exclusively of the first day and exclusively also of every such Sunday or
public holiday."

**IA s1** applies the Act "to the interpretation of every law ... and to the
interpretation of all by-laws, rules, regulations or orders made under the
authority of any such law, unless there is something in the language or
context ... repugnant to such provisions or unless the contrary intention
appears therein". GAR are regulations under OHSA. Nothing quoted above shows a
contrary intention, but that has not been checked against the whole of GAR.

What the engine does (decided 15 Nov 2026):

- **`due`** (alerts and states use this one): the basis's local date is
  excluded and the period counted from the next day; due by the end of the
  last day in the regime's `time_zone`. It is **not** extended when the last
  day is a Sunday or public holiday. That is never later than the law's date
  (Rev 3 §8.2: "conservative alerts").
- **`legal_latest`**: the same count, with the s4 extension: while the last
  day is a Sunday or a public holiday, it moves to the next day. Saturdays
  don't move it; s4 names only Sundays and public holidays. Shown next to
  `due` with IA s4 as its source, never used for alerts. Example: an incident
  on Fri 18 Dec 2026 is `due` by the end of Fri 25 Dec; `legal_latest` is Mon
  28 Dec (25 and 26 Dec are public holidays, 27 Dec is a Sunday).
- *Masindi* is cited only for s4's wording in 2018, not for its holding,
  which concerns a period in years.

**Open question: which Sundays and public holidays s4 excludes.** "Exclusively
also of every such Sunday or public holiday" can be read two ways once the last
day is a Sunday or public holiday:

- (A) only the run of Sundays and public holidays at the end is skipped, and
  the period ends on the next day that is neither. This is what
  `legal_latest` computes.
- (B) every Sunday and public holiday inside the period is excluded too, so
  the period runs longer. For an incident on Fri 18 Dec 2026, (A) gives
  Mon 28 Dec and (B) gives at least Tue 29 Dec.

The only source found that addresses the point is commentary, not a court,
and it reads s4 as (A). Momentum's legal update on Pension Funds Act s13A
(No. 7 of 2012, 16 Aug 2012; repeated in No. 7 of 2015, May 2015) quotes s4
and says: "This means that the seven day period must be calculated excluding
the first day and including the last day, unless the last day falls on a
Sunday or a public holiday, in which case the period will end on the next
business day, but including a Saturday." Its table puts October 2015's due
date on "9 November 2015*" ("*8th falls on a Sunday"). No Sunday or holiday
falls inside that period, so the example doesn't test (B).

A Deeds Office counting note, quoted by Schindlers Attorneys ("Deeds Office v
Municipalities: The 60 Day Rule and Rates Clearances", 25 Sep 2018), is also
consistent with (A): "Count 60 days (This include Saturdays, Sundays and
Public holidays)", then "UNLESS day 60 falls on a Sunday or Public holiday – of
which the next working day will be the expiry date". That is one office's
practice, and its rule that a Saturday day 60 moves back to the Friday isn't in
s4.

Not settled: no judgment deciding (A) against (B) has been read. *Nedcor Bank
v Master* [2001] ZASCA 106 and *Latiff v Donro* [2004] ZALC 56 apply s4, but
SAFLII refused automated reads, so neither was checked. Until a court's
reading is quoted here, `legal_latest` stays (A). (A) is never later than (B),
so the date shown is one the law allows *at least* until; the handover
(docs/api/m8-handover.md) words it that way.

**PHA s2(1):** "The days mentioned in Schedule 1 shall be public holidays,
and whenever any public holiday falls on a Sunday, the following Monday shall
be a public holiday." s2(2) lets an employer and employee exchange a public
holiday for another day; that is between them and doesn't move a statutory
deadline.

**PHA Schedule 1**, "Calendar of public holidays": New Year's Day, 1 January;
Human Rights Day, 21 March; Good Friday, "Friday before Easter Sunday"; Family
Day, "Monday after Easter Sunday"; Freedom Day, 27 April; Workers' Day, 1 May;
Youth Day, 16 June; National Women's Day, 9 August; Heritage Day, 24
September; Day of Reconciliation, 16 December; Christmas Day, 25 December; Day
of Goodwill, 26 December.

Reading: s2(1) makes "the following Monday" a public holiday. When that Monday
is already one (25 December on a Sunday), s2(1) adds nothing; any extra day
needs a proclamation under s2A (as for 27 December 2022). A Saturday holiday
doesn't move.

**Proclaimed days (PHA s2A)** in the calendar's range:

| date | proclamation | quote |
|---|---|---|
| Wed 29 May 2024 | Proclamation Notice 157 of 2024, GG 50165 | "In terms of section 2A of the Public Holidays Act, 1994 (Act No. 36 of 1994), I hereby declare the Twenty-Ninth day of May 2024 as a public holiday throughout the Republic." |
| Wed 4 Nov 2026 | Proclamation Notice 346 of 2026, GG 55352 | "In terms of section 2A of the Public Holidays Act, 1994 (Act No. 36 of 1994), I hereby declare the Fourth day of November 2026 as a public holiday throughout the Republic in connection with the holding of local government elections." |

Acts Online's list has no other declared day from 2024 to Nov 2026. Its
pages don't label which date is the Gazette's (PN 157: "23 February 2024",
signed 21 February; PN 346: "8 September 2026", signed 4 September). The PN 157
page also carries a wrong heading copied from the 2023 notice; its title is
the 29 May 2024 one.

The calendar is the `public_holidays` table (migration 000015), not a
constant. A newly proclaimed day is added by a new migration with its
proclamation and Gazette number, and quoted here.

**Working days:** no rule here uses them, and none of these sources defines a
working day. JBCC defines its own; it will be regime data when JBCC lands.
**Site shutdowns** (the builders' holiday) are a contractual matter. Nothing
quoted here stops a statutory clock for one, so they don't move a `due` or
`legal_latest` date.

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

## Toolbox topics (`toolbox_topics.json`, M10)

Read on **16 Nov 2026** from the same CR 2014 copy. CR 2014 doesn't mention
toolbox talks; each topic is a subject a talk covers, and its `source_ref` is
the regulation that sets the duty the talk is about. The quote is the
regulation's heading and the start of its first duty. A talk stores the topic's
key and its own title, so a later wording fix never alters a past talk.

| key | source_ref | CR 2014 |
|---|---|---|
| `fall_protection` | reg 10 | "10. Fall protection (1) A contractor must - (a) designate a competent person to be responsible for the preparation of a fall protection plan" |
| `excavations` | reg 13 | "13. Excavation (1) A contractor must - (a) ensure that all excavation work is carried out under the supervision of a competent person who has been appointed in writing for that purpose; and (b) evaluate, as far as is reasonably practicable, the stability of the ground before excavation work begins." |
| `scaffolding` | reg 16 | "16. Scaffolding (1) A contractor must appoint a competent person in writing who must ensure that all scaffolding work operations are carried out under his or her supervision and that all scaffold erectors, team leaders and inspectors are competent to carry out their work." |
| `mobile_plant` | reg 23 | "23. Construction vehicles and mobile plant (1) A contractor must ensure that all construction vehicles and mobile plant - (a) are of an acceptable design and construction; (b) are maintained in a good working order" |
| `electrical_safety` | reg 24 | "24. Electrical installations and machinery on construction sites … (a) before construction commences and during the progress thereof, adequate steps are taken to ascertain the presence of and guard against danger to workers from any electrical cable or apparatus which is under, over or on the site" |
| `flammable_liquids` | reg 25 | "25. Use and temporary storage of flammable liquids on construction sites A contactor must, in addition to compliance with the provisions for the use and storage of flammable liquids in the General Safety Regulations, 2003, ensure that -" ("contactor" as printed) |
| `housekeeping` | reg 27 | "27. Housekeeping and general safeguarding on construction sites … ensure that suitable housekeeping is continuously implemented on each construction site, including - (a) the proper storage of materials and equipment; (b) the removal of scrap, waste and debris at appropriate intervals" |
| `stacking_storage` | reg 28 | "28. Stacking and storage on construction sites … (a) a competent person is appointed in writing with the duty of supervising all stacking and storage on a construction site; (b) adequate storage areas are provided" |
| `fire_precautions` | reg 29 | "29. Fire precautions on construction sites … (a) all appropriate measures are taken to avoid the risk of fire" |

Left out: **PPE** has no regulation of its own in CR 2014 (it's in the General
Safety Regulations, which this data set doesn't quote yet). **Induction**
(reg 7(5): "No contractor may allow or permit any employee or person to enter
any site, unless that employee or person has undergone health and safety
induction training pertaining to the hazards prevalent on the site at the time
of entry") is a record of its own (Rev 4 6.B, "Induction records"), not a talk.

**Signatures (D2).** POPIA s1: "'biometrics' means a technique of personal
identification that is based on physical, physiological or behavioural
characterisation including blood typing, fingerprinting, DNA analysis, retinal
scanning and voice recognition". Read on 16 Nov 2026 from a secondary copy of
the Act, https://popia.co.za/section-1-definitions/ (not the Gazette). A drawn
signature captured with its stroke dynamics would be behavioural
characterisation; Rev 4 §2.10 rules biometrics out, so a toolbox signature is a
tap and stores no drawing.

## Workforce, documents and reminders (`workforce.json`, `documents.json`, `reminders.json`, M11)

CR 2014 quotes were read on **18 Nov 2026** from the same Acts Online copy (the
ecr.co.za link now redirects to `turntable.kagiso.io/documents/SA_OSHACT.pdf`,
the same file: "7 February 2014 – to date", 51 pp.). In each table below, the
second column is the type's `source_ref`. `TestWorkforceTypesQuotedInSources`
ties every key to its row.

### Competency types

CR 2014 doesn't list certificates as such. Each type is a competence that a
regulation requires of the person doing the work.

| key | source_ref | quote |
|---|---|---|
| `mobile_plant_operator` | CR 2014 reg 23(1)(d)(i) | "are operated by a person who - (i) has received appropriate training, is certified competent and in possession of proof of competency and is authorised in writing to operate those construction vehicles and mobile plant" |
| `tower_crane_operator` | CR 2014 reg 22(e) | "the tower crane operators are competent to carry out the work safely" |
| `scaffold_erector` | CR 2014 reg 16(1) | "all scaffold erectors, team leaders and inspectors are competent to carry out their work" |
| `scaffold_inspector` | CR 2014 reg 16(1) | (as above) |
| `suspended_platform_operator` | CR 2014 reg 17(1) | "all suspended platform erectors, operators and inspectors are competent to carry out their work" |
| `rope_access_licence` | CR 2014 reg 18(1)(c) | "ensure that all rope access operators are competent and licensed to carry out their work" |
| `construction_hs_officer_registration` | CR 2014 reg 8(6) | "the construction health and safety officer that he or she intends to appoint is registered with a statutory body approved by the Chief Inspector" |

### Appointment types

Every one is an appointment **in writing**. Left out:
- reg 10(1)(a), fall protection: it says "designate a competent person", not appoint;
- reg 13(1)(k), explosives in excavations: it says "appoint a competent person" without "in writing";
- regs 21 and 22: they require competence, not an appointment.

| key | source_ref | quote |
|---|---|---|
| `construction_manager` | CR 2014 reg 8(1) | "A principal contractor must in writing appoint one full-time competent person as the construction manager with the duty of managing all the construction work on a single site" |
| `assistant_construction_manager` | CR 2014 reg 8(2) | "A principal contractor must upon having considered the size of the project, in writing appoint one or more assistant construction managers for different sections thereof" |
| `construction_hs_officer` | CR 2014 reg 8(5) | "appoint a full-time or part-time construction health and safety officer in writing to assist in the control of all health and safety related aspects on the site" |
| `construction_supervisor` | CR 2014 reg 8(7) | "A construction manager must in writing appoint construction supervisors responsible for construction activities and ensuring occupational health and safety compliance on the construction site." |
| `assistant_construction_supervisor` | CR 2014 reg 8(8) | "A contractor must, upon having considered the size of the project, in writing appoint one or more competent employees for different sections thereof to assist the construction supervisor" |
| `risk_assessor` | CR 2014 reg 9(1) | "have risk assessments performed by a competent person appointed in writing" |
| `temporary_works_designer` | CR 2014 reg 12(1) | "A contractor must appoint a temporary works designer in writing to design, inspect and approve the erected temporary works on site before use." |
| `temporary_works_supervisor` | CR 2014 reg 12(2) | "all temporary works operations are carried out under the supervision of a competent person who has been appointed in writing for that purpose" |
| `excavation_supervisor` | CR 2014 reg 13(1)(a) | "ensure that all excavation work is carried out under the supervision of a competent person who has been appointed in writing for that purpose" |
| `demolition_supervisor` | CR 2014 reg 14(1) | "A contractor must appoint a competent person in writing to supervise and control all demolition work on site." |
| `scaffolding_supervisor` | CR 2014 reg 16(1) | "A contractor must appoint a competent person in writing who must ensure that all scaffolding work operations are carried out under his or her supervision" |
| `suspended_platform_supervisor` | CR 2014 reg 17(1) | "A contractor must appoint a competent person in writing who must ensure that all suspended platforms work operations are carried out under his or her supervision" |
| `rope_access_supervisor` | CR 2014 reg 18(1)(a) | "appoint a competent person in writing as a rope access supervisor with the duty of supervising all rope access work on the site" |
| `material_hoist_inspector` | CR 2014 reg 19(8)(a) | "is inspected on daily basis by a competent person appointed in writing by the contractor" |
| `bulk_mixing_plant_supervisor` | CR 2014 reg 20(1) | "the operation of a bulk mixing plant is supervised by a competent person who has been appointed in writing" |
| `stacking_storage_supervisor` | CR 2014 reg 28(a) | "a competent person is appointed in writing with the duty of supervising all stacking and storage on a construction site" |

### Medical certificate of fitness

- **CR 2014 reg 7(8):** "A contractor must ensure that all his or her
  employees have a valid medical certificate of fitness specific to the
  construction work to be performed and issued by an occupational health
  practitioner in the form of Annexure 3."
  - Reg 1 defines it: "'medical certificate of fitness' means a certificate
    contemplated in regulation 7(8)".
  - Reg 7(1)(g) puts the same duty on a principal contractor.
  - So Rev 4 §3.4's "Regulation 7(8)" is right.
- **Validity is not in the regulations.** Neither reg 7 nor Annexure 3 says
  how long a certificate lasts; Annexure 3 has a single "Date".
  - Rev 4 §3.4's "valid for one year" is therefore a product default
    (`default_validity_months: 12`), proposed to the user.
  - The date on the certificate wins, and `expires_on` is always entered.
- **Outcome.** Annexure 3's declaration is only "satisfied myself that the
  abovementioned employee is fit to perform the duties as described by the
  employer".
  - `fit_with_restrictions` and `unfit` come from Rev 3 §2.9.4 ("fit /
    fit-with-conditions / unfit").
  - Rev 3 §2.9.4 also says to store "Not findings, diagnoses or restrictions".
  - Annexure 3 also has an "ID Number" field; we don't store it.
- **Practice number:** Annexure 3, "Practice Number".
- **POPIA**, from a secondary copy read on 18 Nov 2026
  (https://popia.co.za/section-26-prohibition-on-processing-of-special-personal-information/ and the section 32 page):
  - **s26:** "A responsible party may, subject to section 27, not process
    personal information concerning— ... the religious or philosophical
    beliefs, race or ethnic origin, trade union membership, political
    persuasion, health or sex life or biometric information of a data
    subject".
  - **s32(1)(f):** "administrative bodies, pension funds, employers or
    institutions working for them, if such processing is necessary for— (i)
    the implementation of the provisions of laws, pension regulations or
    collective agreements which create rights dependent on the health or sex
    life of the data subject".
  - **s32(2):** "the information may only be processed by responsible parties
    subject to an obligation of confidentiality by virtue of office,
    employment, profession or legal provision".
- **Who reads the certificate (decided 18 Nov, M11 D6).**
  - **The spec:** Rev 4 §3.4 says "Status only, never contents", and §2.12
    lets a SHE officer see health documents only "with the claims-handler
    capability".
  - **What we do:** we store the certificate itself, because inspectors and
    principal contractors ask for the certificate, not a status. Only an
    owner or SHE officer can open it, and every view is logged. A supervisor
    can capture it but not read it back.
  - **Why that's allowed:** it keeps processing to the reg 7(8) duty
    (s32(1)(f)(i)) and to people bound by their employment (s32(2)).

### Document types

| key | source_ref | quote |
|---|---|---|
| `health_and_safety_specification` | CR 2014 reg 5(1)(b) | "prepare a suitable, sufficiently documented and coherent site specific health and safety specification for the intended construction work" |
| `health_and_safety_plan` | CR 2014 reg 7(1)(a) | "provide and demonstrate to the client a suitable, sufficiently documented and coherent site specific health and safety plan" |
| `risk_assessment` | CR 2014 reg 9(1) | "have risk assessments performed by a competent person appointed in writing, which risk assessments form part of the health and safety plan" |
| `construction_work_permit` | CR 2014 reg 3(6) | "A client must ensure that the principal contractor keeps a copy of the construction work permit contemplated in subregulation (1) in the occupational health and safety file" |
| `notification_of_construction_work` | CR 2014 reg 4(1) | "must at least 7 days before that work is to be carried out notify the provincial director in writing in a form similar to Annexure 2" |
| `fall_protection_plan` | CR 2014 reg 10(1)(a) | "designate a competent person to be responsible for the preparation of a fall protection plan" |
| `principal_contractor_appointment` | CR 2014 reg 5(1)(k) | "appoint every principal contractor in writing for the project or part thereof on the construction site" |
| `coida_letter_of_good_standing` | Rev 4 §2.8 | "Issuer-aware: FEM, Compensation Fund or RMA for Letters of Good Standing." The Domain Reference §2.9 says "Letter of Good Standing, valid for 12 months". Not law: see Gaps. |
| `coida_registration` | Domain Reference §2.9 | "Registration is mandatory for any employer with one or more employees … CF reference number". COIDA s80 ("Employer to register with Compensation Fund and to furnish particulars") was not read. |
| `cidb_registration` | CIDB register of contractors | cidb.org.za, "Register of contractors: overview", as summarised (not quoted): a grade is valid for three years and must be updated annually. Not verified verbatim: see Gaps. |
| `tax_compliance_status` | Rev 4 A.8 | "Organisation document vault P1 LGS, tax, insurances, B-BBEE, registrations". The spec never names the tax document; see Gaps. |
| `public_liability_insurance` | Rev 4 A.8 | (as above: "insurances") |
| `bbbee_certificate` | Rev 4 A.8 | (as above: "B-BBEE") |

**The H&S file itself:**
- CR 2014 reg 7(1)(b): "open and keep on site a health and safety file, which
  must include all documentation required in terms of the Act and these
  Regulations, which must be made available on request to an inspector, the
  client, the client's agent or a contractor".
- Reg 1: "'health and safety file' means a file, or other record containing
  the information in writing required by these Regulations".
- **Required site types** are the three the regulations require of every
  project: the specification, the plan and the risk assessment. The others
  depend on the work.

### Reminders (`reminders.json`)

These are product defaults (M11 D25), not law:
- **Company documents:** 60, 30 and 7 days before, and on the day. Rev 4 §2.8:
  "escalating expiry alerts at 60, 30 and 7 days".
- **Competencies, medicals, appointments and H&S file documents:** 30, 14 and
  7 days before, and on the day. Nothing in the spec sets these.
- **Incident deadlines:** 2 days before and on the due day, then overdue.
  Forthwith deadlines: at once.
- **Actions:** Rev 4 A.4, "Escalation up the role hierarchy P1 To the owner on
  lapse". When overdue:
  - the action's owner and raiser at once;
  - SHE officers after 1 day;
  - owners after 3.

  A verification counts as stalled after 2 days.

A practitioner should review these together with the severity wording.

### Gaps (M11)

- **Letter of Good Standing:** neither COIDA's text nor the Compensation
  Fund's pages were read.
  - labour.gov.za has no page on it.
  - Acts Online lists Notices 3200 and 3227 of 2025, "Extension of Letters of
    Good Standing (LOG) Validity", but their bodies weren't readable.
  - So `expires_on` is required and comes from the letter itself; no
    validity is computed.
- **CIDB:** only cidb.org.za's summary was read, not the CIDB Act 38 of 2000
  or its Regulations. `expires_on` is entered from the certificate.
- **Tax:** SARS's Tax Compliance Status page returned 404.
  - The type is named `tax_compliance_status`, SARS's current term for what
    replaced the tax clearance certificate. It has an optional expiry.
  - To verify against SARS.

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
