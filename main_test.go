package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"meetlet/internal/settings"
)

var testNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func TestCLIWorkflow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEETLET_CONFIG", filepath.Join(dir, "config.json"))
	invoke := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(args, &out, testNow); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}
	text := invoke("check", "--date", "2026-11-02", "--time", "3pm")
	for _, want := range []string{"London", "Campinas", "Florida (Eastern)", "Bonn", "10:00", "12:00", "16:00", "01 Nov 2026"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	invoke("add", "--name", "Office", "Asia/Tokyo")
	invoke("source", "Office")
	if text := invoke("locations"); !strings.Contains(text, "Office (source)") {
		t.Fatal(text)
	}
	invoke("source", "London")
	invoke("remove", "Office")
	if text := invoke("locations"); strings.Contains(text, "Office") {
		t.Fatal(text)
	}
	invite := filepath.Join(dir, "sync.ics")
	invoke("export", "--from", "london", "--date", "2026-11-02", "--time", "3pm", "--duration", "30m", "--title", "Team sync", "--out", invite)
	b, err := os.ReadFile(invite)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("DTSTART:20261102T150000Z")) || !bytes.Contains(b, []byte("DTEND:20261102T153000Z")) {
		t.Fatal(string(b))
	}
	// Deleting an exported file does not depend on the locations config.
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke("delete-ics", invite)
	if _, err := os.Stat(invite); !os.IsNotExist(err) {
		t.Fatalf("calendar file was not deleted: %v", err)
	}
}

func TestCLIErrorsAndCustomDestination(t *testing.T) {
	t.Setenv("MEETLET_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	for _, args := range [][]string{
		{"check", "--date", "2026-03-29", "--time", "01:30"},
		{"check", "--date", "2026-10-25", "--time", "01:30"},
		{"export", "--duration", "0m"}, {"remove", "London"}, {"check", "--to", "made up"},
		{"delete-ics"}, {"delete-ics", "one.ics", "two.ics"}, {"delete-ics", "notes.txt"},
	} {
		if err := run(args, &bytes.Buffer{}, testNow); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"check", "--date", "2026-10-25", "--time", "01:30", "--fold", "later", "--to", "Tokyo"}, &out, testNow); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Tokyo") || strings.Contains(out.String(), "Campinas") {
		t.Fatal(out.String())
	}
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
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := settings.Default()
	meeting, _, err := meetingArgs(nil, cfg, testNow, &bytes.Buffer{}, false)
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
	if err != nil || saved.Source != "Tokyo" {
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
	meeting, _, err := meetingArgs([]string{"--date", "2026-10-24", "--time", "01:30"}, cfg, testNow, &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	m := newUI(cfg, filepath.Join(t.TempDir(), "config.json"), meeting, testNow)
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
