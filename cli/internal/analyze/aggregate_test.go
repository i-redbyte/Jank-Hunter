package analyze

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var benchmarkUint64Percentile uint64

func BenchmarkUint64SampleSetRepeatedMillion(b *testing.B) {
	for range b.N {
		var set uint64SampleSet
		for index := range 1_000_000 {
			set.add(uint64(index % 1_500))
		}
		benchmarkUint64Percentile = set.percentile(0.95)
	}
}

func BenchmarkUint64SampleSetUniqueMillion(b *testing.B) {
	for range b.N {
		var set uint64SampleSet
		for index := range 1_000_000 {
			set.add(uint64(index))
		}
		benchmarkUint64Percentile = set.percentile(0.95)
	}
}

func TestUint64SampleSetCompactsRepeatedValuesWithoutApproximation(t *testing.T) {
	var set uint64SampleSet
	for index := range 100_000 {
		set.add(uint64(index % 1_500))
	}
	if len(set.values) != 0 || len(set.denseCounts) != 1_500 {
		t.Fatalf("repeated exact set was not compacted: values=%d dense=%d", len(set.values), len(set.denseCounts))
	}
	if got := set.percentile(0.50); got != 746 {
		t.Fatalf("p50 = %d, want 746", got)
	}
	if got := set.percentile(0.95); got != 1_424 {
		t.Fatalf("p95 = %d, want 1424", got)
	}
	if set.seen != 100_000 || set.max != 1_499 {
		t.Fatalf("exact cardinality/max changed: seen=%d max=%d", set.seen, set.max)
	}
}

func TestUint64SampleSetKeepsSparseUniqueValuesExact(t *testing.T) {
	var set uint64SampleSet
	for index := range 20_025 {
		set.add(uint64(index) * 1_000_003)
	}
	if len(set.denseCounts) != 0 || len(set.values) != set.seen {
		t.Fatalf("sparse unique set used a lossy representation: values=%d dense=%d seen=%d", len(set.values), len(set.denseCounts), set.seen)
	}
	if got, want := set.percentile(0.95), uint64(19_023)*1_000_003; got != want {
		t.Fatalf("p95 = %d, want %d", got, want)
	}
}

func TestUint64SampleSetPreservesOutliersAfterDensePromotion(t *testing.T) {
	var set uint64SampleSet
	for index := range 10_000 {
		set.add(uint64(index % 100))
	}
	set.add(1_000_000)
	set.add(2)
	if got := set.percentile(1); got != 1_000_000 {
		t.Fatalf("max percentile lost exact outlier: %d", got)
	}
	if set.seen != 10_002 || set.max != 1_000_000 {
		t.Fatalf("outlier changed exact counters: seen=%d max=%d", set.seen, set.max)
	}
}

func TestUint64SampleSetMatchesSortedReferenceAfterDensePromotion(t *testing.T) {
	random := rand.New(rand.NewSource(32_597))
	for scenario := range 48 {
		values := make([]uint64, 0, 6_144)
		var set uint64SampleSet
		for index := 0; index < cap(values); index++ {
			value := uint64(random.Intn(257))
			if index >= 4_096 && index%13 == 0 {
				value = uint64(random.Int63()) + uint64(scenario)
			}
			values = append(values, value)
			set.add(value)
		}
		if len(set.denseCounts) == 0 || len(set.values) != 0 {
			t.Fatalf("scenario %d did not exercise exact dense promotion", scenario)
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		for _, percentile := range []float64{-1, 0, 0.01, 0.50, 0.90, 0.95, 0.99, 1, 2} {
			got := set.percentile(percentile)
			target := int(math.Ceil(float64(len(values)) * percentile))
			if target < 1 {
				target = 1
			}
			if target > len(values) {
				target = len(values)
			}
			want := values[target-1]
			if got != want {
				t.Fatalf("scenario %d percentile %v = %d, want %d", scenario, percentile, got, want)
			}
		}
		if set.seen != len(values) || set.min != values[0] || set.max != values[len(values)-1] {
			t.Fatalf(
				"scenario %d metadata = seen:%d min:%d max:%d, want seen:%d min:%d max:%d",
				scenario,
				set.seen,
				set.min,
				set.max,
				len(values),
				values[0],
				values[len(values)-1],
			)
		}
	}
}

func TestInspectSampleIncludesFPSAndGauges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	log := readJhlogForTest(t, path)

	summary := inspectLogsForTest("sample", []jhlog.Log{log})
	if summary.UIAvgFPS <= 0 {
		t.Fatalf("UIAvgFPS = %.2f, want > 0", summary.UIAvgFPS)
	}
	if summary.UIMinFPS <= 0 {
		t.Fatalf("UIMinFPS = %.2f, want > 0", summary.UIMinFPS)
	}
	if len(summary.Gauges) == 0 {
		t.Fatalf("expected gauges")
	}
	if summary.HTTPCount != 3 {
		t.Fatalf("HTTPCount = %d, want 3", summary.HTTPCount)
	}
	if len(summary.SignalContexts) == 0 {
		t.Fatalf("expected operation attribution")
	}
	if len(summary.LogSpam) == 0 {
		t.Fatalf("expected log spam attribution")
	}
	if len(summary.ProblemWindows) == 0 {
		t.Fatalf("expected problem windows")
	}
	if len(summary.RuntimeCalls) == 0 {
		t.Fatalf("expected runtime call graph")
	}
	if !summary.Influence.HasRuntimeGraph || len(summary.Influence.TopEdges) == 0 {
		t.Fatalf("expected influence runtime edges: %+v", summary.Influence)
	}
	if len(summary.CodeProblems) == 0 {
		t.Fatalf("expected code problem registry")
	}
	if summary.CodeProblems[0].Score <= 0 {
		t.Fatalf("top code problem has no score: %+v", summary.CodeProblems[0])
	}
	if len(summary.CodeProblems[0].Signals) == 0 {
		t.Fatalf("top code problem has no signals: %+v", summary.CodeProblems[0])
	}
	if !codeProblemsHaveSignal(summary.CodeProblems, "Сигнал удержания памяти") {
		t.Fatalf("expected memory leak signal in code problem registry: %+v", summary.CodeProblems)
	}
}

func TestFPSRequiresContinuousFrameEvidence(t *testing.T) {
	tests := []struct {
		name   string
		window *jhlog.UIWindowEvent
		want   bool
	}{
		{
			name: "single frame across idle screen",
			window: &jhlog.UIWindowEvent{
				WindowMS: 90_000, FrameCount: 1, P95MS: 12, FrameDeadlineUS: 16_667,
			},
			want: false,
		},
		{
			name: "many sparse frames",
			window: &jhlog.UIWindowEvent{
				WindowMS: 90_000, FrameCount: 30, P95MS: 16, FrameDeadlineUS: 16_667,
			},
			want: false,
		},
		{
			name: "continuous frames",
			window: &jhlog.UIWindowEvent{
				WindowMS: 1_000, FrameCount: 60, P95MS: 16, FrameDeadlineUS: 16_667,
			},
			want: true,
		},
		{
			name: "genuinely slow continuous frames",
			window: &jhlog.UIWindowEvent{
				WindowMS: 6_000, FrameCount: 30, P95MS: 300, FrameDeadlineUS: 16_667,
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := fpsWindowReliable(test.window); got != test.want {
				t.Fatalf("fpsWindowReliable(%+v) = %v, want %v", test.window, got, test.want)
			}
		})
	}
}

