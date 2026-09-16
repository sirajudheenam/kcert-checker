// CertStatus mirrors the four status values returned by the Go backend's
// result.Thresholds.Classify(). Keep in sync with internal/result/result.go.
export type CertStatus = "OK" | "WARNING" | "CRITICAL" | "EXPIRED";

// Certificate maps the JSON fields from /api/certificates. Field names use
// snake_case to match Go's json tags directly without any transformation.
export interface Certificate {
  namespace: string;
  // "secret" or "container" — determines which table section it belongs to
  source_type: string;
  // For secrets: "<secret-name>/<key>". For containers: "<pod>:<path>".
  source_name: string;
  // Only set when source_type is "container"
  container?: string;
  subject: string;
  issuer: string;
  // RFC 3339 formatted expiry timestamp from the certificate's NotAfter field
  not_after: string;
  // Positive = days until expiry; negative = days since expiry
  days_left: number;
  status: CertStatus;
  // True when the certificate has the CA basic constraint set
  is_ca: boolean;
  // SHA-256 hex fingerprint; used as a stable deduplication key
  fingerprint: string;
}
