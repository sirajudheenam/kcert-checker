// "use client" marks this as a Client Component: it runs in the browser and
// can use React state, effects, and browser events. The parent page.tsx is a
// Server Component that fetches the initial data and passes it as props so the
// first render is populated without a loading flash.
"use client";

import { useMemo, useState, useCallback, useEffect } from "react";
import type { Certificate, CertStatus } from "./types";
import { STATUS_CONFIG, daysColor, formatDays } from "./status";
import { fetchCertificates } from "./api-client";

const STATUS_OPTIONS: { value: "" | CertStatus; label: string }[] = [
  { value: "", label: "All statuses" },
  { value: "OK", label: "Healthy" },
  { value: "WARNING", label: "Warning" },
  { value: "CRITICAL", label: "Critical" },
  { value: "EXPIRED", label: "Expired" },
];

type SortKey = keyof Certificate;

// SummaryCard renders one stat tile (total / healthy / warning / critical / expired).
function SummaryCard({
  label,
  value,
  colorClass,
}: {
  label: string;
  value: number;
  colorClass: string;
}) {
  return (
    <div className="rounded-xl border border-white/8 bg-white/4 p-5">
      <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">
        {label}
      </p>
      <p className={`mt-2 text-4xl font-bold tabular-nums ${colorClass}`}>
        {value}
      </p>
    </div>
  );
}

