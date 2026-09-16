// status.ts — presentation-layer constants and helpers for certificate status.
// All Tailwind class strings are static so the JIT compiler can include them
// in the build without needing to scan dynamic string interpolation.
import type { CertStatus } from "./types";

// STATUS_CONFIG drives badges, row backgrounds, and indicator dots.
// Keeping all per-status styling here avoids scattered conditionals.
export const STATUS_CONFIG: Record<
  CertStatus,
  { label: string; badge: string; row: string; dot: string }
> = {
  OK: {
    label: "Healthy",
    badge: "bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-500/30",
    row: "",
    dot: "bg-emerald-400",
  },
  WARNING: {
    label: "Warning",
    badge: "bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30",
    row: "",
    dot: "bg-amber-400",
  },
  CRITICAL: {
    label: "Critical",
    badge: "bg-red-500/15 text-red-400 ring-1 ring-red-500/30",
    // Subtle background tint draws attention without being alarming
    row: "bg-red-950/20",
    dot: "bg-red-400",
  },
  EXPIRED: {
    label: "Expired",
    badge: "bg-violet-500/15 text-violet-400 ring-1 ring-violet-500/30",
    row: "bg-violet-950/20",
    dot: "bg-violet-400",
  },
};

// daysColor returns a Tailwind text colour class matching the urgency of the
// remaining lifetime. Thresholds mirror the Go backend (7d critical, 30d warning).
export function daysColor(days: number): string {
  if (days < 0) return "text-violet-400";
  if (days <= 7) return "text-red-400";
  if (days <= 30) return "text-amber-400";
  return "text-emerald-400";
}

// formatDays renders days_left as a human-readable string.
// Negative values are shown as "Nd ago" to make expired certs obvious.
export function formatDays(days: number): string {
  if (days < 0) return `${Math.abs(days)}d ago`;
  return `${days}d`;
}
