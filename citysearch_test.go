package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"meetlet/internal/places"
	"meetlet/internal/settings"
)

var harwichChoices = []places.Location{
	{Name: "Harwich, Massachusetts, United States", Zone: "America/New_York"},
	{Name: "Harwich, England, United Kingdom", Zone: "Europe/London"},
}

func harwichSearch(context.Context, string, int) ([]places.Location, error) {
	return harwichChoices, nil
}

func TestCityCLIRequiresAChoiceAndSavesTheZone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("MEETLET_CONFIG", path)
	var out bytes.Buffer
	if err := runWithCitySearch([]string{"search", "harwich"}, &out, testNow, harwichSearch); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Massachusetts", "England", "America/New_York", "Europe/London"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	out.Reset()
	if err := runWithCitySearch([]string{"add", "harwich"}, &out, testNow, harwichSearch); err == nil {
		t.Fatal("ambiguous city was added without a choice")
	}
	for _, number := range []string{"1", "2"} {
		if err := runWithCitySearch([]string{"add", "--pick", number, "harwich"}, &out, testNow, harwichSearch); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Locations) != 6 {
		t.Fatal(cfg.Locations)
	}
	for _, want := range harwichChoices {
		if !places.Contains(cfg.Locations, want) {
			t.Fatal("missing saved city:", want)
		}
	}
	// Qualified city names contain commas, so a full label must still work as
	// one --to destination. A semicolon separates several qualified labels.
	for i, dest := range []string{harwichChoices[0].Name, harwichChoices[0].Name + ";" + harwichChoices[1].Name} {
		m, _, err := meetingArgs([]string{"--to", dest}, cfg, testNow, &out, false)
		if err != nil || len(m.Locations) != i+1 {
			t.Fatal(m, err)
		}
	}
	if err := runWithCitySearch([]string{"add", "--pick", "3", "harwich"}, &out, testNow, harwichSearch); err == nil {
		t.Fatal("accepted invalid result number")
	}
}

func TestCityEditorAddsSelectedResultAndDiscardsStaleSearches(t *testing.T) {
	cfg := settings.Default()
	meeting, _, _ := meetingArgs(nil, cfg, testNow, &bytes.Buffer{}, false)
	m := newUI(cfg, filepath.Join(t.TempDir(), "config.json"), meeting, testNow)
	m.findCities = harwichSearch
	m = press(m, "a")
	next, _ := m.Update(tea.PasteMsg{Content: "harwich"})
	m = next.(ui)
	if !m.searching {
		t.Fatal("typing did not schedule a search")
	}
	generation := m.searchGeneration
	next, cmd := m.Update(citySearchRequest{generation, "harwich"})
	m = next.(ui)
	if cmd == nil {
		t.Fatal("search was not dispatched")
	}
	next, _ = m.Update(cmd())
	m = next.(ui)
	if m.searching || len(m.matches) != 2 {
		t.Fatal(m.matches)
	}
	m = press(m, "ctrl+n")
	// A late response must not overwrite the current choice or city list.
	next, _ = m.Update(citySearchResults{generation - 1, []places.Location{{Name: "Wrong", Zone: "UTC"}}, nil})
	m = next.(ui)
	if len(m.matches) != 2 || m.suggestion != 1 {
		t.Fatal("stale response changed the selection")
	}
	m = press(m, "enter")
	if m.mode != "" || !places.Contains(m.cfg.Locations, harwichChoices[1]) {
		t.Fatal(m.status, m.cfg.Locations)
	}
	if places.Contains(m.cfg.Locations, harwichChoices[0]) {
		t.Fatal("wrong Harwich was saved")
	}
	next, _ = m.Update(citySearchResults{generation, harwichChoices, nil})
	m = next.(ui)
	if m.mode != "" {
		t.Fatal("late response reopened the search")
	}
}

func TestCityEditorCancelsAndExplainsOfflineSearch(t *testing.T) {
	cfg := settings.Default()
	meeting, _, _ := meetingArgs(nil, cfg, testNow, &bytes.Buffer{}, false)
	m := newUI(cfg, filepath.Join(t.TempDir(), "config.json"), meeting, testNow)
	canceled := false
	m.cancelSearch = func() { canceled = true }
	m = press(m, "a")
	if !canceled {
		t.Fatal("superseded search was not canceled")
	}
	m.input.SetValue("harwich")
	m.startCitySearch()
	generation := m.searchGeneration
	m = press(m, "enter")
	if m.mode != "add" || !m.problem {
		t.Fatal("search in progress added a city")
	}
	next, _ := m.Update(citySearchResults{generation, nil, fmt.Errorf("offline")})
	m = next.(ui)
	if !strings.Contains(m.View().Content, "Worldwide lookup unavailable") {
		t.Fatal(m.View().Content)
	}
	m = press(m, "esc")
	next, cmd := m.Update(citySearchRequest{generation, "harwich"})
	m = next.(ui)
	if cmd != nil || m.mode != "" {
		t.Fatal("closed editor dispatched a stale request")
	}
}
