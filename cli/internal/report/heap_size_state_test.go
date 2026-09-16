package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestHeapClassSizeStatesCannotLookExact(t *testing.T) {
	for _, tc := range []struct {
		state        analyze.HeapSizeState
		want, absent string
	}{
		{analyze.HeapSizeExact, "64.0 МБ", "неизвестен"},
		{analyze.HeapSizeEstimated, "≥ 64.0 МБ", "объектов в поддереве: 7"},
		{analyze.HeapSizeUnknown, "неизвестен", "64.0 МБ"},
		{"", "неизвестен", "64.0 МБ"},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			cached := newCachedReportTemplate("size", `{{template "heap-class-evidence" .}}`)
			parsed, err := cached.parsed()
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = parsed.Execute(&out, analyze.HeapLeakEvidence{ClassName: "app.Screen", RetainedSizeKB: 65536, RetainedObjectCount: 7, RetainedSizeState: tc.state})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), tc.absent) {
				t.Fatalf("size state %q presented misleadingly: %s", tc.state, out.String())
			}
		})
	}
}

func TestHeapEstimatedSizeDoesNotRoundLowerBoundUp(t *testing.T) {
	cached := newCachedReportTemplate("size", `{{template "heap-class-evidence" .}}`)
	parsed, err := cached.parsed()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, analyze.HeapLeakEvidence{ClassName: "app.Screen", RetainedSizeKB: 1, RetainedSizeBytes: 32, RetainedSizeState: analyze.HeapSizeEstimated}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "≥ 32 Б") {
		t.Fatalf("32-byte lower bound was rounded up: %s", out.String())
	}
}
