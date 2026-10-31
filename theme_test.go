package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"meetlet/internal/planner"
	"meetlet/internal/settings"
)

func TestConfiguredColoursAcrossViews(t *testing.T) {
	cfg := settings.Default()
	cfg.Colours = settings.Colours{Accent: "#112233", Muted: "#445566", Warning: "#778899"}
	meeting, err := newMeeting(cfg, testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"overview", "date", "cities"} {
		t.Run(mode, func(t *testing.T) {
			m := newUI(cfg, "unused.yaml", meeting, testNow)
			switch mode {
			case "date":
				m.edit("date", "2026-10-28")
			case "cities":
				m.edit("add", "")
				m.matches, m.searchQuery = harwichChoices, "harwich"
			}
			m.status, m.problem = "Example warning", true
			view := m.View().Content
			// View returns true-colour ANSI; Bubble Tea adapts it to the terminal.
			for _, code := range []string{"38;2;17;34;51", "38;2;68;85;102", "38;2;119;136;153"} {
				if !strings.Contains(view, code) {
					t.Errorf("configured colour %s missing from %s view", code, mode)
				}
			}
			// Creating another UI must not change this model's palette.
			newUI(settings.Default(), "other.yaml", meeting, testNow)
			if m.View().Content != view {
				t.Fatal("another UI changed the configured colours")
			}
		})
	}
	m := newUI(cfg, "unused.yaml", meeting, testNow)
	styles := m.input.Styles()
	if styles.Focused.Prompt.Render(">") != m.accent.Render(">") ||
		styles.Focused.Placeholder.Render("hint") != m.muted.Render("hint") {
		t.Fatal("text editor does not use the configured palette")
	}
	if view := m.View(); view.ForegroundColor != nil || view.BackgroundColor != nil {
		t.Fatal("default text/background should follow the terminal")
	}
	cfg.Colours.Text, cfg.Colours.Background = "#D4BE98", "#282828"
	m = newUI(cfg, "unused.yaml", meeting, testNow)
	view := m.View()
	if view.ForegroundColor != lipgloss.Color(cfg.Colours.Text) || view.BackgroundColor != lipgloss.Color(cfg.Colours.Background) {
		t.Fatal("configured text/background colours were not applied")
	}
}

func TestClockChangeWarningColours(t *testing.T) {
	cfg := settings.Default()
	cfg.Colours.Warning = "#D8A657"
	cfg.Colours.Muted = "#928374"
	for _, date := range []string{"2026-10-28", "2026-11-02"} {
		t.Run(date, func(t *testing.T) {
			meeting := meetingAt(t, cfg, date, "15:00")
			m := newUI(cfg, "unused.yaml", meeting, testNow)
			m.width = 110
			if date == "2026-11-02" {
				m.selected = 2 // Florida changes clocks between now and the meeting.
			}
			loc, err := cfg.Locations[m.selected].Load()
			if err != nil {
				t.Fatal(err)
			}
			view := m.View().Content
			next := planner.NextChange(loc, meeting.At)
			if next == nil || !strings.Contains(view, m.warning.Render("Next after meeting: "+next.String())) {
				t.Fatal("upcoming clock-change notice does not use the configured warning colour")
			}
			changes := planner.Changes(loc, testNow, meeting.At)
			if len(changes) > 0 && !strings.Contains(view, m.warning.Render(changes[0].String())) {
				t.Fatal("change before the meeting does not use the configured warning colour")
			}
			if len(changes) == 0 && !strings.Contains(view, m.muted.Render("None between now and the meeting.")) {
				t.Fatal("no-change message should retain the muted colour")
			}
		})
	}
}

func TestSeparateSelectorColour(t *testing.T) {
	cfg := settings.Default()
	cfg.Colours.Accent, cfg.Colours.Selector = "#A9B665", "#EA6962"
	meeting, err := newMeeting(cfg, testNow)
	if err != nil {
		t.Fatal(err)
	}
	m := newUI(cfg, "unused.yaml", meeting, testNow)
	if !strings.Contains(m.View().Content, m.selector.Render("› ")) {
		t.Fatal("location chevron does not use the selector colour")
	}
	m.edit("date", "2026-10-28")
	calendar := m.datePickerView()
	if !strings.Contains(calendar, m.selector.Bold(true).Reverse(true).Render("[28]")) {
		t.Fatal("calendar selection does not use the selector colour")
	}
	if !strings.Contains(calendar, m.accent.Bold(true).Render("October 2026")) {
		t.Fatal("month heading should retain the accent colour")
	}
	styles := m.input.Styles()
	if styles.Cursor.Color != lipgloss.Color("#EA6962") || styles.Focused.Prompt.Render(">") != m.selector.Render(">") {
		t.Fatal("input prompt/cursor does not use the selector colour")
	}
	m.edit("add", "")
	m.matches, m.searchQuery = harwichChoices, "harwich"
	if !strings.Contains(m.citySearchView(), m.selector.Render("› "+harwichChoices[0].Name)) {
		t.Fatal("selected city does not use the selector colour")
	}
}
