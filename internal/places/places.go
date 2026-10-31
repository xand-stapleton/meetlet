// Package places resolves friendly city names and IANA time zone identifiers.
package places

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Keep the executable useful without a system zone database.
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed zones.txt
var zoneData string

// Location separates a friendly label (Bonn) from its rule set (Europe/Berlin).
type Location struct {
	Name string `yaml:"name"`
	Zone string `yaml:"zone"`
}

type entry struct {
	Location
	aliases string
}

var cities = []entry{
	{Location{"London", "Europe/London"}, "uk,england"},
	{Location{"Campinas", "America/Sao_Paulo"}, "campinas brazil"},
	{Location{"Florida (Eastern)", "America/New_York"}, "florida,miami,orlando,tampa,jacksonville"},
	{Location{"Florida (Central)", "America/Chicago"}, "pensacola,panama city"},
	{Location{"Bonn", "Europe/Berlin"}, "bonn germany"},
	{Location{"Berlin", "Europe/Berlin"}, "germany"},
	{Location{"São Paulo", "America/Sao_Paulo"}, "sao paulo"},
	{Location{"Rio de Janeiro", "America/Sao_Paulo"}, "rio"},
	{Location{"New York", "America/New_York"}, "nyc"},
	{Location{"Chicago", "America/Chicago"}, ""},
	{Location{"Denver", "America/Denver"}, ""},
	{Location{"Los Angeles", "America/Los_Angeles"}, "la,san francisco,seattle"},
	{Location{"Phoenix", "America/Phoenix"}, "arizona"},
	{Location{"Honolulu", "Pacific/Honolulu"}, "hawaii"},
	{Location{"Toronto", "America/Toronto"}, ""},
	{Location{"Vancouver", "America/Vancouver"}, ""},
	{Location{"Mexico City", "America/Mexico_City"}, ""},
	{Location{"Buenos Aires", "America/Argentina/Buenos_Aires"}, ""},
	{Location{"Lisbon", "Europe/Lisbon"}, ""},
	{Location{"Paris", "Europe/Paris"}, ""},
	{Location{"Madrid", "Europe/Madrid"}, ""},
	{Location{"Rome", "Europe/Rome"}, ""},
	{Location{"Amsterdam", "Europe/Amsterdam"}, ""},
	{Location{"Zurich", "Europe/Zurich"}, ""},
	{Location{"Helsinki", "Europe/Helsinki"}, ""},
	{Location{"Kyiv", "Europe/Kyiv"}, "kiev"},
	{Location{"Istanbul", "Europe/Istanbul"}, ""},
	{Location{"Johannesburg", "Africa/Johannesburg"}, ""},
	{Location{"Cairo", "Africa/Cairo"}, ""},
	{Location{"Nairobi", "Africa/Nairobi"}, ""},
	{Location{"Dubai", "Asia/Dubai"}, ""},
	{Location{"Mumbai", "Asia/Kolkata"}, "delhi,new delhi,india,kolkata"},
	{Location{"Kathmandu", "Asia/Kathmandu"}, "nepal"},
	{Location{"Bangkok", "Asia/Bangkok"}, ""},
	{Location{"Singapore", "Asia/Singapore"}, ""},
	{Location{"Hong Kong", "Asia/Hong_Kong"}, ""},
	{Location{"Shanghai", "Asia/Shanghai"}, "beijing"},
	{Location{"Tokyo", "Asia/Tokyo"}, ""},
	{Location{"Seoul", "Asia/Seoul"}, ""},
	{Location{"Perth", "Australia/Perth"}, ""},
	{Location{"Sydney", "Australia/Sydney"}, ""},
	{Location{"Melbourne", "Australia/Melbourne"}, ""},
	{Location{"Brisbane", "Australia/Brisbane"}, ""},
	{Location{"Auckland", "Pacific/Auckland"}, ""},
	{Location{"UTC", "UTC"}, "etc/utc"},
}

func normalize(s string) string {
	s = norm.NFD.String(strings.ToLower(strings.TrimSpace(s)))
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	s = strings.NewReplacer("ß", "ss", "ø", "o", "ł", "l", "æ", "ae", "œ", "oe").Replace(s)
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "_", " ")), " ")
}

func (p Location) Load() (*time.Location, error) { return time.LoadLocation(p.Zone) }

func (p Location) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || strings.ContainsFunc(p.Name, unicode.IsControl) {
		return fmt.Errorf("location name must be 1–200 bytes without control characters")
	}
	if p.Zone == "Local" || p.Zone == "" {
		return fmt.Errorf("use an explicit IANA zone such as Europe/London")
	}
	if _, err := p.Load(); err != nil {
		return fmt.Errorf("unknown time zone %q", p.Zone)
	}
	return nil
}

// Resolve accepts a known city, an IANA zone, or a custom Name=Area/City pair.
func Resolve(query string, saved []Location) (Location, error) {
	if name, zone, ok := strings.Cut(query, "="); ok {
		p, err := Resolve(zone, nil)
		if err != nil {
			return Location{}, err
		}
		p.Name = strings.TrimSpace(name)
		return p, p.Validate()
	}
	q := normalize(query)
	for _, p := range saved {
		if normalize(p.Name) == q {
			return p, nil
		}
	}
	for _, c := range cities {
		if normalize(c.Name) == q {
			return c.Location, nil
		}
		for alias := range strings.SplitSeq(c.aliases, ",") {
			if alias != "" && normalize(alias) == q {
				return c.Location, nil
			}
		}
	}
	for zone := range strings.FieldsSeq(zoneData) {
		if normalize(zone) == q {
			parts := strings.Split(zone, "/")
			return Location{strings.ReplaceAll(parts[len(parts)-1], "_", " "), zone}, nil
		}
	}
	return Location{}, fmt.Errorf("unknown location %q; search for a city or use an IANA zone (Asia/Tokyo) or Name=Area/City", query)
}

func Search(query string, limit int) []Location {
	if limit <= 0 {
		return nil
	}
	type match struct {
		place Location
		score int
	}
	var matches []match
	var out []Location
	seen := map[string]bool{}
	add := func(p Location, score int) {
		key := p.Name + "|" + p.Zone
		if !seen[key] && score >= 0 {
			matches = append(matches, match{p, score})
			seen[key] = true
		}
	}
	if p, err := Resolve(query, nil); err == nil {
		add(p, 0)
	}
	for _, c := range cities {
		score := matchScore(query, c.Name)
		for _, name := range append(strings.Split(c.aliases, ","), c.Zone) {
			if name == "" {
				continue
			}
			if s := matchScore(query, name); s >= 0 && (score < 0 || s < score) {
				score = s
			}
		}
		add(c.Location, score)
	}
	for zone := range strings.FieldsSeq(zoneData) {
		if score := matchScore(query, zone); score >= 0 {
			parts := strings.Split(zone, "/")
			add(Location{strings.ReplaceAll(parts[len(parts)-1], "_", " "), zone}, score)
		}
	}
	slices.SortStableFunc(matches, func(a, b match) int { return a.score - b.score })
	for _, m := range matches[:min(limit, len(matches))] {
		out = append(out, m.place)
	}
	return out
}

func Contains(locations []Location, p Location) bool {
	return slices.ContainsFunc(locations, func(other Location) bool {
		// A qualified city label also matches its older unqualified name. Two
		// fully qualified cities remain distinct even if they share a zone.
		unqualified := !strings.Contains(other.Name, ",") || !strings.Contains(p.Name, ",")
		return strings.EqualFold(other.Name, p.Name) || (unqualified && other.Zone == p.Zone && cityKey(other.Name) == cityKey(p.Name))
	})
}
