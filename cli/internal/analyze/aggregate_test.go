package analyze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestInspectSampleIncludesFPSAndGauges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	log, err := jhlog.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	summary := Inspect("sample", []jhlog.Log{log})
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
	if len(summary.Flows) == 0 {
		t.Fatalf("expected flow attribution")
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
	if !codeProblemsHaveSignal(summary.CodeProblems, "Подозрение утечки памяти") {
		t.Fatalf("expected memory leak signal in code problem registry: %+v", summary.CodeProblems)
	}
}

func TestInspectFilesAppliesOwnerMap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sample.jhlog")
	if err := jhlog.WriteSample(logPath); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	mapPath := filepath.Join(dir, "owner-map.json")
	if err := os.WriteFile(mapPath, []byte(`{"owners":{"FeedRepository.refresh":"feed owner"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ownerMap, err := LoadOwnerMap(mapPath)
	if err != nil {
		t.Fatalf("LoadOwnerMap() error = %v", err)
	}

	summary, err := InspectFilesWithOptions("sample", []string{logPath}, Options{OwnerMap: ownerMap})
	if err != nil {
		t.Fatalf("InspectFilesWithOptions() error = %v", err)
	}
	found := false
	for _, owner := range summary.Owners {
		if owner.Owner == "feed owner" {
			found = true
		}
	}
	if !found {
		t.Fatalf("owner map was not applied: %+v", summary.Owners)
	}
}

func TestInspectFilesStreamsSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	summary, err := InspectFiles("sample", []string{path})
	if err != nil {
		t.Fatalf("InspectFiles() error = %v", err)
	}
	if summary.EventCount == 0 || summary.HTTPCount != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Dictionary == 0 {
		t.Fatalf("expected dictionary count")
	}
	if len(summary.Processes) != 1 || summary.Processes[0].Name != "main" {
		t.Fatalf("unexpected processes: %+v", summary.Processes)
	}
	if len(summary.RetainedClasses) != 1 || summary.RetainedClasses[0].Name != "com.app.checkout.CheckoutActivity" {
		t.Fatalf("unexpected retained classes: %+v", summary.RetainedClasses)
	}
	if len(summary.RetainedAgeBuckets) != 1 || summary.RetainedAgeBuckets[0].Name != "10s-30s" {
		t.Fatalf("unexpected retained age buckets: %+v", summary.RetainedAgeBuckets)
	}
	if len(summary.MemoryLeaks) != 1 {
		t.Fatalf("unexpected memory leak suspects: %+v", summary.MemoryLeaks)
	}
	leak := summary.MemoryLeaks[0]
	if leak.ClassName != "com.app.checkout.CheckoutActivity" || leak.Holder != "CheckoutPresenter.render" {
		t.Fatalf("unexpected memory leak attribution: %+v", leak)
	}
	if leak.Screen != "CheckoutScreen" || leak.Flow != "checkout.open" || leak.Step != "render_list" {
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
	if summary.TotalMemoryKB == 0 || summary.FreeStorageKB == 0 {
		t.Fatalf("expected memory/storage context: %+v", summary)
	}
	if len(summary.Cohorts) == 0 {
		t.Fatalf("expected cohorts")
	}
}

func TestInspectFilesFiltersRetainedObjectsByClass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(path); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}

	matching, err := InspectFilesWithFilter("sample", []string{path}, Filter{ClassContains: "CheckoutActivity"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter(class match) error = %v", err)
	}
	if len(matching.MemoryLeaks) != 1 || matching.MemoryLeaks[0].ClassName != "com.app.checkout.CheckoutActivity" {
		t.Fatalf("expected checkout leak with class filter: %+v", matching.MemoryLeaks)
	}

	nonMatching, err := InspectFilesWithFilter("sample", []string{path}, Filter{ClassContains: "FeedActivity"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter(class miss) error = %v", err)
	}
	if len(nonMatching.MemoryLeaks) != 0 || nonMatching.Retained != 0 {
		t.Fatalf("expected retained objects to be filtered by class: leaks=%+v retained=%d", nonMatching.MemoryLeaks, nonMatching.Retained)
	}

	ownerOnly, err := InspectFilesWithFilter("sample", []string{path}, Filter{OwnerContains: "CheckoutActivity"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter(owner class name) error = %v", err)
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
				RouteID:    1,
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

	summary, err := InspectFiles("sample", []string{path})
	if err != nil {
		t.Fatalf("InspectFiles() error = %v", err)
	}
	if summary.DurationMS != 120_000 {
		t.Fatalf("DurationMS = %d, want 120000", summary.DurationMS)
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
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictFlow, ID: 4, Value: "feed.refresh"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStep, ID: 5, Value: "network"}},
		{Type: jhlog.EventFlow, TimeMS: 1, Flow: &jhlog.FlowEvent{ScreenID: 2, OwnerID: 3, FlowID: 4, StepID: 5}},
		{Type: jhlog.EventHTTP, TimeMS: 2, HTTP: &jhlog.HTTPEvent{RouteID: 1, DurationMS: 100, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 3, HTTP: &jhlog.HTTPEvent{RouteID: 1, DurationMS: 1000, Status: jhlog.Status2xx}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	summary, err := InspectFiles("sample", []string{path})
	if err != nil {
		t.Fatalf("InspectFiles() error = %v", err)
	}
	if summary.HTTPP95MS != 1000 {
		t.Fatalf("HTTPP95MS = %d, want 1000", summary.HTTPP95MS)
	}
	if len(summary.Routes) != 1 || summary.Routes[0].P95MS != 1000 {
		t.Fatalf("route p95 = %+v, want 1000", summary.Routes)
	}
	if len(summary.Flows) != 1 || summary.Flows[0].HTTPP95MS != 1000 {
		t.Fatalf("flow p95 = %+v, want 1000", summary.Flows)
	}
}

func TestInspectFilesResetsFlowContextBetweenLogs(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jhlog")
	firstFile, firstWriter, err := jhlog.Create(first)
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	firstEvents := []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictScreen, ID: 1, Value: "CheckoutScreen"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictOwner, ID: 2, Value: "CheckoutPresenter.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictFlow, ID: 3, Value: "checkout.open"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStep, ID: 4, Value: "render_list"}},
		{Type: jhlog.EventFlow, TimeMS: 1, Flow: &jhlog.FlowEvent{ScreenID: 1, OwnerID: 2, FlowID: 3, StepID: 4}},
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
		{Type: jhlog.EventHTTP, TimeMS: 1, HTTP: &jhlog.HTTPEvent{RouteID: 1, DurationMS: 120, Status: jhlog.Status2xx}},
	}
	for _, event := range secondEvents {
		if err := secondWriter.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(second %d) error = %v", event.Type, err)
		}
	}
	if err := secondFile.Close(); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}

	summary, err := InspectFiles("sample", []string{first, second})
	if err != nil {
		t.Fatalf("InspectFiles() error = %v", err)
	}
	if len(summary.Flows) != 1 {
		t.Fatalf("Flows = %+v, want exactly one HTTP flow", summary.Flows)
	}
	flow := summary.Flows[0]
	if flow.Screen != "unknown" || flow.Flow != "unknown" || flow.Step != "unknown" || flow.Owner != "unknown" {
		t.Fatalf("second log inherited stale flow context: %+v", flow)
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

	summary, err := InspectFilesWithFilter("sample", []string{path}, Filter{RouteContains: "/checkout"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter() error = %v", err)
	}
	if summary.HTTPCount != 1 {
		t.Fatalf("HTTPCount = %d, want 1", summary.HTTPCount)
	}
	if len(summary.Routes) != 1 || summary.Routes[0].Route != "POST /checkout" {
		t.Fatalf("unexpected routes: %+v", summary.Routes)
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
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictFlow, ID: 7, Value: "feed.open"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictFlow, ID: 8, Value: "checkout.open"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStep, ID: 9, Value: "render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictLogSource, ID: 10, Value: "FeedLogger.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictLogSource, ID: 11, Value: "CheckoutLogger.render"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 12, Value: "main_thread_stall"}},
		{Type: jhlog.EventLogSpam, TimeMS: 1, LogSpam: &jhlog.LogSpamEvent{ScreenID: 1, OwnerID: 3, FlowID: 7, StepID: 9, SourceID: 10, Level: 5, Count: 3}},
		{Type: jhlog.EventLogSpam, TimeMS: 2, LogSpam: &jhlog.LogSpamEvent{ScreenID: 2, OwnerID: 4, FlowID: 8, StepID: 9, SourceID: 11, Level: 5, Count: 5}},
		{Type: jhlog.EventProblem, TimeMS: 3, Problem: &jhlog.ProblemEvent{ScreenID: 1, OwnerID: 3, FlowID: 7, StepID: 9, KindID: 12, WindowMS: 5000, Count: 2, MaxMS: 80}},
		{Type: jhlog.EventProblem, TimeMS: 4, Problem: &jhlog.ProblemEvent{ScreenID: 2, OwnerID: 4, FlowID: 8, StepID: 9, KindID: 12, WindowMS: 5000, Count: 4, MaxMS: 120}},
		{Type: jhlog.EventRuntimeCall, TimeMS: 5, RuntimeCall: &jhlog.RuntimeCallEvent{ScreenID: 1, CallerID: 3, FlowID: 7, StepID: 9, CalleeID: 5, Count: 1, TotalMS: 20, MaxMS: 20}},
		{Type: jhlog.EventRuntimeCall, TimeMS: 6, RuntimeCall: &jhlog.RuntimeCallEvent{ScreenID: 2, CallerID: 4, FlowID: 8, StepID: 9, CalleeID: 6, Count: 1, TotalMS: 30, MaxMS: 30}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent(%d) error = %v", event.Type, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	feedOnly, err := InspectFilesWithFilter("sample", []string{path}, Filter{ScreenContains: "FeedScreen"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter(screen) error = %v", err)
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

	loggerOnly, err := InspectFilesWithFilter("sample", []string{path}, Filter{ClassContains: "FeedLogger"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter(class) error = %v", err)
	}
	if len(loggerOnly.LogSpam) != 1 || loggerOnly.LogSpam[0].Source != "FeedLogger.render" {
		t.Fatalf("class filter did not select log source: %+v", loggerOnly.LogSpam)
	}
	if len(loggerOnly.ProblemWindows) != 0 || len(loggerOnly.RuntimeCalls) != 0 {
		t.Fatalf("class filter leaked non-matching signals: problems=%+v runtime=%+v", loggerOnly.ProblemWindows, loggerOnly.RuntimeCalls)
	}

	calleeOnly, err := InspectFilesWithFilter("sample", []string{path}, Filter{OwnerContains: "FeedCallee"})
	if err != nil {
		t.Fatalf("InspectFilesWithFilter(owner callee) error = %v", err)
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
		},
		Events: []jhlog.Event{
			{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricID: 1, Value: 3}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricID: 2, Value: 18}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricID: 2, Value: 22}},
		},
	}

	summary := Inspect("jankstats", []jhlog.Log{log})
	if len(summary.JankStats) != 2 {
		t.Fatalf("unexpected jankstats metrics: %+v", summary.JankStats)
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

func TestEvaluateGateFailsOnMinConfidenceOnly(t *testing.T) {
	comparison := Compare(
		Summary{LogCount: 1, EventCount: 10, HTTPCount: 3, HTTPP95MS: 100},
		Summary{LogCount: 1, EventCount: 10, HTTPCount: 3, HTTPP95MS: 100},
	)

	result := EvaluateGate(comparison, ThresholdConfig{MinConfidence: "medium"})
	if !result.Failed {
		t.Fatalf("expected confidence gate failure")
	}
}
