package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"meetlet/internal/calendar"
	"meetlet/internal/places"
	"meetlet/internal/planner"
	"meetlet/internal/settings"
)

var (
	accent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6E3A1"))
	muted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9299B0"))
	warning = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9D58B"))
	strong  = lipgloss.NewStyle().Bold(true)
)

// ui has one screen and one reusable input. mode is empty while browsing, or
// names the field being edited. This keeps keyboard behavior in one place.
type ui struct {
	cfg                  settings.Config
	configPath           string
	meeting              planner.Meeting
	now                  time.Time
	input                textinput.Model
	mode                 string
	selected, suggestion int
	width, height        int
	status               string
	problem              bool
	ambiguous            *planner.AmbiguousError
	calendarDate         time.Time // A date in UTC, used only for calendar arithmetic.
	findCities           citySearch
	matches              []places.Location
	searchQuery          string
	searchGeneration     uint64
	searching            bool
	searchErr            error
	cancelSearch         context.CancelFunc
	lastExport           string // Absolute path of the last file exported this session.
}

// writeAll is a helper function which saves inefficient string concatenation
func writeAll(b *strings.Builder, parts ...string) {
	for _, p := range parts {
		b.WriteString(p)
	}
}

type tickMsg time.Time

func launchUI(cfg settings.Config, path string, m planner.Meeting, now time.Time) error {
	_, err := tea.NewProgram(newUI(cfg, path, m, now)).Run()
	return err
}

func newUI(cfg settings.Config, path string, m planner.Meeting, now time.Time) ui {
	input := textinput.New()
	input.CharLimit = 240
	input.SetWidth(60)
	return ui{cfg: cfg, configPath: path, meeting: m, now: now, input: input, width: 90, height: 36, findCities: places.Find}
}

