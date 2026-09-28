package analyze

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestComparisonSnapshotRoundTripPreservesComparisonSufficientStatistics(t *testing.T) {
	summary := snapshotTestSummary()
	snapshot, err := NewComparisonSnapshot(summary, "2026-09-22T20:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != ComparisonSnapshotSchema || snapshot.Hash == "" || len(snapshot.Capabilities) == 0 || len(snapshot.Runs) != 1 {
		t.Fatalf("snapshot envelope = %+v", snapshot)
	}
	restored, err := ValidateComparisonSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	before := Compare(summary, summary)
	after := Compare(restored, restored)
	if before.Scope.Comparability != after.Scope.Comparability || before.Outcome != after.Outcome || len(before.Deltas) != len(after.Deltas) {
		t.Fatalf("comparison contract changed: before=%+v after=%+v", before.Scope, after.Scope)
	}
	if restored.HTTPP95MS != summary.HTTPP95MS || restored.DurationMS != summary.DurationMS || restored.OperationAnalysis.Profiles[0].Stats.Count != 40 {
		t.Fatalf("sufficient statistics changed: %+v", restored)
	}
	if restored.AnalysisFilter.RouteContains != "feed" || restored.LogGrowth.CapturedAtMS != 1_750_000_000_000 ||
		len(restored.Detectors) != 1 || len(restored.Detectors[0].Thresholds) != 1 || snapshot.Provenance.GeneratedAt != "2026-09-22T20:00:00Z" {
		t.Fatalf("filter, detector config, or timestamps changed: %+v", restored)
	}
}

func TestComparisonSnapshotRemovesSensitiveEvidence(t *testing.T) {
	summary := snapshotTestSummary()
	summary.AnalysisInputs.ArtifactDirectory = "/Users/private/build/output"
	summary.Routes = []RouteStats{{Route: "GET https://api.example.test/items?token=secret"}}
	summary.DatabaseAnalysis = &DatabaseAnalysis{Statements: []DatabaseStatementStats{{Query: "SELECT password FROM accounts"}}}
	snapshot, err := NewComparisonSnapshot(summary, "2026-09-22T20:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/Users/private", "token=secret", "SELECT password", "accounts"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, payload)
		}
	}
	restored, err := ValidateComparisonSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AnalysisInputs.ArtifactDirectory != "" || len(restored.DatabaseAnalysis.Statements) != 0 {
		t.Fatalf("unsafe evidence restored: %+v", restored)
	}
	if len(restored.Routes) != 1 || restored.Routes[0].Route != "GET https://api.example.test/items" {
		t.Fatalf("route was not safely canonicalized: %+v", restored.Routes)
	}
}

func TestComparisonSnapshotExcludesLargeRuntimeGraphDetails(t *testing.T) {
	summary := snapshotTestSummary()
	summary.RuntimeCalls = []RuntimeCallStats{{
		Caller: strings.Repeat("x", comparisonSnapshotSummaryMaxBytes+1),
		Callee: "target",
		Count:  1,
	}}
	summary.Influence = InfluenceSummary{
		Available:       true,
		HasRuntimeGraph: true,
		Workspace: InfluenceGraphWorkspace{
			Nodes: []InfluenceGraphNode{{InfluenceNode: InfluenceNode{ClassName: strings.Repeat("y", 1<<20)}}},
		},
	}

	snapshot, err := NewComparisonSnapshot(summary, "2026-09-22T20:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Payload) >= 1<<20 {
		t.Fatalf("comparison snapshot retained runtime graph scale: %d bytes", len(snapshot.Payload))
	}
	restored, err := ValidateComparisonSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.RuntimeCalls) != 0 || len(restored.Influence.Workspace.Nodes) != 0 {
		t.Fatalf("runtime graph details leaked into comparison snapshot: calls=%d influence=%+v", len(restored.RuntimeCalls), restored.Influence)
	}
	if !restored.Influence.Available || !restored.Influence.HasRuntimeGraph {
		t.Fatalf("runtime graph capability metadata was lost: %+v", restored.Influence)
	}
	before := Compare(summary, summary)
	after := Compare(restored, restored)
	if before.Scope.Comparability != after.Scope.Comparability || before.Outcome != after.Outcome || len(before.Deltas) != len(after.Deltas) {
		t.Fatalf("comparison contract changed: before=%+v after=%+v", before.Scope, after.Scope)
	}
}

func TestComparisonSnapshotRejectsHashAndSchemaTampering(t *testing.T) {
	snapshot, err := NewComparisonSnapshot(snapshotTestSummary(), "2026-09-22T20:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Payload = "A" + snapshot.Payload[1:]
	if _, err := ValidateComparisonSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("tampered payload error = %v", err)
	}
	snapshot, _ = NewComparisonSnapshot(snapshotTestSummary(), "2026-09-22T20:00:00Z")
	snapshot.Schema = "jankhunter.comparison-snapshot/v999"
	if _, err := ValidateComparisonSnapshot(snapshot); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("unsupported schema error = %v", err)
	}
}

func snapshotTestSummary() Summary {
	return Summary{
		Title: "snapshot", LogCount: 1, EventCount: 120, DurationMS: 30_000,
		HTTPCount: 40, HTTPFailed: 2, HTTPP95MS: 180,
		UIFrames: 100, UIJank: 5, UIJankPct: 5,
		Acquisition:       &AcquisitionEvidence{IndependentGroups: 1, IdentityComplete: true, DistinctRunIDs: 1, DistinctProcessInstances: 1},
		CollectionQuality: CollectionQuality{Level: "high", Complete: true, ChainValid: true, ExactAdmission: true, ProcessScope: "all_processes", AllowedProcessCount: 1},
		AnalysisFilter:    &Filter{RouteContains: "feed"}, CollectorFlagsAll: ^uint64(0),
		LogGrowth: LogGrowthSummary{Available: true, CapturedAtMS: 1_750_000_000_000},
		Devices:   []NamedValue{{Name: "Pixel", Value: 1}}, SDKs: []NamedValue{{Name: "35", Value: 1}}, Network: []NamedValue{{Name: "wifi", Value: 1}},
		Processes: []NamedValue{{Name: "com.example.app", Value: 1}}, AppVersions: []NamedValue{{Name: "1", Value: 1}}, Cohorts: []NamedValue{{Name: "run", Value: 1}},
		Detectors: []DetectorMetadata{{ID: "ui.jank", Version: "1", Thresholds: []DetectorThreshold{{Name: "rate", Value: 5, Unit: "%"}}}},
		OperationAnalysis: &OperationAnalysis{Profiles: []OperationProfile{{
			Root: true, RunID: "01000000000000000000000000000000", ProcessInstanceID: "02000000000000000000000000000000", ProcessName: "com.example.app",
			Attributes: []OperationProfileAttribute{{Key: "jh.scenario", Value: "feed.open"}, {Key: "jh.scenario_rev", Value: "1"}},
			Steps:      []OperationProfileStep{{Operation: "load", Kind: "stage", Screen: "Feed"}},
			Stats:      OperationStats{Operation: "feed.open", Kind: "user", Screen: "Feed", Count: 40, Success: 40, TotalMS: 4_000},
		}}},
	}
}
