package places

import "testing"

var searchMatches []Location

func BenchmarkSearch(b *testing.B) {
	for _, query := range []string{"london", "harwcih", "sao paulo"} {
		b.Run(query, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				searchMatches = Search(query, 20)
			}
		})
	}
}
