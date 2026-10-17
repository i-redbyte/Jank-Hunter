package analyze

import (
	"encoding/json"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func profileSequenceEvent(a *operationAnalysisAccumulator, dict map[uint64]string, id, parent, name uint64, kind jhlog.OperationKind, phase jhlog.OperationPhase) {
	op := jhlog.OperationEvent{ID: id, ParentID: parent, NameRef: jhlog.LocalSymbol(name), Kind: kind, Phase: phase, Outcome: jhlog.OperationOutcomeSuccess, DurationUS: 100_000}
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
}
func profileSequenceRows(t *testing.T, a *operationAnalysisAccumulator) []struct {
	Stats OperationStats `json:"stats"`
	Steps []struct {
		Operation string         `json:"operation"`
		Stats     OperationStats `json:"stats"`
	} `json:"steps"`
} {
	t.Helper()
	data, err := json.Marshal(a.finalize())
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Profiles []struct {
			Stats OperationStats `json:"stats"`
			Steps []struct {
				Operation string         `json:"operation"`
				Stats     OperationStats `json:"stats"`
			} `json:"steps"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result.Profiles
}
func TestOperationProfilesKeepOrderedStages(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "load", 3: "render"}
	for run, steps := range [][]uint64{{2, 3}, {3, 2}} {
		root := uint64(run*10 + 1)
		profileSequenceEvent(&a, dict, root, 0, 1, jhlog.OperationKindUser, jhlog.OperationPhaseStarted)
		for i, name := range steps {
			id := root + uint64(i) + 1
			profileSequenceEvent(&a, dict, id, root, name, jhlog.OperationKindStage, jhlog.OperationPhaseStarted)
			profileSequenceEvent(&a, dict, id, root, name, jhlog.OperationKindStage, jhlog.OperationPhaseFinished)
		}
		profileSequenceEvent(&a, dict, root, 0, 1, jhlog.OperationKindUser, jhlog.OperationPhaseFinished)
	}
	roots := 0
	seen := map[string]bool{}
	for _, row := range profileSequenceRows(t, &a) {
		if row.Stats.Operation == "chat.open" {
			roots++
			if len(row.Steps) != 2 || row.Stats.Count != 1 || row.Steps[0].Stats.Count != 1 || row.Steps[0].Stats.TotalMS != 100 {
				t.Fatalf("sequence lost: %+v", row)
			}
			seen[row.Steps[0].Operation] = true
		}
	}
	if roots != 2 || !seen["load"] || !seen["render"] {
		t.Fatalf("different paths merged: roots=%d seen=%v", roots, seen)
	}
}
func TestOperationProfilesKeepStagesWithoutScreenContext(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "load"}
	root := uint64(1)
	a.recordLifecycle(dict, jhlog.Event{Operation: &jhlog.OperationEvent{ID: root, NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindUser, Phase: jhlog.OperationPhaseStarted}}, "unknown", Filter{})
	profileSequenceEvent(&a, dict, 2, root, 2, jhlog.OperationKindStage, jhlog.OperationPhaseStarted)
	profileSequenceEvent(&a, dict, 2, root, 2, jhlog.OperationKindStage, jhlog.OperationPhaseFinished)
	a.recordLifecycle(dict, jhlog.Event{Operation: &jhlog.OperationEvent{ID: root, NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindUser, Phase: jhlog.OperationPhaseFinished, Outcome: jhlog.OperationOutcomeSuccess}}, "unknown", Filter{})

	rows := profileSequenceRows(t, &a)
	if len(rows) != 2 || len(rows[0].Steps) != 1 || rows[0].Steps[0].Operation != "load" {
		t.Fatalf("unknown screen discarded the scenario sequence: %+v", rows)
	}
}
func TestOperationProfilesRejectUnfinishedStageAndSequenceOverflow(t *testing.T) {
	for _, count := range []int{1, 65} {
		a := profileTestAccumulator()
		dict := map[uint64]string{1: "chat.open", 2: "load"}
		profileSequenceEvent(&a, dict, 1, 0, 1, jhlog.OperationKindUser, jhlog.OperationPhaseStarted)
		for i := 0; i < count; i++ {
			id := uint64(i + 2)
			profileSequenceEvent(&a, dict, id, 1, 2, jhlog.OperationKindStage, jhlog.OperationPhaseStarted)
			if count > 1 {
				profileSequenceEvent(&a, dict, id, 1, 2, jhlog.OperationKindStage, jhlog.OperationPhaseFinished)
			}
		}
		profileSequenceEvent(&a, dict, 1, 0, 1, jhlog.OperationKindUser, jhlog.OperationPhaseFinished)
		for _, row := range profileSequenceRows(t, &a) {
			if row.Stats.Operation == "chat.open" {
				t.Fatalf("incomplete root admitted at count=%d: %+v", count, row)
			}
		}
		_, _, invalid := decodedOperationProfiles(t, &a)
		if invalid == 0 {
			t.Fatal("missing loss accounting")
		}
	}
}

func TestOperationProfilesVerifySequenceAfterIndexHashCollision(t *testing.T) {
	a := profileTestAccumulator()
	active := activeOperation{profileValid: true, included: true, profileRun: a.header.RunID, key: operationInstanceKey{process: a.header.ProcessInstanceID, id: 1}, group: operationGroupKey{name: "chat.open", kind: "user", screen: "Chat"}, profileDigest: 42, profileSteps: []OperationProfileStep{{Operation: "load", Kind: "stage", Screen: "Chat"}}}
	finish := jhlog.OperationEvent{DurationUS: 1000, Outcome: jhlog.OperationOutcomeSuccess}
	a.recordProfile(&active, &finish, 1, nil)
	active.profileSteps = []OperationProfileStep{{Operation: "render", Kind: "stage", Screen: "Chat"}}
	a.recordProfile(&active, &finish, 1, nil)
	if len(a.profiles) != 1 || a.invalidProfileSamples != 1 {
		t.Fatalf("hash collision silently matched: profiles=%d invalid=%d", len(a.profiles), a.invalidProfileSamples)
	}
	for _, profile := range a.profiles {
		if profile.aggregate.count != 1 || profile.steps[0].Operation != "load" {
			t.Fatal("collision contaminated evidence")
		}
	}
}

func TestOperationProfilesDistinguishRootAndNestedUserOperations(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open"}
	profileSequenceEvent(&a, dict, 1, 0, 1, jhlog.OperationKindUser, jhlog.OperationPhaseStarted)
	profileSequenceEvent(&a, dict, 2, 1, 1, jhlog.OperationKindUser, jhlog.OperationPhaseStarted)
	profileSequenceEvent(&a, dict, 2, 1, 1, jhlog.OperationKindUser, jhlog.OperationPhaseFinished)
	profileSequenceEvent(&a, dict, 1, 0, 1, jhlog.OperationKindUser, jhlog.OperationPhaseFinished)
	data, err := json.Marshal(a.finalize())
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Profiles []struct {
			Root  bool           `json:"root"`
			Stats OperationStats `json:"stats"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Profiles) != 2 {
		t.Fatalf("root and nested observations merged: %s", data)
	}
	roots := 0
	for _, row := range result.Profiles {
		if row.Root {
			roots++
		}
		if row.Stats.Count != 1 {
			t.Fatalf("exposure counted twice: %+v", row)
		}
	}
	if roots != 1 {
		t.Fatalf("want one root, got %d", roots)
	}
}

func TestOperationProfilesPreserveProcessName(t *testing.T) {
	a := profileTestAccumulator()
	a.header.ProcessName = "com.example.app:worker"
	profileTestRecord(&a, 1, nil, map[uint64]string{1: "chat.open"}, true)
	rows := a.profileRows()
	if len(rows) != 1 || rows[0].ProcessName != "com.example.app:worker" {
		t.Fatalf("process identity lost: %+v", rows)
	}
}
