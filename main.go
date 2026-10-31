// Meetlet plans meeting times offline and searches worldwide cities online.
package main

import (
	"fmt"
	"os"
	"time"

	"meetlet/internal/planner"
	"meetlet/internal/settings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "meetlet:", err)
		os.Exit(1)
	}
}

func run() error {
	path, err := settings.Path()
	if err != nil {
		return err
	}
	cfg, err := settings.Load(path)
	if err != nil {
		return err
	}
	now := time.Now()
	meeting, err := newMeeting(cfg, now)
	if err != nil {
		return err
	}
	return launchUI(cfg, path, meeting, now)
}

// newMeeting starts at 15:00 today in the configured source location.
func newMeeting(cfg settings.Config, now time.Time) (planner.Meeting, error) {
	origin := cfg.Origin()
	loc, err := origin.Load()
	if err != nil {
		return planner.Meeting{}, err
	}
	at, err := planner.WallTime(now.In(loc).Format("2006-01-02"), "15:00", loc, "")
	if err != nil {
		return planner.Meeting{}, err
	}
	meeting := planner.Meeting{
		At: at, Origin: origin, Locations: cfg.Locations,
		Title: "Meeting", Duration: time.Hour,
	}
	return meeting, meeting.Validate()
}
