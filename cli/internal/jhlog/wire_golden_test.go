package jhlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWireGolden(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "wire", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestScenarioTelemetryARTFixtureUsesOnlyOperationWireEvents(t *testing.T) {
	log, err := readLog("../../../wire/testdata/scenario-telemetry-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var rootID uint64
	stageOutcomes := map[string]OperationOutcome{}
	for _, event := range log.Events {
		operation := event.Operation
		if operation == nil {
			continue
		}
		name := ResolveSymbol(log.Dict, operation.NameRef)
		if name == "chat.open" && operation.Phase == OperationPhaseStarted {
			rootID = operation.ID
			if operation.Kind != OperationKindUser || len(operation.Attributes) != MaxOperationAttributes {
				t.Fatalf("root operation = %+v", operation)
			}
			attributes := make(map[string]string, len(operation.Attributes))
			for _, attribute := range operation.Attributes {
				attributes[ResolveSymbol(log.Dict, attribute.KeyRef)] = ResolveSymbol(log.Dict, attribute.ValueRef)
			}
			for key, want := range map[string]string{
				"jh.scenario": "chat.open", "jh.scenario_rev": "2", "account": "existing",
				"cache": "warm", "network": "wifi", "payload": "small", "source": "push", "experiment": "control",
			} {
				if attributes[key] != want {
					t.Errorf("attribute %s=%q, want %q", key, attributes[key], want)
				}
			}
		}
		if operation.Kind == OperationKindStage {
			if rootID == 0 || operation.ParentID != rootID || len(operation.Attributes) != 0 {
				t.Fatalf("stage %q = %+v, root=%d", name, operation, rootID)
			}
			if operation.Phase == OperationPhaseFinished {
				stageOutcomes[name] = operation.Outcome
			}
		}
	}
	if rootID == 0 {
		t.Fatal("scenario root is missing")
	}
	wantOutcomes := map[string]OperationOutcome{
		"load.messages":        OperationOutcomeSuccess,
		"render.messages":      OperationOutcomeFailure,
		"prefetch.attachments": OperationOutcomeCancelled,
	}
	for name, want := range wantOutcomes {
		if stageOutcomes[name] != want {
			t.Errorf("stage %q outcome=%d, want %d", name, stageOutcomes[name], want)
		}
	}
	if quality := log.Result.LatestQuality; quality != nil {
		for id, value := range quality.Counters {
			name := QualityCounterName(id)
			if value > 0 && (strings.Contains(name, "loss") || strings.Contains(name, "lost")) {
				t.Errorf("wire fixture has collection loss: %s=%d", name, value)
			}
		}
	}
}
