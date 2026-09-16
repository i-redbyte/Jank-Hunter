package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

func identityTestFiles(t *testing.T, count int) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, count)
	for i := range paths {
		paths[i] = filepath.Join(dir, fmt.Sprintf("%05d.jhlog", i))
		if err := os.WriteFile(paths[i], []byte("identical content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

var identityMemorySink *fileIdentityIndex

func TestIdentityIndexDuplicateInputsDoNotPreallocateLargeHashTable(t *testing.T) {
	paths := identityTestFiles(t, 1)
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	const inputs = 100000
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			index := newFileIdentityIndex(inputs, os.SameFile)
			index.identity = func(string, os.FileInfo) (physicalFileID, bool) { return physicalFileID{volume: 1}, true }
			index.add(paths[0], info)
			index.add(paths[0], info)
			identityMemorySink = index
		}
	})
	identityMemorySink = nil
	// The existing result buffer is sized for the input. The identity map must
	// grow with distinct files, not reserve another table for every alias.
	limit := int64(inputs)*int64(unsafe.Sizeof(canonicalLogInput{})) + 65536
	if got := result.AllocedBytesPerOp(); got > limit {
		t.Fatalf("two aliases allocate %d bytes, want <=%d", got, limit)
	}
}

func TestCanonicalInputIdentityComparisonWorkIsLinear(t *testing.T) {
	paths := identityTestFiles(t, 1000)
	requireNativeIdentity(t, paths[0])
	calls := 0
	same := func(a, b os.FileInfo) bool { calls++; return os.SameFile(a, b) }
	got, err := canonicalizeFileInputsWithSameFile(paths, "log", same)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(paths) {
		t.Fatal("distinct same-content files collapsed")
	}
	if calls > 2*len(paths) {
		t.Fatalf("%d distinct files required %d pairwise comparisons; expected linear work", len(paths), calls)
	}
	t.Logf("%d files: %d SameFile fallback comparisons", len(paths), calls)
}

func TestComparisonOverlapIdentityWorkIsLinear(t *testing.T) {
	paths := identityTestFiles(t, 2000)
	requireNativeIdentity(t, paths[0])
	calls := 0
	same := func(a, b os.FileInfo) bool { calls++; return os.SameFile(a, b) }
	if err := rejectLogInputOverlapWithSameFile("baseline", paths[:1000], "candidate", paths[1000:], same); err != nil {
		t.Fatal(err)
	}
	if calls > 2*len(paths) {
		t.Fatalf("disjoint inputs required %d pairwise comparisons; expected linear work", calls)
	}
	t.Logf("%d files: %d SameFile fallback comparisons", len(paths), calls)
}

func requireNativeIdentity(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, known := nativeFileIdentity(path, info); !known {
		t.Skip("native identity unavailable; correctness covered by explicit fallback tests")
	}
}

func TestFileIdentityIndexMixedFallbackMatchesPairwiseOracle(t *testing.T) {
	paths := identityTestFiles(t, 16)
	infos := make([]os.FileInfo, len(paths))
	for i, p := range paths {
		var err error
		infos[i], err = os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
	}
	for seed := int64(0); seed < 32; seed++ {
		rng := rand.New(rand.NewSource(seed))
		index := newFileIdentityIndex(16, os.SameFile)
		calls := 0
		index.identity = func(path string, info os.FileInfo) (physicalFileID, bool) {
			calls++
			var n int
			fmt.Sscanf(filepath.Base(path), "%05d.jhlog", &n)
			id := physicalFileID{volume: 1}
			binary.LittleEndian.PutUint64(id.object[:], uint64(n))
			return id, seed%3 != 0 && (n+int(seed))%3 != 0
		}
		var expected []canonicalLogInput
		for i := 0; i < 512; i++ {
			n := rng.Intn(len(paths))
			path, info := paths[n], infos[n]
			duplicate := false
			for _, existing := range expected {
				if os.SameFile(existing.info, info) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				expected = append(expected, canonicalLogInput{path: path, info: info})
			}
			index.add(path, info)
			if len(index.files) != len(expected) {
				t.Fatalf("seed%d input%d changed deduplication", seed, i)
			}
			for j := range expected {
				if index.files[j].path != expected[j].path {
					t.Fatal("first occurrence order changed")
				}
			}
		}
		if calls != 512 || len(index.byID)+len(index.unkeyed) > 16 {
			t.Fatalf("unbounded or repeated identity work: calls%d", calls)
		}
		// Unknown lookup must also compare entries already stored under known IDs.
		index.identity = func(string, os.FileInfo) (physicalFileID, bool) { return physicalFileID{}, false }
		for n, p := range paths {
			position, _, _ := index.find(p, infos[n])
			if position < 0 || !os.SameFile(index.files[position].info, infos[n]) {
				t.Fatal("fallback omitted indexed entries")
			}
		}
	}
}

func TestFileIdentityIndexUnknownFirstCanMatchKnownLater(t *testing.T) {
	paths := identityTestFiles(t, 1)
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	index := newFileIdentityIndex(2, os.SameFile)
	calls := 0
	index.identity = func(string, os.FileInfo) (physicalFileID, bool) {
		calls++
		return physicalFileID{volume: 42}, calls > 1
	}
	index.add("first", info)
	index.add("alias", info)
	if len(index.files) != 1 || index.files[0].path != "first" {
		t.Fatal("known identity failed to check prior unknown entry")
	}
}

func TestSingletonAndEmptyIdentityIndexNeedNoNativeLookup(t *testing.T) {
	paths := identityTestFiles(t, 1)
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	index := newFileIdentityIndex(1, os.SameFile)
	index.identity = func(string, os.FileInfo) (physicalFileID, bool) {
		t.Fatal("unnecessary metadata lookup")
		return physicalFileID{}, false
	}
	if position, _, _ := index.find(paths[0], info); position >= 0 {
		t.Fatal("empty set matched a file")
	}
	index.add(paths[0], info)
}

func TestIdentityIndexPreservesErrorKindAndValidationOrder(t *testing.T) {
	paths := identityTestFiles(t, 1)
	missing := filepath.Join(t.TempDir(), "missing.jhlog")
	if _, err := canonicalizeFileInputs([]string{paths[0], missing}, "heap dump"); err == nil || !strings.Contains(err.Error(), "resolve heap dump path") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("lost input error: %v", err)
	}
	if _, err := canonicalizeFileInputs([]string{t.TempDir()}, "log"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory accepted: %v", err)
	}
	if err := rejectLogInputOverlap("baseline", []string{paths[0], missing}, "candidate", paths); err == nil || !strings.Contains(err.Error(), "stat baseline log") {
		t.Fatalf("overlap hid later baseline validation error: %v", err)
	}
	if err := rejectLogInputOverlap("baseline", nil, "candidate", []string{missing}); err == nil || !strings.Contains(err.Error(), "stat candidate log") {
		t.Fatalf("empty baseline hid candidate validation error: %v", err)
	}
}
