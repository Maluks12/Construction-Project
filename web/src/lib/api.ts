// The only place the dashboard talks to the Go API. Runs on the Next.js
// server: the browser never calls the API or sees a token.

import "server-only";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { friendlyMessage } from "./errors";
import { ACCESS_COOKIE } from "./session";

export const API_URL = (process.env.API_URL ?? "http://localhost:8080").replace(
  /\/$/,
  "",
);

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

type Options = {
  method?: "GET" | "POST" | "PATCH" | "PUT" | "DELETE";
  body?: unknown;
  // false for signup, login, refresh, and invite preview/accept.
  auth?: boolean;
};

export async function api<T>(path: string, opts: Options = {}): Promise<T> {
  const { method = "GET", body, auth = true } = opts;
  const headers: Record<string, string> = { Accept: "application/json" };

  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (auth) {
    const token = (await cookies()).get(ACCESS_COOKIE)?.value;
    // proxy.ts only lets a request through without an access token when the
    // refresh call couldn't reach the API, so report that instead of
    // signing the person out.
    if (!token) {
      throw new ApiError(
        503,
        `Can't reach the SiteProof API at ${API_URL}. Is it running?`,
      );
    }
    headers.Authorization = `Bearer ${token}`;
  }

  let res: Response;
  try {
    res = await fetch(`${API_URL}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
    });
  } catch {
    throw new ApiError(
      503,
      `Can't reach the SiteProof API at ${API_URL}. Is it running?`,
    );
  }

  // The session was revoked or the token rejected. Server Components can't
  // delete cookies, so a route handler clears them and goes to /login.
  if (auth && res.status === 401) redirect("/session-expired");

  if (!res.ok) {
    let raw = "";
    try {
      raw = ((await res.json()) as { error?: string }).error ?? "";
    } catch {
      // Not JSON (e.g. a proxy error page); fall back to the generic message.
    }
    throw new ApiError(res.status, friendlyMessage(res.status, raw));
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
