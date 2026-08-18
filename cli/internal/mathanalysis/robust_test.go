package mathanalysis

import (
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

var benchmarkRobustStat RobustStat

func BenchmarkSummarizeRobustSetRepeatedMillion(b *testing.B) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /repeated", Metric: "HTTP задержка", Unit: "мс"}
	for range b.N {
		set := &robustSampleSet{}
		for index := range 1_000_000 {
			set.add(float64(index % 1_500))
		}
		benchmarkRobustStat = summarizeRobustSet(key, set)
	}
}

func BenchmarkSummarizeRobustSetUniqueMillion(b *testing.B) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /unique", Metric: "HTTP задержка", Unit: "мс"}
	for range b.N {
		set := &robustSampleSet{}
		for index := range 1_000_000 {
			set.add(float64(index))
		}
		benchmarkRobustStat = summarizeRobustSet(key, set)
	}
}

func TestSummarizeRobustSetComputesDeterministicStats(t *testing.T) {
	set := &robustSampleSet{}
	for i := 1; i <= 100; i++ {
		set.add(float64(i))
	}

	stat := summarizeRobustSet(robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "мс"}, set)

	assertFloat(t, stat.Median, 50.5)
	assertFloat(t, stat.P90, 90)
	assertFloat(t, stat.P95, 95)
	assertFloat(t, stat.P99, 99)
	assertFloat(t, stat.MAD, 25)
	assertFloat(t, stat.TrimmedMean, 50.5)
	if !stat.HasP95Confidence {
		t.Fatalf("expected p95 bootstrap CI")
	}
	if stat.SampleQuality != "хорошая" || stat.SampleQualitySeverity != "ok" {
		t.Fatalf("unexpected sample quality: %+v", stat)
	}
}

func TestSummarizeRobustSetKeepsEverySampleBeyondFormerReservoirBoundary(t *testing.T) {
	set := &robustSampleSet{}
	const total = 20_025
	for i := 1; i <= total; i++ {
		set.add(float64(i))
	}

	stat := summarizeRobustSet(
		robustKey{Dimension: "Маршрут", Name: "GET /history", Metric: "HTTP задержка", Unit: "мс"},
		set,
	)

	if stat.Count != total || stat.P95 != 19_024 || stat.SampleDetail != "сэмплов=20025" {
		t.Fatalf("robust distribution is not exact: %+v", stat)
	}
	if set.compacted() || len(set.values) != total {
		t.Fatalf("unique exact distribution unexpectedly changed representation: values=%d dense=%d", len(set.values), len(set.denseCounts))
	}
}

func TestSummarizeRobustSetCompactsRepeatedValuesWithoutChangingStatistics(t *testing.T) {
	set := &robustSampleSet{}
	values := make([]float64, 0, 100_000)
	for index := range 100_000 {
		value := float64(index % 1_500)
		set.add(value)
		values = append(values, value)
	}
	sort.Float64s(values)
	wantMedian := medianSorted(values)
	wantLow, wantHigh, wantCI := bootstrapP95CI(values)

	stat := summarizeRobustSet(
		robustKey{Dimension: "Маршрут", Name: "GET /compact", Metric: "HTTP задержка", Unit: "мс"},
		set,
	)

	if !set.compacted() || len(set.values) != 0 || len(set.denseCounts) != 1_500 {
		t.Fatalf("repeated robust set was not compacted: values=%d dense=%d", len(set.values), len(set.denseCounts))
	}
	for label, values := range map[string][2]float64{
		"median":       {stat.Median, wantMedian},
		"p90":          {stat.P90, percentileSorted(values, 0.90)},
		"p95":          {stat.P95, percentileSorted(values, 0.95)},
		"p99":          {stat.P99, percentileSorted(values, 0.99)},
		"mad":          {stat.MAD, medianAbsoluteDeviation(values, wantMedian)},
		"trimmed mean": {stat.TrimmedMean, trimmedMeanSorted(values, 0.10)},
		"ci low":       {stat.P95ConfidenceLow, wantLow},
		"ci high":      {stat.P95ConfidenceHigh, wantHigh},
	} {
		if values[0] != values[1] {
			t.Fatalf("%s changed after exact compaction: got=%f want=%f", label, values[0], values[1])
		}
	}
	if stat.HasP95Confidence != wantCI || stat.Count != len(values) || stat.Min != 0 || stat.Max != 1_499 {
		t.Fatalf("compacted robust metadata changed: %+v", stat)
	}
}

