package benchfixture_test

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestMetadataCountsSemanticEventsSeparatelyFromDictionaryRecords(t *testing.T) {
	profile, err := benchfixture.ProfileByName("smoke")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "smoke.jhlog")
	metadata, err := benchfixture.Write(path, profile)
	if err != nil {
		t.Fatal(err)
	}

	callbackRecords := 0
	semanticEvents := 0
	result, err := jhlog.StreamFileWithResult(path, func(event jhlog.Event, _ map[uint64]string) error {
		callbackRecords++
		if event.Type.IsSemanticData() {
			semanticEvents++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if metadata.Events != 1+profile.RuntimeCallEvents+profile.AttributedEvents+profile.SignalEvents {
		t.Fatalf("metadata events = %d, want semantic profile total", metadata.Events)
	}
	if metadata.Schema != 4 {
		t.Fatalf("metadata schema = %d, want 4", metadata.Schema)
	}
	if metadata.RuntimeCallBlocks <= 0 || metadata.RuntimeCallBlocks >= metadata.RuntimeCallEvents {
		t.Fatalf(
			"runtime call blocks = %d, want a positive batched count below %d events",
			metadata.RuntimeCallBlocks,
			metadata.RuntimeCallEvents,
		)
	}
	if semanticEvents != metadata.Events || result.Events != uint64(metadata.Events) {
		t.Fatalf("semantic events: callbacks=%d result=%d metadata=%d", semanticEvents, result.Events, metadata.Events)
	}
	if result.DataRecords != uint64(metadata.DataRecords) ||
		result.DictionaryRecords != uint64(metadata.DictionaryRecords) ||
		result.ControlRecords != uint64(metadata.ControlRecords) ||
		result.TotalRecords != uint64(metadata.TotalRecords) {
		t.Fatalf(
			"stream records = total:%d data:%d dictionary:%d control:%d, metadata=%d/%d/%d/%d",
			result.TotalRecords,
			result.DataRecords,
			result.DictionaryRecords,
			result.ControlRecords,
			metadata.TotalRecords,
			metadata.DataRecords,
			metadata.DictionaryRecords,
			metadata.ControlRecords,
		)
	}
	if metadata.DictionaryEntries != metadata.DictionaryRecords {
		t.Fatalf("dictionary entries = %d, records = %d", metadata.DictionaryEntries, metadata.DictionaryRecords)
	}
	if callbackRecords != metadata.Events+metadata.DictionaryRecords {
		t.Fatalf("callback records = %d, want %d semantic plus dictionary", callbackRecords, metadata.Events+metadata.DictionaryRecords)
	}
}

func TestRepresentativeProfileKeepsPerformanceQualityEventVolume(t *testing.T) {
	profile, err := benchfixture.ProfileByName("representative")
	if err != nil {
		t.Fatal(err)
	}
	const wantEvents = 51_142
	if got := 1 + profile.RuntimeCallEvents + profile.AttributedEvents + profile.SignalEvents; got != wantEvents {
		t.Fatalf("representative semantic events = %d, want %d", got, wantEvents)
	}
}