func TestReadArtifactMetadataNamespaceReadsCompactIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact-metadata.json")
	const namespaceHex = "00112233445566778899aabbccddeeff"
	data := `{"format":1,"kind":"artifact-metadata","symbolNamespace":"` + namespaceHex + `","networkWholeApplication":true}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	namespace, err := ReadArtifactMetadataNamespace(path)
	if err != nil {
		t.Fatalf("ReadArtifactMetadataNamespace() error = %v", err)
	}
	if got := hex.EncodeToString(namespace); got != namespaceHex {
		t.Fatalf("namespace = %q, want %q", got, namespaceHex)
	}

	if err := os.WriteFile(path, []byte(data+"\n{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArtifactMetadataNamespace(path); err == nil {
		t.Fatal("ReadArtifactMetadataNamespace() succeeded with a trailing record")
	}
}

func TestInspectFilesStreamsSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	if summary.EventCount == 0 || summary.HTTPCount != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	counters := namedValuesByName(summary.Counters)
	for _, name := range []string{
		"com.app.feed.FeedRepository.refresh",
		"com.app.checkout.CheckoutPresenter.render",
		"com.app.checkout.CheckoutRepository.load",
	} {
		if counters[name].Value != 1 {
			t.Fatalf("embedded stable counter %q was not resolved: %+v", name, summary.Counters)
		}
	}
	for _, counter := range summary.Counters {
		if strings.HasPrefix(counter.Name, "stable:") {
			t.Fatalf("self-contained sample leaked unresolved stable ID: %+v", counter)
		}
	}
	if len(summary.RuntimeCalls) != 1 ||
		summary.RuntimeCalls[0].Caller != "com.app.checkout.CheckoutButton.onClick" ||
		summary.RuntimeCalls[0].Callee != "com.app.checkout.CheckoutRepository.load" {
		t.Fatalf("embedded runtime edge was not resolved: %+v", summary.RuntimeCalls)
	}
	var firstEventMS uint64
	var lastEventMS uint64
	streamResult, err := jhlog.StreamFileWithResult(path, func(event jhlog.Event, _ map[uint64]string) error {
		if !event.Type.IsSemanticData() {
			return nil
		}
		if firstEventMS == 0 || event.TimeMS < firstEventMS {
			firstEventMS = event.TimeMS
		}
		if event.TimeMS > lastEventMS {
			lastEventMS = event.TimeMS
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamFileWithResult() error = %v", err)
	}
	if uint64(summary.EventCount) != streamResult.Events || summary.DataRecordCount != streamResult.DataRecords {
		t.Fatalf("semantic accounting summary=%+v stream=%+v", summary, streamResult)
	}
	if summary.TotalRecordCount != summary.DataRecordCount+summary.DictionaryRecords+summary.ControlRecords {
		t.Fatalf("summary record classes do not add up: %+v", summary)
	}
	if summary.DurationMS != lastEventMS-firstEventMS {
		t.Fatalf("duration=%d, semantic event range=%d..%d", summary.DurationMS, firstEventMS, lastEventMS)
	}
	if !summary.CollectionQuality.Complete || summary.CollectionQuality.Level != "high" || summary.CollectionQuality.UnsealedSegments != 0 {
		t.Fatalf("sample collection quality = %+v", summary.CollectionQuality)
	}
	if summary.Dictionary == 0 {
		t.Fatalf("expected dictionary count")
	}
	if len(summary.Processes) != 1 || summary.Processes[0].Name != "main" {
		t.Fatalf("unexpected processes: %+v", summary.Processes)
	}
	retainedClasses := namedValuesByName(summary.RetainedClasses)
	if len(retainedClasses) != 4 || retainedClasses["com.app.checkout.CheckoutActivity"].Value != 2 || retainedClasses["com.app.checkout.CheckoutCacheEntry"].Value != 3 {
		t.Fatalf("unexpected retained classes: %+v", summary.RetainedClasses)
	}
	retainedBuckets := namedValuesByName(summary.RetainedAgeBuckets)
	if len(retainedBuckets) != 2 || retainedBuckets["10s-30s"].Value != 5 || retainedBuckets["30s-60s"].Value != 2 {
		t.Fatalf("unexpected retained age buckets: %+v", summary.RetainedAgeBuckets)
	}
	if len(summary.MemoryLeaks) != 4 {
		t.Fatalf("unexpected memory leak suspects: %+v", summary.MemoryLeaks)
	}
	leak, ok := memoryLeakByClass(summary.MemoryLeaks, "com.app.checkout.CheckoutActivity")
	if !ok {
		t.Fatalf("CheckoutActivity leak missing: %+v", summary.MemoryLeaks)
	}
	if leak.ClassName != "com.app.checkout.CheckoutActivity" || leak.Holder != "CheckoutPresenter.render" {
		t.Fatalf("unexpected memory leak attribution: %+v", leak)
	}
	if leak.Screen != "CheckoutScreen" || leak.Operation != "" {
		t.Fatalf("unexpected memory leak context: %+v", leak)
	}
	if leak.EstimatedRetainedKB == 0 || leak.RetainedSizeConfidence == "" {
		t.Fatalf("expected retained size estimate: %+v", leak)
	}
	if len(leak.DominatorPath) < 2 || leak.DominatorTreeConfidence == "" {
		t.Fatalf("expected retained dominator path: %+v", leak)
	}
	if leak.LeakChainConfidence == "" || leak.LeakChainSummary == "" || len(leak.LeakChainActions) == 0 {
		t.Fatalf("expected retained leak chain guidance: %+v", leak)
	}
	if len(summary.AppVersions) != 1 || summary.AppVersions[0].Name != "0.1.0-debug" {
		t.Fatalf("unexpected app versions: %+v", summary.AppVersions)
	}
	if len(summary.SDKs) != 1 || summary.SDKs[0].Name != "api-35" {
		t.Fatalf("unexpected SDKs: %+v", summary.SDKs)
	}
	if summary.Environment.Title != "Pixel 8 / API 35" {
		t.Fatalf("unexpected environment title: %+v", summary.Environment)
	}
	if !summary.DeviceRootKnown || summary.DeviceRooted {
		t.Fatalf("unexpected root state: known=%v rooted=%v", summary.DeviceRootKnown, summary.DeviceRooted)
	}
	if !environmentHasItem(summary.Environment, "Рут-доступ", "нет") {
		t.Fatalf("root state is missing from environment: %+v", summary.Environment)
	}
	if !environmentHasItem(summary.Environment, "Батарея", "82%") ||
		!environmentHasItem(summary.Environment, "Сеть", "wifi") ||
		!environmentHasItem(summary.Environment, "Свободная RAM", "1.9 ГБ") ||
		!environmentHasItem(summary.Environment, "Свободное хранилище", "45.8 ГБ") {
		t.Fatalf("environment labels should be Russian: %+v", summary.Environment)
	}
	if !environmentItemDetailContains(summary.Environment, "Сеть", "валидирована да") ||
		!environmentItemDetailContains(summary.Environment, "Android", "патч безопасности") {
		t.Fatalf("environment details should be Russian: %+v", summary.Environment)
	}
	if summary.TotalMemoryKB == 0 || summary.FreeStorageKB == 0 {
		t.Fatalf("expected memory/storage context: %+v", summary)
	}
	if len(summary.Cohorts) == 0 {
		t.Fatalf("expected cohorts")
	}
}

func TestValidateSegmentChainsRejectsDuplicatesAndIdentityChanges(t *testing.T) {
	header := collectionTestHeader(7, 0)
	sealed := func(source string, value jhlog.SegmentHeader) jhlog.StreamResult {
		return jhlog.StreamResult{
			Source: source,
			Header: value,
			Status: jhlog.SegmentStatusClosedClean,
			Sealed: true,
		}
	}

	if _, err := validateSegmentChains([]jhlog.StreamResult{
		sealed("first.jhlog", header),
		sealed("duplicate.jhlog", header),
	}); err == nil || !strings.Contains(err.Error(), "duplicate segment index") {
		t.Fatalf("duplicate segment error = %v", err)
	}

	changed := collectionTestHeader(7, 1)
	changed.ProcessName = "remote"
	if _, err := validateSegmentChains([]jhlog.StreamResult{
		sealed("first.jhlog", header),
		sealed("changed.jhlog", changed),
	}); err == nil || !strings.Contains(err.Error(), "changes process_name") {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

func TestValidateSegmentChainsReportsGaps(t *testing.T) {
	first := collectionTestHeader(9, 0)
	third := collectionTestHeader(9, 2)
	issues, err := validateSegmentChains([]jhlog.StreamResult{
		{Source: "first.jhlog", Header: first, Status: jhlog.SegmentStatusClosedClean, Sealed: true},
		{Source: "third.jhlog", Header: third, Status: jhlog.SegmentStatusClosedClean, Sealed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !warningsContain(issues, "разрыв segment chain: 0 → 2") {
		t.Fatalf("chain issues = %+v", issues)
	}
}

func TestValidateSegmentChainsAcceptsExactRotationHandoff(t *testing.T) {
	first := collectionTestHeader(10, 0)
	second := collectionTestHeader(10, 1)
	digest := bytes.Repeat([]byte{0x5a}, 32)
	second.PreviousSegmentDigest = append([]byte(nil), digest...)
	issues, err := validateSegmentChains([]jhlog.StreamResult{
		{
			Source:        "first.jhlog",
			Header:        first,
			Status:        jhlog.SegmentStatusClosedClean,
			Sealed:        true,
			SegmentDigest: digest,
			SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndRotation},
		},
		{
			Source:        "second.jhlog",
			Header:        second,
			Status:        jhlog.SegmentStatusClosedClean,
			Sealed:        true,
			SegmentDigest: bytes.Repeat([]byte{0x6b}, 32),
			SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("valid rotation chain issues = %+v", issues)
	}
}

func TestValidateSegmentChainsRejectsValidCRCSubstitutionByDigest(t *testing.T) {
	first := collectionTestHeader(20, 0)
	second := collectionTestHeader(20, 1)
	firstDigest := bytes.Repeat([]byte{0x11}, 32)
	second.PreviousSegmentDigest = bytes.Repeat([]byte{0x22}, 32)
	_, err := validateSegmentChains([]jhlog.StreamResult{
		{
			Source: "first.jhlog", Header: first, Sealed: true,
			SegmentDigest: firstDigest, SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndRotation},
		},
		{
			Source: "substitute.jhlog", Header: second, Sealed: true,
			SegmentDigest: bytes.Repeat([]byte{0x33}, 32), SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "predecessor digest does not match") {
		t.Fatalf("substitution error = %v", err)
	}
}

func TestValidateSegmentChainsRequiresRotationSuccessor(t *testing.T) {
	header := collectionTestHeader(11, 0)
	issues, err := validateSegmentChains([]jhlog.StreamResult{{
		Source:     "orphaned-rotation.jhlog",
		Header:     header,
		Status:     jhlog.SegmentStatusClosedClean,
		Sealed:     true,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndRotation},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !warningsContain(issues, "ожидаемый следующий сегмент не передан") {
		t.Fatalf("orphaned rotation issues = %+v", issues)
	}
}

func TestCollectionQualityRequiresExactAdmissionFeature(t *testing.T) {
	collector := newCollector("best effort", 1, Options{})
	header := collectionTestHeader(14, 0)
	header.RequiredFeatures &^= jhlog.FeatureExactEventAdmission
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
	collector.addStreamResult(jhlog.StreamResult{
		Source:        "best-effort.jhlog",
		Header:        header,
		Status:        jhlog.SegmentStatusClosedClean,
		Sealed:        true,
		LatestQuality: &quality,
		SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	if err := collector.validateSegmentIdentityConsistency(); err != nil {
		t.Fatal(err)
	}
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.ExactAdmission || got.Complete || got.Level != "medium" {
		t.Fatalf("best-effort collection quality = %+v", got)
	}
	if !warningsContain(got.Reasons, "без EXACT admission") {
		t.Fatalf("best-effort reasons = %+v", got.Reasons)
	}
}

func TestCollectionQualityTreatsExactBackpressureAsNoLoss(t *testing.T) {
	collector := newCollector("exact", 1, Options{})
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
		jhlog.QualityAcceptedEventTotal:             2,
		jhlog.QualityWrittenEventTotal:              2,
		jhlog.QualityWriterAdmissionContentionTotal: 7,
		jhlog.QualityWriterBackpressureCount:        1,
		jhlog.QualityWriterBackpressureNanos:        250_000,
	}}
	collector.addStreamResult(jhlog.StreamResult{
		Source:        "exact.jhlog",
		Header:        collectionTestHeader(15, 0),
		Status:        jhlog.SegmentStatusClosedClean,
		Sealed:        true,
		LatestQuality: &quality,
		SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
		DataRecords:   2,
	})
	collector.summary.EventCount = 2
	if err := collector.validateSegmentIdentityConsistency(); err != nil {
		t.Fatal(err)
	}
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if !got.ExactAdmission || !got.Complete || got.Level != "high" || got.KnownLostEvents != 0 {
		t.Fatalf("exact collection quality = %+v", got)
	}
	if got.WriterBackpressureCount != 1 || got.WriterBackpressureNanos != 250_000 {
		t.Fatalf("exact backpressure = %+v", got)
	}
}

func TestCollectionQualityFailsClosedOnSuppressedRuntimeHookFailure(t *testing.T) {
	collector := newCollector("hook failure", 1, Options{})
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
		jhlog.QualityRuntimeHookFailureTotal: 3,
	}}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "hook-failure.jhlog",
		Header: collectionTestHeader(27, 0), Status: jhlog.SegmentStatusClosedClean,
		Sealed: true, LatestQuality: &quality,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.Level != "low" || got.Complete || got.RuntimeHookFailures != 3 ||
		got.CriticalRuntimeHookFailures != 3 ||
		!warningsContain(got.Reasons, "влияющих на evidence") {
		t.Fatalf("suppressed hook failure quality = %+v", got)
	}
}

func TestCollectionQualityExplainsJankStatsFallbackWithoutClaimingEvidenceCorruption(t *testing.T) {
	collector := newCollector("jankstats fallback", 1, Options{})
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
		jhlog.QualityRuntimeHookFailureTotal:    1,
		jhlog.QualityJankStatsDependencyMissing: 1,
	}}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "jankstats-fallback.jhlog", Header: collectionTestHeader(28, 0),
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &quality,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.Level != "high" || got.Complete || got.DiagnosticCompletenessPercent != 100 ||
		got.CriticalRuntimeHookFailures != 0 || len(got.RuntimeHookFailureDetails) != 1 ||
		!warningsContain(got.Notices, "Choreographer fallback") {
		t.Fatalf("jankstats fallback quality = %+v", got)
	}
}

func TestQualityWarningsDescribeExactAdmissionContentionAsLosslessBackpressure(t *testing.T) {
	counters := map[uint64]uint64{jhlog.QualityWriterAdmissionContentionTotal: 2}
	if warnings := qualityCounterWarnings(counters, true); len(warnings) != 0 {
		t.Fatalf("EXACT contention warnings = %+v", warnings)
	}
	warnings := qualityCounterWarnings(counters, false)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "BEST_EFFORT") {
		t.Fatalf("BEST_EFFORT contention warnings = %+v", warnings)
	}
}

func TestQualityWarningsExplainPreparedStatementEvidenceLoss(t *testing.T) {
	warnings := qualityCounterWarnings(map[uint64]uint64{
		jhlog.QualityPreparedStatementRegistryEviction: 3,
		jhlog.QualityPreparedStatementResolutionMiss:   2,
	}, true)
	if len(warnings) != 2 || !warningsContain(warnings, "SQL-шаблон") {
		t.Fatalf("prepared statement warnings = %+v", warnings)
	}
}

func TestQualityWarningsExplainReceiverAsyncEvidenceLoss(t *testing.T) {
	warnings := qualityCounterWarnings(map[uint64]uint64{
		jhlog.QualityReceiverAsyncRegistryEviction: 2,
		jhlog.QualityReceiverAsyncResolutionMiss:   1,
	}, true)
	if len(warnings) != 2 || !warningsContain(warnings, "PendingResult.finish") {
		t.Fatalf("receiver async warnings = %+v", warnings)
	}
}

func TestAnalysisInputCompletenessSeparatesRuntimeOnlyFromCompleteDeveloperEvidence(t *testing.T) {
	runtimeOnlyCollector := &collector{stableSymbols: stableSymbolResolver{unresolved: map[string]struct{}{}}}
	runtimeOnly := runtimeOnlyCollector.analysisInputCompleteness(Summary{
		LogCount:        1,
		DataRecordCount: 10,
	})
	missing := strings.Join(runtimeOnly.Missing, ",")
	if runtimeOnly.Status != "runtime_only" || runtimeOnly.Complete ||
		!strings.Contains(missing, "class-graph.jsonl") ||
		!strings.Contains(missing, "instrumentation-diagnostics.jsonl") {
		t.Fatalf("runtime-only completeness = %+v", runtimeOnly)
	}

	completeCollector := &collector{
		diagnostics: &InstrumentationDiagnostics{Available: true, ClassCount: 1},
		stableSymbols: stableSymbolResolver{
			unresolved: map[string]struct{}{},
		},
		artifactDirectory: "/project/app/build/generated/jankhunter/debug",
		artifactAuto:      true,
		artifactNamespace: make([]byte, symbolNamespaceBytes),
	}
	complete := completeCollector.analysisInputCompleteness(Summary{
		LogCount:        1,
		DataRecordCount: 10,
		Influence:       InfluenceSummary{HasClassGraph: true},
	})
	if !complete.Complete || complete.Status != "complete" || len(complete.Missing) != 0 ||
		!complete.ArtifactsAutoDiscovered || !complete.ArtifactIdentityVerified || complete.ArtifactDirectory == "" {
		t.Fatalf("complete developer inputs = %+v", complete)
	}
}

func TestArtifactNamespaceRejectsAnotherBuildVariant(t *testing.T) {
	header := jhlog.SegmentHeader{SymbolNamespace: bytes.Repeat([]byte{0x01}, symbolNamespaceBytes)}
	wrongNamespace := bytes.Repeat([]byte{0xff}, symbolNamespaceBytes)

	err := validateArtifactNamespace(wrongNamespace, header, "session.jhlog", "/project/wrong-variant")
	if err == nil || !strings.Contains(err.Error(), "does not match") || !strings.Contains(err.Error(), "exact --artifacts-dir") {
		t.Fatalf("namespace mismatch error = %v", err)
	}
}

func TestCollectionQualityReportsConfiguredProcessScope(t *testing.T) {
	tests := []struct {
		name            string
		scope           jhlog.ProcessScope
		allowed         uint64
		fingerprint     []byte
		want            string
		allProcesses    bool
		wantScopeNotice bool
	}{
		{name: "all", scope: jhlog.ProcessScopeAll, want: "all_processes", allProcesses: true},
		{name: "main", scope: jhlog.ProcessScopeMainOnly, want: "main_process_only", wantScopeNotice: true},
		{
			name: "allowlist", scope: jhlog.ProcessScopeAllowlist, allowed: 2,
			fingerprint: bytes.Repeat([]byte{0xa5}, 32), want: "process_allowlist", wantScopeNotice: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector := newCollector(test.name, 1, Options{})
			header := collectionTestHeader(16, 0)
			header.ProcessScope = test.scope
			header.AllowedProcessCount = test.allowed
			header.ProcessScopeFingerprint = test.fingerprint
			quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
			collector.addStreamResult(jhlog.StreamResult{
				Source: "scope.jhlog", Header: header,
				Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &quality,
				SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
			})
			collector.finalizeCollectionQuality()

			got := collector.summary.CollectionQuality
			if got.ProcessScope != test.want || got.AllProcessesConfigured != test.allProcesses ||
				!got.ProcessScopeConsistent || !got.Complete || got.Level != "high" {
				t.Fatalf("process scope quality = %+v", got)
			}
			if got.ProcessScopeFingerprint != hex.EncodeToString(test.fingerprint) {
				t.Fatalf("process scope fingerprint = %q", got.ProcessScopeFingerprint)
			}
			if test.wantScopeNotice != (len(got.Notices) > 0) {
				t.Fatalf("process scope notices = %+v", got.Notices)
			}
		})
	}
}

func TestCollectionQualityRequiresCompleteDeclaredProcessRoster(t *testing.T) {
	expectedNames := []string{"main", "remote"}
	fingerprint := jhlog.ProcessRosterFingerprint(expectedNames)
	stream := func(session byte, processName string) jhlog.StreamResult {
		header := collectionTestHeader(session, 0)
		header.ProcessName = processName
		header.ExpectedProcessCount = 2
		header.ExpectedProcessFingerprint = append([]byte(nil), fingerprint...)
		header.ProcessRosterDeclarationComplete = true
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
		return jhlog.StreamResult{
			Source: processName + ".jhlog", Header: header,
			Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &quality,
			SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
		}
	}

	incomplete := newCollector("incomplete roster", 1, Options{})
	incomplete.addStreamResult(stream(21, "main"))
	incomplete.finalizeCollectionQuality()
	if got := incomplete.summary.CollectionQuality; got.Complete || got.ProcessRosterComplete ||
		got.ObservedProcessCount != 1 || got.ExpectedProcessCount != 2 ||
		got.Level != "high" || warningsContain(got.Reasons, "process roster неполон") ||
		!warningsContain(got.Notices, "могли не запускаться") {
		t.Fatalf("incomplete roster quality = %+v", got)
	}

	complete := newCollector("complete roster", 2, Options{})
	complete.addStreamResult(stream(22, "main"))
	complete.addStreamResult(stream(23, "remote"))
	complete.finalizeCollectionQuality()
	if got := complete.summary.CollectionQuality; !got.Complete || !got.ProcessRosterComplete ||
		got.ObservedProcessCount != 2 || !got.RunCohortConsistent || got.RunCohortCount != 1 ||
		got.ExpectedProcessFingerprint != hex.EncodeToString(fingerprint) {
		t.Fatalf("complete roster quality = %+v", got)
	}

	mixed := newCollector("mixed run roster", 2, Options{})
	main := stream(25, "main")
	remote := stream(26, "remote")
	remote.Header.RunID[1] = 9
	mixed.addStreamResult(main)
	mixed.addStreamResult(remote)
	mixed.finalizeCollectionQuality()
	if got := mixed.summary.CollectionQuality; got.Complete || got.ProcessRosterComplete ||
		got.RunCohortConsistent || got.RunCohortCount != 2 ||
		!warningsContain(got.Reasons, "разным запускам приложения") {
		t.Fatalf("mixed run roster quality = %+v", got)
	}
}

func TestCollectionQualityFailsClosedWhenManifestRosterDiscoveryFailed(t *testing.T) {
	collector := newCollector("unknown roster", 1, Options{})
	header := collectionTestHeader(24, 0)
	header.ProcessRosterDeclarationComplete = false
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "main.jhlog", Header: header,
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &quality,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	collector.finalizeCollectionQuality()
	if got := collector.summary.CollectionQuality; got.Complete || got.ProcessRosterDeclarationComplete ||
		!warningsContain(got.Reasons, "не смог полностью объявить process roster") {
		t.Fatalf("failed roster discovery quality = %+v", got)
	}
}

func TestCollectionQualityFailsClosedWithoutProcessScopeFeature(t *testing.T) {
	collector := newCollector("missing scope", 1, Options{})
	header := collectionTestHeader(19, 0)
	header.RequiredFeatures &^= jhlog.FeatureProcessScope
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "missing-scope.jhlog", Header: header,
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &quality,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.Level != "low" || got.Complete || got.ProcessScope != "unknown" ||
		got.ProcessScopeConsistent || !warningsContain(got.Reasons, "охват процессов подтвердить невозможно") {
		t.Fatalf("missing process scope quality = %+v", got)
	}
}

func TestCollectionQualityFailsClosedOnImpossibleCounters(t *testing.T) {
	tests := []struct {
		name     string
		counters map[uint64]uint64
		decoded  uint64
		reason   string
	}{
		{
			name: "written exceeds accepted",
			counters: map[uint64]uint64{
				jhlog.QualityAcceptedEventTotal: 10,
				jhlog.QualityWrittenEventTotal:  11,
			},
			reason: "невозможное состояние writer",
		},
		{
			name: "graph output exceeds input",
			counters: map[uint64]uint64{
				jhlog.QualityRuntimeGraphInputTotal:   10,
				jhlog.QualityRuntimeGraphEmittedTotal: 11,
			},
			decoded: 11,
			reason:  "невозможное состояние runtime-графа",
		},
		{
			name: "graph emitted counter misses decoded calls",
			counters: map[uint64]uint64{
				jhlog.QualityRuntimeGraphInputTotal: 5,
			},
			decoded: 5,
			reason:  "writer сообщает 0 записанных runtime-вызовов, но декодировано 5",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector := newCollector(test.name, 1, Options{})
			quality := jhlog.QualitySnapshot{Sequence: 1, Counters: test.counters}
			collector.addStreamResult(jhlog.StreamResult{
				Source: "invalid.jhlog",
				Header: collectionTestHeader(17, 0), Status: jhlog.SegmentStatusClosedClean,
				Sealed: true, LatestQuality: &quality,
				RuntimeGraphLogicalCalls: test.decoded,
				SegmentEnd:               &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
			})
			collector.finalizeCollectionQuality()

			got := collector.summary.CollectionQuality
			if got.Level != "low" || got.Complete || !warningsContain(got.Reasons, test.reason) {
				t.Fatalf("fail-closed quality = %+v", got)
			}
		})
	}
}

func TestCollectionQualityRejectsRegressedCrossSegmentSnapshot(t *testing.T) {
	collector := newCollector("regressed", 2, Options{})
	first := jhlog.QualitySnapshot{Sequence: 4, CapturedElapsedUS: 2_000, Counters: map[uint64]uint64{
		jhlog.QualityAcceptedEventTotal: 10,
	}}
	second := jhlog.QualitySnapshot{Sequence: 5, CapturedElapsedUS: 3_000, Counters: map[uint64]uint64{
		jhlog.QualityAcceptedEventTotal: 9,
	}}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "first.jhlog", Header: collectionTestHeader(18, 0),
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &first,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndRotation},
	})
	collector.addStreamResult(jhlog.StreamResult{
		Source: "second.jhlog", Header: collectionTestHeader(18, 1),
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &second,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.Level != "low" || got.Complete || got.QualityProgressionValid || got.ChainValid ||
		!warningsContain(got.Reasons, "counter 1 regressed from 10 to 9") {
		t.Fatalf("regressed quality = %+v", got)
	}
}

func TestCollectionQualityTreatsSizeLimitAsIncompleteWithApparentSuccessor(t *testing.T) {
	collector := newCollector("size limit", 2, Options{})
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
	collector.addStreamResult(jhlog.StreamResult{
		Source:        "size-limited.jhlog",
		Header:        collectionTestHeader(12, 0),
		Status:        jhlog.SegmentStatusClosedClean,
		Sealed:        true,
		LatestQuality: &quality,
		SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndSizeLimit},
	})
	collector.addStreamResult(jhlog.StreamResult{
		Source:        "apparent-successor.jhlog",
		Header:        collectionTestHeader(12, 1),
		Status:        jhlog.SegmentStatusClosedClean,
		Sealed:        true,
		LatestQuality: &quality,
		SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndNormal},
	})
	if err := collector.validateSegmentIdentityConsistency(); err != nil {
		t.Fatal(err)
	}
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.Level != "low" || got.Complete || got.ChainValid {
		t.Fatalf("size-limited quality = %+v", got)
	}
	reasons := strings.Join(got.Reasons, "\n")
	if !strings.Contains(reasons, "достиг лимита размера") ||
		!strings.Contains(reasons, "сбор завершён раньше") ||
		!strings.Contains(reasons, "вместо rotation") {
		t.Fatalf("size-limited reasons = %q", reasons)
	}
}

func TestCollectionQualityCapsConfidenceForUnsealedAndLossyStreams(t *testing.T) {
	t.Run("live clean snapshot", func(t *testing.T) {
		collector := newCollector("live", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
		collector.addStreamResult(jhlog.StreamResult{
			Source:        "active.jhlog",
			Header:        collectionTestHeader(9, 0),
			Status:        jhlog.SegmentStatusOpenClean,
			TailBytes:     0,
			LatestQuality: &quality,
		})
		if err := collector.validateSegmentIdentityConsistency(); err != nil {
			t.Fatal(err)
		}
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.Level != "high" || got.Complete || got.UnsealedSegments != 1 ||
			!warningsContain(got.Notices, "снимок активной сессии") || len(got.Reasons) != 0 {
			t.Fatalf("live snapshot quality = %+v", got)
		}
	})

	t.Run("unsealed", func(t *testing.T) {
		collector := newCollector("unsealed", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
		collector.addStreamResult(jhlog.StreamResult{
			Source:        "open.jhlog",
			Header:        collectionTestHeader(10, 0),
			Status:        jhlog.SegmentStatusOpenWithTail,
			TailBytes:     17,
			LatestQuality: &quality,
		})
		if err := collector.validateSegmentIdentityConsistency(); err != nil {
			t.Fatal(err)
		}
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.Level != "low" || got.Complete || got.UnsealedSegments != 1 || !warningsContain(got.Reasons, "не запечатан") {
			t.Fatalf("unsealed quality = %+v", got)
		}
	})

	t.Run("lossy", func(t *testing.T) {
		collector := newCollector("lossy", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
			jhlog.QualityAcceptedEventTotal:             1_000,
			jhlog.QualityWrittenEventTotal:              900,
			jhlog.QualityQueueFullTotal:                 10,
			jhlog.QualityWriterAdmissionContentionTotal: 7,
		}}
		header := collectionTestHeader(11, 0)
		header.RequiredFeatures &^= jhlog.FeatureExactEventAdmission
		collector.addStreamResult(jhlog.StreamResult{
			Source:        "lossy.jhlog",
			Header:        header,
			Status:        jhlog.SegmentStatusClosedClean,
			Sealed:        true,
			LatestQuality: &quality,
		})
		if err := collector.validateSegmentIdentityConsistency(); err != nil {
			t.Fatal(err)
		}
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.Level != "low" || got.KnownLostEvents != 117 ||
			!warningsContain(got.Reasons, "потерю как минимум 117 событий") ||
			warningsContain(got.Reasons, "служебный канал") {
			t.Fatalf("lossy quality = %+v", got)
		}
	})

	t.Run("runtime graph completeness", func(t *testing.T) {
		collector := newCollector("runtime graph", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
			jhlog.QualityRuntimeGraphInputTotal:          1_000,
			jhlog.QualityRuntimeGraphEmittedTotal:        980,
			jhlog.QualityRuntimeGraphWriterRejectionLoss: 20,
		}}
		collector.addStreamResult(jhlog.StreamResult{
			Source: "graph.jhlog",
			Header: collectionTestHeader(13, 0), Status: jhlog.SegmentStatusClosedClean,
			Sealed: true, LatestQuality: &quality, RuntimeGraphLogicalCalls: 980,
		})
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.RuntimeGraphCompletenessRatio != 0.98 || got.Level != "low" || got.DiagnosticCompletenessPercent != 99.6 ||
			got.BoundedEvidenceLoss != 20 || got.OtherEvidenceLoss != 0 ||
			!warningsContain(got.Reasons, "полнота runtime-графа 98.00%") {
			t.Fatalf("runtime graph completeness = %+v", got)
		}
	})

	t.Run("other evidence loss is isolated from graph coverage", func(t *testing.T) {
		collector := newCollector("other evidence loss", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
			jhlog.QualityMetricCardinalityLoss: 1,
		}}
		collector.addStreamResult(jhlog.StreamResult{
			Source: "metric-loss.jhlog",
			Header: collectionTestHeader(15, 0), Status: jhlog.SegmentStatusClosedClean,
			Sealed: true, LatestQuality: &quality,
		})
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.RuntimeGraphCompletenessRatio != 1 || got.OtherEvidenceLoss != 1 ||
			got.DiagnosticCompletenessPercent != 80 || got.DiagnosticCompletenessComponents[3].MissingPoints != 20 {
			t.Fatalf("other evidence loss quality = %+v", got)
		}
	})

	t.Run("runtime graph disabled", func(t *testing.T) {
		collector := newCollector("runtime graph disabled", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
			jhlog.QualityRuntimeGraphDisabled: 1,
		}}
		collector.addStreamResult(jhlog.StreamResult{
			Source: "graph-disabled.jhlog",
			Header: collectionTestHeader(14, 0), Status: jhlog.SegmentStatusClosedClean,
			Sealed: true, LatestQuality: &quality,
		})
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.RuntimeGraphEnabled || got.RuntimeGraphCompletenessRatio != 0 || got.DiagnosticCompletenessPercent != 100 ||
			got.Level != "high" || !got.Complete || warningsContain(got.Reasons, "runtime-граф отключён") ||
			!warningsContain(got.Notices, "полностью исключён") {
			t.Fatalf("disabled runtime graph quality = %+v", got)
		}
		if len(got.DiagnosticCompletenessComponents) != 4 || got.DiagnosticCompletenessComponents[1].ID != "runtime_graph" ||
			!got.DiagnosticCompletenessComponents[1].Excluded || got.DiagnosticCompletenessComponents[1].MissingPoints != 0 {
			t.Fatalf("disabled runtime graph diagnostic completeness = %+v", got.DiagnosticCompletenessComponents)
		}
	})
}

func TestDiagnosticCompletenessUsesFiveExplicitTiers(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{score: 100, want: "excellent"},
		{score: 95, want: "excellent"},
		{score: 94.99, want: "high"},
		{score: 85, want: "high"},
		{score: 84.99, want: "sufficient"},
		{score: 65, want: "sufficient"},
		{score: 64.99, want: "limited"},
		{score: 40, want: "limited"},
		{score: 39.99, want: "low"},
	}
	for _, test := range cases {
		got, explanation := describeDiagnosticCompleteness(test.score, nil)
		if got != test.want || explanation == "" {
			t.Fatalf("score %.2f = %q (%q), want %q", test.score, got, explanation, test.want)
		}
	}
}

func TestDiagnosticCompletenessExplainsKnownLossWithoutInternalCounters(t *testing.T) {
	quality := CollectionQuality{
		ExactAdmission:                   true,
		WrittenEvents:                    800,
		KnownLostEvents:                  200,
		RuntimeGraphEnabled:              true,
		RuntimeGraphCompletenessRatio:    1,
		ProcessRosterComplete:            true,
		ExpectedProcessCount:             1,
		ProcessScope:                     jhlog.ProcessScopeAll.String(),
		ChainValid:                       true,
		CounterInvariantsValid:           true,
		QualityProgressionValid:          true,
		ProcessRosterDeclarationComplete: true,
		RunCohortConsistent:              true,
		ProcessScopeConsistent:           true,
	}
	score, components := collectionDiagnosticCompleteness(quality)
	if score != 92 || len(components) != 4 {
		t.Fatalf("diagnostic completeness = %.2f components=%+v", score, components)
	}
	transport := components[0]
	if transport.ID != "transport" || transport.CoveragePercent != 80 ||
		transport.EarnedPoints != 32 || transport.MissingPoints != 8 ||
		!strings.Contains(strings.ToLower(transport.Explanation), "часть событий журнала недоступна") ||
		strings.Contains(transport.Explanation, "200") {
		t.Fatalf("transport completeness component = %+v", transport)
	}
}

func TestDiagnosticCompletenessDoesNotDoubleCountRuntimeGraphLoss(t *testing.T) {
	quality := CollectionQuality{
		ExactAdmission:                true,
		WrittenEvents:                 3,
		RuntimeGraphEnabled:           true,
		RuntimeGraphInputEvents:       10,
		DecodedRuntimeGraphCalls:      2,
		RuntimeGraphCompletenessRatio: 0.2,
		ProcessRosterComplete:         true,
		ExpectedProcessCount:          1,
		ProcessScope:                  jhlog.ProcessScopeAll.String(),
		ChainValid:                    true,
		CounterInvariantsValid:        true,
		QualityProgressionValid:       true,
		BoundedEvidenceLoss:           8,
	}

	score, components := collectionDiagnosticCompleteness(quality)
	if score != 84 || components[1].EarnedPoints != 4 || components[3].EarnedPoints != 20 {
		t.Fatalf("runtime graph loss must affect only graph component: score=%v components=%+v", score, components)
	}
}

func collectionTestHeader(session byte, segmentIndex uint64) jhlog.SegmentHeader {
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = session
	header.SegmentIndex = segmentIndex
	if segmentIndex > 0 {
		header.PreviousSegmentDigest = make([]byte, 32)
	}
	header.OSPID = 42
	header.CollectorStartElapsedUS = 1_000
	header.SegmentStartElapsedUS = 1_000 + segmentIndex*100
	header.SegmentStartUnixMS = 2_000 + segmentIndex*100
	header.IdentitySource = 1
	header.ProcessName = "main"
	header.ExpectedProcessCount = 1
	header.ExpectedProcessFingerprint = jhlog.ProcessRosterFingerprint([]string{"main"})
	header.ProcessRosterDeclarationComplete = true
	header.SymbolNamespace = []byte{3}
	return header
}

func TestInspectFilesFiltersRetainedObjectsByClass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	matching, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{ClassContains: "CheckoutActivity"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest(class match) error = %v", err)
	}
	if len(matching.MemoryLeaks) != 1 || matching.MemoryLeaks[0].ClassName != "com.app.checkout.CheckoutActivity" {
		t.Fatalf("expected checkout leak with class filter: %+v", matching.MemoryLeaks)
	}

	nonMatching, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{ClassContains: "FeedActivity"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest(class miss) error = %v", err)
	}
	if len(nonMatching.MemoryLeaks) != 0 || nonMatching.Retained != 0 {
		t.Fatalf("expected retained objects to be filtered by class: leaks=%+v retained=%d", nonMatching.MemoryLeaks, nonMatching.Retained)
	}

	ownerOnly, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{OwnerContains: "CheckoutActivity"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest(owner class name) error = %v", err)
	}
	if ownerOnly.Retained != 0 {
		t.Fatalf("owner filter should not match retained class names: retained=%d leaks=%+v", ownerOnly.Retained, ownerOnly.MemoryLeaks)
	}
}

func TestInspectDurationIgnoresInitialAndroidUptimeDelta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uptime-offset.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	const uptimeOffsetMS = 12 * 60 * 60 * 1000
	events := []jhlog.Event{
		{
			Type:   jhlog.EventDictionary,
			TimeMS: uptimeOffsetMS,
			Dictionary: &jhlog.DictionaryEntry{
				Kind:  jhlog.DictRoute,
				ID:    1,
				Value: "GET /feed",
			},
		},
		{
			Type:   jhlog.EventSession,
			TimeMS: uptimeOffsetMS + 1,
			Session: &jhlog.SessionEvent{
				SDKInt: 35,
			},
		},
		{
			Type:   jhlog.EventHTTP,
			TimeMS: uptimeOffsetMS + 120_000,
			HTTP: &jhlog.HTTPEvent{
				RouteRef:   jhlog.LocalSymbol(1),
				DurationMS: 120,
				Status:     jhlog.Status2xx,
			},
		},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	// Dictionary records are transport metadata, so the observed duration
	// starts at the first semantic session record rather than the dictionary.
	if summary.DurationMS != 119_999 {
		t.Fatalf("DurationMS = %d, want 119999", summary.DurationMS)
	}
}

func TestInspectMultipleLogsSumsPerLogDuration(t *testing.T) {
	summary := inspectLogsForTest("sample", []jhlog.Log{
		{Events: []jhlog.Event{
			{Type: jhlog.EventSession, TimeMS: 0, Session: &jhlog.SessionEvent{}},
			{Type: jhlog.EventHTTP, TimeMS: 120_000, HTTP: &jhlog.HTTPEvent{DurationMS: 100, Status: jhlog.Status2xx}},
		}},
		{Events: []jhlog.Event{
			{Type: jhlog.EventSession, TimeMS: 0, Session: &jhlog.SessionEvent{}},
			{Type: jhlog.EventHTTP, TimeMS: 120_000, HTTP: &jhlog.HTTPEvent{DurationMS: 200, Status: jhlog.Status2xx}},
		}},
	})

	if summary.DurationMS != 240_000 {
		t.Fatalf("DurationMS = %d, want 240000", summary.DurationMS)
	}
	if len(summary.Warnings) == 0 {
		t.Fatalf("expected multi-log duration warning")
	}
}

func TestInspectTrafficUsesPerLogDelta(t *testing.T) {
	summary := inspectLogsForTest("sample", []jhlog.Log{
		{Events: []jhlog.Event{
			{Type: jhlog.EventContext, TimeMS: 0, Context: &jhlog.ContextEvent{RxBytes: 1_000, TxBytes: 2_000}},
			{Type: jhlog.EventContext, TimeMS: 1_000, Context: &jhlog.ContextEvent{RxBytes: 1_250, TxBytes: 2_300}},
		}},
		{Events: []jhlog.Event{
			{Type: jhlog.EventContext, TimeMS: 0, Context: &jhlog.ContextEvent{RxBytes: 10_000, TxBytes: 20_000}},
			{Type: jhlog.EventContext, TimeMS: 1_000, Context: &jhlog.ContextEvent{RxBytes: 10_400, TxBytes: 20_500}},
		}},
	})

	if summary.TrafficRxMax != 650 || summary.TrafficTxMax != 800 {
		t.Fatalf("traffic deltas = rx %d tx %d, want rx 650 tx 800", summary.TrafficRxMax, summary.TrafficTxMax)
	}
}

func TestInspectHTTPP95UsesNearestRankForSmallSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-p95.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictRoute, ID: 1, Value: "GET /feed"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictScreen, ID: 2, Value: "FeedScreen"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 3, Value: "FeedRepository.refresh"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 4, Value: "feed.refresh"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 5, Value: "network"}},
		{Type: jhlog.EventHTTP, TimeMS: 2, Attribution: attributionForTest(2, 3, 4, 5), HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 100, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 3, Attribution: attributionForTest(2, 3, 4, 5), HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 1000, Status: jhlog.Status2xx}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	if summary.HTTPP95MS != 1000 {
		t.Fatalf("HTTPP95MS = %d, want 1000", summary.HTTPP95MS)
	}
	if len(summary.Routes) != 1 || summary.Routes[0].P95MS != 1000 {
		t.Fatalf("route p95 = %+v, want 1000", summary.Routes)
	}
	if len(summary.SignalContexts) != 1 || summary.SignalContexts[0].HTTPP95MS != 1000 {
		t.Fatalf("context p95 = %+v, want 1000", summary.SignalContexts)
	}
}

func TestInspectBuildsAdvancedNetworkAnalysisByCallsiteAndContext(t *testing.T) {
	dict := map[uint64]string{
		1: "POST /checkout",
		2: "payments",
		3: "CheckoutScreen",
		4: "CheckoutViewModel.submit",
		5: "checkout.pay",
		6: "authorize",
	}
	const initiatorID = 0x32621
	baseHTTP := jhlog.HTTPEvent{
		RouteRef:     jhlog.LocalSymbol(1),
		ServiceRef:   jhlog.LocalSymbol(2),
		InitiatorRef: jhlog.StableSymbol(initiatorID),
		Attempts:     1,
		DNSAttempts:  1,
		Protocol:     jhlog.HTTPProtocol2,
	}
	first := baseHTTP
	first.DurationMS = 500
	first.QueueMS = 100
	first.DNSMS = 20
	first.ConnectMS = 50
	first.TLSMS = 30
	first.RequestMS = 10
	first.TTFBMS = 250
	first.ResponseMS = 40
	first.RxBytes = 4_096
	first.TxBytes = 512
	first.StatusCode = 503
	first.Attempts = 3
	first.ConnectAttempts = 2
	first.TLSAttempts = 1
	first.ConnectFailures = 1
	first.Redirects = 1

	second := baseHTTP
	second.DurationMS = 400
	second.QueueMS = 50
	second.RequestMS = 10
	second.TTFBMS = 300
	second.ResponseMS = 40
	second.RxBytes = 1_024
	second.StatusCode = 200

	failed := baseHTTP
	failed.InitiatorRef = jhlog.StableSymbol(initiatorID + 1)
	failed.DurationMS = 50
	failed.QueueMS = 50
	failed.StatusCode = 0
	failed.FailurePhase = jhlog.HTTPFailurePhaseCancelled
	failed.FailureKind = jhlog.HTTPFailureKindCancelled
	failed.DNSAttempts = 0

	summary := inspectLogsForTest("advanced network", []jhlog.Log{{Dict: dict, Events: []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStableSymbol, ID: initiatorID, Value: "CheckoutRepository.authorize"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStableSymbol, ID: initiatorID + 1, Value: "CheckoutRepository.cancel"}},
		{Type: jhlog.EventSession, TimeMS: 0, Session: &jhlog.SessionEvent{}},
		{Type: jhlog.EventHTTP, TimeMS: 800, Flags: uint64(jhlog.FlagHTTPFailed | jhlog.FlagHTTPCancelled), Attribution: attributionForTest(3, 4, 5, 6), HTTP: &failed},
		{Type: jhlog.EventHTTP, TimeMS: 1_000, Flags: uint64(jhlog.FlagHTTPRequestBytesKnown | jhlog.FlagHTTPResponseBytesKnown), Attribution: attributionForTest(3, 4, 5, 6), HTTP: &first},
		{Type: jhlog.EventHTTP, TimeMS: 1_100, Flags: uint64(jhlog.FlagHTTPCacheHit | jhlog.FlagHTTPReusedConnection | jhlog.FlagHTTPResponseBytesKnown), Attribution: attributionForTest(3, 4, 5, 6), HTTP: &second},
	}}})

	if got := summary.NetworkAnalysis.MaxConcurrency; got != 3 {
		t.Fatalf("max concurrency = %d, want 3", got)
	}
	if summary.NetworkAnalysis.PeakConcurrencyAtMS != 750 ||
		summary.NetworkAnalysis.TransportFailures != 1 ||
		summary.NetworkAnalysis.HTTP5xx != 1 ||
		summary.NetworkAnalysis.Canceled != 1 ||
		summary.NetworkAnalysis.CacheHits != 1 ||
		summary.NetworkAnalysis.ReusedConnections != 1 ||
		summary.NetworkAnalysis.Retries != 1 ||
		summary.NetworkAnalysis.Redirects != 1 ||
		summary.NetworkAnalysis.ConnectFailures != 1 {
		t.Fatalf("network summary = %+v", summary.NetworkAnalysis)
	}
	if len(summary.NetworkAnalysis.Calls) != 2 {
		t.Fatalf("network calls = %+v", summary.NetworkAnalysis.Calls)
	}
	call := summary.NetworkAnalysis.Calls[0]
	if call.Route != "POST /checkout" || call.Service != "payments" ||
		call.Initiator != "CheckoutRepository.authorize" || call.Screen != "CheckoutScreen" ||
		call.Operation != "unknown" ||
		call.Owner != "CheckoutViewModel.submit" || call.Count != 2 || call.P95MS != 500 ||
		call.Failures != 1 || call.HTTP5xx != 1 || call.CacheHits != 1 ||
		call.Retries != 1 || call.Redirects != 1 {
		t.Fatalf("primary call group = %+v", call)
	}
	assertNamedValue(t, summary.NetworkAnalysis.StatusCodes, "200", 1)
	assertNamedValue(t, summary.NetworkAnalysis.StatusCodes, "503", 1)
	assertNamedValue(t, summary.NetworkAnalysis.FailurePhases, "cancelled", 1)
	assertNamedValue(t, summary.NetworkAnalysis.FailureKinds, "cancelled", 1)
	assertNamedValue(t, summary.NetworkAnalysis.Protocols, "http/2", 3)
	if len(summary.Routes) != 1 {
		t.Fatalf("routes = %+v", summary.Routes)
	}
	route := summary.Routes[0]
	if route.ContextCount != 2 || route.MaxConcurrency != 3 || route.HTTP5xx != 1 ||
		route.TransportFailures != 1 || route.Retries != 1 || route.ConnectFailures != 1 ||
		len(route.Phases) != 7 || route.Phases[0].Name != "queue" || route.Phases[0].P95MS != 100 {
		t.Fatalf("advanced route = %+v", route)
	}
}

func TestInspectBuildsTypedWebSocketAnalysisByRouteAndContext(t *testing.T) {
	dict := map[uint64]string{1: "GET /socket", 2: "ChatScreen", 3: "RealtimeRepository", 4: "chat.open"}
	summary := inspectLogsForTest("websocket", []jhlog.Log{{Dict: dict, Events: []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 0, Session: &jhlog.SessionEvent{}},
		{Type: jhlog.EventWebSocket, TimeMS: 75, Attribution: attributionForTest(2, 3, 4, 0), WebSocket: &jhlog.WebSocketEvent{
			RouteRef: jhlog.LocalSymbol(1), ConnectionID: 10, Stage: jhlog.WebSocketStageOpened,
			DurationMS: 75, StatusCode: 101,
		}},
		{Type: jhlog.EventWebSocket, TimeMS: 12_075, Attribution: attributionForTest(2, 3, 4, 0), WebSocket: &jhlog.WebSocketEvent{
			RouteRef: jhlog.LocalSymbol(1), ConnectionID: 10, Stage: jhlog.WebSocketStageFailed,
			DurationMS: 12_000, StatusCode: 101, FailureKind: jhlog.WebSocketFailureTimeout,
			TextMessages: 7, BinaryMessages: 3, ReceivedBytes: 4_096,
		}},
		{Type: jhlog.EventWebSocket, TimeMS: 12_150, Attribution: attributionForTest(2, 3, 4, 0), WebSocket: &jhlog.WebSocketEvent{
			RouteRef: jhlog.LocalSymbol(1), ConnectionID: 11, Stage: jhlog.WebSocketStageOpened,
			DurationMS: 75, StatusCode: 101, ReconnectOrdinal: 1,
		}},
	}}})

	analysis := summary.WebSocketAnalysis
	if analysis == nil || analysis.Opened != 2 || analysis.Failures != 1 || analysis.ActiveAtEnd != 1 ||
		analysis.Reconnects != 1 || analysis.ConnectP95MS != 75 || analysis.LifetimeP95MS != 12_000 ||
		analysis.TextMessages != 7 || analysis.BinaryMessages != 3 || analysis.ReceivedBytes != 4_096 {
		t.Fatalf("WebSocket analysis = %+v", analysis)
	}
	if len(analysis.Connections) != 1 {
		t.Fatalf("WebSocket rows = %+v", analysis.Connections)
	}
	row := analysis.Connections[0]
	if row.Route != "GET /socket" || row.Screen != "ChatScreen" || row.Owner != "RealtimeRepository" ||
		row.Opened != 2 || row.Failures != 1 || row.ActiveAtEnd != 1 {
		t.Fatalf("WebSocket row = %+v", row)
	}
	assertNamedValue(t, analysis.FailureKinds, "timeout", 1)
}

func TestInspectBuildsTypedDatabaseAnalysis(t *testing.T) {
	dict := map[uint64]string{
		1: "SELECT * FROM messages WHERE id = ?", 2: "MessagesScreen", 3: "MessagesRepository",
	}
	const sourceID = 0x32621
	events := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStableSymbol, ID: sourceID, Value: "MessagesDao.load"}},
		{Type: jhlog.EventSession, TimeMS: 0, Session: &jhlog.SessionEvent{}},
	}
	for index := 0; index < 25; index++ {
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeMS: 100 + uint64(index*20), Flags: uint64(jhlog.FlagThreadMain),
			Attribution: attributionForTest(2, 3, 0, 0),
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.StableSymbol(sourceID),
				Framework: jhlog.DatabaseFrameworkRoom, Operation: jhlog.DatabaseOperationQuery,
				Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 25_000,
				StatementFingerprint: 0x1234,
			},
		})
	}
	summary := inspectLogsForTest("database", []jhlog.Log{{Dict: dict, Events: events}})

	analysis := summary.DatabaseAnalysis
	if analysis == nil || analysis.Overall.Calls != 25 || analysis.Main.Calls != 25 || analysis.Background.Calls != 0 ||
		analysis.Overall.P95DurationUS != 25_000 || analysis.Main.P95DurationUS != 25_000 ||
		analysis.PeakCallsPerSecond != 25 || analysis.RapidRepeats != 24 ||
		analysis.KnownSQLCalls != 25 {
		t.Fatalf("database analysis = %+v", analysis)
	}
	if len(analysis.Statements) != 1 {
		t.Fatalf("database statements = %+v", analysis.Statements)
	}
	row := analysis.Statements[0]
	if row.Query != "SELECT * FROM messages WHERE id = ?" || row.Operation != "чтение" ||
		row.OperationCode != "query" || row.StatementFingerprint != 0x1234 ||
		row.Overall.Calls != 25 || row.Main.Calls != 25 || row.Background.Calls != 0 ||
		row.RapidRepeats != 24 || len(row.Contexts) != 1 || row.Contexts[0].Source != "MessagesDao.load" ||
		row.Contexts[0].Framework != "Room" {
		t.Fatalf("database row = %+v", row)
	}
}

func TestDatabaseAnalysisGroupsCanonicalStatementAndKeepsExecutionContexts(t *testing.T) {
	dict := map[uint64]string{
		1: "SELECT * FROM messages WHERE id = ?", 2: "Chat", 3: "ChatRepository",
		4: "MessagesDao.load", 5: "chat.open", 6: "Search", 7: "SearchRepository",
		8: "SearchDao.load", 9: "search.run",
	}
	var processID jhlog.ID128
	processID[0] = 0x11
	var sessionID jhlog.ID128
	sessionID[0] = 0x22
	header := jhlog.SegmentHeader{ProcessInstanceID: processID, SessionID: sessionID, ProcessName: "app"}
	events := []jhlog.Event{
		{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{ProcessName: "app"}},
		{Type: jhlog.EventOperation, TimeMS: 1, Attribution: attributionForTest(2, 3, 0, 0), Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(5), ID: 10, Phase: jhlog.OperationPhaseStarted, Kind: jhlog.OperationKindUser,
		}},
		{Type: jhlog.EventDatabase, TimeMS: 2, Attribution: jhlog.AttributionContext{
			Present: true, Screen: jhlog.LocalSymbol(2), Owner: jhlog.LocalSymbol(3), OperationID: 10,
		}, Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.LocalSymbol(4),
			Framework: jhlog.DatabaseFrameworkRoom, Operation: jhlog.DatabaseOperationQuery,
			Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 2_000,
		}},
		{Type: jhlog.EventOperation, TimeMS: 3, Attribution: attributionForTest(6, 7, 0, 0), Operation: &jhlog.OperationEvent{
			NameRef: jhlog.LocalSymbol(9), ID: 11, Phase: jhlog.OperationPhaseStarted, Kind: jhlog.OperationKindUser,
		}},
		{Type: jhlog.EventDatabase, TimeMS: 4, Attribution: jhlog.AttributionContext{
			Present: true, Screen: jhlog.LocalSymbol(6), Owner: jhlog.LocalSymbol(7), OperationID: 11,
		}, Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.LocalSymbol(8),
			Framework: jhlog.DatabaseFrameworkRoom, Operation: jhlog.DatabaseOperationQuery,
			Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 3_000,
		}},
	}

	analysis := inspectLogsForTest("database-contexts", []jhlog.Log{{
		Dict: dict, Events: events, Result: jhlog.StreamResult{Header: header},
	}}).DatabaseAnalysis
	if analysis == nil || len(analysis.Statements) != 1 || analysis.Statements[0].Overall.Calls != 2 {
		t.Fatalf("canonical statements = %+v", analysis)
	}
	contexts := analysis.Statements[0].Contexts
	if len(contexts) != 2 {
		t.Fatalf("database contexts = %+v", contexts)
	}
	byOperation := make(map[string]DatabaseStatementContextStats, len(contexts))
	for _, context := range contexts {
		byOperation[context.ContextOperation] = context
	}
	chat := byOperation["chat.open"]
	search := byOperation["search.run"]
	if chat.Source != "MessagesDao.load" || chat.Screen != "Chat" || chat.OperationID != 10 ||
		chat.Process != "app" || chat.ProcessInstanceID != fmt.Sprintf("%x", processID[:]) ||
		chat.SessionID != fmt.Sprintf("%x", sessionID[:]) || search.Source != "SearchDao.load" ||
		search.Screen != "Search" || search.OperationID != 11 {
		t.Fatalf("database contexts = %+v", contexts)
	}
}

func TestDatabaseAnalysisKeepsSessionsSeparateBeforeCanonicalMerge(t *testing.T) {
	dict := map[uint64]string{1: "SELECT value FROM samples", 2: "Samples", 3: "SamplesDao.load"}
	logs := make([]jhlog.Log, 0, 2)
	for index := byte(1); index <= 2; index++ {
		var processID jhlog.ID128
		processID[0] = index
		var sessionID jhlog.ID128
		sessionID[0] = index + 10
		logs = append(logs, jhlog.Log{
			Dict: dict,
			Result: jhlog.StreamResult{Header: jhlog.SegmentHeader{
				ProcessInstanceID: processID, SessionID: sessionID, ProcessName: "app",
			}},
			Events: []jhlog.Event{{
				Type: jhlog.EventDatabase, TimeMS: 1,
				Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(2)},
				Database: &jhlog.DatabaseEvent{
					QueryRef: jhlog.LocalSymbol(1), SourceRef: jhlog.LocalSymbol(3),
					Framework: jhlog.DatabaseFrameworkSQLite, Operation: jhlog.DatabaseOperationQuery,
					Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
				},
			}},
		})
	}

	analysis := inspectLogsForTest("database-sessions", logs).DatabaseAnalysis
	if analysis == nil || len(analysis.Statements) != 1 || analysis.Statements[0].Overall.Calls != 2 ||
		len(analysis.Statements[0].Contexts) != 2 {
		t.Fatalf("session-aware database analysis = %+v", analysis)
	}
	left, right := analysis.Statements[0].Contexts[0], analysis.Statements[0].Contexts[1]
	if left.SessionID == right.SessionID || left.ProcessInstanceID == right.ProcessInstanceID {
		t.Fatalf("database sessions were merged: %+v", analysis.Statements[0].Contexts)
	}
}

func TestDatabaseAnalysisKeepsMainAndBackgroundLatencyIndependent(t *testing.T) {
	dict := map[uint64]string{1: "SELECT value FROM samples"}
	events := []jhlog.Event{{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{}}}
	events = append(events, jhlog.Event{
		Type: jhlog.EventDatabase, TimeMS: 1, Flags: uint64(jhlog.FlagThreadMain),
		Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(1), Framework: jhlog.DatabaseFrameworkSQLite,
			Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
			DurationUS: 1_000,
		},
	})
	for index := 0; index < 25; index++ {
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeMS: uint64(index + 2),
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(1), Framework: jhlog.DatabaseFrameworkSQLite,
				Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
				DurationUS: 200_000,
			},
		})
	}

	analysis := inspectLogsForTest("database-threads", []jhlog.Log{{Dict: dict, Events: events}}).DatabaseAnalysis
	if analysis == nil || analysis.Overall.Calls != 26 || analysis.Main.Calls != 1 || analysis.Background.Calls != 25 ||
		analysis.Main.P95DurationUS != 1_000 || analysis.Main.MaxDurationUS != 1_000 ||
		analysis.Background.P95DurationUS != 200_000 || analysis.Background.MaxDurationUS != 200_000 {
		t.Fatalf("thread-specific database analysis = %+v", analysis)
	}
	if len(analysis.Statements) != 1 || analysis.Statements[0].Main.P95DurationUS != 1_000 ||
		analysis.Statements[0].Background.P95DurationUS != 200_000 {
		t.Fatalf("thread-specific statement = %+v", analysis.Statements)
	}
}

func TestDatabaseQuantilesSwitchToBoundedDeterministicStorage(t *testing.T) {
	dict := map[uint64]string{1: "SELECT value FROM samples WHERE id = ?"}
	events := []jhlog.Event{{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{}}}
	for index := uint64(1); index <= 256; index++ {
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeMS: index,
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(1), Framework: jhlog.DatabaseFrameworkSQLite,
				Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
				DurationUS: index * 1_000,
			},
		})
	}

	analysis := inspectLogsForTest("database-quantiles", []jhlog.Log{{Dict: dict, Events: events}}).DatabaseAnalysis
	if analysis == nil || !analysis.Overall.QuantilesApproximated || analysis.Overall.Calls != 256 ||
		analysis.Overall.MaxDurationUS != 256_000 {
		t.Fatalf("bounded database analysis = %+v", analysis)
	}
	if analysis.Overall.P50DurationUS > analysis.Overall.P95DurationUS ||
		analysis.Overall.P95DurationUS > analysis.Overall.MaxDurationUS || len(analysis.Statements) != 1 ||
		!analysis.Statements[0].Overall.QuantilesApproximated {
		t.Fatalf("invalid bounded database quantiles = %+v", analysis)
	}
}

func TestDatabaseDetailCardinalityIsBoundedWithoutLosingTotals(t *testing.T) {
	dict := make(map[uint64]string, databaseStatementGroupLimit+1)
	events := make([]jhlog.Event, 1, databaseStatementGroupLimit+2)
	events[0] = jhlog.Event{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{}}
	for index := 1; index <= databaseStatementGroupLimit+1; index++ {
		id := uint64(index)
		dict[id] = fmt.Sprintf("SELECT value_%d FROM samples", index)
		events = append(events, jhlog.Event{
			Type: jhlog.EventDatabase, TimeMS: id,
			Database: &jhlog.DatabaseEvent{
				QueryRef: jhlog.LocalSymbol(id), Framework: jhlog.DatabaseFrameworkSQLite,
				Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess,
				DurationUS: 1_000,
			},
		})
	}

	analysis := inspectLogsForTest("database-cardinality", []jhlog.Log{{Dict: dict, Events: events}}).DatabaseAnalysis
	if analysis == nil || analysis.Overall.Calls != databaseStatementGroupLimit+1 ||
		len(analysis.Statements) != databaseStatementGroupLimit || analysis.DroppedStatementEvents != 1 ||
		analysis.EvictedStatementGroups > 1 || analysis.FrequencyEstimateError == 0 {
		t.Fatalf("bounded database groups: calls=%d groups=%d dropped=%d evicted=%d error=%d",
			analysis.Overall.Calls, len(analysis.Statements), analysis.DroppedStatementEvents,
			analysis.EvictedStatementGroups, analysis.FrequencyEstimateError)
	}
}

func TestDatabaseHeavyHittersRetainLateCriticalStatement(t *testing.T) {
	dict := make(map[uint64]string, databaseStatementGroupLimit+1)
	events := make([]jhlog.Event, 0, databaseStatementGroupLimit+1)
	for index := 1; index <= databaseStatementGroupLimit; index++ {
		id := uint64(index)
		dict[id] = fmt.Sprintf("SELECT ordinary_%d FROM samples", index)
		events = append(events, jhlog.Event{Type: jhlog.EventDatabase, TimeMS: id, Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(id), Framework: jhlog.DatabaseFrameworkSQLite,
			Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
		}})
	}
	criticalID := uint64(databaseStatementGroupLimit + 1)
	dict[criticalID] = "SELECT critical FROM samples"
	events = append(events, jhlog.Event{
		Type: jhlog.EventDatabase, TimeMS: criticalID, Flags: uint64(jhlog.FlagThreadMain),
		Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(criticalID), Framework: jhlog.DatabaseFrameworkSQLite,
			Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeFailure,
			DurationUS: 500_000,
		},
	})

	analysis := inspectLogsForTest("database-critical-retention", []jhlog.Log{{Dict: dict, Events: events}}).DatabaseAnalysis
	if analysis == nil || len(analysis.Statements) != databaseStatementGroupLimit ||
		analysis.EvictedStatementGroups != 1 || analysis.DroppedStatementEvents != 1 {
		t.Fatalf("heavy hitter analysis = %+v", analysis)
	}
	for _, statement := range analysis.Statements {
		if statement.Query == "SELECT critical FROM samples" {
			if statement.Main.Calls != 1 || statement.Overall.Failures != 1 || statement.EstimatedCalls == 0 {
				t.Fatalf("critical statement = %+v", statement)
			}
			return
		}
	}
	t.Fatal("late critical statement was not retained")
}

func TestDatabaseHeavyHittersAdmitRepeatedStatementAfterCapacity(t *testing.T) {
	dict := make(map[uint64]string, databaseStatementGroupLimit+1)
	events := make([]jhlog.Event, 0, databaseStatementGroupLimit+2)
	for index := 1; index <= databaseStatementGroupLimit; index++ {
		id := uint64(index)
		dict[id] = fmt.Sprintf("SELECT ordinary_%d FROM samples", index)
		events = append(events, jhlog.Event{Type: jhlog.EventDatabase, TimeMS: id, Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(id), Framework: jhlog.DatabaseFrameworkSQLite,
			Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
		}})
	}
	hotID := uint64(databaseStatementGroupLimit + 1)
	dict[hotID] = "SELECT repeated FROM samples"
	for offset := uint64(0); offset < 2; offset++ {
		events = append(events, jhlog.Event{Type: jhlog.EventDatabase, TimeMS: hotID + offset, Database: &jhlog.DatabaseEvent{
			QueryRef: jhlog.LocalSymbol(hotID), Framework: jhlog.DatabaseFrameworkSQLite,
			Operation: jhlog.DatabaseOperationQuery, Outcome: jhlog.DatabaseOutcomeSuccess, DurationUS: 1_000,
		}})
	}

	analysis := inspectLogsForTest("database-frequency-retention", []jhlog.Log{{Dict: dict, Events: events}}).DatabaseAnalysis
	for _, statement := range analysis.Statements {
		if statement.Query == "SELECT repeated FROM samples" {
			if statement.EstimatedCalls < 2 || statement.Overall.Calls == 0 ||
				statement.EstimatedCalls-statement.Overall.Calls > statement.FrequencyEstimateError {
				t.Fatalf("repeated statement = %+v", statement)
			}
			return
		}
	}
	t.Fatal("repeated statement was not admitted by heavy-hitter estimator")
}

func TestInspectKeepsOwnerKindsSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner-kinds.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 1, Value: "SharedOwner"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictRoute, ID: 2, Value: "GET /shared"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStack, ID: 3, Value: "SharedOwner.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictClass, ID: 4, Value: "SharedOwner"}},
		{Type: jhlog.EventHTTP, TimeMS: 1, Attribution: attributionForTest(0, 1, 0, 0), HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(2), DurationMS: 100, Status: jhlog.Status2xx}},
		{Type: jhlog.EventStall, TimeMS: 2, Attribution: attributionForTest(0, 1, 0, 0), Stall: &jhlog.StallEvent{StackRef: jhlog.LocalSymbol(3), DurationMS: 250}},
		{Type: jhlog.EventRetained, TimeMS: 3, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(4), AgeMS: 10_000, Count: 1}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	byKind := map[string]OwnerStats{}
	for _, owner := range summary.Owners {
		if owner.Owner == "SharedOwner" {
			byKind[owner.Kind] = owner
		}
	}
	for _, kind := range []string{"http", "main_thread_stall", "retained_object"} {
		if _, ok := byKind[kind]; !ok {
			t.Fatalf("missing owner kind %q in %+v", kind, summary.Owners)
		}
	}
	if byKind["http"].TotalMS != 100 || byKind["main_thread_stall"].TotalMS != 250 || byKind["retained_object"].TotalMS != 10_000 {
		t.Fatalf("owner durations were merged incorrectly: %+v", byKind)
	}
	if byKind["main_thread_stall"].StackHint != "SharedOwner.render" {
		t.Fatalf("stall stack hint = %q", byKind["main_thread_stall"].StackHint)
	}
}

func TestInspectInfersRetainedHolderFromOwnerOrClass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retained-holder-fallback.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictClass, ID: 1, Value: "com.example.LeakyActivity"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictClass, ID: 2, Value: "com.example.LeakyView"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 3, Value: "com.example.LeakOwner"}},
		{Type: jhlog.EventRetained, TimeMS: 1, Attribution: attributionForTest(0, 3, 0, 0), Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(1), AgeMS: 10_000, Count: 1}},
		{Type: jhlog.EventRetained, TimeMS: 2, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(2), AgeMS: 12_000, Count: 1}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	activity, ok := memoryLeakByClass(summary.MemoryLeaks, "com.example.LeakyActivity")
	if !ok || activity.Holder != "com.example.LeakOwner" {
		t.Fatalf("expected holder inferred from retained owner, got %+v", activity)
	}
	view, ok := memoryLeakByClass(summary.MemoryLeaks, "com.example.LeakyView")
	if !ok || view.Holder != "com.example.LeakyView" {
		t.Fatalf("expected holder inferred from retained class, got %+v", view)
	}
}

func TestInspectFilesKeepsExactPercentilesBeyondFormerReservoirBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exact-aggregate.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictRoute, ID: 1, Value: "GET /feed"},
		{Kind: jhlog.DictOwner, ID: 2, Value: "FeedRepository.refresh"},
		{Kind: jhlog.DictMetric, ID: 3, Value: "executor.queue.depth"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}
	const total = 20_025
	for i := 1; i <= total; i++ {
		value := uint64(i)
		if err := writer.WriteEvent(jhlog.Event{
			Type:        jhlog.EventHTTP,
			TimeMS:      value,
			Attribution: attributionForTest(0, 2, 0, 0),
			HTTP: &jhlog.HTTPEvent{
				RouteRef:   jhlog.LocalSymbol(1),
				DurationMS: value,
				Status:     jhlog.Status2xx,
			},
		}); err != nil {
			t.Fatalf("WriteEvent(http %d) error = %v", i, err)
		}
		if err := writer.WriteEvent(jhlog.Event{
			Type:   jhlog.EventGauge,
			TimeMS: value,
			Metric: &jhlog.MetricEvent{
				MetricRef: jhlog.LocalSymbol(3),
				Value:     value,
			},
		}); err != nil {
			t.Fatalf("WriteEvent(gauge %d) error = %v", i, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	if summary.HTTPCount != total {
		t.Fatalf("HTTPCount = %d, want %d", summary.HTTPCount, total)
	}
	if len(summary.Routes) != 1 {
		t.Fatalf("Routes = %+v, want one route", summary.Routes)
	}
	route := summary.Routes[0]
	expectedP50 := uint64((total*50 + 99) / 100)
	expectedP95 := uint64((total*95 + 99) / 100)
	if route.Count != total || route.MaxMS != total ||
		route.P50MS != expectedP50 || route.P95MS != expectedP95 {
		t.Fatalf("route stats are not exact: %+v", route)
	}
	if summary.HTTPP95MS != expectedP95 {
		t.Fatalf("global HTTP p95 is not exact: %d", summary.HTTPP95MS)
	}
	if len(summary.SignalContexts) != 1 || summary.SignalContexts[0].HTTPP95MS != expectedP95 {
		t.Fatalf("context HTTP p95 is not exact: %+v", summary.SignalContexts)
	}
	if len(summary.Gauges) != 1 {
		t.Fatalf("Gauges = %+v, want one gauge", summary.Gauges)
	}
	expectedExtra := fmt.Sprintf("среднее=%d максимум=%d наблюдений=%d", uint64(total+1)/2, uint64(total), uint64(total))
	if summary.Gauges[0].Extra != expectedExtra {
		t.Fatalf("gauge Extra = %q, want %q", summary.Gauges[0].Extra, expectedExtra)
	}
}

func TestInspectFilesDoesNotCarryOperationContextAcrossEventsOrLogs(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jhlog")
	firstFile, firstWriter, err := jhlog.Create(first)
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	firstEvents := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictScreen, ID: 1, Value: "CheckoutScreen"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 2, Value: "CheckoutPresenter.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 3, Value: "checkout.open"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 4, Value: "render_list"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictLogSource, ID: 5, Value: "test"}},
		{Type: jhlog.EventLogSpam, TimeMS: 1, Attribution: attributionForTest(1, 2, 3, 4), LogSpam: &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(5), Level: 2, Count: 1}},
	}
	for _, event := range firstEvents {
		if err := firstWriter.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(first %d) error = %v", event.Type, err)
		}
	}
	if err := firstFile.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	second := filepath.Join(dir, "second.jhlog")
	secondFile, secondWriter, err := jhlog.Create(second)
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	secondEvents := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictRoute, ID: 1, Value: "GET /feed"}},
		{Type: jhlog.EventHTTP, TimeMS: 1, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(1), DurationMS: 120, Status: jhlog.Status2xx}},
	}
	for _, event := range secondEvents {
		if err := secondWriter.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(second %d) error = %v", event.Type, err)
		}
	}
	if err := secondFile.Close(); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}

	summary, err := inspectFilesForTest("sample", []string{first, second})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	if len(summary.SignalContexts) != 2 {
		t.Fatalf("SignalContexts = %+v, want an attributed log event and an unattributed HTTP context", summary.SignalContexts)
	}
	var httpContext *SignalContextStats
	for index := range summary.SignalContexts {
		if summary.SignalContexts[index].HTTPCount > 0 {
			httpContext = &summary.SignalContexts[index]
			break
		}
	}
	if httpContext == nil {
		t.Fatalf("HTTP context missing: %+v", summary.SignalContexts)
	}
	if httpContext.Screen != "unknown" || httpContext.Operation != "unknown" || httpContext.Owner != "unknown" {
		t.Fatalf("HTTP inherited stale operation context: %+v", httpContext)
	}
}

func environmentHasItem(environment RunEnvironment, label string, value string) bool {
	for _, item := range environment.Items {
		if item.Label == label && item.Value == value {
			return true
		}
	}
	return false
}

func environmentItemDetailContains(environment RunEnvironment, label string, text string) bool {
	for _, item := range environment.Items {
		if item.Label == label && strings.Contains(item.Detail, text) {
			return true
		}
	}
	return false
}

func codeProblemsHaveSignal(rows []CodeProblemStats, name string) bool {
	for _, row := range rows {
		for _, signal := range row.Signals {
			if signal.Name == name {
				return true
			}
		}
	}
	return false
}

func TestInspectFilesAppliesRouteFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	summary, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{RouteContains: "/checkout"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest() error = %v", err)
	}
	if summary.HTTPCount != 1 {
		t.Fatalf("HTTPCount = %d, want 1", summary.HTTPCount)
	}
	if len(summary.Routes) != 1 || summary.Routes[0].Route != "POST /checkout" {
		t.Fatalf("unexpected routes: %+v", summary.Routes)
	}
}

func TestInspectFilesWarnsWhenFilterKeepsGlobalSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	summary, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{RouteContains: "/checkout"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest() error = %v", err)
	}
	if len(summary.Warnings) == 0 {
		t.Fatalf("expected global signal warning")
	}
	warning := strings.Join(summary.Warnings, "\n")
	for _, want := range []string{"показаны глобально", "контекст устройства", "custom metrics"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning %q does not contain %q", warning, want)
		}
	}
}

func TestInspectFilesExposesOpenTailWithoutCorruptionWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.jhlog")
	var output bytes.Buffer
	writer, err := jhlog.NewWriter(&output)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventMemory, TimeMS: 1, Memory: &jhlog.MemoryEvent{PSSKB: 42}}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	output.Write([]byte{'J', 'H', 'C', '9', 32, 0, 1})
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	summary, err := inspectFilesForTest("partial", []string{path})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	if summary.EventCount == 0 {
		t.Fatalf("expected preserved events")
	}
	if len(summary.CollectionSegments) != 1 || summary.CollectionSegments[0].Status != string(jhlog.SegmentStatusOpenWithTail) || summary.CollectionSegments[0].TailBytes != 7 {
		t.Fatalf("segment status = %+v", summary.CollectionSegments)
	}
	if warning := strings.Join(summary.Warnings, "\n"); strings.Contains(warning, "ignored partial trailing compact event") || strings.Contains(warning, "corrupt") {
		t.Fatalf("active uncommitted tail was reported as corruption: %+v", summary.Warnings)
	}
}

func TestInspectFilesExplainsSegmentEndReasons(t *testing.T) {
	cases := []struct {
		name            string
		reason          jhlog.SegmentEndReason
		wantReason      string
		warningFragment string
	}{
		{name: "normal", reason: jhlog.SegmentEndNormal, wantReason: "normal"},
		{name: "size limit", reason: jhlog.SegmentEndSizeLimit, wantReason: "size_limit", warningFragment: "достиг лимита размера"},
		{name: "io error", reason: jhlog.SegmentEndIOError, wantReason: "io_error", warningFragment: "из-за ошибки ввода-вывода"},
		{name: "shutdown", reason: jhlog.SegmentEndShutdown, wantReason: "shutdown"},
		{name: "rotation", reason: jhlog.SegmentEndRotation, wantReason: "rotation"},
		{name: "storage budget", reason: jhlog.SegmentEndStorageBudget, wantReason: "storage_budget_exhausted", warningFragment: "storage_budget_exhausted"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reason.jhlog")
			writeJhlogWithEndReason(t, path, test.reason)
			summary, err := inspectFilesForTest(test.name, []string{path})
			if err != nil {
				t.Fatalf("inspectFilesForTest() error = %v", err)
			}
			if len(summary.CollectionSegments) != 1 {
				t.Fatalf("segments = %+v", summary.CollectionSegments)
			}
			segment := summary.CollectionSegments[0]
			if segment.EndReason != test.wantReason || segment.EndReasonCode != uint64(test.reason) {
				t.Fatalf("segment reason = %q/%d, want %q/%d", segment.EndReason, segment.EndReasonCode, test.wantReason, test.reason)
			}
			warnings := strings.Join(summary.Warnings, "\n")
			if test.warningFragment == "" {
				for _, unexpected := range []string{"достиг лимита размера", "из-за ошибки ввода-вывода", "неизвестной причиной"} {
					if strings.Contains(warnings, unexpected) {
						t.Fatalf("normal end reason produced warning %q", warnings)
					}
				}
			} else if !strings.Contains(warnings, test.warningFragment) {
				t.Fatalf("warnings %q do not contain %q", warnings, test.warningFragment)
			}
			if test.reason == jhlog.SegmentEndSizeLimit {
				for _, stale := range []string{"следующ", "продолж", "segment chain"} {
					if strings.Contains(strings.ToLower(warnings), stale) {
						t.Fatalf("size-limit warning contains stale continuation wording %q: %q", stale, warnings)
					}
				}
			}
		})
	}
}

func TestCollectionQualityReportsWholeRunArchiveEvictionAsNotice(t *testing.T) {
	collector := newCollector("archive eviction", 1, Options{})
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{
		jhlog.QualityArchiveEvictedRunTotal:     2,
		jhlog.QualityArchiveEvictedSegmentTotal: 7,
		jhlog.QualityArchiveEvictedBytesTotal:   12_345,
	}}
	collector.addStreamResult(jhlog.StreamResult{
		Source: "current-run.jhlog", Header: collectionTestHeader(42, 0),
		Status: jhlog.SegmentStatusClosedClean, Sealed: true, LatestQuality: &quality,
		SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndShutdown},
	})
	collector.finalizeCollectionQuality()

	got := collector.summary.CollectionQuality
	if got.ArchiveEvictedRuns != 2 || got.ArchiveEvictedSegments != 7 || got.ArchiveEvictedBytes != 12_345 {
		t.Fatalf("archive eviction evidence = %+v", got)
	}
	if !warningsContain(got.Notices, "удалено 2 завершённых запусков") || warningsContain(got.Reasons, "циклическое хранение") {
		t.Fatalf("archive eviction notices=%+v reasons=%+v", got.Notices, got.Reasons)
	}
}

func writeJhlogWithEndReason(t *testing.T, path string, reason jhlog.SegmentEndReason) {
	t.Helper()
	var output bytes.Buffer
	writer, err := jhlog.NewWriter(&output)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.CloseWithReason(reason); err != nil {
		t.Fatalf("CloseWithReason(%d) error = %v", reason, err)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestInspectFilesAppliesContextFiltersToProblemSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "problem-filters.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictScreen, ID: 1, Value: "FeedScreen"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictScreen, ID: 2, Value: "CheckoutScreen"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 3, Value: "FeedOwner"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 4, Value: "CheckoutOwner"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 5, Value: "FeedCallee"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 6, Value: "CheckoutCallee"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 7, Value: "feed.open"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 8, Value: "checkout.open"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 9, Value: "render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictLogSource, ID: 10, Value: "FeedLogger.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictLogSource, ID: 11, Value: "CheckoutLogger.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 12, Value: "main_thread_stall"}},
		{Type: jhlog.EventLogSpam, TimeMS: 1, Attribution: attributionForTest(1, 3, 7, 9), LogSpam: &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(10), Level: 5, Count: 3}},
		{Type: jhlog.EventLogSpam, TimeMS: 2, Attribution: attributionForTest(2, 4, 8, 9), LogSpam: &jhlog.LogSpamEvent{SourceRef: jhlog.LocalSymbol(11), Level: 5, Count: 5}},
		{Type: jhlog.EventProblem, TimeMS: 3, Attribution: attributionForTest(1, 3, 7, 9), Problem: &jhlog.ProblemEvent{KindRef: jhlog.LocalSymbol(12), WindowMS: 5000, Count: 2, MaxMS: 80}},
		{Type: jhlog.EventProblem, TimeMS: 4, Attribution: attributionForTest(2, 4, 8, 9), Problem: &jhlog.ProblemEvent{KindRef: jhlog.LocalSymbol(12), WindowMS: 5000, Count: 4, MaxMS: 120}},
		{Type: jhlog.EventRuntimeCall, TimeMS: 5, Attribution: attributionForTest(1, 3, 7, 9), RuntimeCall: &jhlog.RuntimeCallEvent{CalleeRef: jhlog.LocalSymbol(5), Count: 1, TotalMS: 20, MaxMS: 20}},
		{Type: jhlog.EventRuntimeCall, TimeMS: 6, Attribution: attributionForTest(2, 4, 8, 9), RuntimeCall: &jhlog.RuntimeCallEvent{CalleeRef: jhlog.LocalSymbol(6), Count: 1, TotalMS: 30, MaxMS: 30}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	feedOnly, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{ScreenContains: "FeedScreen"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest(screen) error = %v", err)
	}
	if len(feedOnly.LogSpam) != 1 || feedOnly.LogSpam[0].Screen != "FeedScreen" {
		t.Fatalf("screen filter leaked log spam: %+v", feedOnly.LogSpam)
	}
	if len(feedOnly.ProblemWindows) != 1 || feedOnly.ProblemWindows[0].Screen != "FeedScreen" {
		t.Fatalf("screen filter leaked problems: %+v", feedOnly.ProblemWindows)
	}
	if len(feedOnly.RuntimeCalls) != 1 || feedOnly.RuntimeCalls[0].Screen != "FeedScreen" {
		t.Fatalf("screen filter leaked runtime calls: %+v", feedOnly.RuntimeCalls)
	}

	loggerOnly, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{ClassContains: "FeedLogger"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest(class) error = %v", err)
	}
	if len(loggerOnly.LogSpam) != 1 || loggerOnly.LogSpam[0].Source != "FeedLogger.render" {
		t.Fatalf("class filter did not select log source: %+v", loggerOnly.LogSpam)
	}
	if len(loggerOnly.ProblemWindows) != 0 || len(loggerOnly.RuntimeCalls) != 0 {
		t.Fatalf("class filter leaked non-matching signals: problems=%+v runtime=%+v", loggerOnly.ProblemWindows, loggerOnly.RuntimeCalls)
	}

	calleeOnly, err := inspectFilesWithFilterForTest("sample", []string{path}, Filter{OwnerContains: "FeedCallee"})
	if err != nil {
		t.Fatalf("inspectFilesWithFilterForTest(owner callee) error = %v", err)
	}
	if len(calleeOnly.RuntimeCalls) != 1 || calleeOnly.RuntimeCalls[0].Callee != "FeedCallee" {
		t.Fatalf("owner filter did not match runtime callee: %+v", calleeOnly.RuntimeCalls)
	}
	if len(calleeOnly.LogSpam) != 0 || len(calleeOnly.ProblemWindows) != 0 {
		t.Fatalf("owner filter leaked unrelated signals: logspam=%+v problems=%+v", calleeOnly.LogSpam, calleeOnly.ProblemWindows)
	}
}

func TestInspectGroupsJankStatsMetrics(t *testing.T) {
	log := jhlog.Log{
		Dict: map[uint64]string{
			1: "jankstats.frame.count",
			2: "jankstats.frame.duration_ms",
			3: "ui.frame.source.choreographer",
		},
		Events: []jhlog.Event{
			{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(1), Value: 3}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(2), Value: 18}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(2), Value: 22}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(3), Value: 1}},
		},
	}

	summary := inspectLogsForTest("jankstats", []jhlog.Log{log})
	if len(summary.JankStats) != 2 {
		t.Fatalf("unexpected jankstats metrics: %+v", summary.JankStats)
	}
	metrics := namedValuesByName(summary.JankStats)
	if _, leaked := metrics["ui.frame.source.choreographer"]; leaked {
		t.Fatalf("non-JankStats source leaked into JankStats metrics: %+v", summary.JankStats)
	}
}

func TestMissingEmbeddedStableSymbolViolatesSelfContainedContract(t *testing.T) {
	const stableID = 0x0123456789abcdef
	namespace := append([]byte{0xaa, 0xbb}, make([]byte, 14)...)
	header := jhlog.DefaultSegmentHeader()
	header.SymbolNamespace = namespace
	path := filepath.Join(t.TempDir(), "missing-embedded-symbol.jhlog")
	closer, writer, err := jhlog.CreateWithHeader(path, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(jhlog.Event{
		Type:   jhlog.EventCounter,
		Metric: &jhlog.MetricEvent{MetricRef: jhlog.StableSymbol(stableID), Value: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = InspectFilesWithOptions("invalid", []string{path}, Options{})
	if err == nil || !strings.Contains(err.Error(), "self-contained") {
		t.Fatalf("missing embedded symbol error = %v, want self-contained contract failure", err)
	}
}

func TestInspectMergesAggregatedGaugesBySamplesAndMode(t *testing.T) {
	log := jhlog.Log{
		Dict: map[uint64]string{
			1: "memory.pss",
			2: "battery.status",
			3: "battery.charging",
		},
		Events: []jhlog.Event{
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricRef: jhlog.LocalSymbol(1),
					Value:     100,
					Count:     2,
					Sum:       200,
					Max:       140,
					Mode:      jhlog.MetricModeAverage,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricRef: jhlog.LocalSymbol(1),
					Value:     200,
					Count:     4,
					Sum:       800,
					Max:       260,
					Mode:      jhlog.MetricModeAverage,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricRef: jhlog.LocalSymbol(2),
					Value:     2,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricRef: jhlog.LocalSymbol(2),
					Value:     5,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricRef: jhlog.LocalSymbol(3),
					Value:     50,
					Count:     2,
					Sum:       1,
					Max:       1,
					Mode:      jhlog.MetricModeBooleanRate,
				},
			},
		},
	}

	summary := inspectLogsForTest("metrics", []jhlog.Log{log})
	gauges := namedValuesByName(summary.Gauges)
	if got := gauges["memory.pss"]; got.Value != 166 || got.Extra != "среднее=166 максимум=260 наблюдений=6" {
		t.Fatalf("memory.pss = %+v", got)
	}
	if got := gauges["battery.status"]; got.Value != 5 || got.Extra != "состояние=5 наблюдений=2" {
		t.Fatalf("battery.status = %+v", got)
	}
	if got := gauges["battery.charging"]; got.Value != 50 || got.Extra != "доля включённого состояния=50 включено=1 наблюдений=2" {
		t.Fatalf("battery.charging = %+v", got)
	}
}

func TestCompareWarnsOnCohortMismatch(t *testing.T) {
	baseline := Summary{
		LogCount:    5,
		EventCount:  500,
		AppVersions: []NamedValue{{Name: "1.0.0", Value: 5}},
		SDKs:        []NamedValue{{Name: "api-34", Value: 5}},
		Devices:     []NamedValue{{Name: "Pixel 7", Value: 5}},
		Processes:   []NamedValue{{Name: "main", Value: 5}},
		Network:     []NamedValue{{Name: "wifi", Value: 10}},
		Cohorts:     []NamedValue{{Name: "app=1.0.0 build=100 sdk=api-34 device=Pixel 7 process=main network=wifi", Value: 100}},
	}
	candidate := Summary{
		LogCount:    5,
		EventCount:  500,
		AppVersions: []NamedValue{{Name: "1.1.0", Value: 5}},
		SDKs:        []NamedValue{{Name: "api-35", Value: 5}},
		Devices:     []NamedValue{{Name: "Pixel 8", Value: 5}},
		Processes:   []NamedValue{{Name: "main", Value: 5}},
		Network:     []NamedValue{{Name: "cellular", Value: 10}},
		Cohorts:     []NamedValue{{Name: "app=1.1.0 build=101 sdk=api-35 device=Pixel 8 process=main network=cellular", Value: 100}},
	}

	comparison := Compare(baseline, candidate)
	if len(comparison.Warnings) == 0 {
		t.Fatalf("expected cohort warnings")
	}
	found := false
	for _, delta := range comparison.Deltas {
		if delta.Name == "Cohort mix" && delta.Severity != "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Cohort mix delta: %+v", comparison.Deltas)
	}
}

func TestCompareIgnoresCategoricalCountGrowthWhenSharesStayStable(t *testing.T) {
	baseline := Summary{
		LogCount:     1,
		EventCount:   1_030,
		ContextCount: 81,
		Network:      []NamedValue{{Name: "wifi", Value: 81}},
		Cohorts: []NamedValue{
			{Name: "wifi", Value: 1_025},
			{Name: "unknown", Value: 5},
		},
	}
	candidate := Summary{
		LogCount:     1,
		EventCount:   784,
		ContextCount: 42,
		Network:      []NamedValue{{Name: "wifi", Value: 42}},
		Cohorts: []NamedValue{
			{Name: "wifi", Value: 780},
			{Name: "unknown", Value: 4},
		},
	}

	comparison := Compare(baseline, candidate)
	if len(comparison.CohortWarnings) != 0 {
		t.Fatalf("scaled categorical counts produced warnings: %+v", comparison.CohortWarnings)
	}
	deltas := deltasByName(comparison.Deltas)
	for _, name := range []string{"Network mix", "Cohort mix"} {
		delta, ok := deltas[name]
		if !ok {
			t.Fatalf("missing %s delta: %+v", name, comparison.Deltas)
		}
		if delta.Severity != "ok" || delta.Change != "без существенных изменений" {
			t.Fatalf("%s = %+v, want stable shares", name, delta)
		}
		if !strings.Contains(delta.ComparisonNote, "сравниваются доли категорий") {
			t.Fatalf("%s has no normalization explanation: %+v", name, delta)
		}
	}
}

func TestCompareDoesNotTreatMissingCategoricalTelemetryAsAChange(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 1, ContextCount: 4, Network: []NamedValue{{Name: "wifi", Value: 4}}},
		Summary{LogCount: 1},
	)
	delta, ok := deltasByName(comparison.Deltas)["Network mix"]
	if !ok {
		t.Fatalf("missing Network mix delta: %+v", comparison.Deltas)
	}
	if delta.Comparable || delta.Severity != "ok" || delta.Candidate != "нет данных" {
		t.Fatalf("missing network telemetry = %+v, want incomparable data", delta)
	}
	if len(comparison.CohortWarnings) != 0 {
		t.Fatalf("missing categorical telemetry produced cohort warnings: %+v", comparison.CohortWarnings)
	}
}

func TestCompareUsesRealSampleSizesAndDoesNotInventIntervals(t *testing.T) {
	comparison := Compare(
		Summary{
			LogCount:     5,
			EventCount:   500,
			DurationMS:   60_000,
			MemoryCount:  3,
			ContextCount: 50,
			MemoryMaxKB:  100,
			ProblemWindows: []ProblemWindowStats{
				{Kind: "main_thread_stall", Windows: 2, Count: 10},
			},
		},
		Summary{
			LogCount:     5,
			EventCount:   500,
			DurationMS:   60_000,
			MemoryCount:  4,
			ContextCount: 60,
			MemoryMaxKB:  140,
			ProblemWindows: []ProblemWindowStats{
				{Kind: "main_thread_stall", Windows: 3, Count: 30},
			},
		},
	)

	deltas := deltasByName(comparison.Deltas)
	if got := deltas["Max PSS"]; got.SampleSize != 3 || got.Interval != "выборка=3" {
		t.Fatalf("Max PSS delta = %+v", got)
	}
	if got := deltas["Problem windows"]; got.Baseline != "2.00 шт/мин" || got.Candidate != "3.00 шт/мин" {
		t.Fatalf("Problem windows delta = %+v", got)
	}
}

func TestCompareDoesNotReplaceMissingMeasurementsWithZero(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 5, EventCount: 500, DurationMS: 60_000},
		Summary{LogCount: 5, EventCount: 500, DurationMS: 60_000, HTTPCount: 20, HTTPP95MS: 900},
	)

	delta := deltasByName(comparison.Deltas)["HTTP p95"]
	if delta.Comparable || delta.Baseline != "нет данных" || delta.Candidate != "900 мс" || delta.Severity != "ok" {
		t.Fatalf("missing HTTP measurement became a regression: %+v", delta)
	}
	gate := EvaluateGate(comparison, ThresholdConfig{MaxSeverity: "ok"})
	if gate.Failed {
		t.Fatalf("gate failed on an incomparable metric: %+v", gate.Failures)
	}
}

func TestCompareUsesHTTPFailureRateInsteadOfRawCount(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 5, EventCount: 500, DurationMS: 60_000, HTTPCount: 100, HTTPFailed: 10, HTTPP95MS: 300},
		Summary{LogCount: 5, EventCount: 500, DurationMS: 60_000, HTTPCount: 200, HTTPFailed: 20, HTTPP95MS: 300},
	)

	delta := deltasByName(comparison.Deltas)["HTTP failure rate"]
	if !delta.Comparable || delta.Severity != "ok" || delta.Baseline != "10.00 п.п." || delta.Candidate != "10.00 п.п." {
		t.Fatalf("equal failure rates were reported as different: %+v", delta)
	}
	if _, exists := deltasByName(comparison.Deltas)["HTTP failures"]; exists {
		t.Fatal("raw HTTP failure count is still exposed as a regression metric")
	}
}

func TestCompareNormalizesCountSignalsByDuration(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 5, EventCount: 500, DurationMS: 60_000, LogSpam: []LogSpamStats{{Count: 10}}, ProblemWindows: []ProblemWindowStats{{Windows: 4}}},
		Summary{LogCount: 5, EventCount: 500, DurationMS: 120_000, LogSpam: []LogSpamStats{{Count: 20}}, ProblemWindows: []ProblemWindowStats{{Windows: 8}}},
	)

	deltas := deltasByName(comparison.Deltas)
	for _, name := range []string{"Log spam", "Problem windows"} {
		if delta := deltas[name]; delta.Severity != "ok" || delta.Change != "+0.0%" {
			t.Fatalf("%s was not normalized by duration: %+v", name, delta)
		}
	}
	if len(comparison.ExposureWarnings) != 1 || len(comparison.CohortWarnings) != 0 {
		t.Fatalf("duration warning classes = exposure:%v cohort:%v", comparison.ExposureWarnings, comparison.CohortWarnings)
	}
}

func TestDeltaFloatShowsRegressionWhenBaselineIsZero(t *testing.T) {
	delta := deltaFloat("rate", 0, 4, "п.п.", true, 100)
	if delta.RegressionPct != 100 || delta.Severity != "high" {
		t.Fatalf("new float regression is not visible: %+v", delta)
	}
}

func TestEvaluateGateFailsOnSeverity(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 5, EventCount: 500, HTTPCount: 100, HTTPP95MS: 100},
		Summary{LogCount: 5, EventCount: 500, HTTPCount: 100, HTTPP95MS: 150},
	)

	result := EvaluateGate(comparison, ThresholdConfig{MaxSeverity: "medium"})
	if !result.Failed {
		t.Fatalf("expected gate failure")
	}
}

func TestEvaluateGateFailsOnMetricRegression(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 5, EventCount: 500, HTTPCount: 100, HTTPP95MS: 100},
		Summary{LogCount: 5, EventCount: 500, HTTPCount: 100, HTTPP95MS: 112},
	)

	result := EvaluateGate(comparison, ThresholdConfig{
		Metrics: map[string]MetricThreshold{
			"HTTP p95": {MaxRegressionPct: 10},
		},
	})
	if !result.Failed {
		t.Fatalf("expected metric gate failure")
	}
}

func TestAndroidComponentGateIsOptInAndFailsOnConfiguredRegression(t *testing.T) {
	comparison := Comparison{
		AndroidComponents: AndroidComponentComparison{
			Comparable: true,
			Metrics: []Delta{
				{Name: "Binder client p95", Comparable: true, RegressionPct: 25},
			},
		},
	}

	if result := EvaluateGate(comparison, ThresholdConfig{}); result.Failed {
		t.Fatalf("Android component gate must be disabled by default: %+v", result)
	}
	result := EvaluateGate(comparison, ThresholdConfig{
		AndroidComponents: AndroidComponentGateThreshold{
			Enabled:                       true,
			MaxBinderClientP95IncreasePct: floatPointer(10),
		},
	})
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, "\n"), "Binder client p95") {
		t.Fatalf("expected Binder p95 gate failure, got %+v", result)
	}
}

func TestAndroidComponentGateFailsClosedOnPartialAnalysisUnlessExplicitlyAllowed(t *testing.T) {
	comparison := Comparison{
		AndroidComponents: AndroidComponentComparison{
			Comparable: true,
			Partial:    true,
			Note:       "Разрешён частичный анализ: secondary process отсутствует.",
			Metrics: []Delta{
				{Name: "Service failure rate", Comparable: true, RegressionAbs: 2},
			},
		},
	}
	threshold := AndroidComponentGateThreshold{
		Enabled:                         true,
		MaxServiceFailureRateIncreasePP: floatPointer(1),
	}

	result := EvaluateGate(comparison, ThresholdConfig{AndroidComponents: threshold})
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, "\n"), "partial") {
		t.Fatalf("expected partial analysis failure, got %+v", result)
	}
	threshold.AllowPartial = true
	result = EvaluateGate(comparison, ThresholdConfig{AndroidComponents: threshold})
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, "\n"), "Service failure rate") {
		t.Fatalf("expected configured local metric failure, got %+v", result)
	}
}

func TestAndroidComponentGateRejectsMissingAndInvalidThresholds(t *testing.T) {
	comparison := Comparison{AndroidComponents: AndroidComponentComparison{Comparable: true}}
	result := EvaluateGate(comparison, ThresholdConfig{
		AndroidComponents: AndroidComponentGateThreshold{Enabled: true},
	})
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, "\n"), "no thresholds") {
		t.Fatalf("expected missing threshold failure, got %+v", result)
	}

	result = EvaluateGate(comparison, ThresholdConfig{
		AndroidComponents: AndroidComponentGateThreshold{
			Enabled:                         true,
			MinBinderCorrelationCoveragePct: floatPointer(101),
		},
	})
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, "\n"), "0..100") {
		t.Fatalf("expected invalid threshold failure, got %+v", result)
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

func TestEvaluateGateFailsOnMinConfidenceOnly(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 1, EventCount: 10, HTTPCount: 3, HTTPP95MS: 100},
		Summary{LogCount: 1, EventCount: 10, HTTPCount: 3, HTTPP95MS: 100},
	)

	result := EvaluateGate(comparison, ThresholdConfig{MinConfidence: "medium"})
	if !result.Failed {
		t.Fatalf("expected confidence gate failure")
	}
	if got := strings.Join(result.Failures, "\n"); !strings.Contains(got, "baseline logs/events=1/10") {
		t.Fatalf("confidence failure is not diagnostic enough: %q", got)
	}
}

func TestEvaluateGateExplainsCollectionQualityConfidenceCap(t *testing.T) {
	baseline := Summary{
		LogCount:   5,
		EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level:   "low",
			Reasons: []string{"quality snapshots фиксируют потерю как минимум 14508 событий"},
		},
	}
	candidate := Summary{
		LogCount:   5,
		EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level:    "high",
			Complete: true,
		},
	}
	result := EvaluateGate(Compare(baseline, candidate), ThresholdConfig{MinConfidence: "high"})
	if !result.Failed {
		t.Fatal("expected collection-quality confidence failure")
	}
	failure := strings.Join(result.Failures, "\n")
	for _, fragment := range []string{"baseline=low", "candidate=high", "14508 событий"} {
		if !strings.Contains(failure, fragment) {
			t.Fatalf("confidence failure %q does not contain %q", failure, fragment)
		}
	}
}

func TestComparisonFailsClosedWhenProcessScopesDiffer(t *testing.T) {
	baseline := Summary{
		LogCount: 5, EventCount: 500,
		CollectionQuality: CollectionQuality{Level: "high", ProcessScope: "main_process_only"},
	}
	candidate := Summary{
		LogCount: 5, EventCount: 500,
		CollectionQuality: CollectionQuality{Level: "high", ProcessScope: "all_processes"},
	}

	comparison := Compare(baseline, candidate)
	if len(comparison.Deltas) == 0 || comparison.Deltas[0].Confidence != "low" {
		t.Fatalf("scope mismatch confidence = %+v", comparison.Deltas)
	}
	if !warningsContain(comparison.QualityWarnings, "Process scope отличается") {
		t.Fatalf("scope mismatch warnings = %+v", comparison.QualityWarnings)
	}
	result := EvaluateGate(comparison, ThresholdConfig{MinConfidence: "high"})
	if !result.Failed {
		t.Fatalf("scope mismatch must fail a high-confidence gate: %+v", result)
	}
}

func TestComparisonFailsClosedWhenAllowlistFingerprintsDiffer(t *testing.T) {
	baseline := Summary{
		LogCount: 5, EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level: "high", ProcessScope: "process_allowlist", AllowedProcessCount: 2,
			ProcessScopeFingerprint: strings.Repeat("a5", 32),
		},
	}
	candidate := Summary{
		LogCount: 5, EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level: "high", ProcessScope: "process_allowlist", AllowedProcessCount: 2,
			ProcessScopeFingerprint: strings.Repeat("5a", 32),
		},
	}

	comparison := Compare(baseline, candidate)
	if len(comparison.Deltas) == 0 || comparison.Deltas[0].Confidence != "low" {
		t.Fatalf("allowlist fingerprint mismatch confidence = %+v", comparison.Deltas)
	}
	if !warningsContain(comparison.QualityWarnings, "Process scope отличается") {
		t.Fatalf("comparison warnings = %+v", comparison.QualityWarnings)
	}
}

func TestEvaluateGateFailsOnDirtyCohortsWhenRequired(t *testing.T) {
	comparison := Compare(
		Summary{
			LogCount:   5,
			EventCount: 500,
			Devices:    []NamedValue{{Name: "Pixel 8", Value: 5}},
		},
		Summary{
			LogCount:   5,
			EventCount: 500,
			Devices:    []NamedValue{{Name: "Pixel 5", Value: 5}},
		},
	)

	result := EvaluateGate(comparison, ThresholdConfig{RequireCleanCohorts: true})
	if !result.Failed {
		t.Fatalf("expected cohort gate failure")
	}
	if got := strings.Join(result.Failures, "\n"); !strings.Contains(got, "cohort mismatch") {
		t.Fatalf("expected cohort mismatch failure, got %q", got)
	}
}

func TestRequireCleanCohortsDoesNotMisclassifyCollectionLoss(t *testing.T) {
	baseline := Summary{LogCount: 5, EventCount: 500}
	candidate := Summary{
		LogCount:   5,
		EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level:   "medium",
			Reasons: []string{"словарь деградировал: overflow=1, truncated=0"},
		},
	}
	comparison := Compare(baseline, candidate)
	if len(comparison.CohortWarnings) != 0 || len(comparison.QualityWarnings) == 0 {
		t.Fatalf("warning classes = cohort:%+v quality:%+v", comparison.CohortWarnings, comparison.QualityWarnings)
	}
	result := EvaluateGate(comparison, ThresholdConfig{RequireCleanCohorts: true})
	if result.Failed {
		t.Fatalf("collection loss was treated as cohort mismatch: %+v", result.Failures)
	}
}

func TestValidationScorecardSerializesCollectionQuality(t *testing.T) {
	baseline := Summary{
		LogCount:   5,
		EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level:           "low",
			Complete:        false,
			ChainValid:      true,
			KnownLostEvents: 14508,
			Reasons:         []string{"quality snapshots фиксируют потерю как минимум 14508 событий"},
		},
	}
	candidate := Summary{
		LogCount:   5,
		EventCount: 500,
		CollectionQuality: CollectionQuality{
			Level:      "high",
			Complete:   true,
			ChainValid: true,
		},
	}
	scorecard := BuildValidationScorecard(
		[]string{"baseline.jhlog"},
		[]string{"candidate.jhlog"},
		Compare(baseline, candidate),
	)
	if scorecard.DataQuality.BaselineCollectionQuality.KnownLostEvents != 14508 ||
		len(scorecard.DataQuality.CohortWarnings) != 0 ||
		len(scorecard.DataQuality.QualityWarnings) == 0 {
		t.Fatalf("scorecard quality = %+v", scorecard.DataQuality)
	}
	if !strings.Contains(scorecard.Scores["data_quality"].Evidence, "14508 событий") ||
		!warningsContain(scorecard.Scores["data_quality"].NextActions, "14508 событий") {
		t.Fatalf("quality evidence/actions = %+v", scorecard.Scores["data_quality"])
	}
	if scorecard.Summary.GoNoGo != "blocked" {
		t.Fatalf("go/no-go = %q, want blocked for low collection quality", scorecard.Summary.GoNoGo)
	}
	raw, err := json.Marshal(scorecard)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"baseline_collection_quality":{"level":"low"`,
		`"known_lost_events":14508`,
		`"candidate_collection_quality":{"level":"high"`,
	} {
		if !bytes.Contains(raw, []byte(fragment)) {
			t.Fatalf("scorecard JSON %s does not contain %s", raw, fragment)
		}
	}
}