func TestCompactedRobustSetPreservesOutliersAndWeightedCliffsDelta(t *testing.T) {
	baseline := &robustSampleSet{}
	candidate := &robustSampleSet{}
	for index := range 10_000 {
		baseline.add(float64(index % 100))
		candidate.add(float64(100 + index%100))
	}
	candidate.add(1_000_000)
	if got := candidate.percentile(1); got != 1_000_000 {
		t.Fatalf("compacted robust set lost max outlier: %f", got)
	}
	if got := cliffDeltaSets(candidate, baseline); got != 1 {
		t.Fatalf("weighted Cliff's delta = %f, want 1", got)
	}
}

func TestCompactedRobustSetMatchesSortedReferenceAcrossMixedDistributions(t *testing.T) {
	random := rand.New(rand.NewSource(32_597))
	for scenario := range 32 {
		values := make([]float64, 0, 6_144)
		set := &robustSampleSet{}
		for index := 0; index < cap(values); index++ {
			var value float64
			switch {
			case index < 4_096:
				value = float64(random.Intn(257))
			case index%11 == 0:
				value = -float64(random.Intn(1_000)) - 0.25
			case index%7 == 0:
				value = float64(random.Intn(2_000)) + 0.5
			case index%5 == 0:
				value = 1_000_000 + float64(random.Intn(10_000))
			default:
				value = float64(random.Intn(257))
			}
			values = append(values, value)
			set.add(value)
		}
		if !set.compacted() {
			t.Fatalf("scenario %d did not exercise dense promotion", scenario)
		}

		sorted := sortedFloatCopy(values)
		for _, percentile := range []float64{-1, 0, 0.01, 0.50, 0.90, 0.95, 0.99, 1, 2} {
			if got, want := set.percentile(percentile), percentileSorted(sorted, percentile); got != want {
				t.Fatalf("scenario %d percentile %.2f = %f, want %f", scenario, percentile, got, want)
			}
		}
		if got, want := set.median(), medianSorted(sorted); got != want {
			t.Fatalf("scenario %d median = %f, want %f", scenario, got, want)
		}
		assertFloat(t, set.medianAbsoluteDeviation(set.median()), medianAbsoluteDeviation(sorted, medianSorted(sorted)))
		for _, ratio := range []float64{0, 0.10, 0.25, 0.50} {
			assertFloat(t, set.trimmedMean(ratio), trimmedMeanSorted(sorted, ratio))
		}
		for _, rank := range []int{-1, 0, len(sorted) / 2, len(sorted) - 1, len(sorted) + 1} {
			bounded := min(max(rank, 0), len(sorted)-1)
			if got, want := set.valueAt(rank), sorted[bounded]; got != want {
				t.Fatalf("scenario %d rank %d = %f, want %f", scenario, rank, got, want)
			}
		}
		for _, probe := range []float64{-1_001, -0.25, 0, 128, 256.5, 1_000_000, 1_010_000} {
			less, greater := set.lessAndGreater(probe)
			wantLess := sort.SearchFloat64s(sorted, probe)
			greaterStart := sort.Search(len(sorted), func(index int) bool { return sorted[index] > probe })
			if less != uint64(wantLess) || greater != uint64(len(sorted)-greaterStart) {
				t.Fatalf(
					"scenario %d probe %f = (%d,%d), want (%d,%d)",
					scenario,
					probe,
					less,
					greater,
					wantLess,
					len(sorted)-greaterStart,
				)
			}
		}
	}
}

