package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"meetlet/internal/settings"
)

func TestHelpNavigation(t *testing.T) {
	cfg := settings.Default()
	meeting, _, err := meetingArgs(nil, cfg, testNow, &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, open := range []string{"?", "h"} {
		for _, close := range []string{"esc", "enter", "?", "h"} {
			t.Run(open+"/"+close, func(t *testing.T) {
				m := newUI(cfg, "unused.yaml", meeting, testNow)
				m.width, m.height, m.selected = 80, 24, 2
				overview := m.View().Content
				if !strings.Contains(overview, "demystify time zones") || strings.Contains(overview, startupJoke) {
					t.Fatal("main screen should show the tagline without the joke")
				}
				m = press(m, open)
				if m.mode != "help" || !strings.Contains(m.View().Content, startupJoke) {
					t.Fatal("help did not open with the joke")
				}
				if height := lipgloss.Height(m.View().Content); height > 24 {
					t.Fatalf("help uses %d rows; should fit an 80×24 terminal", height)
				}
				// Meeting shortcuts must not edit or save anything while reading help.
				for _, key := range []string{"right", "down", "d", "s", "e", "D"} {
					m = press(m, key)
				}
				if !reflect.DeepEqual(m.meeting, meeting) || !reflect.DeepEqual(m.cfg, cfg) || m.selected != 2 {
					t.Fatal("help changed the meeting or saved locations")
				}
				m = press(m, close)
				if m.mode != "" || m.View().Content != overview {
					t.Fatal("closing help did not restore the meeting")
				}
			})
		}
	}
	m := press(newUI(cfg, "unused.yaml", meeting, testNow), "?")
	for _, msg := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Fatal("quit key did not produce a command")
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatal("quit key did not quit from help")
		}
	}
}
