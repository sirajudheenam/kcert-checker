package result

import "testing"

// TestClassifyTable verifies the boundary values for each status tier using
// default thresholds (7d critical, 30d warning). Edge cases at the exact
// threshold values are especially important to check here.
func TestClassifyTable(t *testing.T) {
	th := DefaultThresholds()

	cases := []struct {
		days int
		want Status
	}{
		{-1, StatusExpired},
		{-100, StatusExpired},
		// DaysLeft=0 means the cert expires today — treated as CRITICAL, not EXPIRED.
		// Expired only applies when the date has already passed (days < 0).
		{0, StatusCritical},
		{7, StatusCritical},
		{8, StatusWarning},
		{30, StatusWarning},
		{31, StatusOK},
		{365, StatusOK},
	}
	for _, c := range cases {
		got := th.Classify(c.days)
		if got != c.want {
			t.Errorf("Classify(%d) = %q, want %q", c.days, got, c.want)
		}
	}
}

// TestCustomThresholds verifies that non-default threshold values are applied
// correctly. This matters because each deployment may tune its own alert windows.
func TestCustomThresholds(t *testing.T) {
	th := Thresholds{WarningDays: 14, CriticalDays: 3}

	if th.Classify(15) != StatusOK {
		t.Errorf("expected OK for 15 days with threshold 14")
	}
	if th.Classify(14) != StatusWarning {
		t.Errorf("expected WARNING for 14 days with threshold 14")
	}
	if th.Classify(3) != StatusCritical {
		t.Errorf("expected CRITICAL for 3 days with threshold 3")
	}
	if th.Classify(-1) != StatusExpired {
		t.Errorf("expected EXPIRED for -1 days")
	}
}

// TestDefaultThresholds verifies that the production defaults are the values
// documented in the README alert table (7d critical, 30d warning).
func TestDefaultThresholds(t *testing.T) {
	th := DefaultThresholds()
	if th.WarningDays != 30 {
		t.Errorf("WarningDays = %d, want 30", th.WarningDays)
	}
	if th.CriticalDays != 7 {
		t.Errorf("CriticalDays = %d, want 7", th.CriticalDays)
	}
}