func tick() tea.Cmd        { return tea.Tick(time.Minute, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m ui) Init() tea.Cmd { return tick() }

func (m *ui) message(err error, success string) {
	m.problem = err != nil
	if err != nil {
		m.status = err.Error()
	} else {
		m.status = success
	}
}

func (m *ui) edit(mode, value string) tea.Cmd {
	m.mode, m.suggestion, m.status, m.problem = mode, 0, "", false
	m.input.SetValue(value)
	m.input.CursorEnd()
	if mode == "date" {
		m.calendarDate, _ = time.Parse("2006-01-02", value)
	}
	focus := m.input.Focus()
	if mode == "add" {
		return tea.Batch(focus, m.startCitySearch())
	}
	return focus
}

func (m *ui) closeInput() {
	if m.cancelSearch != nil {
		m.cancelSearch()
		m.cancelSearch = nil
	}
	m.searching = false
	m.mode, m.ambiguous = "", nil
	m.input.Blur()
}

// setWall rejects skipped times and asks for an explicit choice of repeated hour.
func (m *ui) setWall(date, clock string) {
	loc, _ := m.meeting.Origin.Load()
	at, err := planner.WallTime(date, clock, loc, "")
	var ambiguous *planner.AmbiguousError
	if errors.As(err, &ambiguous) {
		m.mode, m.ambiguous, m.status = "fold", ambiguous, ""
		return
	}
	if err != nil {
		m.message(err, "")
		return
	}
	m.meeting.At = at
	m.closeInput()
	m.message(nil, "Meeting time updated")
}

// save commits a new config only after its disk write succeeds.
func (m *ui) save(cfg settings.Config) bool {
	if err := settings.Save(m.configPath, cfg); err != nil {
		m.message(err, "")
		return false
	}
	m.cfg = cfg
	m.meeting.Locations = cfg.Locations
	return true
}

func (m *ui) submit() {
	value := strings.TrimSpace(m.input.Value())
	switch m.mode {
	case "date":
		m.setWall(value, m.meeting.At.Format("15:04"))
	case "time":
		m.setWall(m.meeting.At.Format("2006-01-02"), value)
	case "title":
		if value == "" {
			m.message(fmt.Errorf("enter a meeting title"), "")
			return
		}
		m.meeting.Title = value
		m.closeInput()
	case "duration":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 || d > 168*time.Hour {
			m.message(fmt.Errorf("use a duration such as 45m or 1h (maximum 168h)"), "")
			return
		}
		m.meeting.Duration = d
		m.closeInput()
	case "add":
		p, err := m.selectedCity()
		if err != nil {
			m.message(err, "")
			return
		}
		if places.Contains(m.cfg.Locations, p) {
			m.message(fmt.Errorf("%s is already saved", p.Name), "")
			return
		}
		cfg := m.cfg
		cfg.Locations = append(slices.Clone(cfg.Locations), p)
		if !m.save(cfg) {
			return
		}
		m.selected = len(cfg.Locations) - 1
		m.closeInput()
		m.message(nil, "Added "+p.Name)
	case "export":
		if value == "" {
			m.message(fmt.Errorf("enter a file path"), "")
			return
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			m.message(err, "")
			return
		}
		if err := calendar.Write(abs, m.meeting, m.now); err != nil {
			m.message(err, "")
			return
		}
		m.lastExport = abs
		m.closeInput()
		m.message(nil, "Calendar event saved: "+abs)
	case "delete-ics":
		if value == "" {
			m.message(fmt.Errorf("enter the .ics file path to delete"), "")
			return
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			m.message(err, "")
			return
		}
		if err := calendar.Delete(abs); err != nil {
			m.message(err, "")
			return
		}
		if abs == m.lastExport {
			m.lastExport = ""
		}
		m.closeInput()
		m.message(nil, "Calendar file deleted: "+abs)
	}
}

func (m ui) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, min(80, msg.Width-10)))
		return m, nil
	case tickMsg:
		m.now = time.Time(msg)
		return m, tick()
	case citySearchRequest:
		cmd := m.requestCities(msg)
		return m, cmd
	case citySearchResults:
		m.receiveCities(msg)
		return m, nil
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.closeInput()
			return m, tea.Quit
		}
		if m.mode != "" {
			if m.mode == "date" && m.moveCalendar(key) {
				return m, nil
			}
			switch key {
			case "esc":
				m.closeInput()
				return m, nil
			case "enter":
				if m.mode != "fold" {
					m.submit()
				}
				return m, nil
			case "up", "down", "ctrl+p", "ctrl+n":
				if m.mode == "add" {
					n := len(m.matches)
					if n > 0 {
						step := 1
						if key == "up" || key == "ctrl+p" {
							step = -1
						}
						m.suggestion = (m.suggestion + step + n) % n
					}
					return m, nil
				}
			}
			if m.mode == "fold" {
				if key == "1" || key == "2" {
					m.meeting.At = m.ambiguous.Earlier
					if key == "2" {
						m.meeting.At = m.ambiguous.Later
					}
					m.closeInput()
					m.message(nil, "Repeated hour selected explicitly")
				}
				return m, nil
			}
			var cmd tea.Cmd
			before := m.input.Value()
			m.input, cmd = m.input.Update(msg)
			if m.input.Value() != before {
				m.suggestion = 0
				if m.mode == "add" {
					cmd = tea.Batch(cmd, m.startCitySearch())
				}
			}
			if m.mode == "date" {
				m.calendarDate = m.highlightedDate()
			}
			return m, cmd
		}
		switch key {
		case "q", "esc":
			return m, tea.Quit
		case "j", "down", "ctrl+n":
			m.selected = (m.selected + 1) % len(m.cfg.Locations)
		case "k", "up", "ctrl+p":
			m.selected = (m.selected + len(m.cfg.Locations) - 1) % len(m.cfg.Locations)
		case "d":
			cmd := m.edit("date", m.meeting.At.Format("2006-01-02"))
			return m, cmd
		case "t":
			cmd := m.edit("time", m.meeting.At.Format("15:04"))
			return m, cmd
		case "n":
			cmd := m.edit("title", m.meeting.Title)
			return m, cmd
		case "l":
			cmd := m.edit("duration", m.meeting.Duration.String())
			return m, cmd
		case "a":
			cmd := m.edit("add", "")
			return m, cmd
		case "e":
			cmd := m.edit("export", "meetlet-"+m.meeting.At.Format("20060102-1504")+".ics")
			return m, cmd
		case "D", "shift+d":
			path := m.lastExport
			if path == "" {
				path = "meetlet-" + m.meeting.At.Format("20060102-1504") + ".ics"
			}
			cmd := m.edit("delete-ics", path)
			return m, cmd
		case "left", "right", "ctrl+b", "ctrl+f":
			step := 1
			if key == "left" || key == "ctrl+b" {
				step = -1
			}
			// Calendar-day arithmetic on a neutral date preserves the entered
			// wall clock even when a local day is only 23 or 25 hours long.
			date, _ := time.Parse("2006-01-02", m.meeting.At.Format("2006-01-02"))
			m.setWall(date.AddDate(0, 0, step).Format("2006-01-02"), m.meeting.At.Format("15:04"))
		case "[", "]":
			step := 15 * time.Minute
			if key == "[" {
				step = -step
			}
			m.meeting.At = m.meeting.At.Add(step)
		case "s", "enter":
			p := m.cfg.Locations[m.selected]
			cfg := m.cfg
			cfg.Source = p.Name
			if m.save(cfg) {
				loc, _ := p.Load()
				m.meeting.Origin = p
				m.meeting.At = m.meeting.At.In(loc)
				m.message(nil, "Source: "+p.Name+" · meeting instant preserved")
			}
		case "x":
			p := m.cfg.Locations[m.selected]
			if p.Name == m.cfg.Source || p == m.meeting.Origin {
				m.message(fmt.Errorf("choose another source before removing %s", p.Name), "")
				break
			}
			cfg := m.cfg
			cfg.Locations = slices.Delete(slices.Clone(cfg.Locations), m.selected, m.selected+1)
			if m.save(cfg) {
				m.selected = min(m.selected, len(cfg.Locations)-1)
				m.message(nil, "Removed "+p.Name)
			}
		}
	}
	if m.mode != "" && m.mode != "fold" {
		var cmd tea.Cmd
		before := m.input.Value()
		m.input, cmd = m.input.Update(msg)
		if m.mode == "add" && m.input.Value() != before {
			cmd = tea.Batch(cmd, m.startCitySearch())
		}
		if m.mode == "date" {
			m.calendarDate = m.highlightedDate()
		}
		return m, cmd
	}
	return m, nil
}