func TestWeightedCliffsDeltaMatchesSortedReferenceAfterDensePromotion(t *testing.T) {
	random := rand.New(rand.NewSource(325_970))
	for scenario := range 24 {
		baselineValues := make([]float64, 0, 5_000)
		candidateValues := make([]float64, 0, 5_000)
		baseline := &robustSampleSet{}
		candidate := &robustSampleSet{}
		for index := range 5_000 {
			baseValue := float64(random.Intn(97))
			candidateValue := float64(random.Intn(97) + scenario%13)
			if index >= 4_096 && index%17 == 0 {
				baseValue = -0.5 - float64(random.Intn(100))
				candidateValue = 1_000.5 + float64(random.Intn(100))
			}
			baselineValues = append(baselineValues, baseValue)
			candidateValues = append(candidateValues, candidateValue)
			baseline.add(baseValue)
			candidate.add(candidateValue)
		}
		if !baseline.compacted() || !candidate.compacted() {
			t.Fatalf("scenario %d did not compact both distributions", scenario)
		}
		want := cliffDeltaSorted(sortedFloatCopy(candidateValues), sortedFloatCopy(baselineValues))
		if got := cliffDeltaSets(candidate, baseline); math.Abs(got-want) > 1e-12 {
			t.Fatalf("scenario %d Cliff's delta = %.15f, want %.15f", scenario, got, want)
		}
	}
}

func TestCompareRobustSamplesComputesCliffsDelta(t *testing.T) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "мс"}
	baseline := robustSampleMap{key: robustSet(1, 2, 3)}
	candidate := robustSampleMap{key: robustSet(2, 3, 4)}

	deltas := compareRobustSamples(baseline, candidate)
	if len(deltas) != 1 {
		t.Fatalf("len(deltas) = %d, want 1", len(deltas))
	}

	delta := deltas[0]
	assertFloat(t, delta.BaselineP95, 3)
	assertFloat(t, delta.CandidateP95, 4)
	assertFloat(t, delta.P95DeltaPct, 100.0/3.0)
	assertFloat(t, delta.CliffDelta, 5.0/9.0)
	if delta.EffectSize != "крупный" {
		t.Fatalf("EffectSize = %q, want крупный", delta.EffectSize)
	}
	if delta.Confidence != "низкое: нужна повторная выборка" {
		t.Fatalf("Confidence = %q, want low repeatability warning", delta.Confidence)
	}
}

func TestCompareRobustSamplesCalibratesHighConfidenceRegression(t *testing.T) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "мс"}
	baseValues := make([]float64, 0, 80)
	candidateValues := make([]float64, 0, 80)
	for i := 0; i < 80; i++ {
		baseValues = append(baseValues, 100+float64(i%5))
		candidateValues = append(candidateValues, 180+float64(i%5))
	}
	deltas := compareRobustSamples(
		robustSampleMap{key: robustSet(baseValues...)},
		robustSampleMap{key: robustSet(candidateValues...)},
	)
	if len(deltas) != 1 {
		t.Fatalf("len(deltas) = %d, want 1", len(deltas))
	}
	if deltas[0].Severity != "high" {
		t.Fatalf("Severity = %q, want high: %+v", deltas[0].Severity, deltas[0])
	}
	if deltas[0].Confidence != "высокое: повторяемая выборка и крупный эффект" {
		t.Fatalf("Confidence = %q, want calibrated high", deltas[0].Confidence)
	}
}

func TestCompareRobustSamplesAvoidsHighSeverityForTinySample(t *testing.T) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "мс"}
	deltas := compareRobustSamples(
		robustSampleMap{key: robustSet(100, 101, 102)},
		robustSampleMap{key: robustSet(220, 221, 222)},
	)
	if len(deltas) != 1 {
		t.Fatalf("len(deltas) = %d, want 1", len(deltas))
	}
	if deltas[0].Severity != "medium" {
		t.Fatalf("Severity = %q, want medium for tiny but strong effect: %+v", deltas[0].Severity, deltas[0])
	}
	if deltas[0].Confidence != "низкое: нужна повторная выборка" {
		t.Fatalf("Confidence = %q, want low repeatability warning", deltas[0].Confidence)
	}
}

