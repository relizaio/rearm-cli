package cmd

import "testing"

// --budget (task 6f1b348d): dollars as typed, into USD micros.
func TestParseBudgetUSD(t *testing.T) {
	ok := map[string]int64{
		"2.50": 2_500_000, "$2.50": 2_500_000, "3": 3_000_000, "0": 0, ".5": 500_000,
		"0.000001": 1, " 10.25 ": 10_250_000, "1.123456": 1_123_456,
	}
	for in, want := range ok {
		got, err := parseBudgetUSD(in)
		if err != nil || got != want {
			t.Errorf("parseBudgetUSD(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-1", "abc", "1.", "1.1234567", "1.-5", "1.+5", "1,5"} {
		if _, err := parseBudgetUSD(bad); err == nil {
			t.Errorf("parseBudgetUSD(%q) should be refused", bad)
		}
	}
}
