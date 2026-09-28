package analyze

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type profileTestRow struct {
	RunID      string                        `json:"run_id"`
	ProcessID  string                        `json:"process_instance_id"`
	Attributes []struct{ Key, Value string } `json:"attributes"`
	Stats      OperationStats                `json:"stats"`
}

func decodedOperationProfiles(t *testing.T, a *operationAnalysisAccumulator) ([]profileTestRow, uint64, uint64) {
	t.Helper()
	data, err := json.Marshal(a.finalize())
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Profiles []profileTestRow `json:"profiles"`
		Dropped  uint64           `json:"dropped_profile_samples"`
		Invalid  uint64           `json:"invalid_profile_samples"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result.Profiles, result.Dropped, result.Invalid
}
func profileTestAccumulator() operationAnalysisAccumulator {
	a := newOperationAnalysisAccumulator()
	h := jhlog.DefaultSegmentHeader()
	h.RunID[0] = 1
	h.ProcessInstanceID[0] = 2
	a.startLog(h)
	return a
}
func profileTestRecord(a *operationAnalysisAccumulator, id uint64, attributes []jhlog.OperationAttribute, dict map[uint64]string, start bool, durationUS ...uint64) {
	op := jhlog.OperationEvent{ID: id, NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindUser, Attributes: attributes, Phase: jhlog.OperationPhaseStarted}
	if start {
		a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
	}
	op.Phase = jhlog.OperationPhaseFinished
	op.Outcome = jhlog.OperationOutcomeSuccess
	op.DurationUS = 100_000
	if len(durationUS) > 0 {
		op.DurationUS = durationUS[0]
	}
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
}
func profileAttributes(values ...uint64) []jhlog.OperationAttribute {
	result := make([]jhlog.OperationAttribute, 0, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		result = append(result, jhlog.OperationAttribute{KeyRef: jhlog.LocalSymbol(values[i]), ValueRef: jhlog.LocalSymbol(values[i+1])})
	}
	return result
}
func TestOperationProfilesPreserveJointConditionsAndRunIdentity(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "cache", 3: "cold", 4: "warm", 5: "dataset", 6: "small", 7: "large"}
	profileTestRecord(&a, 1, profileAttributes(2, 3, 5, 6), dict, true)
	profileTestRecord(&a, 2, profileAttributes(5, 6, 2, 3), dict, true)
	profileTestRecord(&a, 3, profileAttributes(2, 4, 5, 7), dict, true)
	h := a.header
	h.SegmentIndex++
	a.startLog(h)
	profileTestRecord(&a, 4, profileAttributes(2, 3, 5, 6), dict, true)
	h.RunID[0] = 3
	h.ProcessInstanceID[0] = 4
	a.startLog(h)
	profileTestRecord(&a, 5, profileAttributes(2, 3, 5, 6), dict, true)
	rows, dropped, invalid := decodedOperationProfiles(t, &a)
	if len(rows) != 3 || dropped != 0 || invalid != 0 {
		t.Fatalf("joint/run profiles lost: %+v dropped=%d invalid=%d", rows, dropped, invalid)
	}
	var counts uint64
	found := false
	for _, row := range rows {
		counts += row.Stats.Count
		if row.Stats.Count == 3 {
			found = true
		}
		if len(row.Attributes) != 2 || row.Attributes[0].Key != "cache" || row.Attributes[1].Key != "dataset" {
			t.Fatalf("noncanonical attributes: %+v", row)
		}
	}
	if counts != 5 || !found {
		t.Fatalf("rotation/order changed exposure: %+v", rows)
	}
}
func TestOperationProfilesRejectUnknownTruncatedAndDuplicateEvidence(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "cache", 3: "cold", 4: strings.Repeat("x", 129)}
	profileTestRecord(&a, 1, profileAttributes(2, 3), dict, false)
	profileTestRecord(&a, 2, profileAttributes(2, 3, 2, 3), dict, true)
	profileTestRecord(&a, 3, profileAttributes(2, 4), dict, true)
	profileTestRecord(&a, 4, profileAttributes(2, 999), dict, true)
	rows, _, invalid := decodedOperationProfiles(t, &a)
	if len(rows) != 0 || invalid != 4 {
		t.Fatalf("incomplete evidence accepted: %+v invalid=%d", rows, invalid)
	}
}
func TestOperationProfilesRetainLateSignals(t *testing.T) {
	a := profileTestAccumulator()
	profileTestRecord(&a, 1, nil, map[uint64]string{1: "chat.open"}, true)
	a.recordSignal(jhlog.Event{Stall: &jhlog.StallEvent{DurationMS: 250}}, 1)
	rows, _, _ := decodedOperationProfiles(t, &a)
	if len(rows) != 1 || rows[0].Stats.CorrelatedStalls != 1 || rows[0].Stats.CorrelatedStallMaxMS != 250 {
		t.Fatalf("late signal lost: %+v", rows)
	}
}

func TestOperationProfilesPreserveTypedResourceExposure(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open"}
	op := jhlog.OperationEvent{ID: 1, NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindUser, Phase: jhlog.OperationPhaseStarted}
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
	a.recordSignal(jhlog.Event{HTTP: &jhlog.HTTPEvent{DurationMS: 50, RxBytes: 4_000, TxBytes: 1_000}, Flags: uint64(jhlog.FlagHTTPResponseBytesKnown | jhlog.FlagHTTPRequestBytesKnown)}, 1)
	a.recordSignal(jhlog.Event{IO: &jhlog.IOEvent{DurationUS: 2_000, Bytes: 8_000}, Flags: uint64(jhlog.FlagIOBytesKnown)}, 1)
	a.recordSignal(jhlog.Event{Metric: &jhlog.MetricEvent{Value: 5_000}}, 1, "", workerMetricDeviceCPU)
	op.Phase, op.Outcome, op.DurationUS = jhlog.OperationPhaseFinished, jhlog.OperationOutcomeSuccess, 100_000
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
	rows, _, _ := decodedOperationProfiles(t, &a)
	if len(rows) != 1 {
		t.Fatalf("profiles=%+v", rows)
	}
	stats := rows[0].Stats
	if stats.CorrelatedHTTPRxBytes != 4_000 || stats.CorrelatedHTTPTxBytes != 1_000 || stats.CorrelatedHTTPBytesKnown != 2 ||
		stats.CorrelatedIOBytes != 8_000 || stats.CorrelatedIOBytesKnown != 1 ||
		stats.CorrelatedCPUSumX100 != 5_000 || stats.CorrelatedCPUSamples != 1 {
		t.Fatalf("resource exposure=%+v", stats)
	}
}

func TestOperationProfilesBoundCardinalityAndReleaseReferences(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "dataset", 3: ""}
	for i := 0; i < operationProfileLimit+7; i++ {
		dict[3] = fmt.Sprint(i)
		profileTestRecord(&a, uint64(i+1), profileAttributes(2, 3), dict, true)
	}
	rows, dropped, invalid := decodedOperationProfiles(t, &a)
	if len(rows) != operationProfileLimit || dropped != 7 || invalid != 0 {
		t.Fatalf("unbounded or silent loss: rows=%d dropped=%d invalid=%d", len(rows), dropped, invalid)
	}
	a.release()
	if a.profiles != nil || a.profileOwners != nil || a.active != nil || a.freeActive != nil || a.completedContexts.entries != nil {
		t.Fatal("release retains profile state")
	}
}
func TestOperationProfilesRejectConflictingStarts(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "cache", 3: "cold"}
	op := jhlog.OperationEvent{ID: 1, NameRef: jhlog.LocalSymbol(1), Kind: jhlog.OperationKindUser, Phase: jhlog.OperationPhaseStarted}
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
	op.Attributes = profileAttributes(2, 3)
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
	op.Attributes = nil
	op.Phase = jhlog.OperationPhaseFinished
	op.DurationUS = 1000
	a.recordLifecycle(dict, jhlog.Event{Operation: &op}, "Chat", Filter{})
	rows, _, invalid := decodedOperationProfiles(t, &a)
	if len(rows) != 0 || invalid != 1 {
		t.Fatalf("conflicting lifecycle admitted: %+v invalid=%d", rows, invalid)
	}
}
func TestProfileKeyCanonicalizationDoesNotAllocate(t *testing.T) {
	active := activeOperation{profileValid: true, attributeCount: 2, firstAttribute: operationAttributeValue{key: "dataset", value: "small"}, moreAttributes: []operationAttributeValue{{key: "cache", value: "cold"}}}
	active.profileRun[0] = 1
	active.key.process[0] = 2
	if got := testing.AllocsPerRun(1000, func() { profileKeySink, profileKeyValidSink = profileKey(&active) }); got != 0 {
		t.Fatalf("key allocated %v times", got)
	}
	if !profileKeyValidSink || profileKeySink.attributes[0].key != "cache" {
		t.Fatal("invalid canonical key")
	}
}

var profileKeySink operationProfileKey
var profileKeyValidSink bool

func TestOperationProfilesSeparateLateSignalsAfterConditionsSplit(t *testing.T) {
	a := profileTestAccumulator()
	dict := map[uint64]string{1: "chat.open", 2: "cache", 3: "cold", 4: "warm"}
	profileTestRecord(&a, 1, profileAttributes(2, 3), dict, true)
	profileTestRecord(&a, 2, profileAttributes(2, 4), dict, true)
	a.recordSignal(jhlog.Event{Stall: &jhlog.StallEvent{DurationMS: 250}}, 1)
	rows, _, _ := decodedOperationProfiles(t, &a)
	if len(rows) != 2 {
		t.Fatalf("profiles=%+v", rows)
	}
	for _, row := range rows {
		want := uint64(0)
		if row.Attributes[0].Value == "cold" {
			want = 1
		}
		if row.Stats.Count != 1 || row.Stats.CorrelatedStalls != want {
			t.Fatalf("profiles shared mutable evidence: %+v", rows)
		}
	}
	if got := a.finalize().Operations[0].CorrelatedStalls; got != 1 {
		t.Fatalf("late signal double-counted: %d", got)
	}
}

func TestOperationProfilesDetachExactAndApproximateQuantiles(t *testing.T) {
	for _, count := range []int{4, 200} {
		a := profileTestAccumulator()
		dict := map[uint64]string{1: "chat.open", 2: "cache", 3: "cold", 4: "warm"}
		for i := 0; i < count; i++ {
			profileTestRecord(&a, uint64(i+1), profileAttributes(2, 3), dict, true)
		}
		for i := 0; i < 200; i++ {
			profileTestRecord(&a, uint64(count+i+1), profileAttributes(2, 4), dict, true, 5_000_000)
		}
		rows, _, _ := decodedOperationProfiles(t, &a)
		if len(rows) != 2 {
			t.Fatal("missing profiles")
		}
		for _, row := range rows {
			if row.Attributes[0].Value == "cold" && (row.Stats.P95MS != 100 || row.Stats.Count != uint64(count)) {
				t.Fatalf("detached profile mutated: %+v", row)
			}
			if row.Attributes[0].Value == "warm" && row.Stats.P95MS != 5000 {
				t.Fatalf("new profile inherited samples: %+v", row)
			}
		}
	}
}
