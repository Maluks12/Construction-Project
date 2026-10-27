// Cookie names and options for the login tokens. Both cookies are httpOnly,
// so browser JavaScript never sees a token; only this Next.js server does.
// Imported by proxy.ts and by Server Actions, so no "server-only" here.

import type { TokenPair } from "./types";

export const ACCESS_COOKIE = "sp_access";
export const REFRESH_COOKIE = "sp_refresh";

// Matches refreshTokenTTL in internal/core/auth/service.go.
const REFRESH_MAX_AGE = 30 * 24 * 60 * 60;

// Drop the access cookie a little before the API rejects the token, so the
// proxy refreshes it ahead of time instead of a request failing with 401.
const EARLY_EXPIRY_SECONDS = 30;

type CookieWriter = {
  set(
    name: string,
    value: string,
    options: {
      httpOnly: boolean;
      secure: boolean;
      sameSite: "lax";
      path: string;
      maxAge: number;
    },
  ): unknown;
};

function options(maxAge: number) {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax" as const,
    path: "/",
    maxAge,
  };
}

export function writeTokens(cookies: CookieWriter, pair: TokenPair) {
  const accessAge = Math.max(pair.expires_in - EARLY_EXPIRY_SECONDS, 10);
  cookies.set(ACCESS_COOKIE, pair.access_token, options(accessAge));
  cookies.set(REFRESH_COOKIE, pair.refresh_token, options(REFRESH_MAX_AGE));
}

export function clearTokens(cookies: CookieWriter) {
  cookies.set(ACCESS_COOKIE, "", options(0));
  cookies.set(REFRESH_COOKIE, "", options(0));
}
