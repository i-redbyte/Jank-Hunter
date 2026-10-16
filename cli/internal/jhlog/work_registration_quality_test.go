package jhlog

import (
	"path/filepath"
	"testing"
)

func TestWorkRegistrationQualityCountersPreserveDistinctUnknownCoverage(t *testing.T) {
	for id, name := range map[uint64]string{
		QualityWorkRegistrationCapacity:    "work_registration_capacity_rejected_total",
		QualityWorkRegistrationTimeout:     "work_registration_timeout_total",
		QualityWorkRegistrationClosed:      "work_registration_unfinished_at_stop_total",
		QualityWorkChainDeclarationOmitted: "work_chain_declaration_omitted_total",
		QualityWorkRequestBuilderOmitted:   "work_request_builder_omitted_total",
	} {
		if !IsKnownQualityCounter(id) || QualityCounterName(id) != name {
			t.Fatalf("unrecognized registration observation coverage %d", id)
		}
	}
}

func TestBoundedWorkDeclarationQualitySurvivesSealedLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded-work.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventQualitySnapshot, Quality: &QualitySnapshot{
		Sequence: 1,
		Counters: map[uint64]uint64{
			QualityWorkChainDeclarationOmitted: 2,
			QualityWorkRequestBuilderOmitted:   3,
		},
	}}})
	if _, err := readLog(path); err != nil {
		t.Fatalf("bounded WorkManager telemetry made the segment unreadable: %v", err)
	}
}
