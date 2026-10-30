package places

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const CityAttribution = "City data: Open-Meteo / GeoNames · https://open-meteo.com/"

var worldwide = geocoder{
	client:   &http.Client{Timeout: 5 * time.Second},
	endpoint: "https://geocoding-api.open-meteo.com/v1/search",
}

type geocoder struct {
	client   *http.Client
	endpoint string
}

type cityResult struct {
	Name    string `json:"name"`
	Region  string `json:"admin1"`
	County  string `json:"admin2"`
	Country string `json:"country"`
	Zone    string `json:"timezone"`
	Feature string `json:"feature_code"`
}

func (c cityResult) label(includeCounty bool) string {
	parts := []string{c.Name}
	county := ""
	if includeCounty {
		county = c.County
	}
	for _, part := range []string{county, c.Region, c.Country} {
		if part != "" && !strings.EqualFold(part, parts[len(parts)-1]) {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

// Find combines worldwide city results with the offline aliases and IANA zones.
// On a network error it returns any offline matches alongside the error, letting
// the UI explain why worldwide results are unavailable. It never guesses a zone
// from a city's longitude: only validated IANA identifiers are accepted.
func Find(ctx context.Context, query string, limit int) ([]Location, error) {
	return worldwide.find(ctx, query, limit)
}

func (g geocoder) find(ctx context.Context, query string, limit int) ([]Location, error) {
	local := Search(query, limit)
	name, qualifier, qualified := strings.Cut(strings.TrimSpace(query), ",")
	letters := []rune(normalize(strings.TrimSpace(name)))
	if limit <= 0 || len(letters) < 2 || strings.ContainsAny(query, "/=") {
		return local, nil
	}

	results, err := g.request(ctx, query)
	if err != nil {
		return local, err
	}
	// The upstream service matches prefixes, not misspellings. If the full
	// spelling misses, retrieve a small prefix pool and rank it locally. Two
	// progressively shorter prefixes cover common mid-word typos without
	// issuing a request for every possible spelling.
	if len(results) == 0 && len(letters) >= 4 {
		prefixes := []int{min(len(letters)-1, max(3, len(letters)/2+1)), 3}
		previous := len(letters)
		for _, size := range prefixes {
			if size >= previous {
				continue
			}
			previous = size
			prefix := string(letters[:size])
			if qualified {
				prefix += "," + qualifier
			}
			pool, err := g.request(ctx, prefix)
			if err != nil {
				return local, err
			}
			for _, p := range pool {
				if matchScore(query, p.Name) >= 0 {
					results = append(results, p)
				}
			}
			if len(results) > 0 {
				break
			}
		}
	}
	// Remote labels identify the region and country. Prefer those over an
	// unqualified built-in name for the same city and time zone.
	seen := map[string]bool{}
	remoteCities := map[string]bool{}
	for _, p := range results {
		remoteCities[cityKey(p.Name)+"|"+p.Zone] = true
	}
	var merged []Location
	for i, p := range append(results, local...) {
		if i >= len(results) && !strings.Contains(p.Name, ",") && remoteCities[cityKey(p.Name)+"|"+p.Zone] {
			continue
		}
		key := normalize(p.Name) + "|" + p.Zone
		if !seen[key] {
			merged = append(merged, p)
			seen[key] = true
		}
	}
	slices.SortStableFunc(merged, func(a, b Location) int {
		x, y := matchScore(query, a.Name), matchScore(query, b.Name)
		// The service also knows translated and alternate city names.
		if x < 0 {
			x = 200
		}
		if y < 0 {
			y = 200
		}
		return x - y
	})
	return merged[:min(limit, len(merged))], nil
}

func (g geocoder) request(ctx context.Context, query string) ([]Location, error) {
	u, err := url.Parse(g.endpoint)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("name", query)
	params.Set("count", "100")
	params.Set("language", "en")
	params.Set("format", "json")
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Meetlet/0.1 (terminal meeting planner)")
	response, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("city lookup unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("city lookup returned HTTP %d; try again later", response.StatusCode)
	}
	var body struct {
		Results []cityResult `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid city lookup response: %w", err)
	}
	var out []Location
	counts := map[string]int{}
	for _, city := range body.Results {
		counts[city.label(false)+"|"+city.Zone]++
	}
	for _, city := range body.Results {
		if !strings.HasPrefix(city.Feature, "PPL") {
			continue // Exclude dams, airports, and countries.
		}
		// County names distinguish duplicate city names within the same region.
		includeCounty := counts[city.label(false)+"|"+city.Zone] > 1
		p := Location{city.label(includeCounty), city.Zone}
		if p.Validate() == nil {
			out = append(out, p)
		}
	}
	return out, nil
}
