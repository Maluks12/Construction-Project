// Runs before every page request (Next.js 16 renamed middleware.ts to proxy.ts).
//
// 1. Signed-out visitors to dashboard pages are sent to /login.
// 2. When the 15-minute access token has run out but the refresh token is
//    still there, it gets a new pair from the Go API, hands the new access
//    token to this request, and stores both cookies in the browser.
//
// This is only the front door. Server Actions and data calls still check
// the token themselves (see lib/api.ts), and the Go API checks it again.

import { NextResponse, type NextRequest } from "next/server";

import { refreshTokens } from "@/lib/refresh";
import {
  ACCESS_COOKIE,
  REFRESH_COOKIE,
  clearTokens,
  writeTokens,
} from "@/lib/session";

const PUBLIC_PATHS = ["/login", "/signup", "/invite", "/session-expired"];
const SIGNED_OUT_ONLY = ["/login", "/signup"];

function matches(pathname: string, paths: string[]) {
  return paths.some((p) => pathname === p || pathname.startsWith(p + "/"));
}

function toLogin(request: NextRequest) {
  const url = request.nextUrl.clone();
  const next = request.nextUrl.pathname + request.nextUrl.search;
  url.pathname = "/login";
  url.search = next && next !== "/" ? `?next=${encodeURIComponent(next)}` : "";
  return NextResponse.redirect(url);
}

export async function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const access = request.cookies.get(ACCESS_COOKIE)?.value;
  const refresh = request.cookies.get(REFRESH_COOKIE)?.value;

  if (matches(pathname, PUBLIC_PATHS)) {
    if (refresh && matches(pathname, SIGNED_OUT_ONLY)) {
      return NextResponse.redirect(new URL("/sites", request.url));
    }
    return NextResponse.next();
  }

  if (!refresh) return toLogin(request);
  if (access) return NextResponse.next();

  const result = await refreshTokens(refresh);

  if (!result.ok) {
    if (result.reason === "invalid") {
      const res = toLogin(request);
      clearTokens(res.cookies);
      return res;
    }
    // API unreachable: let the page render and show its own error.
    return NextResponse.next();
  }

  // Give this request the new tokens so the page renders with them...
  request.cookies.set(ACCESS_COOKIE, result.pair.access_token);
  request.cookies.set(REFRESH_COOKIE, result.pair.refresh_token);
  const res = NextResponse.next({ request: { headers: request.headers } });
  // ...and tell the browser to keep them for the next one.
  writeTokens(res.cookies, result.pair);
  return res;
}

export const config = {
  // Everything except Next.js internals and files with an extension.
  matcher: ["/((?!_next/static|_next/image|.*\\.[a-zA-Z0-9]+$).*)"],
};
