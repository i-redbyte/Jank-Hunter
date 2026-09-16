package analyze

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func comparisonRunSegment(run, process int) CollectionSegment {
	id := func(value int) string {
		if value == 0 {
			return ""
		}
		return fmt.Sprintf("%032x", value)
	}
	return CollectionSegment{RunID: id(run), ProcessInstanceID: id(process), SessionID: id(run*100 + process)}
}

func TestRotationMetadataCannotCrossConfidenceEventThreshold(t *testing.T) {
	for _, tc := range []struct{ groups, events int }{{2, 79}, {5, 499}} {
		one := Summary{LogCount: tc.groups, EventCount: tc.events + tc.groups, CollectorSessions: tc.groups}
		for i := 1; i <= tc.groups; i++ {
			one.CollectionSegments = append(one.CollectionSegments, comparisonRunSegment(i, i))
		}
		many := one
		many.LogCount *= 5
		many.CollectorSessions *= 5
		many.EventCount = tc.events + many.CollectorSessions
		want := "low"
		if tc.groups == 5 {
			want = "medium"
		}
		if a, b := sampleConfidence(one, one), sampleConfidence(many, many); a != want || b != want {
			t.Fatalf("%d application observations: confidence %s/%s, want %s", tc.events, a, b, want)
		}
	}
}

func TestConfidenceGateExplainsAcquisitionUnits(t *testing.T) {
	summary := Summary{LogCount: 5, EventCount: 1000}
	result := EvaluateGate(Compare(summary, summary), ThresholdConfig{MinConfidence: "high"})
	text := fmt.Sprint(result)
	if !strings.Contains(text, "independent acquisition groups") || !strings.Contains(text, "unknown") || strings.Contains(text, "collect 5+ logs") {
		t.Fatalf("gate still recommends file replication: %s", text)
	}
}

func TestEnvironmentDeltaSampleSizeUsesAcquisitionGroups(t *testing.T) {
	summary := Summary{LogCount: 5, EventCount: 1000, Devices: []NamedValue{{Name: "device", Value: 1}}}
	for i := 0; i < 5; i++ {
		summary.CollectionSegments = append(summary.CollectionSegments, comparisonRunSegment(1, 1))
	}
	for _, delta := range Compare(summary, summary).Deltas {
		if delta.Name == "Device mix" && delta.SampleSize != 1 {
			t.Fatalf("device sample size=%d, want 1 acquisition", delta.SampleSize)
		}
	}
}

func TestComparisonConfidenceUsesIndependentAcquisitionGroups(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pairs [][2]int
		want  string
	}{
		{"one", [][2]int{{1, 1}}, "low"},
		{"five_rotations", [][2]int{{1, 1}, {1, 1}, {1, 1}, {1, 1}, {1, 1}}, "low"},
		{"five_processes_one_run", [][2]int{{1, 1}, {1, 2}, {1, 3}, {1, 4}, {1, 5}}, "low"},
		{"five_runs_same_process", [][2]int{{1, 1}, {2, 1}, {3, 1}, {4, 1}, {5, 1}}, "low"},
		{"transitive_process_bridge", [][2]int{{1, 1}, {1, 2}, {2, 2}, {2, 3}, {3, 3}}, "low"},
		{"two_groups", [][2]int{{1, 1}, {1, 2}, {2, 3}, {2, 4}, {2, 4}}, "medium"},
		{"five_distinct_groups", [][2]int{{1, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 5}}, "high"},
		{"missing_run", [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}}, "low"},
		{"missing_process", [][2]int{{1, 0}, {2, 0}, {3, 0}, {4, 0}, {5, 0}}, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary := Summary{LogCount: len(tc.pairs), EventCount: 1000}
			for _, pair := range tc.pairs {
				summary.CollectionSegments = append(summary.CollectionSegments, comparisonRunSegment(pair[0], pair[1]))
			}
			if got := confidence(summary, summary); got != tc.want {
				t.Fatalf("confidence=%s, want %s; file count is not independent replication", got, tc.want)
			}
		})
	}
}

func TestComparisonWithoutRunIdentityCannotInferIndependentRunsFromLogCount(t *testing.T) {
	summary := Summary{LogCount: 5, EventCount: 1000}
	if got := confidence(summary, summary); got != "low" {
		t.Fatalf("unknown identities received %s confidence", got)
	}
}

func TestCIGateReadinessCannotImproveFromRotation(t *testing.T) {
	one := Summary{LogCount: 1, EventCount: 1000, DurationMS: 1000, CollectionSegments: []CollectionSegment{comparisonRunSegment(1, 1)}}
	five := one
	five.LogCount = 5
	for i := 1; i < 5; i++ {
		five.CollectionSegments = append(five.CollectionSegments, comparisonRunSegment(1, 1))
	}
	before, after := ciGateReadinessScore(Compare(one, one)), ciGateReadinessScore(Compare(five, five))
	if before.Score0To10 != after.Score0To10 {
		t.Fatalf("rotation changes CI readiness: %v -> %v", before.Score0To10, after.Score0To10)
	}
}

func TestAcquisitionPartitionsAgainstConnectivityOracle(t *testing.T) {
	for seed := int64(0); seed < 256; seed++ {
		random := rand.New(rand.NewSource(seed))
		n := 1 + random.Intn(32)
		pairs := make([][2]int, n)
		segments := make([]CollectionSegment, n)
		for i := range pairs {
			pairs[i] = [2]int{1 + random.Intn(12), 1 + random.Intn(12)}
			segments[i] = comparisonRunSegment(pairs[i][0], pairs[i][1])
		}
		seen := make([]bool, n)
		want := 0
		for i := range pairs {
			if seen[i] {
				continue
			}
			want++
			seen[i] = true
			queue := []int{i}
			for len(queue) > 0 {
				node := queue[0]
				queue = queue[1:]
				for j := range pairs {
					if !seen[j] && (pairs[j][0] == pairs[node][0] || pairs[j][1] == pairs[node][1]) {
						seen[j] = true
						queue = append(queue, j)
					}
				}
			}
		}
		for attempt := 0; attempt < 4; attempt++ {
			random.Shuffle(n, func(i, j int) { segments[i], segments[j] = segments[j], segments[i] })
			evidence := buildAcquisitionEvidence(segments)
			if !evidence.IdentityComplete || evidence.IndependentGroups != want {
				t.Fatalf("seed=%d attempt=%d got=%+v want=%d", seed, attempt, evidence, want)
			}
		}
	}
}

func TestAcquisitionIdentityAndCollectionQualityCaps(t *testing.T) {
	for _, invalid := range []string{"", strings.Repeat("0", 32), strings.Repeat("z", 32), "123"} {
		for _, field := range []string{"run", "process"} {
			segments := []CollectionSegment{comparisonRunSegment(1, 1), comparisonRunSegment(2, 2)}
			if field == "run" {
				segments[1].RunID = invalid
			} else {
				segments[1].ProcessInstanceID = invalid
			}
			result := buildAcquisitionEvidence(segments)
			if result.IdentityComplete || result.IndependentGroups != 0 || result.UnknownIdentitySegments != 1 {
				t.Fatalf("invalid %s: %+v", field, result)
			}
		}
	}
	summary := Summary{EventCount: 1000, Acquisition: &AcquisitionEvidence{IndependentGroups: 5, IdentityComplete: true}}
	for _, level := range []string{"low", "medium", "high"} {
		summary.CollectionQuality.Level = level
		if got := confidence(summary, summary); got != level {
			t.Fatalf("quality cap %s lost: %s", level, got)
		}
	}
}
