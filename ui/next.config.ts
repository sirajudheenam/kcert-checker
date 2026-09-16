import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // output: standalone produces a self-contained server.js with no node_modules.
  // Required for the Docker multi-stage build (ui/Dockerfile copies .next/standalone).
  output: "standalone",

  // Proxy /api/* from the Next.js dev server to the Go backend so the browser
  // doesn't hit CORS issues during development. In production the reverse proxy
  // (nginx / ingress) should route /api/* directly to the Go service instead.
  async rewrites() {
    const backendUrl =
      process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") ??
      "http://localhost:8080";
    return [
      {
        source: "/api/:path*",
        destination: `${backendUrl}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
