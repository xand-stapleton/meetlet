package calendar

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"meetlet/internal/places"
	"meetlet/internal/planner"
	"meetlet/internal/settings"
)

func event(t *testing.T) planner.Meeting {
	t.Helper()
	p, err := places.Resolve("london", nil)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := p.Load()
	at, err := planner.WallTime("2026-07-15", "3pm", loc, "")
	if err != nil {
		t.Fatal(err)
	}
	return planner.Meeting{At: at, Origin: p, Locations: settings.Default().Locations, Title: "Team sync", Duration: 45 * time.Minute}
}

func TestCalendarUTCAndRequiredProperties(t *testing.T) {
	m := event(t)
	b, err := Encode(m, m.At)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(b), "\r\n ", "") // RFC 5545 unfolding.
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "VERSION:2.0\r\n", "UID:", "DTSTAMP:20260715T140000Z\r\n", "DTSTART:20260715T140000Z\r\n", "DTEND:20260715T144500Z\r\n", "SUMMARY:Team sync\r\n", "Campinas: Wed 15 Jul 2026 11:00", "END:VCALENDAR\r\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	if strings.Contains(strings.ReplaceAll(string(b), "\r\n", ""), "\n") {
		t.Fatal("bare line feed")
	}
	other, _ := Encode(m, m.At)
	if string(other) == string(b) {
		t.Fatal("each export needs a unique UID")
	}
}

func TestEscapingAndUTF8LineFolding(t *testing.T) {
	m := event(t)
	m.Title = strings.Repeat("日本語🌍", 20) + ", one; two\\three\r\nBEGIN:VEVENT"
	b, err := Encode(m, m.At)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) {
			t.Errorf("bad folded line: %q", line)
		}
	}
	unfolded := strings.ReplaceAll(string(b), "\r\n ", "")
	if !strings.Contains(unfolded, "SUMMARY:"+escape(m.Title)+"\r\n") {
		t.Fatal("folding changed the title")
	}
	if strings.Count(unfolded, "\r\nBEGIN:VEVENT\r\n") != 1 {
		t.Fatal("text injected a calendar component")
	}
}

func TestWriteDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting.ics")
	m := event(t)
	if err := Write(path, m, m.At); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := Write(path, m, m.At); err == nil {
		t.Fatal("overwrote an existing invite")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("existing invite was changed")
	}
	m.Duration = 0
	if _, err := Encode(m, m.At); err == nil {
		t.Fatal("accepted zero duration")
	}
}

func TestDeleteExportAndKeepOtherFiles(t *testing.T) {
	dir := t.TempDir()
	path, other := filepath.Join(dir, "meeting.ICS"), filepath.Join(dir, "other.ics")
	m := event(t)
	for _, file := range []string{path, other} {
		if err := Write(file, m, m.At); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(other)
	if err := Delete(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("export still exists: %v", err)
	}
	after, err := os.ReadFile(other)
	if err != nil || string(after) != string(before) {
		t.Fatal("another file was changed", err)
	}
	if err := Delete(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-file error was lost: %v", err)
	}
	// Removing a stale export also makes its filename available for a new one.
	if err := Write(path, m, m.At); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRejectsOtherFilesDirectoriesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	document := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(document, []byte("keep this"), 0600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "folder.ics")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.ics")
	if err := os.Symlink(document, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{document, folder, link} {
		if err := Delete(path); err == nil {
			t.Fatalf("deleted protected path %s", path)
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("path was removed: %s: %v", path, err)
		}
	}
	if got, _ := os.ReadFile(document); string(got) != "keep this" {
		t.Fatal("symlink target changed")
	}
}