func TestEvaluateGateFailsOnLeakRegressionThresholds(t *testing.T) {
	comparison := Compare(
		Summary{
			LogCount:   5,
			EventCount: 500,
			MemoryLeaks: []MemoryLeakSuspect{
				{
					ClassName:        "com.app.checkout.CheckoutActivity",
					Holder:           "CheckoutPresenter",
					Count:            1,
					MaxAgeMS:         10_000,
					Score:            4,
					Severity:         "medium",
					ChainFingerprint: "runtime:checkout-activity",
				},
			},
		},
		Summary{
			LogCount:   5,
			EventCount: 500,
			MemoryLeaks: []MemoryLeakSuspect{
				{
					ClassName:           "com.app.checkout.CheckoutActivity",
					Holder:              "CheckoutPresenter",
					Count:               8,
					MaxAgeMS:            70_000,
					EstimatedRetainedKB: 20 * 1024,
					Score:               22,
					Severity:            "high",
					ChainFingerprint:    "runtime:checkout-activity",
				},
				{
					ClassName:        "com.app.payment.PaymentActivity",
					Holder:           "PaymentSingleton",
					Count:            1,
					MaxAgeMS:         30_000,
					Score:            17,
					Severity:         "high",
					ChainFingerprint: "runtime:payment-activity",
				},
			},
		},
	)

	result := EvaluateGate(comparison, ThresholdConfig{
		Leaks: LeakThreshold{
			FailOnNew:          true,
			FailOnWorse:        true,
			FailOnNewHigh:      true,
			MaxCandidateTotal:  1,
			MaxHigh:            1,
			RequireHeapForHigh: true,
		},
	})
	if !result.Failed {
		t.Fatalf("expected leak gate failure")
	}
	failures := strings.Join(result.Failures, "\n")
	for _, want := range []string{
		"candidate_total=2",
		"fail_on_new=true",
		"fail_on_worse=true",
		"new high severity",
		"high severity without heap evidence",
	} {
		if !strings.Contains(failures, want) {
			t.Fatalf("expected failure containing %q, got %q", want, failures)
		}
	}
}

