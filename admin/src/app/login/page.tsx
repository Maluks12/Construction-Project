import type { Metadata } from "next";
import Link from "next/link";

import { Field, Input, Mark, buttonClass } from "@/components/ui";

export const metadata: Metadata = { title: "Sign in" };

// The form's shape only. Staff sign-in needs an admin auth endpoint (with a
// second factor) that doesn't exist yet, so the button just opens the
// preview. Don't deploy this portal publicly until that is in place.
export default function LoginPage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-side px-4 py-10">
      <div className="w-full max-w-sm rounded-[10px] border border-line bg-surface p-6">
        <div className="mb-6 flex items-center gap-2.5">
          <Mark />
          <span className="font-head text-lg font-bold">SiteProof</span>
          <span className="rounded-full border border-line-strong px-2 py-0.5 text-[11px] tracking-[.08em] text-muted uppercase">Admin</span>
        </div>
        <h1 className="font-head text-2xl font-semibold">Staff sign in</h1>
        <p className="mt-1 mb-5 text-sm text-muted">For the SiteProof team. Customers sign in on the dashboard.</p>
        <form className="space-y-4">
          <Field label="Work email"><Input type="email" autoComplete="username" disabled placeholder="name@yourcompany.co.za" /></Field>
          <Field label="Password"><Input type="password" autoComplete="current-password" disabled /></Field>
          <Field label="Authenticator code"><Input inputMode="numeric" disabled placeholder="6 digits" /></Field>
          <p className="rounded-lg bg-warn-soft px-3 py-2 text-[13px] text-warn">
            Sign-in isn&apos;t connected yet. The preview opens without it.
          </p>
          <Link href="/" className={buttonClass("primary") + " w-full"}>Open the preview</Link>
        </form>
      </div>
    </main>
  );
}