func (m ui) View() tea.View {
	var b strings.Builder
	b.WriteString(accent.Bold(true).Render("meetlet") + muted.Render("  /  a time that travels well") + "\n\n")
	if m.mode == "date" {
		b.WriteString(m.datePickerView())
		return m.terminalView(b.String())
	}
	if m.mode == "add" {
		b.WriteString(m.citySearchView())
		return m.terminalView(b.String())
	}
	b.WriteString(strong.Render(m.meeting.Title) + muted.Render("  ·  "+m.meeting.Duration.String()) + "\n")
	b.WriteString(accent.Render(m.meeting.At.Format("Mon 02 Jan 2006  ·  15:04 MST")) + "\n")
	b.WriteString(muted.Render("Arranged in "+m.meeting.Origin.Name+" · "+m.meeting.Origin.Zone) + "\n\n")

	if m.mode == "" {
		// Render a moving window so adding locations never pushes controls offscreen.
		rows := max(1, (m.height-17)/2)
		start := max(0, m.selected-rows+1)
		end := min(len(m.cfg.Locations), start+rows)
		for i := start; i < end; i++ {
			p := m.cfg.Locations[i]
			loc, _ := p.Load()
			at := m.meeting.At.In(loc)
			zone, offset := at.Zone()
			pointer := "  "
			if i == m.selected {
				pointer = accent.Render("› ")
			}
			label := p.Name
			if p == m.meeting.Origin {
				label += " · source"
			}
			b.WriteString(pointer + strong.Render(label) + "   " + accent.Render(at.Format("15:04")) + "  " + at.Format("Mon 02 Jan") + "\n")
			b.WriteString("  " + muted.Render(zone+" · "+planner.Offset(offset)+" · "+p.Zone) + "\n")
		}
		if end-start < len(m.cfg.Locations) {
			b.WriteString(muted.Render(fmt.Sprintf("  %d–%d of %d locations · ↑/↓ to browse", start+1, end, len(m.cfg.Locations))) + "\n")
		}

		p := m.cfg.Locations[m.selected]
		loc, _ := p.Load()
		b.WriteString("\n" + strong.Render("Clock changes · "+p.Name) + "\n")
		changes := planner.Changes(loc, m.now, m.meeting.At)
		if len(changes) == 0 {
			b.WriteString(muted.Render("None between now and the meeting.") + "\n")
		} else {
			b.WriteString(warning.Render(strconv.Itoa(len(changes))+" between now and the meeting; first:") + "\n")
			b.WriteString(warning.Render(changes[0].String()) + "\n")
		}
		if next := planner.NextChange(loc, m.meeting.At); next != nil {
			b.WriteString(muted.Render("Next after meeting: ") + next.String() + "\n")
		} else {
			b.WriteString(muted.Render("No clock change in the 12 months after the meeting.") + "\n")
		}
		// A global count flags changes in locations outside the visible window.
		count := 0
		for _, place := range m.meeting.AllLocations() {
			zone, _ := place.Load()
			if len(planner.Changes(zone, m.now, m.meeting.At)) > 0 {
				count++
			}
		}
		if count > 0 {
			b.WriteString(warning.Render(fmt.Sprintf("%d location(s) change clocks between now and the meeting.", count)) + "\n")
		}
	}

	if m.mode != "" {
		b.WriteString("\n" + m.inputView() + "\n")
	}
	if m.status != "" {
		style := accent
		if m.problem {
			style = warning
		}
		b.WriteString("\n" + style.Render(m.status) + "\n")
	}
	b.WriteString("\n" + muted.Render("d date · t time · ←/→/^B/^F day · [/] 15m · n title · l length") + "\n")
	b.WriteString(muted.Render("a add · x remove · ↑/↓/^P/^N pick · s source · e export · D delete · q quit"))
	return m.terminalView(b.String())
}

func (m ui) terminalView(content string) tea.View {
	width := max(20, min(m.width-4, 96))
	view := tea.NewView(lipgloss.NewStyle().Width(width).Margin(1, 2).Render(content))
	view.AltScreen = true
	return view
}

func (m ui) inputView() string {
	if m.mode == "fold" {
		return warning.Render("This time occurs twice. Select an occurrence:") + "\n" +
			"1  " + m.ambiguous.Earlier.Format("15:04 MST (Z07:00)") + "\n" +
			"2  " + m.ambiguous.Later.Format("15:04 MST (Z07:00)") + muted.Render("  · esc cancel")
	}
	labels := map[string]string{"date": "Meeting date · YYYY-MM-DD", "time": "Local time · 15:00 or 3pm", "title": "Meeting title", "duration": "Elapsed length · e.g. 45m or 1h", "add": "Add a city, IANA zone, or Name=Area/City", "export": "Save calendar event · .ics file path", "delete-ics": "Delete calendar file · .ics file path"}
	action := "save"
	if m.mode == "delete-ics" {
		action = "delete"
	}
	s := strong.Render(labels[m.mode]) + "\n" + m.input.View() + muted.Render("  enter "+action+" · esc cancel")
	return s
}