func namedValuesByName(values []NamedValue) map[string]NamedValue {
	out := map[string]NamedValue{}
	for _, value := range values {
		out[value.Name] = value
	}
	return out
}

func memoryLeakByClass(values []MemoryLeakSuspect, className string) (MemoryLeakSuspect, bool) {
	for _, value := range values {
		if value.ClassName == className {
			return value, true
		}
	}
	return MemoryLeakSuspect{}, false
}

func deltasByName(values []Delta) map[string]Delta {
	out := map[string]Delta{}
	for _, value := range values {
		out[value.Name] = value
	}
	return out
}

func TestSignedUint64DeltaFloatDoesNotWrapThroughInt64(t *testing.T) {
	maximum := ^uint64(0)
	if got, want := signedUint64DeltaFloat(0, maximum), float64(maximum); got != want {
		t.Fatalf("positive delta = %v, want %v", got, want)
	}
	if got, want := signedUint64DeltaFloat(maximum, 0), -float64(maximum); got != want {
		t.Fatalf("negative delta = %v, want %v", got, want)
	}

	result := delta("counter", maximum, 0, "count", false, 1)
	if result.ChangeAbs >= 0 || result.RegressionAbs <= 0 || result.ChangePct != -100 {
		t.Fatalf("extreme delta wrapped or lost sign: %+v", result)
	}
}

