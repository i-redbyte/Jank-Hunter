package main

import (
	"bytes"
	"encoding/json"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"strings"
	"testing"
)

func TestHandlerCoverageCSVPreservesCountAndUnknownAttribution(t *testing.T) {
	var quality analyze.CollectionQuality
	if err := json.Unmarshal([]byte(`{"async_attribution":{"status":"unknown","handler_posts_without_context":7}}`), &quality); err != nil {
		t.Fatal(err)
	}
	summary := analyze.Summary{CollectionQuality: quality}
	var output bytes.Buffer
	if err := writeComparisonCSV(&output, analyze.Compare(summary, summary)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"async_attribution", "handler_posts_without_context", ",7,7,", "unknown"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("CSV lost async coverage %q: %s", text, output.String())
		}
	}
}
