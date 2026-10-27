import type { Metadata } from "next";
import Link from "next/link";

import { Alert } from "@/components/ui";

import { LoginForm } from "./login-form";

export const metadata: Metadata = { title: "Sign in" };

export default async function LoginPage({ searchParams }: PageProps<"/login">) {
  const { next, expired } = await searchParams;

  return (
    <>
      <h1 className="text-2xl font-semibold tracking-tight">Sign in</h1>
      <p className="mt-1 mb-6 text-sm text-muted">
        Use the email or mobile number on your SiteProof account.
      </p>
      {expired && (
        <div className="mb-4">
          <Alert tone="info">Your session ended. Sign in again to continue.</Alert>
        </div>
      )}
      <LoginForm next={typeof next === "string" ? next : ""} />
      <p className="mt-6 text-sm text-muted">
        New company?{" "}
        <Link href="/signup" className="font-medium text-ink underline underline-offset-4">
          Create an account
        </Link>
      </p>
    </>
  );
}
