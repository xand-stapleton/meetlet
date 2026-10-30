package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"meetlet/internal/places"
)

type citySearch func(context.Context, string, int) ([]places.Location, error)

// Each edit gets a generation number. Results from an older query (or a closed
// editor) are ignored, even if its request completes after the new one.
type citySearchRequest struct {
	generation uint64
	query      string
}
type citySearchResults struct {
	generation uint64
	matches    []places.Location
	err        error
}

func (m *ui) startCitySearch() tea.Cmd {
	if m.cancelSearch != nil {
		m.cancelSearch()
		m.cancelSearch = nil
	}
	m.searchGeneration++
	m.searchQuery = strings.TrimSpace(m.input.Value())
	m.matches = places.Search(m.searchQuery, 20)
	m.suggestion, m.searchErr, m.status, m.problem = 0, nil, "", false
	m.searching = utf8.RuneCountInString(m.searchQuery) >= 2 && !strings.ContainsAny(m.searchQuery, "/=")
	if !m.searching {
		return nil
	}
	request := citySearchRequest{m.searchGeneration, m.searchQuery}
	// Wait for a pause in typing; superseded timers never make HTTP requests.
	return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg { return request })
}

func (m *ui) requestCities(request citySearchRequest) tea.Cmd {
	if m.mode != "add" || request.generation != m.searchGeneration {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	m.cancelSearch = cancel
	find := m.findCities
	return func() tea.Msg {
		defer cancel()
		matches, err := find(ctx, request.query, 20)
		return citySearchResults{request.generation, matches, err}
	}
}

func (m *ui) receiveCities(results citySearchResults) {
	if m.mode != "add" || results.generation != m.searchGeneration {
		return
	}
	m.matches, m.searchErr, m.searching = results.matches, results.err, false
	m.suggestion, m.status, m.problem = 0, "", false
}

func (m ui) selectedCity() (places.Location, error) {
	query := strings.TrimSpace(m.input.Value())
	// Explicit zones and custom labels can always be added without the network.
	if strings.ContainsAny(query, "/=") || query != m.searchQuery {
		return places.Resolve(query, m.cfg.Locations)
	}
	if m.searching {
		return places.Location{}, fmt.Errorf("wait for city results, then select a location")
	}
	if len(m.matches) == 0 {
		return places.Location{}, fmt.Errorf("no city matches; try a shorter name or an IANA zone")
	}
	return m.matches[min(m.suggestion, len(m.matches)-1)], nil
}

func (m ui) citySearchView() string {
	var b strings.Builder
	b.WriteString(strong.Render("Find a town or city") + "\n")
	b.WriteString(m.input.View() + "\n")
	switch {
	case m.searching:
		b.WriteString(muted.Render("Searching worldwide…") + "\n")
	case m.searchErr != nil:
		b.WriteString(warning.Render("Worldwide lookup unavailable; showing offline matches.") + "\n")
	case utf8.RuneCountInString(m.searchQuery) < 2:
		b.WriteString(muted.Render("Type 2+ letters, a zone, or Name=Area/City.") + "\n")
	case len(m.matches) == 0:
		b.WriteString(muted.Render("No matches. Try a shorter spelling or add a country after a comma.") + "\n")
	}
	start := max(0, m.suggestion-5)
	end := min(len(m.matches), start+6)
	for i := start; i < end; i++ {
		line := "  " + m.matches[i].Name
		if i == m.suggestion {
			line = accent.Render("› " + m.matches[i].Name)
		} else {
			line = muted.Render(line)
		}
		b.WriteString(lipgloss.NewStyle().MaxWidth(max(20, m.width-6)).Render(line) + "\n")
	}
	if len(m.matches) > 0 {
		b.WriteString(muted.Render(fmt.Sprintf("%d/%d · %s", m.suggestion+1, len(m.matches), m.matches[m.suggestion].Zone)) + "\n")
	}
	if m.status != "" {
		b.WriteString(warning.Render(m.status) + "\n")
	}
	b.WriteString("\n" + muted.Render("↑/↓ or Ctrl-P/N choose · Enter add · Esc cancel") + "\n")
	b.WriteString(muted.Render(places.CityAttribution))
	return b.String()
}
