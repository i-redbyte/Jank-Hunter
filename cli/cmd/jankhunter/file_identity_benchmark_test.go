package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var benchmarkIdentityPaths []string

func BenchmarkFileInputIdentity(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		for _, mode := range []string{"deduplicate", "overlap"} {
			b.Run(fmt.Sprintf("%s_%d", mode, count), func(b *testing.B) {
				dir := b.TempDir()
				total := count
				if mode == "overlap" {
					total *= 2
				}
				paths := make([]string, total)
				for i := range paths {
					paths[i] = filepath.Join(dir, fmt.Sprintf("%05d.jhlog", i))
					if err := os.WriteFile(paths[i], []byte("same content"), 0600); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if mode == "overlap" {
						if err := rejectLogInputOverlap("baseline", paths[:count], "candidate", paths[count:]); err != nil {
							b.Fatal(err)
						}
					} else {
						var err error
						benchmarkIdentityPaths, err = canonicalizeFileInputs(paths, "log")
						if err != nil {
							b.Fatal(err)
						}
						if len(benchmarkIdentityPaths) != count {
							b.Fatal("files lost")
						}
					}
				}
			})
		}
	}
}
