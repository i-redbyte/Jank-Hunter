package analyze

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestCausalWorkerBindingJSONKeepsUnsignedIdentityExact(t *testing.T) {
	const instanceID uint64 = 0xfedcba9876543210
	encoded, err := json.Marshal(CausalEventView{WorkerInstanceID: instanceID, CoroutineToken: instanceID,
		ObservedNS: 18_000_000_000_000_001, RelativeNS: 1,
		ParentID: instanceID, SchedulerID: instanceID, ActorID: instanceID,
		LeaseScopeID: instanceID, LeaseID: instanceID, TargetJavaID: instanceID,
		ScheduledForNS: instanceID, QueueEnteredNS: instanceID})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for field, expected := range map[string]string{
		"WorkerInstanceID": strconv.FormatUint(instanceID, 10),
		"CoroutineToken":   strconv.FormatUint(instanceID, 10),
		"ParentID":         strconv.FormatUint(instanceID, 10),
		"SchedulerID":      strconv.FormatUint(instanceID, 10),
		"ActorID":          strconv.FormatUint(instanceID, 10),
		"LeaseScopeID":     strconv.FormatUint(instanceID, 10),
		"LeaseID":          strconv.FormatUint(instanceID, 10),
		"TargetJavaID":     strconv.FormatUint(instanceID, 10),
		"ScheduledForNS":   strconv.FormatUint(instanceID, 10),
		"QueueEnteredNS":   strconv.FormatUint(instanceID, 10),
		"observed_ns":      "18000000000000001",
		"relative_ns":      "1",
	} {
		if decoded[field] != expected {
			t.Fatalf("%s lost 64-bit precision: %v", field, decoded[field])
		}
	}
	entityJSON, err := json.Marshal(CausalEntityView{Kind: 2, ID: instanceID, ParentID: instanceID,
		SchedulerID: instanceID, ActorID: instanceID, FirstNS: instanceID,
		LastNS: instanceID, ObservedSpanNS: instanceID})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(entityJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ID", "ParentID", "SchedulerID", "ActorID", "FirstNS", "LastNS", "ObservedSpanNS"} {
		if decoded[field] != strconv.FormatUint(instanceID, 10) {
			t.Fatalf("%s lost 64-bit precision: %v", field, decoded[field])
		}
	}
}

func TestCausalWorkerJoinUsesExactUnsignedLegacyTuple(t *testing.T) {
	const instanceID uint64 = 0xfedcba9876543210
	scope := strings.Repeat("0", 31) + "1/" + strings.Repeat("0", 31) + "2/" + strings.Repeat("0", 31) + "3"
	view := CausalEntityView{Kind: uint64(jhlog.CausalWorkerAttempt), Scope: scope, ID: 7,
		Events: []CausalEventView{{Phase: 1, KnownFields: jhlog.CausalHasWorkerBinding,
			WorkerInstanceID: instanceID, RunAttempt: 2, Generation: 3}}}
	causal := &CausalAnalysis{EntitiesForReview: []CausalEntityView{view}, AllEntities: []CausalEntityView{view}}
	workers := &WorkerAnalysis{Attempts: []WorkerAttempt{{Scope: strings.ReplaceAll(scope, "/", ""),
		InstanceID: strconv.FormatUint(instanceID, 10), RunAttempt: 2, Generation: 3,
		UUID: "12345678-1234-1234-1234-123456789abc", Outcome: "success", Coverage: "complete"}}}
	attachCausalWorkerAttempts(causal, workers)
	for _, linked := range []CausalEntityView{causal.EntitiesForReview[0], causal.AllEntities[0]} {
		if !linked.WorkerAttemptLinked || linked.WorkerUUID != workers.Attempts[0].UUID || linked.WorkerOutcome != "success" {
			t.Fatalf("exact unsigned worker tuple was not joined: %+v", linked)
		}
	}
	workers.Attempts = append(workers.Attempts, workers.Attempts[0])
	causal.AllEntities[0].WorkerAttemptLinked = false
	attachCausalWorkerAttempts(causal, workers)
	if causal.AllEntities[0].WorkerAttemptLinked {
		t.Fatal("ambiguous duplicate legacy tuple was joined")
	}
}

func TestCausalWorkerJoinRejectsForeignScopeAndConflictingBinding(t *testing.T) {
	scope := strings.Repeat("1", 32) + "/" + strings.Repeat("2", 32) + "/" + strings.Repeat("3", 32)
	view := CausalEntityView{Kind: uint64(jhlog.CausalWorkerAttempt), Scope: scope, ID: 7,
		Events: []CausalEventView{{Phase: 1, KnownFields: jhlog.CausalHasWorkerBinding,
			WorkerInstanceID: 8, RunAttempt: 1, Generation: 0}}}
	workers := &WorkerAnalysis{Attempts: []WorkerAttempt{{Scope: strings.Repeat("4", 96),
		InstanceID: "8", RunAttempt: 1, Generation: 0, UUID: "foreign"}}}
	causal := &CausalAnalysis{AllEntities: []CausalEntityView{view}}
	attachCausalWorkerAttempts(causal, workers)
	if causal.AllEntities[0].WorkerAttemptLinked {
		t.Fatal("foreign session worker identity was joined")
	}
	workers.Attempts[0].Scope = strings.ReplaceAll(scope, "/", "")
	view.Events = append(view.Events, CausalEventView{Phase: 2, KnownFields: jhlog.CausalHasWorkerBinding,
		WorkerInstanceID: 9, RunAttempt: 1, Generation: 0})
	causal.AllEntities[0] = view
	attachCausalWorkerAttempts(causal, workers)
	if causal.AllEntities[0].WorkerAttemptLinked {
		t.Fatal("conflicting causal binding was joined")
	}
}
