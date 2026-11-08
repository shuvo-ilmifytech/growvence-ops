import type { NextConfig } from "next";

// The browser only ever talks to Next; /api/* is proxied to the Go API so the
// session cookie stays first-party and SameSite=Lax works without CORS.
const apiURL = process.env.API_INTERNAL_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  // A second dev server (the e2e stack on :3100) needs its own build directory.
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
  // The floating dev badge sat on top of the sidebar menu, sticky footers or sheet close buttons
  // wherever it was placed; errors still show in the dev overlay and console.
  devIndicators: false,
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiURL}/api/:path*` }];
  },
};

export default nextConfig;
