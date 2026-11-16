// Sample records for the screens that aren't connected to the API yet:
// toolbox talks and the site diary (M10) and the outbox (SYNC). Taken from
// the SiteProof prototype. Each screen using this shows a "Sample data"
// note. Replace with real reads when the endpoints exist.

import type { Tone } from "@/theme";

export const toolboxTalk = {
  topic: "Scaffold boards: clip and check before you step",
  when: "Toolbox talk · today 12:30 · Thandi Mokoena",
  people: [
    ["Lerato Dube", "Pillay Construction"],
    ["Johan van Wyk", "Pillay Construction"],
    ["Nomvula Sithole", "Pillay Construction"],
    ["Ayanda Khumalo", "Khumalo Electrical"],
    ["Thabo Mahlangu", "Pillay Construction"],
    ["Pieter Botha", "Botha Steel"],
    ["Zanele Mthembu", "Pillay Construction"],
    ["Kagiso Molefe", "Khumalo Electrical"],
  ] as [string, string][],
  signed: { 0: "12:41", 1: "12:41", 2: "12:42", 4: "12:43" } as Record<number, string>,
};

export const diary = {
  day: "Mon 16 Nov",
  weather: ["Clear", "Thunderstorms", "Rain", "Wind", "Heat"],
  headcount: "41",
  headcountHint: "41 people signed in today (toolbox talk and site sign-in). Change it if more were on site.",
  work: "Level 3 brickwork east; basement waterproofing; excavation north bay stopped.",
  sections: [
    { key: "weather", title: "Weather times", summary: "Rain 14:00–16:30", rows: [["Rain started", "14:00"], ["Stopped", "16:30"], ["Rain gauge", "18 mm"], ["Hours lost", "1.5"]] },
    { key: "labour", title: "Labour by firm", summary: "5 firms", rows: [["Pillay Construction", "24"], ["Botha Steel", "9"], ["Khumalo Electrical", "6"], ["Moloi Plumbing", "4"], ["Sky Scaffolding", "3"]] },
    { key: "plant", title: "Plant and equipment", summary: "1 broken down", rows: [["Tower crane TC1", "Working"], ["Concrete pump", "Broken down"], ["TLB", "Idle"]] },
    { key: "instr", title: "Instructions received", summary: "1 verbal", rows: [["Hold the Level 4 pour", "Verbal, 10:20. Ask for it in writing."]] },
    { key: "delay", title: "Delays", summary: "2 today", rows: [["Thunderstorms, work stopped", "DE-013 · from 14:05"], ["Level 4 pour postponed", "Not logged yet"]] },
    { key: "visit", title: "Visitors and deliveries", summary: "2", rows: [["Principal agent", "Visitor · 10:00–11:30"], ["Rebar, Level 4 slab", "Delivery · 08:15"]] },
  ] as { key: string; title: string; summary: string; rows: [string, string][] }[],
};

export const outbox = {
  status: "No signal since 09:40 · 6 items waiting",
  records: [
    { what: "Incident report", sub: "Starts legal deadlines when it arrives", state: "Waiting", tone: "warn" as Tone },
    { what: "Housekeeping walk: 10 answers", sub: "Started 09:52", state: "Waiting", tone: "warn" as Tone },
    { what: "Witness statement", sub: "Nomvula Sithole", state: "Waiting", tone: "warn" as Tone },
  ],
  photos: [
    { what: "Scaffold board, Level 3", sub: "2.1 MB · hashed on the phone", state: "Queued", tone: "neutral" as Tone },
    { what: "Signed witness statement", sub: "1.4 MB · hashed on the phone", state: "Queued", tone: "neutral" as Tone },
  ],
  conflict: {
    title: "Answer not saved: a newer one exists",
    body: "Mobile plant pre-use, “Reversing alarm”. Your phone has Pass from 09:12; the server has Fail from 09:30 sent from another device.",
  },
};
