// Swaps a refresh token for a new token pair, at most once per token.
//
// The Go API treats a refresh token that comes back a second time as stolen
// and revokes the whole session (reuse detection in auth.Service.Refresh).
// A browser easily sends the same old token twice: several tabs, or a page
// plus its prefetches, all arrive before the new cookie is stored. So:
//   - concurrent calls for one token share a single request (inflight), and
//   - calls that arrive shortly after reuse that result (recent)
// instead of each calling /v1/auth/refresh.

import type { TokenPair } from "./types";

export type RefreshResult =
  | { ok: true; pair: TokenPair }
  | { ok: false; reason: "invalid" | "unavailable" };

const RECENT_MS = 60_000;

const inflight = new Map<string, Promise<RefreshResult>>();
const recent = new Map<string, { result: RefreshResult; at: number }>();

function apiURL() {
  return (process.env.API_URL ?? "http://localhost:8080").replace(/\/$/, "");
}

async function callRefresh(refreshToken: string): Promise<RefreshResult> {
  try {
    const res = await fetch(`${apiURL()}/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken }),
      cache: "no-store",
    });
    if (res.ok) return { ok: true, pair: (await res.json()) as TokenPair };
    if (res.status === 401 || res.status === 400) {
      return { ok: false, reason: "invalid" };
    }
    return { ok: false, reason: "unavailable" };
  } catch {
    return { ok: false, reason: "unavailable" };
  }
}

export function refreshTokens(refreshToken: string): Promise<RefreshResult> {
  const now = Date.now();
  for (const [token, entry] of recent) {
    if (now - entry.at > RECENT_MS) recent.delete(token);
  }

  const done = recent.get(refreshToken);
  if (done) return Promise.resolve(done.result);

  const running = inflight.get(refreshToken);
  if (running) return running;

  const p = callRefresh(refreshToken).then((result) => {
    inflight.delete(refreshToken);
    // Don't remember "API is down"; the next request should try again.
    if (result.ok || result.reason === "invalid") {
      recent.set(refreshToken, { result, at: Date.now() });
    }
    return result;
  });
  inflight.set(refreshToken, p);
  return p;
}
