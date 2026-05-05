package i18n

import "testing"

func TestSinceEnglish(t *testing.T) {
	en := New("en")
	const now int64 = 1_700_000_000

	cases := []struct {
		name string
		then int64
		want string
	}{
		{"just now", now - 30, "just now"},
		{"1 minute", now - 60, "1 minute ago"},
		{"5 minutes", now - 300, "5 minutes ago"},
		{"1 hour", now - 3600, "1 hour ago"},
		{"3 hours", now - 3*3600, "3 hours ago"},
		{"1 day", now - 86400, "1 day ago"},
		{"6 days", now - 6*86400, "6 days ago"},
		{"older falls back to date", now - 30*86400, "2023-10-15"},
		{"future clamps to just now", now + 100, "just now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := en.Since(now, tc.then); got != tc.want {
				t.Errorf("Since: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSincePolishPlurals(t *testing.T) {
	pl := New("pl")
	const now int64 = 1_700_000_000

	// Boundary cases for Polish plural rules.
	cases := []struct {
		name        string
		secondsBack int64
		want        string
	}{
		{"under minute", 30, "przed chwilą"},
		{"1 minute (one)", 60, "1 minutę temu"},
		{"2 minutes (few)", 2 * 60, "2 minuty temu"},
		{"4 minutes (few)", 4 * 60, "4 minuty temu"},
		{"5 minutes (many)", 5 * 60, "5 minut temu"},
		{"12 minutes (teen → many)", 12 * 60, "12 minut temu"},
		{"14 minutes (teen → many)", 14 * 60, "14 minut temu"},
		{"22 minutes (back to few)", 22 * 60, "22 minuty temu"},
		{"25 minutes (many)", 25 * 60, "25 minut temu"},
		{"1 day (one)", 86400, "1 dzień temu"},
		{"3 days (few)", 3 * 86400, "3 dni temu"},
		{"5 days (many)", 5 * 86400, "5 dni temu"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pl.Since(now, now-tc.secondsBack); got != tc.want {
				t.Errorf("Since: got %q, want %q", got, tc.want)
			}
		})
	}
}
