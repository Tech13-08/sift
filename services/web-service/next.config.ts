import type { NextConfig } from "next";

const authURL = (process.env.AUTH_INTERNAL_URL || process.env.AUTH_URL || "http://localhost:3000").replace(
  /\/$/,
  ""
);

const nextConfig: NextConfig = {
  output: "standalone",
  // Same-origin /api and /auth → auth-service (cookies stay on the web host).
  async rewrites() {
    return [
      { source: "/auth/:path*", destination: `${authURL}/auth/:path*` },
      { source: "/api/:path*", destination: `${authURL}/api/:path*` },
    ];
  },
};

export default nextConfig;
