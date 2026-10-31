package main

import (
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

func TestCityEditorAddsSelectedResultAndDiscardsStaleSearches(t *testing.T) {
	cfg := settings.Default()
	meeting, _ := newMeeting(cfg, testNow)
	m := newUI(cfg, filepath.Join(t.TempDir(), "config.yaml"), meeting, testNow)
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
	meeting, _ := newMeeting(cfg, testNow)
	m := newUI(cfg, filepath.Join(t.TempDir(), "config.yaml"), meeting, testNow)
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
