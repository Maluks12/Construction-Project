import { NextResponse, type NextRequest } from "next/server";

import { clearTokens } from "@/lib/session";

// Reached when the API rejects our token. Clears both cookies, otherwise
// /login would see the refresh cookie and send the browser straight back.
export function GET(request: NextRequest) {
  const res = NextResponse.redirect(new URL("/login?expired=1", request.url));
  clearTokens(res.cookies);
  return res;
}