func TestAnalyzeInspectBuildsRobustStats(t *testing.T) {
	path := writeRobustFixture(t)

	report, err := analyzeInspectForTest(t, []string{path}, analyze.Options{})
	if err != nil {
		t.Fatalf("analyzeInspectForTest() error = %v", err)
	}

	route := findRobustStat(report.RobustStats, "Маршрут", "GET /feed", "HTTP задержка")
	if route == nil {
		t.Fatalf("route robust stat not found: %#v", report.RobustStats)
	}
	if route.Count != 3 || route.P95 != 300 {
		t.Fatalf("unexpected route stat: %+v", *route)
	}

	screen := findRobustStat(report.RobustStats, "Экран", "FeedScreen", "UI window-p95")
	if screen == nil {
		t.Fatalf("screen robust stat not found: %#v", report.RobustStats)
	}
	if screen.Count != 2 || screen.P95 != 32 {
		t.Fatalf("unexpected screen stat: %+v", *screen)
	}

	owner := findRobustStat(report.RobustStats, "Источник", "FeedRepository.refresh", "Пауза главного потока")
	if owner == nil {
		t.Fatalf("owner robust stat not found: %#v", report.RobustStats)
	}
	if owner.Count != 1 || owner.P95 != 42 {
		t.Fatalf("unexpected owner stat: %+v", *owner)
	}

	gauge := findRobustStat(report.RobustStats, "Gauge-метрика", "executor.queue.depth", "Значение")
	if gauge == nil {
		t.Fatalf("gauge robust stat not found: %#v", report.RobustStats)
	}
	if gauge.Count != 3 || gauge.Median != 20 || gauge.P95 != 30 {
		t.Fatalf("unexpected gauge stat: %+v", *gauge)
	}
}

func TestCompareRobustSamplesDoesNotGuessGaugeDirection(t *testing.T) {
	key := robustKey{Dimension: "Gauge-метрика", Name: "cache.hit.ratio", Metric: "Значение", Unit: "знач."}
	baselineValues := make([]float64, 80)
	candidateValues := make([]float64, 80)
	for index := range baselineValues {
		baselineValues[index] = 10
		candidateValues[index] = 90
	}

	deltas := compareRobustSamples(
		robustSampleMap{key: robustSet(baselineValues...)},
		robustSampleMap{key: robustSet(candidateValues...)},
	)

	if len(deltas) != 1 || deltas[0].Severity != "ok" {
		t.Fatalf("custom gauge direction must stay neutral: %+v", deltas)
	}
	if !deltas[0].Comparable || !deltas[0].DeltaPctAvailable {
		t.Fatalf("gauge distributions should remain numerically comparable: %+v", deltas[0])
	}
}

func TestCompareRobustSamplesMarksMissingDistributionAsNotComparable(t *testing.T) {
	key := robustKey{Dimension: "Маршрут", Name: "GET /items", Metric: "HTTP задержка", Unit: "мс"}
	deltas := compareRobustSamples(nil, robustSampleMap{key: robustSet(100, 120, 140)})

	if len(deltas) != 1 || deltas[0].Comparable || deltas[0].DeltaPctAvailable {
		t.Fatalf("one-sided distribution must not expose comparison statistics: %+v", deltas)
	}
	if deltas[0].EffectSize != "не применимо" {
		t.Fatalf("EffectSize = %q, want not applicable", deltas[0].EffectSize)
	}
}

