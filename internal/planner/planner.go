// Package planner handles wall times and zone transitions without fixed offsets.
package planner

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"meetlet/internal/places"
)

// Meeting stores one absolute instant. Its local representations are calculated
// on demand, so every displayed location and calendar export stays in sync.
type Meeting struct {
	At        time.Time
	Origin    places.Location
	Locations []places.Location
	Title     string
	Duration  time.Duration
}

// AmbiguousError exposes both instants when clocks repeat a local time.
type AmbiguousError struct{ Earlier, Later time.Time }

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("this local time occurs twice as clocks go back: %s or %s; choose an occurrence", e.Earlier.Format("15:04 MST (Z07:00)"), e.Later.Format("15:04 MST (Z07:00)"))
}

// WallTime interprets a date and clock reading in loc. Unlike time.Date, it
// rejects skipped hours and requires a choice when the same reading occurs twice.
func WallTime(date, clock string, loc *time.Location, fold string) (time.Time, error) {
	if fold != "" && fold != "earlier" && fold != "later" {
		return time.Time{}, fmt.Errorf("fold must be earlier or later")
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil || d.Year() < 1900 || d.Year() > 9998 {
		return time.Time{}, fmt.Errorf("use a date YYYY-MM-DD between 1900 and 9998")
	}
	clock = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(clock), " ", ""))
	var c time.Time
	for _, layout := range []string{"15:04", "3:04pm", "3pm"} {
		c, err = time.Parse(layout, clock)
		if err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("use a time like 15:00, 3pm, or 3:30pm")
	}
	wall := time.Date(d.Year(), d.Month(), d.Day(), c.Hour(), c.Minute(), 0, 0, time.UTC)
	// Gather actual offsets around the requested date, including half-hour DST
	// and skipped days. Validate every candidate by converting back to wall time.
	offsets := map[int]bool{}
	for probe := wall.Add(-72 * time.Hour); !probe.After(wall.Add(72 * time.Hour)); probe = probe.Add(time.Hour) {
		_, offset := probe.In(loc).Zone()
		offsets[offset] = true
	}
	var candidates []time.Time
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second).In(loc)
		if candidate.Format("2006-01-02 15:04") == date+" "+c.Format("15:04") {
			candidates = append(candidates, candidate)
		}
	}
	slices.SortFunc(candidates, func(a, b time.Time) int { return a.Compare(b) })
	if len(candidates) == 0 {
		return time.Time{}, fmt.Errorf("%s %s does not exist in %s because the clocks jump forward; choose another time", date, c.Format("15:04"), loc)
	}
	if len(candidates) > 1 {
		if fold == "" {
			return time.Time{}, &AmbiguousError{candidates[0], candidates[len(candidates)-1]}
		}
		if fold == "later" {
			return candidates[len(candidates)-1], nil
		}
	}
	return candidates[0], nil
}

// Change describes an offset transition; At uses the new local clock reading.
type Change struct {
	At                        time.Time
	Before, After             string
	BeforeOffset, AfterOffset int
}

func Offset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, seconds/3600, seconds%3600/60)
}

func (c Change) String() string {
	return fmt.Sprintf("%s: %s (%s) → %s (%s)", c.At.Format("02 Jan 2006 15:04"), c.Before, Offset(c.BeforeOffset), c.After, Offset(c.AfterOffset))
}

// Changes returns real offset changes in (start, end], using the zone database's
// exact bounds rather than assuming transitions happen on a particular Sunday.
func Changes(loc *time.Location, start, end time.Time) []Change {
	if end.Before(start) {
		start, end = end, start
	}
	var changes []Change
	for cursor := start.In(loc); cursor.Before(end); {
		_, boundary := cursor.ZoneBounds()
		if boundary.IsZero() || boundary.After(end) {
			break
		}
		if !boundary.After(cursor) {
			// Go's extrapolated POSIX rules can return a synthetic year-end
			// bound one day early in leap years. Check the real UTC year end.
			boundary = time.Date(cursor.UTC().Year()+1, 1, 1, 0, 0, 0, 0, time.UTC)
			if boundary.After(end) {
				break
			}
		}
		before, bo := boundary.Add(-time.Nanosecond).In(loc).Zone()
		after, ao := boundary.In(loc).Zone()
		if bo != ao {
			changes = append(changes, Change{boundary.In(loc), before, after, bo, ao})
		}
		cursor = boundary.In(loc)
	}
	return changes
}

func NextChange(loc *time.Location, at time.Time) *Change {
	changes := Changes(loc, at, at.AddDate(1, 0, 0))
	if len(changes) == 0 {
		return nil
	}
	return &changes[0]
}

func (m Meeting) AllLocations() []places.Location {
	out := []places.Location{m.Origin}
	for _, p := range m.Locations {
		if p != m.Origin {
			out = append(out, p)
		}
	}
	return out
}

func (m Meeting) Validate() error {
	if m.At.IsZero() {
		return fmt.Errorf("meeting time is required")
	}
	if strings.TrimSpace(m.Title) == "" {
		return fmt.Errorf("meeting title is required")
	}
	if m.Duration <= 0 || m.Duration > 7*24*time.Hour {
		return fmt.Errorf("duration must be greater than zero and at most 168h")
	}
	for _, p := range m.AllLocations() {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}
