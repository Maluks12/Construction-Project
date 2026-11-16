// The only place the app talks to the Go API.
//
// Unlike the dashboard, the phone calls the API directly, so it holds the
// tokens itself: in the device's secure storage (Keychain / Keystore), never
// in plain storage. The access token lasts 15 minutes; the refresh token is
// swapped for a new pair when it runs out.

import * as SecureStore from "expo-secure-store";

import type { TokenPair } from "./types";

export const API_URL = (process.env.EXPO_PUBLIC_API_URL ?? "http://localhost:8080").replace(/\/$/, "");

const TOKENS_KEY = "sp_tokens";
// Refresh a little before the API would reject the token.
const EARLY_MS = 30_000;

type Stored = { access: string; refresh: string; accessUntil: number };

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    // The whole error body, for the structured ones (IncompleteInspection's
    // item_keys, EvidenceProblem's evidence_ids...).
    public body: Record<string, unknown> = {},
  ) {
    super(message);
  }
}

let tokens: Stored | null = null;
let loaded = false;
let onSignedOut: (() => void) | null = null;

export function setSignedOutHandler(fn: (() => void) | null) {
  onSignedOut = fn;
}

async function load() {
  if (!loaded) {
    const raw = await SecureStore.getItemAsync(TOKENS_KEY);
    tokens = raw ? (JSON.parse(raw) as Stored) : null;
    loaded = true;
  }
  return tokens;
}

export async function saveTokens(pair: TokenPair) {
  tokens = {
    access: pair.access_token,
    refresh: pair.refresh_token,
    accessUntil: Date.now() + pair.expires_in * 1000 - EARLY_MS,
  };
  loaded = true;
  await SecureStore.setItemAsync(TOKENS_KEY, JSON.stringify(tokens));
}

export async function clearTokens() {
  tokens = null;
  loaded = true;
  await SecureStore.deleteItemAsync(TOKENS_KEY);
}

export const hasSession = async () => (await load()) !== null;

// The API's own messages are written for people; this only tidies them and
// replaces the ones that aren't.
function friendly(status: number, raw: string) {
  if (status === 403 && (raw === "forbidden" || !raw)) return "Your role doesn't allow this.";
  if (status === 503) return "The server is busy. Try again in a moment.";
  if (!raw || raw === "internal error") return "Something went wrong on the server. Try again.";
  if (raw.includes("Field validation")) return "Some details are missing or not valid. Check the form.";
  return raw.charAt(0).toUpperCase() + raw.slice(1) + (raw.endsWith(".") ? "" : ".");
}

async function send(path: string, init: RequestInit) {
  try {
    return await fetch(`${API_URL}${path}`, init);
  } catch {
    throw new ApiError(0, "Can't reach SiteProof. Check your signal and try again.");
  }
}

async function fail(res: Response): Promise<never> {
  let body: Record<string, unknown> = {};
  try {
    body = (await res.json()) as Record<string, unknown>;
  } catch {
    // Not JSON (e.g. a proxy error page).
  }
  throw new ApiError(res.status, friendly(res.status, typeof body.error === "string" ? body.error : ""), body);
}

// The API revokes a whole session if a refresh token is used twice, so
// every caller that finds the access token expired shares one refresh call.
let refreshing: Promise<boolean> | null = null;

function refresh(): Promise<boolean> {
  refreshing ??= (async () => {
    const current = await load();
    if (!current) return false;
    const res = await send("/v1/auth/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ refresh_token: current.refresh }),
    });
    if (!res.ok) {
      // 400/401: the session is over. Anything else (503...) is temporary.
      if (res.status === 400 || res.status === 401) {
        await clearTokens();
        onSignedOut?.();
        return false;
      }
      await fail(res);
    }
    await saveTokens((await res.json()) as TokenPair);
    return true;
  })().finally(() => {
    refreshing = null;
  });
  return refreshing;
}

type Options = {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  // false for login and invite accept.
  auth?: boolean;
};

export async function api<T>(path: string, opts: Options = {}): Promise<T> {
  const { method = "GET", body, auth = true } = opts;

  const call = async () => {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (auth) {
      let current = await load();
      if (current && current.accessUntil <= Date.now()) {
        if (await refresh()) current = await load();
        else current = null;
      }
      if (!current) throw new ApiError(401, "Your session ended. Sign in again.");
      headers.Authorization = `Bearer ${current.access}`;
    }
    return send(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  };

  let res = await call();
  // The token was rejected early (revoked, or the clock is off): one refresh, one retry.
  if (auth && res.status === 401 && (await refresh())) res = await call();
  if (auth && res.status === 401) {
    await clearTokens();
    onSignedOut?.();
    throw new ApiError(401, "Your session ended. Sign in again.");
  }
  if (!res.ok) await fail(res);
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const sitePath = (siteId: string, rest: string) => `/v1/sites/${encodeURIComponent(siteId)}${rest}`;
