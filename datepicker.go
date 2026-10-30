package main

import (
	"fmt"
	"strings"
	"time"
)

// highlightedDate follows a valid typed date. While the user is partway through
// typing, keep the last valid selection visible instead of jumping months.
func (m ui) highlightedDate() time.Time {
	day, err := time.Parse("2006-01-02", strings.TrimSpace(m.input.Value()))
	if err == nil && day.Year() >= 1900 && day.Year() <= 9998 {
		return day
	}
	return m.calendarDate
}

// todayInSource returns a neutral calendar date, not the computer's local date.
// UTC arithmetic avoids skipping or repeating days around local clock changes.
func (m ui) todayInSource() time.Time {
	loc, _ := m.meeting.Origin.Load()
	today, _ := time.Parse("2006-01-02", m.now.In(loc).Format("2006-01-02"))
	return today
}

// moveCalendar handles only navigation keys. Other keys still reach the date's
// text input, and Enter uses the existing wall-time and daylight-saving checks.
func (m *ui) moveCalendar(key string) bool {
	day := m.highlightedDate()
	switch key {
	case "left", "ctrl+b":
		day = day.AddDate(0, 0, -1)
	case "right", "ctrl+f":
		day = day.AddDate(0, 0, 1)
	case "up", "ctrl+p":
		day = day.AddDate(0, 0, -7)
	case "down", "ctrl+n":
		day = day.AddDate(0, 0, 7)
	case "pgup", "pgdown":
		step := 1
		if key == "pgup" {
			step = -1
		}
		// Clamp the day: January 31 goes to February 28 (or 29), rather
		// than letting time.AddDate normalize it into March.
		month := time.Date(day.Year(), day.Month()+time.Month(step), 1, 0, 0, 0, 0, time.UTC)
		last := month.AddDate(0, 1, -1).Day()
		day = month.AddDate(0, 0, min(day.Day(), last)-1)
	case "home":
		day = m.todayInSource()
	default:
		return false
	}
	if day.Year() < 1900 || day.Year() > 9998 {
		m.message(fmt.Errorf("choose a date between 1900 and 9998"), "")
		return true
	}
	m.calendarDate = day
	m.input.SetValue(day.Format("2006-01-02"))
	m.input.CursorEnd()
	m.status, m.problem = "", false
	return true
}

func (m ui) datePickerView() string {
	day, today := m.highlightedDate(), m.todayInSource()
	first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	// Go numbers Sunday as zero; shift it to the final column of the week.
	padding := (int(first.Weekday()) + 6) % 7
	var b strings.Builder
	b.WriteString(strong.Render("Meeting date · "+m.meeting.Origin.Name) + "\n")
	b.WriteString(muted.Render("Keep "+m.meeting.At.Format("15:04")+" local time · "+m.meeting.Origin.Zone) + "\n\n")
	b.WriteString(accent.Bold(true).Render(day.Format("January 2006")) + "\n")
	b.WriteString(muted.Render("Mon Tue Wed Thu Fri Sat Sun") + "\n")
	b.WriteString(strings.Repeat("    ", padding))
	for n := 1; n <= last; n++ {
		cell := fmt.Sprintf("%3d ", n)
		switch {
		case n == day.Day():
			cell = accent.Bold(true).Reverse(true).Render(fmt.Sprintf("[%2d]", n))
		case today.Year() == day.Year() && today.Month() == day.Month() && today.Day() == n:
			cell = strong.Render(fmt.Sprintf("%3d*", n))
		}
		b.WriteString(cell)
		if (padding+n)%7 == 0 || n == last {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n" + strong.Render(day.Format("Monday, 02 January 2006")) + "\n")
	b.WriteString(muted.Render("* Today: "+today.Format("Mon 02 Jan 2006")) + "\n")
	dateInput := m.input
	dateInput.SetWidth(12) // Keep the short date field and its format hint on one line.
	b.WriteString(dateInput.View() + muted.Render("  YYYY-MM-DD") + "\n")
	if m.status != "" {
		b.WriteString(warning.Render(m.status) + "\n")
	}
	b.WriteString("\n" + muted.Render("←/→/^B/^F day · ↑/↓/^P/^N week · PgUp/PgDn month · Home today") + "\n")
	b.WriteString(muted.Render("Type a date · Enter select · Esc cancel"))
	return b.String()
}
