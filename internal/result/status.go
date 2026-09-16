package result

// Status classifies a certificate's urgency based on days remaining.
type Status string

const (
	StatusOK       Status = "OK"
	StatusWarning  Status = "WARNING"
	StatusCritical Status = "CRITICAL"
	StatusExpired  Status = "EXPIRED"
)

// Thresholds holds the day boundaries used to classify certificates.
type Thresholds struct {
	WarningDays  int // default 30
	CriticalDays int // default 7
}

// DefaultThresholds returns the recommended threshold values.
func DefaultThresholds() Thresholds {
	return Thresholds{WarningDays: 30, CriticalDays: 7}
}

// Classify returns the Status for a certificate with the given days remaining.
func (t Thresholds) Classify(daysLeft int) Status {
	switch {
	case daysLeft < 0:
		return StatusExpired
	case daysLeft <= t.CriticalDays:
		return StatusCritical
	case daysLeft <= t.WarningDays:
		return StatusWarning
	default:
		return StatusOK
	}
}