func TestAnalyzeInspectRobustStatsHonorClassFilter(t *testing.T) {
	path := writeRobustFixture(t)

	report, err := analyzeInspectForTest(t, []string{path}, analyze.Options{
		Filter: analyze.Filter{ClassContains: "CheckoutActivity"},
	})
	if err != nil {
		t.Fatalf("analyzeInspectForTest() error = %v", err)
	}

	checkout := findRobustStat(report.RobustStats, "Источник", "com.app.CheckoutActivity", "Возраст удержанного объекта")
	if checkout == nil {
		t.Fatalf("checkout retained stat not found: %#v", report.RobustStats)
	}
	if checkout.Count != 1 || checkout.P95 != 30_000 {
		t.Fatalf("unexpected checkout retained stat: %+v", *checkout)
	}
	if feed := findRobustStat(report.RobustStats, "Источник", "com.app.FeedActivity", "Возраст удержанного объекта"); feed != nil {
		t.Fatalf("class filter leaked feed retained stat: %+v", *feed)
	}
}

func robustSet(values ...float64) *robustSampleSet {
	set := &robustSampleSet{}
	for _, value := range values {
		set.add(value)
	}
	return set
}

func writeRobustFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "robust.jhlog")
	file, writer, err := jhlog.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	entries := []jhlog.DictionaryEntry{
		{Kind: jhlog.DictOwner, ID: 1, Value: "FeedRepository.refresh"},
		{Kind: jhlog.DictRoute, ID: 2, Value: "GET /feed"},
		{Kind: jhlog.DictScreen, ID: 3, Value: "FeedScreen"},
		{Kind: jhlog.DictMetric, ID: 4, Value: "executor.queue.depth"},
		{Kind: jhlog.DictClass, ID: 5, Value: "com.app.CheckoutActivity"},
		{Kind: jhlog.DictClass, ID: 6, Value: "com.app.FeedActivity"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatalf("WriteEvent(dictionary) error = %v", err)
		}
	}
	events := []jhlog.Event{
		{Type: jhlog.EventHTTP, TimeMS: 100, Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(2), DurationMS: 100, DNSMS: 5, ConnectMS: 10, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 200, Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(2), DurationMS: 200, DNSMS: 7, ConnectMS: 12, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 300, Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(2), DurationMS: 300, DNSMS: 9, ConnectMS: 14, Status: jhlog.Status2xx}},
		{Type: jhlog.EventUIWindow, TimeMS: 1100, Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(3)}, UIWindow: typedUIWindow(1000, 60, 3, 24)},
		{Type: jhlog.EventUIWindow, TimeMS: 2100, Attribution: jhlog.AttributionContext{Present: true, Screen: jhlog.LocalSymbol(3)}, UIWindow: typedUIWindow(1000, 60, 9, 32)},
		{Type: jhlog.EventStall, TimeMS: 2200, Attribution: jhlog.AttributionContext{Present: true, Owner: jhlog.LocalSymbol(1)}, Stall: &jhlog.StallEvent{DurationMS: 42}},
		{Type: jhlog.EventGauge, TimeMS: 2300, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(4), Value: 10}},
		{Type: jhlog.EventGauge, TimeMS: 2400, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(4), Value: 20}},
		{Type: jhlog.EventGauge, TimeMS: 2450, Metric: &jhlog.MetricEvent{MetricRef: jhlog.LocalSymbol(4), Value: 30, Count: 3, Sum: 90, Max: 30, Mode: jhlog.MetricModeAverage}},
		{Type: jhlog.EventRetained, TimeMS: 2500, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(5), AgeMS: 30_000, Count: 1}},
		{Type: jhlog.EventRetained, TimeMS: 2600, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(6), AgeMS: 40_000, Count: 1}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%v) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return path
}

func findRobustStat(stats []RobustStat, dimension, name, metric string) *RobustStat {
	for i := range stats {
		if stats[i].Dimension == dimension && stats[i].Name == name && stats[i].Metric == metric {
			return &stats[i]
		}
	}
	return nil
}

func assertFloat(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("got %.6f, want %.6f", got, want)
	}
}
