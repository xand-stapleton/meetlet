package places

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// All HTTP tests use an in-memory transport; tests never depend on an external
// service, a network connection, or a listener port.
type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockGeocoder(fn func(*http.Request) (string, int)) geocoder {
	return geocoder{endpoint: "https://cities.example/search", client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		body, status := fn(r)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}

const harwichResponse = `{"results":[
  {"name":"Harwich","admin1":"Massachusetts","country":"United States","timezone":"America/New_York","feature_code":"PPL"},
  {"name":"Harwich","admin1":"England","country":"United Kingdom","timezone":"Europe/London","feature_code":"PPL"},
  {"name":"Harwich Dam","admin1":"Massachusetts","country":"United States","timezone":"America/New_York","feature_code":"DAM"},
  {"name":"Harwich","admin1":"Invalid","country":"Test","timezone":"Invalid/Zone","feature_code":"PPL"}
]}`

func TestWorldwideCitiesAreDisambiguatedAndValidated(t *testing.T) {
	g := mockGeocoder(func(r *http.Request) (string, int) {
		if r.URL.Query().Get("name") != "harwich" || r.URL.Query().Get("count") != "100" || r.URL.Query().Get("language") != "en" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		return harwichResponse, http.StatusOK
	})
	got, err := g.find(context.Background(), "harwich", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the two cities, got %+v", got)
	}
	if got[0] != (Location{"Harwich, Massachusetts, United States", "America/New_York"}) || got[1] != (Location{"Harwich, England, United Kingdom", "Europe/London"}) {
		t.Fatal(got)
	}
	// Qualified labels persist through the existing offline resolver.
	p, err := Resolve(got[1].Name, got)
	if err != nil || p.Zone != "Europe/London" {
		t.Fatal(p, err)
	}
}

func TestWorldwideTypoFallbackAndLimit(t *testing.T) {
	var names []string
	g := mockGeocoder(func(r *http.Request) (string, int) {
		name := r.URL.Query().Get("name")
		names = append(names, name)
		if name == "harw" {
			return harwichResponse, http.StatusOK
		}
		return `{}`, http.StatusOK
	})
	got, err := g.find(context.Background(), "harwcih", 1)
	if err != nil || len(got) != 1 || got[0].Zone != "America/New_York" {
		t.Fatalf("%+v %v", got, err)
	}
	if strings.Join(names, ",") != "harwcih,harw" {
		t.Fatal("unexpected fallback requests:", names)
	}
	for _, limit := range []int{0, -1} {
		if got, err := g.find(context.Background(), "harwich", limit); len(got) != 0 || err != nil {
			t.Fatal(got, err)
		}
	}
}

func TestDifferentCitiesCanShareTheSameTimeZone(t *testing.T) {
	g := mockGeocoder(func(*http.Request) (string, int) {
		return `{"results":[
 {"name":"Springfield","admin1":"Illinois","country":"United States","timezone":"America/Chicago","feature_code":"PPL"},
 {"name":"Springfield","admin1":"Missouri","country":"United States","timezone":"America/Chicago","feature_code":"PPL"},
 {"name":"Springfield","admin1":"Wisconsin","admin2":"Dane County","country":"United States","timezone":"America/Chicago","feature_code":"PPL"},
 {"name":"Springfield","admin1":"Wisconsin","admin2":"St. Croix County","country":"United States","timezone":"America/Chicago","feature_code":"PPL"}
]}`, http.StatusOK
	})
	got, err := g.find(context.Background(), "springfield", 20)
	if err != nil || len(got) != 4 {
		t.Fatal(got, err)
	}
	if Contains(got[:1], got[1]) {
		t.Fatal("distinct qualified cities were treated as duplicates")
	}
	if !strings.Contains(got[2].Name, "Dane County") || !strings.Contains(got[3].Name, "St. Croix County") {
		t.Fatal("missing county disambiguation", got)
	}
}

func TestWorldwideErrorsAndOfflineZones(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{{`{broken`, 200}, {`{}`, 429}, {`{}`, 503}} {
		g := mockGeocoder(func(*http.Request) (string, int) { return tc.body, tc.status })
		got, err := g.find(context.Background(), "london", 20)
		if err == nil || len(got) == 0 {
			t.Fatalf("expected error with offline London fallback, got %+v %v", got, err)
		}
	}
	g := mockGeocoder(func(*http.Request) (string, int) { t.Fatal("explicit zones must stay offline"); return "", 0 })
	if got, err := g.find(context.Background(), "Europe/London", 20); err != nil || len(got) == 0 {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.find(ctx, "harwich", 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation was lost: %v", err)
	}
}

func TestFuzzyNamesAndAccents(t *testing.T) {
	for _, tc := range []struct{ query, label string }{
		{"harwcih", "Harwich, England, United Kingdom"},
		{"harw", "Harwich, Massachusetts, United States"},
		{"harwich, UK", "Harwich, England, United Kingdom"},
		{"sao paulo", "São Paulo, Brazil"},
		{"Quebec", "Québec, Canada"},
		{"Krakow", "Kraków, Poland"},
	} {
		if score := matchScore(tc.query, tc.label); score < 0 {
			t.Errorf("%q did not match %q", tc.query, tc.label)
		}
	}
	if score := matchScore("harwich, UK", "Harwich, Massachusetts, United States"); score >= 0 {
		t.Fatal("country qualifier was ignored")
	}
	if matchScore("harwich", "Tokyo") >= 0 {
		t.Fatal("unrelated city matched")
	}
	for _, zone := range []string{"Etc/GMT+3", "Etc/GMT-3"} {
		p, err := Resolve(zone, nil)
		if err != nil || p.Zone != zone {
			t.Fatalf("zone sign changed: %+v %v", p, err)
		}
	}
}