// StatusBadge renders the coloured pill (OK / WARNING / CRITICAL / EXPIRED).
function StatusBadge({ status }: { status: CertStatus }) {
  const cfg = STATUS_CONFIG[status];
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold ${cfg.badge}`}
    >
      {status}
    </span>
  );
}

// ThSortable is a table header cell that toggles ascending/descending sort on click.
// Passing the current sort state down avoids a separate context or prop-drilling.
function ThSortable({
  children,
  sortKey,
  current,
  asc,
  onSort,
}: {
  children: React.ReactNode;
  sortKey: SortKey;
  current: SortKey;
  asc: boolean;
  onSort: (k: SortKey) => void;
}) {
  const active = current === sortKey;
  return (
    <th
      className="cursor-pointer select-none whitespace-nowrap px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-400 hover:text-slate-200"
      onClick={() => onSort(sortKey)}
    >
      {children}
      <span className="ml-1 inline-block w-3 text-slate-500">
        {active ? (asc ? "↑" : "↓") : ""}
      </span>
    </th>
  );
}

// Dashboard is the top-level interactive component.
// initialCerts and loadedAt come from the SSR pass in page.tsx so the first
// paint shows data; subsequent refreshes update state client-side.
export default function Dashboard({
  initialCerts,
  loadedAt,
}: {
  initialCerts: Certificate[];
  loadedAt: string;
}) {
  const [certs, setCerts] = useState<Certificate[]>(initialCerts);
  const [lastUpdated, setLastUpdated] = useState(loadedAt);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Filter / sort state — persisted only in memory (reset on page reload).
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"" | CertStatus>("");
  const [sortKey, setSortKey] = useState<SortKey>("days_left");
  const [sortAsc, setSortAsc] = useState(true);

  // refresh() is stable across renders (useCallback) so the useEffect interval
  // below doesn't re-register on every render.
  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchCertificates();
      setCerts(data);
      setLastUpdated(new Date().toLocaleTimeString());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load");
    } finally {
      setLoading(false);
    }
  }, []);

  // Auto-refresh every 60 s so a long-lived browser tab stays up-to-date
  // without the user having to manually hit Refresh.
  useEffect(() => {
    const id = setInterval(refresh, 60_000);
    return () => clearInterval(id);
  }, [refresh]);

  // Clicking a column header a second time reverses the sort direction.
  const handleSort = useCallback(
    (key: SortKey) => {
      if (key === sortKey) setSortAsc((a) => !a);
      else {
        setSortKey(key);
        setSortAsc(true);
      }
    },
    [sortKey]
  );

  // counts is a per-status frequency map used by the summary cards.
  // useMemo avoids recomputing on every keystroke in the filter input.
  const counts = useMemo(
    () =>
      certs.reduce(
        (acc, c) => {
          acc[c.status] = (acc[c.status] ?? 0) + 1;
          return acc;
        },
        {} as Record<CertStatus, number>
      ),
    [certs]
  );

  // filtered applies text search, status filter, and sort in one pass.
  // The spread copy before sort is required because Array.sort mutates in place,
  // which would corrupt the original certs state.
  const filtered = useMemo(() => {
    const q = query.toLowerCase();
    let rows = certs.filter((c) => {
      if (statusFilter && c.status !== statusFilter) return false;
      if (q) {
        const hay = [
          c.namespace,
          c.source_type,
          c.source_name,
          c.subject,
          c.issuer,
          c.container ?? "",
        ]
          .join(" ")
          .toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });

    rows = [...rows].sort((a, b) => {
      const av = a[sortKey] ?? "";
      const bv = b[sortKey] ?? "";
      if (av < bv) return sortAsc ? -1 : 1;
      if (av > bv) return sortAsc ? 1 : -1;
      return 0;
    });

    return rows;
  }, [certs, query, statusFilter, sortKey, sortAsc]);

  return (
    <div className="min-h-screen bg-[#0d1117] text-slate-200">
      {/* Header */}
      <header className="border-b border-white/8 bg-[#161b22] px-6 py-4">
        <div className="mx-auto flex max-w-screen-xl items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="text-lg font-bold tracking-tight">
              k<span className="text-emerald-400">cert</span>
            </span>
            <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-0.5 text-xs text-slate-400">
              Certificate Inventory
            </span>
          </div>
          <div className="flex items-center gap-3 text-xs text-slate-500">
            <span>Updated {lastUpdated}</span>
            <button
              onClick={refresh}
              disabled={loading}
              className="flex items-center gap-1.5 rounded-md border border-white/10 bg-white/5 px-3 py-1.5 text-xs font-medium text-slate-300 hover:border-emerald-500/50 hover:text-emerald-400 disabled:opacity-50"
            >
              {loading ? (
                <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-slate-600 border-t-emerald-400" />
              ) : (
                "↻"
              )}
              Refresh
            </button>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-screen-xl px-6 py-6 space-y-6">
        {/* Error banner — shown when the backend is unreachable or returns an error */}
        {error && (
          <div className="rounded-lg border border-red-500/30 bg-red-950/30 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {/* Summary cards */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
          <SummaryCard label="Total" value={certs.length} colorClass="text-slate-200" />
          <SummaryCard label="Healthy" value={counts.OK ?? 0} colorClass="text-emerald-400" />
          <SummaryCard label="Warning" value={counts.WARNING ?? 0} colorClass="text-amber-400" />
          <SummaryCard label="Critical" value={counts.CRITICAL ?? 0} colorClass="text-red-400" />
          <SummaryCard label="Expired" value={counts.EXPIRED ?? 0} colorClass="text-violet-400" />
        </div>

        {/* Filter controls — text search + status dropdown + result count */}
        <div className="flex flex-wrap items-center gap-3">
          <input
            type="text"
            placeholder="Filter by namespace, subject, source…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="flex-1 min-w-48 rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-200 placeholder-slate-500 outline-none focus:border-emerald-500/50 focus:ring-0"
          />
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as "" | CertStatus)}
            className="rounded-lg border border-white/10 bg-[#161b22] px-3 py-2 text-sm text-slate-300 outline-none"
          >
            {STATUS_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <span className="text-xs text-slate-500">
            {filtered.length} / {certs.length} certificates
          </span>
        </div>

        {/* Certificate table */}
        <div className="overflow-x-auto rounded-xl border border-white/8">
          <table className="w-full text-sm">
            <thead className="border-b border-white/8 bg-white/3">
              <tr>
                <ThSortable sortKey="status" current={sortKey} asc={sortAsc} onSort={handleSort}>Status</ThSortable>
                <ThSortable sortKey="namespace" current={sortKey} asc={sortAsc} onSort={handleSort}>Namespace</ThSortable>
                <ThSortable sortKey="source_type" current={sortKey} asc={sortAsc} onSort={handleSort}>Source</ThSortable>
                <ThSortable sortKey="source_name" current={sortKey} asc={sortAsc} onSort={handleSort}>Source Name</ThSortable>
                <ThSortable sortKey="subject" current={sortKey} asc={sortAsc} onSort={handleSort}>Subject</ThSortable>
                <ThSortable sortKey="issuer" current={sortKey} asc={sortAsc} onSort={handleSort}>Issuer</ThSortable>
                <ThSortable sortKey="not_after" current={sortKey} asc={sortAsc} onSort={handleSort}>Expires</ThSortable>
                <ThSortable sortKey="days_left" current={sortKey} asc={sortAsc} onSort={handleSort}>Days Left</ThSortable>
                <th className="px-4 py-3 text-left text-xs font-semibold uppercase tracking-wider text-slate-400">Fingerprint</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {filtered.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-4 py-16 text-center text-slate-500">
                    No certificates match the current filter.
                  </td>
                </tr>
              ) : (
                filtered.map((cert, i) => (
                  // fingerprint alone is not unique (same cert in multiple places),
                  // so we combine it with the array index as the React key.
                  <tr
                    key={`${cert.fingerprint}-${i}`}
                    className={`transition-colors hover:bg-white/3 ${STATUS_CONFIG[cert.status].row}`}
                  >
                    <td className="px-4 py-3">
                      <StatusBadge status={cert.status} />
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-300">{cert.namespace}</td>
                    <td className="px-4 py-3">
                      <span className="rounded border border-white/10 bg-white/5 px-1.5 py-0.5 font-mono text-xs text-slate-400">
                        {cert.source_type}
                      </span>
                    </td>
                    <td className="max-w-48 px-4 py-3 font-mono text-xs text-slate-300 truncate" title={cert.source_name}>
                      {cert.source_name}
                    </td>
                    <td className="px-4 py-3 text-slate-200">{cert.subject || "—"}</td>
                    <td className="px-4 py-3 text-slate-400 text-xs">{cert.issuer || "—"}</td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-400">{cert.not_after}</td>
                    <td className={`px-4 py-3 font-mono text-sm font-semibold tabular-nums ${daysColor(cert.days_left)}`}>
                      {formatDays(cert.days_left)}
                    </td>
                    {/* Truncate fingerprint to 12 chars; full value in tooltip */}
                    <td className="px-4 py-3 font-mono text-xs text-slate-600" title={cert.fingerprint}>
                      {cert.fingerprint ? cert.fingerprint.slice(0, 12) + "…" : "—"}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </main>
    </div>
  );
}
