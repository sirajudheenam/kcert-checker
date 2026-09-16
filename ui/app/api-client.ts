// api-client.ts — thin wrapper around the Go backend's /api/certificates endpoint.
// Used by both the server component (SSR) and the client component (browser refresh).
import type { Certificate } from "./types";

// NEXT_PUBLIC_API_URL is set in .env.local (or the environment) to point at
// the running kcert-checker process. The rewrites() in next.config.ts proxy
// /api/* at the Next.js dev server so this default works during development.
const API_BASE =
  process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") ?? "http://localhost:8080";

// fetchCertificates always bypasses the Next.js fetch cache so the UI reflects
// the latest scan, not a stale snapshot from a prior request.
export async function fetchCertificates(): Promise<Certificate[]> {
  const res = await fetch(`${API_BASE}/api/certificates`, {
    cache: "no-store",
    next: { revalidate: 0 },
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}
