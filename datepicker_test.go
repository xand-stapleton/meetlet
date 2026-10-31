package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"meetlet/internal/places"
	"meetlet/internal/settings"
)

func dateEditor(t *testing.T, date, clock string) ui {
	t.Helper()
	cfg := settings.Default()
	meeting := meetingAt(t, cfg, date, clock)
	return press(newUI(cfg, filepath.Join(t.TempDir(), "config.yaml"), meeting, testNow), "d")
}

func TestCalendarNavigationAndConfirmation(t *testing.T) {
	for _, tc := range []struct{ date, key, want string }{
		{"2026-12-31", "ctrl+f", "2027-01-01"},
		{"2026-01-01", "ctrl+b", "2025-12-31"},
		{"2026-10-28", "ctrl+p", "2026-10-21"},
		{"2026-10-28", "ctrl+n", "2026-11-04"},
		{"2026-03-28", "right", "2026-03-29"},
		{"2026-01-31", "pgdown", "2026-02-28"},
		{"2024-01-31", "pgdown", "2024-02-29"},
		{"2024-03-31", "pgup", "2024-02-29"},
	} {
		t.Run(tc.date+tc.key, func(t *testing.T) {
			m := dateEditor(t, tc.date, "15:30")
			original := m.meeting.At
			m = press(m, tc.key)
			if got := m.input.Value(); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
			if !m.meeting.At.Equal(original) {
				t.Fatal("navigation changed the meeting before confirmation")
			}
			m = press(m, "enter")
			if m.mode != "" || m.meeting.At.Format("2006-01-02 15:04") != tc.want+" 15:30" {
				t.Fatalf("date was not applied: %s %v", m.status, m.meeting.At)
			}
		})
	}
	m := dateEditor(t, "2026-10-28", "3pm")
	original := m.meeting.At
	m = press(press(m, "pgdown"), "esc")
	if m.mode != "" || !m.meeting.At.Equal(original) {
		t.Fatal("cancel changed the meeting")
	}
}

func TestCalendarWeekdayAlignmentAndTerminalSize(t *testing.T) {
	m := dateEditor(t, "2024-02-29", "3pm")
	stripStyle := regexp.MustCompile("\x1b\\[[0-9;:]*m")
	plain := stripStyle.ReplaceAllString(m.View().Content, "")
	if !strings.Contains(plain, "Thursday, 29 February 2024") {
		t.Fatal(plain)
	}
	if !strings.Contains(plain, "Mon Tue Wed Thu Fri Sat Sun") {
		t.Fatal("missing weekday headers", plain)
	}
	lines := strings.Split(plain, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Mon Tue Wed Thu Fri Sat Sun") {
			continue
		}
		indent := strings.Index(line, "Mon")
		if strings.Index(lines[i+1], "1") != indent+3*4+2 {
			t.Fatal("February 1 should be in the Thursday column", plain)
		}
		if strings.Index(lines[i+5], "[29]") != indent+3*4 {
			t.Fatal("February 29 should be in the Thursday column", plain)
		}
	}
	// August 2026 spans six calendar rows. Its controls must fit an 80×24 TTY.
	m = dateEditor(t, "2026-08-31", "3pm")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(ui)
	if height := lipgloss.Height(m.View().Content); height > 24 {
		t.Fatalf("calendar uses %d terminal rows", height)
	}
	if !strings.Contains(stripStyle.ReplaceAllString(m.View().Content, ""), "Monday, 31 August 2026") {
		t.Fatal("missing selected weekday")
	}
}

func TestCalendarTypedDatesAndSourceToday(t *testing.T) {
	m := dateEditor(t, "2026-10-28", "3pm")
	// Pasted dates follow the same update path as the actual terminal input.
	next, _ := m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	next, _ = next.Update(tea.PasteMsg{Content: "2024-02-29"})
	m = next.(ui)
	if got := m.highlightedDate().Format("2006-01-02"); got != "2024-02-29" {
		t.Fatal(got)
	}
	m.input.SetValue("2024-02-")
	if got := m.highlightedDate().Format("2006-01-02"); got != "2024-02-29" {
		t.Fatal("partial input lost the last valid date:", got)
	}

	cfg := settings.Default()
	cfg.Source = "Tokyo"
	cfg.Locations = []places.Location{{Name: "Tokyo", Zone: "Asia/Tokyo"}}
	meeting, err := newMeeting(cfg, testNow)
	if err != nil {
		t.Fatal(err)
	}
	// It is already the 29th in Tokyo, while the UTC date is still the 28th.
	now := time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)
	m = newUI(cfg, filepath.Join(t.TempDir(), "config.yaml"), meeting, now)
	m = press(press(m, "d"), "home")
	if m.input.Value() != "2026-10-29" {
		t.Fatal("Home did not use the source's date:", m.input.Value())
	}
}

func TestCalendarKeepsClockChangeValidation(t *testing.T) {
	m := dateEditor(t, "2026-03-28", "01:30")
	original := m.meeting.At
	m = press(press(m, "right"), "enter")
	if m.mode != "date" || !m.problem || !m.meeting.At.Equal(original) {
		t.Fatal("calendar accepted a skipped hour")
	}
	m = dateEditor(t, "2026-10-24", "01:30")
	m = press(press(m, "right"), "enter")
	if m.mode != "fold" {
		t.Fatal("calendar did not ask which repeated hour to use")
	}
	m = press(m, "2")
	if got := m.meeting.At.UTC().Format("2006-01-02 15:04"); got != "2026-10-25 01:30" {
		t.Fatal(got)
	}
	for _, tc := range []struct{ date, key string }{{"1900-01-01", "left"}, {"9998-12-31", "right"}} {
		m = dateEditor(t, tc.date, "3pm")
		m = press(m, tc.key)
		if !m.problem || m.input.Value() != tc.date {
			t.Fatal("calendar moved beyond supported dates")
		}
	}
}
