package main

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	"meetlet/internal/settings"
)

func TestRenderFingerprints(t *testing.T) {
	cfg := settings.Default()
	meeting, _, err := meetingArgs(nil, cfg, testNow, &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"overview", "warnings", "date", "cities", "time", "title", "export", "delete-ics", "fold"} {
		m := newUI(cfg, "unused.json", meeting, testNow)
		switch mode {
		case "warnings":
			m.meeting.At = time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
		case "date":
			m.edit("date", "2026-08-31")
		case "cities":
			m.edit("add", "")
			m.input.SetValue("harwich")
			m.searchQuery, m.matches = "harwich", harwichChoices
		case "fold":
			m.setWall("2026-10-25", "01:30")
		case "overview":
		default:
			m.edit(mode, "Example · São Paulo")
		}
		t.Logf("fingerprint %s %x", mode, sha256.Sum256([]byte(m.View().Content)))
	}
}
