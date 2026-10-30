package calendar

import (
	"strings"
	"testing"
)

var escapedText string

func BenchmarkEscape(b *testing.B) {
	text := strings.Repeat("Team sync; São Paulo, London\\Bonn\r\nNext line\n", 20)
	b.ReportAllocs()
	for b.Loop() {
		escapedText = escape(text)
	}
}
