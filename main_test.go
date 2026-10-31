package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"meetlet/internal/places"
	"meetlet/internal/planner"
	"meetlet/internal/settings"
)

var testNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func TestMeetingDefaultsFromConfig(t *testing.T) {
	cfg := settings.Default()
	cfg.Source = "Tokyo"
	cfg.Locations = []places.Location{{Name: "Tokyo", Zone: "Asia/Tokyo"}, {Name: "Home", Zone: "Europe/London"}}
	// Tokyo is already on the next date when the UTC day is still the 28th.
	now := time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)
	meeting, err := newMeeting(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	if meeting.At.Format("2006-01-02 15:04 MST") != "2026-10-29 15:00 JST" {
		t.Fatal("meeting did not use the source's local date and time:", meeting.At)
	}
	if meeting.Origin != cfg.Origin() || !reflect.DeepEqual(meeting.Locations, cfg.Locations) {
		t.Fatal("meeting did not use configured locations:", meeting)
	}
	if meeting.Title != "Meeting" || meeting.Duration != time.Hour {
		t.Fatal("incorrect initial title or duration:", meeting)
	}
}

// meetingAt sets up a particular local date for UI navigation and DST checks.
func meetingAt(t testing.TB, cfg settings.Config, date, clock string) planner.Meeting {
	t.Helper()
	meeting, err := newMeeting(cfg, testNow)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := meeting.Origin.Load()
	if err != nil {
		t.Fatal(err)
	}
	meeting.At, err = planner.WallTime(date, clock, loc, "")
	if err != nil {
		t.Fatal(err)
	}
	return meeting
}

func press(m ui, key string) ui {
	msg := tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	if strings.HasPrefix(key, "ctrl+") && len(key) == 6 {
		msg = tea.KeyPressMsg{Code: rune(key[5]), Mod: tea.ModCtrl}
	}
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "home":
		msg = tea.KeyPressMsg{Code: tea.KeyHome}
	}
	next, _ := m.Update(msg)
	return next.(ui)
}

func TestInteractiveEditingAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := settings.Default()
	cfg.Colours.Accent = "#123456"
	cfg.Colours.Selector = "#EA6962"
	meeting, err := newMeeting(cfg, testNow)
	if err != nil {
		t.Fatal(err)
	}
	m := newUI(cfg, path, meeting, testNow)
	m = press(m, "ctrl+n")
	if m.selected != 1 {
		t.Fatal("Ctrl-N did not select the next location")
	}
	m = press(m, "ctrl+p")
	if m.selected != 0 {
		t.Fatal("Ctrl-P did not select the previous location")
	}
	m = press(m, "t")
	if m.mode != "time" {
		t.Fatal("time shortcut did not open input")
	}
	m.input.SetValue("4:30pm")
	m = press(m, "enter")
	if m.mode != "" || m.meeting.At.Hour() != 16 || m.meeting.At.Minute() != 30 {
		t.Fatal("time was not updated")
	}
	m = press(m, "a")
	m.input.SetValue("tokyo")
	m = press(m, "enter")
	if m.problem || len(m.cfg.Locations) != 5 {
		t.Fatalf("add failed: %s", m.status)
	}
	instant := m.meeting.At
	m = press(m, "s")
	if m.meeting.Origin.Name != "Tokyo" || !m.meeting.At.Equal(instant) {
		t.Fatal("changing source moved the meeting")
	}
	saved, err := settings.Load(path)
	if err != nil || saved.Source != "Tokyo" || saved.Colours != cfg.Colours {
		t.Fatalf("source not saved: %+v %v", saved, err)
	}
	m = press(m, "e")
	invite := filepath.Join(t.TempDir(), "ui.ics")
	m.input.SetValue(invite)
	m = press(m, "enter")
	if _, err := os.Stat(invite); err != nil {
		t.Fatal(err)
	}
	m = press(m, "D")
	if m.mode != "delete-ics" || m.input.Value() != invite {
		t.Fatal("delete did not select the last exported file")
	}
	m = press(m, "esc")
	if _, err := os.Stat(invite); err != nil {
		t.Fatal("cancel deleted the calendar file:", err)
	}
	m = press(press(m, "D"), "enter")
	if m.mode != "" || m.problem || m.lastExport != "" {
		t.Fatal("delete failed:", m.status)
	}
	if _, err := os.Stat(invite); !os.IsNotExist(err) {
		t.Fatalf("calendar file was not deleted: %v", err)
	}
	// These keys edit text inside an input, rather than changing the date.
	instant = m.meeting.At
	m = press(m, "n")
	m.input.SetValue("Team")
	m = press(press(m, "ctrl+b"), "!")
	m = press(press(m, "ctrl+f"), "?")
	m = press(m, "enter")
	if m.meeting.Title != "Tea!m?" || !m.meeting.At.Equal(instant) {
		t.Fatal("Readline cursor movement changed the meeting:", m.meeting.Title, m.meeting.At)
	}
}

func TestInteractiveRepeatedHourAndWallDay(t *testing.T) {
	cfg := settings.Default()
	meeting := meetingAt(t, cfg, "2026-10-24", "01:30")
	m := newUI(cfg, filepath.Join(t.TempDir(), "config.yaml"), meeting, testNow)
	m = press(m, "ctrl+f")
	if m.mode != "fold" {
		t.Fatalf("expected occurrence chooser, got %s: %s", m.mode, m.status)
	}
	m = press(m, "2")
	if m.mode != "" || m.meeting.At.UTC().Format("2006-01-02 15:04") != "2026-10-25 01:30" {
		t.Fatal("wrong repeated hour")
	}
	m = press(m, "a")
	next, _ := m.Update(tea.PasteMsg{Content: "Sydney"})
	m = next.(ui)
	if m.input.Value() != "Sydney" {
		t.Fatalf("paste did not reach text input: %q", m.input.Value())
	}
}
