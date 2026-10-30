// Meetlet plans meeting times offline and searches worldwide cities online.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"meetlet/internal/calendar"
	"meetlet/internal/places"
	"meetlet/internal/planner"
	"meetlet/internal/settings"
)

const usage = `meetlet — find a time that travels well

Usage:
  meetlet                         Open the interactive planner
  meetlet check [options]         Print local meeting times and clock changes
  meetlet export [options]        Export a calendar event (.ics)
  meetlet delete-ics FILE.ics      Delete an exported calendar file
  meetlet add [options] PLACE     Save a city or IANA time zone
  meetlet remove NAME             Remove a saved location
  meetlet locations              List saved locations
  meetlet search QUERY           Fuzzy city search with region/country labels
  meetlet source NAME            Set the default meeting location

Meeting options (check / export):
  --from london        Meeting's original location (default: saved source)
  --date 2026-11-02    Date in that location (default: today there)
  --time 3pm           Local time; also accepts 15:00 (default: 15:00)
  --to campinas,bonn   Destinations (default: saved locations)
  --title "Catch up"   Calendar event title (default: Meeting)
  --duration 45m       Elapsed meeting length (default: 1h, maximum: 168h)
  --fold earlier      Choose earlier / later when a local time occurs twice
  --out meeting.ics    Calendar file path (export only)

Examples:
  meetlet check --from london --date 2026-11-02 --time 3pm
  meetlet export --date 2026-11-02 --time 3pm --title "Team sync" --out sync.ics
  meetlet add tokyo
  meetlet add --name "My office" Europe/Paris
  meetlet search harwich
  meetlet add --pick 2 harwich
  meetlet delete-ics sync.ics

Use --config PATH before a command, or set MEETLET_CONFIG, to choose a settings file.
Florida defaults to Eastern time; use "Florida (Central)" or pensacola for Central.
With redirected output, running meetlet prints times instead of opening the UI.
Worldwide city search uses Open-Meteo / GeoNames and needs an internet connection.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "meetlet:", err)
		os.Exit(1)
	}
}

// run keeps command parsing separate from the terminal UI and is easy to test.
func run(args []string, out io.Writer, now time.Time) error {
	return runWithCitySearch(args, out, now, places.Find)
}

func runWithCitySearch(args []string, out io.Writer, now time.Time, find citySearch) error {
	path, err := settings.Path()
	if err != nil {
		return err
	}
	root := flag.NewFlagSet("meetlet", flag.ContinueOnError)
	root.SetOutput(out)
	root.StringVar(&path, "config", path, "settings file")
	root.Usage = func() { fmt.Fprint(out, usage) }
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = root.Args()
	command := ""
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	if command == "help" {
		fmt.Fprint(out, usage)
		return nil
	}
	if command == "version" {
		fmt.Fprintln(out, "meetlet 0.1.0")
		return nil
	}
	// A file operation does not need to load the saved meeting locations.
	if command == "delete-ics" {
		f := flag.NewFlagSet("delete-ics", flag.ContinueOnError)
		f.SetOutput(out)
		f.Usage = func() { fmt.Fprintln(out, "Usage: meetlet delete-ics FILE.ics") }
		if err := f.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if f.NArg() != 1 {
			return fmt.Errorf("usage: meetlet delete-ics FILE.ics")
		}
		file, err := filepath.Abs(f.Arg(0))
		if err != nil {
			return err
		}
		if err := calendar.Delete(file); err != nil {
			return err
		}
		fmt.Fprintln(out, "Calendar file deleted:", file)
		return nil
	}
	if !slices.Contains([]string{"", "check", "export", "locations", "search", "add", "remove", "source"}, command) {
		return fmt.Errorf("unknown command %q; run meetlet help", command)
	}
	cfg, err := settings.Load(path)
	if err != nil {
		return err
	}
	switch command {
	case "", "check", "export":
		m, dest, err := meetingArgs(args, cfg, now, out, command == "export")
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		if command == "" && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			return launchUI(cfg, path, m, now)
		}
		if command == "export" {
			if err := calendar.Write(dest, m, now); err != nil {
				return err
			}
			abs, err := filepath.Abs(dest)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "Calendar event saved:", abs)
		}
		printMeeting(out, m, now)
		return nil
	case "locations":
		if len(args) != 0 {
			return fmt.Errorf("usage: meetlet locations")
		}
		for _, p := range cfg.Locations {
			mark := ""
			if p.Name == cfg.Source {
				mark = " (source)"
			}
			fmt.Fprintf(out, "%s%s\t%s\n", p.Name, mark, p.Zone)
		}
		fmt.Fprintln(out, "\nSettings:", path)
		return nil
	case "search":
		if len(args) == 0 {
			return fmt.Errorf("usage: meetlet search QUERY")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		matches, lookupErr := find(ctx, strings.Join(args, " "), 30)
		if len(matches) == 0 {
			if lookupErr != nil {
				return lookupErr
			}
			return fmt.Errorf("no matches; try an IANA zone such as Asia/Tokyo")
		}
		printCityMatches(out, matches)
		if lookupErr != nil {
			fmt.Fprintln(out, "\nWorldwide lookup unavailable; these are offline matches.")
		}
		fmt.Fprintln(out, "\n"+places.CityAttribution)
		return nil
	case "add":
		f := flag.NewFlagSet("add", flag.ContinueOnError)
		f.SetOutput(out)
		name := f.String("name", "", "custom location name (put this flag before the place)")
		pick := f.Int("pick", 0, "choose a numbered city result from meetlet search")
		if err := f.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if len(f.Args()) == 0 {
			return fmt.Errorf("usage: meetlet add [--name NAME] [--pick N] PLACE")
		}
		if *pick < 0 {
			return fmt.Errorf("pick must be a positive result number")
		}
		query := strings.Join(f.Args(), " ")
		p, err := places.Resolve(query, cfg.Locations)
		if err != nil || *pick > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			matches, lookupErr := find(ctx, query, 30)
			if lookupErr != nil {
				return lookupErr
			}
			if len(matches) == 0 {
				return fmt.Errorf("no city matches for %q", query)
			}
			if *pick == 0 && len(matches) > 1 {
				printCityMatches(out, matches)
				return fmt.Errorf("%q matches several locations; use add --pick N %q to choose one", query, query)
			}
			index := max(1, *pick) - 1
			if index >= len(matches) {
				return fmt.Errorf("pick must be between 1 and %d", len(matches))
			}
			p = matches[index]
		}
		if *name != "" {
			p.Name = *name
		}
		if places.Contains(cfg.Locations, p) {
			return fmt.Errorf("location %q is already saved", p.Name)
		}
		cfg.Locations = append(cfg.Locations, p)
		if err := settings.Save(path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Added %s (%s)\n", p.Name, p.Zone)
		return nil
	case "remove", "source":
		if len(args) == 0 {
			return fmt.Errorf("usage: meetlet %s NAME", command)
		}
		name := strings.Join(args, " ")
		i := slices.IndexFunc(cfg.Locations, func(p places.Location) bool { return strings.EqualFold(p.Name, name) })
		if i < 0 {
			return fmt.Errorf("%q is not a saved location; run meetlet locations", name)
		}
		p := cfg.Locations[i]
		if command == "source" {
			cfg.Source = p.Name
		} else {
			if p.Name == cfg.Source {
				return fmt.Errorf("choose another source before removing %s", p.Name)
			}
			cfg.Locations = slices.Delete(cfg.Locations, i, i+1)
		}
		if err := settings.Save(path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %s\n", command, p.Name)
		return nil
	}
	return nil
}

func printCityMatches(out io.Writer, matches []places.Location) {
	w := tabwriter.NewWriter(out, 0, 4, 3, ' ', 0)
	for i, p := range matches {
		fmt.Fprintf(w, "%d\t%s\t%s\n", i+1, p.Name, p.Zone)
	}
	w.Flush()
}

func meetingArgs(args []string, cfg settings.Config, now time.Time, out io.Writer, exporting bool) (planner.Meeting, string, error) {
	f := flag.NewFlagSet("meeting", flag.ContinueOnError)
	f.SetOutput(out)
	from := f.String("from", cfg.Source, "source city or IANA zone")
	date := f.String("date", "", "meeting date: YYYY-MM-DD")
	clock := f.String("time", "15:00", "meeting time: 15:00 or 3pm")
	to := f.String("to", "", "destinations separated by commas (or semicolons for qualified city names)")
	title := f.String("title", "Meeting", "event title")
	duration := f.Duration("duration", time.Hour, "elapsed meeting duration, e.g. 45m")
	fold := f.String("fold", "", "repeated local time: earlier or later")
	var dest string
	if exporting {
		f.StringVar(&dest, "out", "", "output .ics path (never overwritten)")
	}
	if err := f.Parse(args); err != nil {
		return planner.Meeting{}, "", err
	}
	if len(f.Args()) > 0 {
		return planner.Meeting{}, "", fmt.Errorf("unexpected argument %q; use flags such as --date and --time", f.Args()[0])
	}
	origin, err := places.Resolve(*from, cfg.Locations)
	if err != nil {
		return planner.Meeting{}, "", err
	}
	loc, err := origin.Load()
	if err != nil {
		return planner.Meeting{}, "", err
	}
	if *date == "" {
		*date = now.In(loc).Format("2006-01-02")
	}
	at, err := planner.WallTime(*date, *clock, loc, *fold)
	if err != nil {
		return planner.Meeting{}, "", err
	}
	m := planner.Meeting{At: at, Origin: origin, Locations: cfg.Locations, Title: *title, Duration: *duration}
	if *to != "" {
		m.Locations = nil
		names := strings.Split(*to, ",")
		if strings.Contains(*to, ";") {
			names = strings.Split(*to, ";")
		} else if _, err := places.Resolve(*to, cfg.Locations); err == nil {
			names = []string{*to} // A saved city label may itself contain commas.
		}
		for _, name := range names {
			p, err := places.Resolve(name, cfg.Locations)
			if err != nil {
				return m, "", err
			}
			if !slices.Contains(m.Locations, p) {
				m.Locations = append(m.Locations, p)
			}
		}
	}
	if dest == "" {
		dest = "meetlet-" + at.Format("20060102-1504") + ".ics"
	}
	return m, dest, m.Validate()
}

func printMeeting(out io.Writer, m planner.Meeting, now time.Time) {
	fmt.Fprintf(out, "\n%s · %s in %s · %s\n\n", m.Title, m.At.Format("Mon 02 Jan 2006 15:04 MST"), m.Origin.Name, m.Duration)
	w := tabwriter.NewWriter(out, 0, 4, 3, ' ', 0)
	fmt.Fprintln(w, "LOCATION\tLOCAL DATE\tTIME\tZONE\tUTC OFFSET")
	for _, p := range m.AllLocations() {
		loc, _ := p.Load() // Locations have already been validated.
		t := m.At.In(loc)
		zone, offset := t.Zone()
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.Name, t.Format("Mon 02 Jan 2006"), t.Format("15:04"), zone, planner.Offset(offset))
	}
	w.Flush()
	fmt.Fprintln(out, "\nClock changes between now and the meeting:")
	count := 0
	for _, p := range m.AllLocations() {
		loc, _ := p.Load()
		for _, change := range planner.Changes(loc, now, m.At) {
			fmt.Fprintf(out, "  %s — %s\n", p.Name, change)
			count++
		}
	}
	if count == 0 {
		fmt.Fprintln(out, "  None.")
	}
	fmt.Fprintln(out, "\nNext clock change after the meeting (within 12 months):")
	for _, p := range m.AllLocations() {
		loc, _ := p.Load()
		if next := planner.NextChange(loc, m.At); next != nil {
			fmt.Fprintf(out, "  %s — %s\n", p.Name, next)
		} else {
			fmt.Fprintf(out, "  %s — none\n", p.Name)
		}
	}
}
