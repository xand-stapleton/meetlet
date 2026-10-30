package places

import "testing"

func TestResolutionAndSearch(t *testing.T) {
	for query, want := range map[string]string{"campinas": "America/Sao_Paulo", "Florida": "America/New_York", "pensacola": "America/Chicago", "bonn": "Europe/Berlin", "sao paulo": "America/Sao_Paulo", "asia/kathmandu": "Asia/Kathmandu", "Office=Europe/Paris": "Europe/Paris"} {
		p, err := Resolve(query, nil)
		if err != nil || p.Zone != want {
			t.Errorf("%s: %+v %v", query, p, err)
		}
	}
	if _, err := Resolve("Local", nil); err == nil {
		t.Fatal("machine-dependent zone should not resolve")
	}
	p, err := Resolve("Office", []Location{{"Office", "Asia/Tokyo"}})
	if err != nil || p.Zone != "Asia/Tokyo" {
		t.Fatal(p, err)
	}
	if matches := Search("florida", 6); len(matches) != 2 {
		t.Fatal(matches)
	}
	for _, p := range Search("", 1000) {
		if err := p.Validate(); err != nil {
			t.Fatalf("embedded zone %+v: %v", p, err)
		}
	}
}
