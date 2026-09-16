// Server component: runs on the server at request time (or at build time for
// static pages). It SSR-fetches the certificate list so the browser receives
// populated HTML on the first paint rather than a blank loading state.
import type { Certificate } from "./types";
import { fetchCertificates } from "./api-client";
import Dashboard from "./Dashboard";

// force-dynamic prevents Next.js from caching this route; cert data changes
// on every scan so we always want a fresh fetch from the Go backend.
export const dynamic = "force-dynamic";

export default async function Page() {
  let initialCerts: Certificate[] = [];
  let loadedAt = "—";

  try {
    initialCerts = await fetchCertificates();
    loadedAt = new Date().toLocaleTimeString("en-US", { hour12: false });
  } catch {
    // Client side will retry; empty initial state is fine
  }

  return <Dashboard initialCerts={initialCerts} loadedAt={loadedAt} />;
}
