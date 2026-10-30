package planner

import (
	"errors"
	"testing"
	"time"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestLondonMeetingAcrossSeasons(t *testing.T) {
	// Europe and the US end daylight saving on different Sundays. The middle
	// case is the easy-to-miss week when Florida is only four hours behind.
	for _, tc := range []struct{ date, brazil, florida, bonn string }{
		{"2026-07-15", "11:00 -03", "10:00 EDT", "16:00 CEST"},
		{"2026-10-28", "12:00 -03", "11:00 EDT", "16:00 CET"},
		{"2026-11-02", "12:00 -03", "10:00 EST", "16:00 CET"},
	} {
		t.Run(tc.date, func(t *testing.T) {
			at, err := WallTime(tc.date, "3pm", zone(t, "Europe/London"), "")
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{"America/Sao_Paulo": tc.brazil, "America/New_York": tc.florida, "Europe/Berlin": tc.bonn} {
				if got := at.In(zone(t, name)).Format("15:04 MST"); got != want {
					t.Errorf("%s: got %s, want %s", name, got, want)
				}
			}
		})
	}
}

func TestSkippedAndRepeatedWallTimes(t *testing.T) {
	for _, tc := range []struct {
		zone, date, clock string
		repeated          bool
		gap               time.Duration
	}{
		{"Europe/London", "2026-03-29", "01:30", false, 0},
		{"Europe/London", "2026-10-25", "01:30", true, time.Hour},
		{"America/New_York", "2026-03-08", "02:30", false, 0},
		{"America/New_York", "2026-11-01", "01:30", true, time.Hour},
		{"Australia/Lord_Howe", "2026-10-04", "02:15", false, 0},
		{"Australia/Lord_Howe", "2026-04-05", "01:45", true, 30 * time.Minute},
		{"Pacific/Apia", "2011-12-30", "12:00", false, 0},
	} {
		t.Run(tc.zone+tc.date, func(t *testing.T) {
			loc := zone(t, tc.zone)
			_, err := WallTime(tc.date, tc.clock, loc, "")
			if err == nil {
				t.Fatal("expected an invalid or ambiguous wall time")
			}
			var ambiguous *AmbiguousError
			if errors.As(err, &ambiguous) != tc.repeated {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.repeated {
				return
			}
			early, err := WallTime(tc.date, tc.clock, loc, "earlier")
			if err != nil {
				t.Fatal(err)
			}
			late, err := WallTime(tc.date, tc.clock, loc, "later")
			if err != nil {
				t.Fatal(err)
			}
			if late.Sub(early) != tc.gap {
				t.Fatalf("expected %v between occurrences, got %v", tc.gap, late.Sub(early))
			}
		})
	}
}

func TestWallTimeInput(t *testing.T) {
	for _, clock := range []string{"15:00", "3pm", "3:00 PM"} {
		at, err := WallTime("2026-10-28", clock, time.UTC, "")
		if err != nil || at.Hour() != 15 {
			t.Errorf("%s: %v %v", clock, at, err)
		}
	}
	for _, tc := range []struct{ date, clock, fold string }{
		{"2026-02-30", "15:00", ""}, {"2026-10-28", "25:00", ""},
		{"2026-10-28", "15:00", "maybe"}, {"1800-01-01", "15:00", ""},
	} {
		if _, err := WallTime(tc.date, tc.clock, time.UTC, tc.fold); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
}

func TestChangesIncludeEveryTransition(t *testing.T) {
	start := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	for name, day := range map[string]int{"Europe/London": 25, "Europe/Berlin": 25, "America/New_York": 1, "America/Sao_Paulo": 0} {
		changes := Changes(zone(t, name), start, end)
		if day == 0 {
			if len(changes) != 0 {
				t.Fatal(changes)
			}
			continue
		}
		if len(changes) != 1 || changes[0].At.Day() != day {
			t.Fatalf("%s: %+v", name, changes)
		}
		if len(Changes(zone(t, name), end, start)) != 1 {
			t.Fatal("past meeting should also show intervening change")
		}
		if len(Changes(zone(t, name), changes[0].At, end)) != 0 {
			t.Fatal("start boundary should be excluded")
		}
	}
	// Even with identical endpoint offsets, both annual changes must be reported.
	for _, year := range []int{2026, 2040} {
		a := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		changes := Changes(zone(t, "Europe/London"), a, a.AddDate(1, 0, 0))
		if len(changes) != 2 {
			t.Fatalf("%d: expected two changes, got %+v", year, changes)
		}
	}
	if NextChange(time.UTC, start) != nil {
		t.Fatal("UTC never changes")
	}
}

func TestInternationalDateRollover(t *testing.T) {
	at, err := WallTime("2026-10-28", "23:30", zone(t, "Europe/London"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := at.In(zone(t, "Asia/Tokyo")).Format("2006-01-02 15:04"); got != "2026-10-29 08:30" {
		t.Fatal(got)
	}
	if Offset(20700) != "UTC+05:45" || Offset(-12600) != "UTC-03:30" {
		t.Fatal("fractional-hour offsets")
	}
}
