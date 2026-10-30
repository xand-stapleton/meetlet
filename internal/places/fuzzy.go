package places

import "strings"

func cityKey(label string) string {
	name, _, _ := strings.Cut(label, ",")
	return normalize(name)
}

// matchScore prefers exact names, prefixes, substrings, and then minor typos.
// The comma suffix is a region/country qualifier, not part of the city spelling.
// A negative score means the candidate is too different to suggest.
func matchScore(query, label string) int {
	q, value := normalize(query), normalize(label)
	if q == "" || q == value {
		return 0
	}
	name, qualifier, _ := strings.Cut(q, ",")
	name, qualifier = strings.TrimSpace(name), strings.TrimSpace(qualifier)
	if qualifier == "uk" {
		qualifier = "united kingdom"
	}
	if qualifier == "us" || qualifier == "united states of america" {
		qualifier = "united states"
	}
	if qualifier != "" && !strings.Contains(value, qualifier) {
		return -1
	}
	city := cityKey(label)
	if name == city {
		return 0
	}
	if strings.HasPrefix(city, name) {
		return 10 + len([]rune(city)) - len([]rune(name))
	}
	if strings.Contains(city, name) {
		return 40 + len([]rune(city)) - len([]rune(name))
	}
	// Bound typo tolerance to two edits, so short queries do not match everything.
	length := len([]rune(name))
	if length < 4 {
		return -1
	}
	if difference := len([]rune(city)) - length; difference < -2 || difference > 2 {
		return -1
	}
	if d := editDistance(name, city); d <= min(2, length/3) {
		return 100 + d
	}
	return -1
}

// editDistance is Damerau-Levenshtein distance with adjacent transpositions:
// "harwcih" is one edit from "harwich". Names are short, so a plain matrix is
// easier to follow than a specialized fuzzy-search dependency.
func editDistance(a, b string) int {
	x, y := []rune(a), []rune(b)
	d := make([][]int, len(x)+1)
	for i := range d {
		d[i] = make([]int, len(y)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(x); i++ {
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(x)][len(y)]
}