func TestCollectorMergesTypedUIFrameHistogramsAcrossWindows(t *testing.T) {
	dict := map[uint64]string{1: "Feed"}
	first := make([]uint64, jhlog.UIFrameHistogramBucketCount)
	first[1], first[8] = 95, 5
	second := make([]uint64, jhlog.UIFrameHistogramBucketCount)
	second[5], second[8] = 95, 5
	summary := inspectLogsForTest("ui", []jhlog.Log{{Dict: dict, Events: []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorFPS | jhlog.CollectorJankStats)}},
		{Type: jhlog.EventUIWindow, TimeMS: 10_000, Attribution: attributionForTest(1, 0, 0, 0), UIWindow: &jhlog.UIWindowEvent{WindowMS: 10_000, FrameCount: 100, JankCount: 5, Source: jhlog.UIFrameSourceJankStats, FrameDeadlineUS: 16_667, FrameDurationBuckets: first}},
		{Type: jhlog.EventUIWindow, TimeMS: 20_000, Attribution: attributionForTest(1, 0, 0, 0), UIWindow: &jhlog.UIWindowEvent{WindowMS: 10_000, FrameCount: 100, JankCount: 5, Source: jhlog.UIFrameSourceJankStats, FrameDeadlineUS: 16_667, FrameDurationBuckets: second}},
	}}})
	if len(summary.Screens) != 1 {
		t.Fatalf("screens = %+v", summary.Screens)
	}
	screen := summary.Screens[0]
	if screen.FrameP50MS != 32 || screen.FrameP95MS != 32 || screen.FrameP99MS != 67 ||
		screen.FrameSource != "jankstats" || screen.FrameDeadlineUS != 16_667 ||
		screen.FrameDistributionState != "mergeable_histogram_v2" {
		t.Fatalf("merged screen = %+v", screen)
	}
	if summary.CollectorFlagsAll != uint64(jhlog.CollectorFPS|jhlog.CollectorJankStats) {
		t.Fatalf("collector flags = 0x%x", summary.CollectorFlagsAll)
	}
	quality := semanticEvidenceQuality(summary)
	if quality.Status != EvidenceQualityComplete {
		t.Fatalf("semantic evidence = %+v", quality)
	}
}

