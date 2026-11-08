import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// Optimistic check only: no cookie means "go sign in". The Go API validates
// the session and enforces every permission.
export function proxy(request: NextRequest) {
  const hasSession = request.cookies.has("opsflow_session");
  const isLogin = request.nextUrl.pathname === "/login";
  if (!hasSession && !isLogin) {
    return NextResponse.redirect(new URL("/login", request.url));
  }
  return NextResponse.next();
}

export const config = {
  // Skip the API, Next internals and any static file (has an extension), e.g. /logo-mark.png,
  // which the image optimizer fetches without the user's cookies.
  matcher: ["/((?!api|_next/static|_next/image|.*\\..*).*)"],
};
