package main

import (
	"bytes"
	"testing"

	"meetlet/internal/settings"
)

var renderedView string

// Benchmark rendering without starting a terminal, making HTTP requests, or
// saving settings. Report allocations as well as time to catch string churn.
func BenchmarkView(b *testing.B) {
	cfg := settings.Default()
	meeting, _, err := meetingArgs(nil, cfg, testNow, &bytes.Buffer{}, false)
	if err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"overview", "date", "cities"} {
		b.Run(mode, func(b *testing.B) {
			m := newUI(cfg, "unused.json", meeting, testNow)
			switch mode {
			case "date":
				m.edit("date", "2026-08-31")
			case "cities":
				m.edit("add", "")
				m.input.SetValue("harwich")
				m.searchQuery, m.matches = "harwich", harwichChoices
			}
			b.ReportAllocs()
			for b.Loop() {
				renderedView = m.View().Content
			}
		})
	}
}