func TestRouteBurstAccumulatorKeepsPeakInBoundedState(t *testing.T) {
	var burst routeBurstAccumulator
	for index := uint64(0); index < 12; index++ {
		burst.add(1, 2_000+index)
	}
	for second := uint64(3); second < 20; second++ {
		burst.add(1, second*1_000)
	}
	if burst.peak != 12 || burst.peakWindowStartMS != 2_000 || len(burst.buckets) != routeBurstRetainedSeconds || !burst.approximate {
		t.Fatalf("burst = %+v", burst)
	}
}

func TestMaxHTTPConcurrencyKeepsLogsIndependentAndIntervalsEndExclusive(t *testing.T) {
	intervals := []httpInterval{
		{logIndex: 2, startMS: 0, endMS: 100},
		{logIndex: 1, startMS: 100, endMS: 200},
		{logIndex: 1, startMS: 0, endMS: 100},
		{logIndex: 2, startMS: 25, endMS: 75},
		{logIndex: 2, startMS: 50, endMS: 125},
	}
	peak, peakAtMS := maxHTTPConcurrency(intervals)
	if peak != 3 || peakAtMS != 50 {
		t.Fatalf("concurrency = %d at %d ms, want 3 at 50 ms", peak, peakAtMS)
	}
}

func warningsContain(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func assertNamedValue(t *testing.T, values []NamedValue, name string, want uint64) {
	t.Helper()
	for _, value := range values {
		if value.Name == name {
			if value.Value != want {
				t.Fatalf("named value %q = %d, want %d", name, value.Value, want)
			}
			return
		}
	}
	t.Fatalf("named value %q is absent in %+v", name, values)
}

func inspectLogsForTest(title string, logs []jhlog.Log) Summary {
	collector := newCollector(title, len(logs), Options{})
	for _, log := range logs {
		collector.startLog(log.Result.Header)
		collector.operationAnalysis.startLog(log.Result.Header)
		collector.summary.Dictionary += len(log.Dict)
		for _, event := range log.Events {
			collector.add(log.Dict, event)
		}
		collector.finishLog()
	}
	return collector.finish()
}

func inspectFilesForTest(title string, paths []string) (Summary, error) {
	return InspectFilesWithOptions(title, paths, Options{})
}

func inspectFilesWithFilterForTest(title string, paths []string, filter Filter) (Summary, error) {
	return InspectFilesWithOptions(title, paths, Options{Filter: filter})
}

func readJhlogForTest(t *testing.T, path string) jhlog.Log {
	t.Helper()

	log := jhlog.Log{
		Source: path,
		Dict:   map[uint64]string{},
		Kinds:  map[uint64]jhlog.DictKind{},
	}
	result, err := jhlog.StreamFileWithResult(path, func(event jhlog.Event, _ map[uint64]string) error {
		if event.Dictionary != nil {
			if event.Dictionary.Kind == jhlog.DictStableSymbol {
				log.Events = append(log.Events, event)
			} else {
				log.Dict[event.Dictionary.ID] = event.Dictionary.Value
				log.Kinds[event.Dictionary.ID] = event.Dictionary.Kind
			}
		}
		if event.Type.IsSemanticData() {
			log.Events = append(log.Events, event)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamFileWithResult(%q) error = %v", path, err)
	}
	log.Result = result
	return log
}

func attributionForTest(screenID, ownerID, _, _ uint64) jhlog.AttributionContext {
	return jhlog.AttributionContext{
		Present: true,
		Screen:  jhlog.LocalSymbol(screenID),
		Owner:   jhlog.LocalSymbol(ownerID),
	}
}
