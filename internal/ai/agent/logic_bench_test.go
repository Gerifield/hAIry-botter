package agent

import (
	"testing"
)

func BenchmarkReadPersonality(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := readPersonality()
		if err != nil {
			b.Fatal(err)
		}
	}
}
