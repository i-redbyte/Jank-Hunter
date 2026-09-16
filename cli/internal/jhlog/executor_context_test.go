package jhlog

import "testing"

func TestExecutorARTFixturePreservesEnqueueContext(t *testing.T) {
	log, err := readLog("../../../wire/testdata/executor-context-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	var parentID, childID uint64
	for _, event := range log.Events {
		if op := event.Operation; op != nil && op.Phase == OperationPhaseStarted {
			switch ResolveSymbol(log.Dict, op.NameRef) {
			case "submit":
				parentID = op.ID
			case "execute":
				childID = op.ID
				if parentID == 0 || op.ParentID != parentID {
					t.Fatalf("child parent=%d, expected submit operation=%d", op.ParentID, parentID)
				}
			}
		}
	}
	if parentID == 0 || childID == 0 || parentID == childID {
		t.Fatalf("missing distinct operations: parent=%d child=%d", parentID, childID)
	}
	want := map[string]uint64{
		"probe.task": parentID, "probe.child": childID, "probe.worker": 0,
		"executor.wire.started.count": parentID, "executor.wire.service_ms": parentID,
	}
	seen := make(map[string]int)
	for _, event := range log.Events {
		if event.Metric == nil {
			continue
		}
		name := ResolveSymbol(log.Dict, event.Metric.MetricRef)
		operationID, relevant := want[name]
		if !relevant {
			continue
		}
		screen, owner := "SubmitScreen", "SubmitOwner"
		if name == "probe.worker" {
			screen, owner = "WorkerScreen", "WorkerOwner"
		}
		context := event.Attribution
		if !context.Present || ResolveSymbol(log.Dict, context.Screen) != screen ||
			ResolveSymbol(log.Dict, context.Owner) != owner || context.OperationID != operationID {
			t.Errorf("%s context=%+v, want %s/%s/%d", name, context, screen, owner, operationID)
		}
		seen[name]++
	}
	for name := range want {
		if seen[name] != 1 {
			t.Errorf("%s count=%d, want exactly one", name, seen[name])
		}
	}
}
