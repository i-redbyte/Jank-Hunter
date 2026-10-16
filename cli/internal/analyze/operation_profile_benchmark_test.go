package analyze

import (
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func BenchmarkInspectOperationProfiles(b *testing.B) { benchmarkInspectOperationProfiles(b, 0) }
func BenchmarkInspectOperationProfilesWithStages(b *testing.B) {
	benchmarkInspectOperationProfiles(b, 2)
}
func benchmarkInspectOperationProfiles(b *testing.B, stages int) {
	const count = 100_000
	path := filepath.Join(b.TempDir(), "profiles.jhlog")
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = 3
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		b.Fatal(err)
	}
	for _, entry := range []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOperation, ID: 1, Value: "chat.open"},
		{Kind: jhlog.DictAttributeKey, ID: 2, Value: "cache"},
		{Kind: jhlog.DictAttributeValue, ID: 3, Value: "cold"},
		{Kind: jhlog.DictScreen, ID: 4, Value: "Chat"},
	} {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			b.Fatal(err)
		}
	}
	attrs := []jhlog.OperationAttribute{{KeyRef: jhlog.LocalSymbol(2), ValueRef: jhlog.LocalSymbol(3)}}
	for id := uint64(1); id <= count; id++ {
		op := jhlog.OperationEvent{ID: id * uint64(stages+1), NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindUser, Phase: jhlog.OperationPhaseStarted, Attributes: attrs}
		event := jhlog.Event{Type: jhlog.EventOperation, TimeMS: id * 100, Operation: &op, Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(4)}}
		if err := writer.WriteEvent(event); err != nil {
			b.Fatal(err)
		}
		for stage := 0; stage < stages; stage++ {
			child := jhlog.OperationEvent{ID: op.ID + uint64(stage) + 1, ParentID: op.ID, NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindStage, Phase: jhlog.OperationPhaseStarted}
			childEvent := jhlog.Event{Type: jhlog.EventOperation, TimeMS: event.TimeMS + uint64(stage)*10, Operation: &child, Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(4)}}
			if err := writer.WriteEvent(childEvent); err != nil {
				b.Fatal(err)
			}
			child.Phase = jhlog.OperationPhaseFinished
			child.Outcome = jhlog.OperationOutcomeSuccess
			child.DurationUS = 5_000
			childEvent.TimeMS += 5
			if err := writer.WriteEvent(childEvent); err != nil {
				b.Fatal(err)
			}
		}
		op.Phase = jhlog.OperationPhaseFinished
		op.Outcome = jhlog.OperationOutcomeSuccess
		op.DurationUS = 50_000
		event.TimeMS += 50
		if err := writer.WriteEvent(event); err != nil {
			b.Fatal(err)
		}
	}
	if err := closer.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		summary, err := InspectFilesWithOptions("profiles", []string{path}, Options{})
		if err != nil {
			b.Fatal(err)
		}
		if summary.OperationAnalysis == nil || summary.OperationAnalysis.Completed != count*uint64(stages+1) {
			b.Fatal("operation observations lost")
		}
		benchmarkOperationAnalysisSink = summary.OperationAnalysis
	}
}
