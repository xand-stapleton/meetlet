// Package calendar writes portable RFC 5545 calendar events.
package calendar

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"meetlet/internal/planner"
)

// Encode uses UTC date-times, which RFC 5545 supports without a VTIMEZONE block.
// Calendar applications convert that instant to their own display time zone.
func Encode(m planner.Meeting, now time.Time) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	var description strings.Builder
	fmt.Fprintf(&description, "Arranged in %s (%s).\n\nLocal times:\n", m.Origin.Name, m.Origin.Zone)
	for _, p := range m.AllLocations() {
		loc, err := p.Load()
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&description, "%s: %s (%s)\n", p.Name, m.At.In(loc).Format("Mon 02 Jan 2006 15:04 MST"), p.Zone)
	}
	stamp := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Meetlet//Meeting planner//EN", "CALSCALE:GREGORIAN", "METHOD:PUBLISH",
		"BEGIN:VEVENT", "UID:" + hex.EncodeToString(id[:]) + "@meetlet.local", "DTSTAMP:" + stamp(now),
		"DTSTART:" + stamp(m.At), "DTEND:" + stamp(m.At.Add(m.Duration)),
		"SUMMARY:" + escape(m.Title), "DESCRIPTION:" + escape(description.String()),
		"STATUS:CONFIRMED", "TRANSP:OPAQUE", "END:VEVENT", "END:VCALENDAR",
	}
	var out strings.Builder
	for _, line := range lines {
		out.WriteString(fold(line))
		out.WriteString("\r\n")
	}
	return []byte(out.String()), nil
}

func escape(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n").Replace(s)
}

// Fold at 75 octets, counting the continuation space and preserving UTF-8.
func fold(line string) string {
	var out strings.Builder
	limit := 75
	for len(line) > limit {
		n := limit
		for n > 0 && !utf8.RuneStart(line[n]) {
			n--
		}
		out.WriteString(line[:n])
		out.WriteString("\r\n ")
		line, limit = line[n:], 74
	}
	out.WriteString(line)
	return out.String()
}

// Write refuses to silently replace an existing invitation.
func Write(path string, m planner.Meeting, now time.Time) error {
	b, err := Encode(m, now)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create invite: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}

// Delete removes one calendar file. Checking the extension and file type avoids
// deleting a directory, symlink, or unrelated file entered by mistake.
func Delete(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".ics") {
		return fmt.Errorf("choose an .ics calendar file")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("delete calendar file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("choose a regular .ics file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete calendar file: %w", err)
	}
	return nil
}
