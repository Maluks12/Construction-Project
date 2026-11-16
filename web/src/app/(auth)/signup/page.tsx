import type { Metadata } from "next";
import Link from "next/link";

import { SignupForm } from "./signup-form";

export const metadata: Metadata = { title: "Create an account" };

export default function SignupPage() {
  return (
    <>
      <h1 className="font-head text-2xl font-semibold">Create your company</h1>
      <p className="mt-1 mb-6 text-sm text-muted">
        You&apos;ll be the owner. You can invite your SHE officer and supervisors next.
      </p>
      <SignupForm />
      <p className="mt-6 text-sm text-muted">
        Already on SiteProof?{" "}
        <Link href="/login" className="link">
          Sign in
        </Link>
      </p>
    </>
  );
}
