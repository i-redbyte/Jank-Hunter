package analyze

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

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
	if !codeProblemsHaveSignal(summary.CodeProblems, "Сигнал удержания памяти") {
		t.Fatalf("expected memory leak signal in code problem registry: %+v", summary.CodeProblems)
	}
}

func TestLoadOwnerMapResolvesNamespacedStableOwner(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "owner-map.json")
	data := `{"format":4,"kind":"metadata","variant":"debug","idAlgorithm":"fnv1a64-canonical-stable-v1","generatedOwners":true,"symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0000000000001234","owner":"com.app.FeedRepository.refresh","class":"com.app.FeedRepository","method":"refresh","descriptor":"()V"}` + "\n"
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ownerMap, err := LoadOwnerMap(mapPath)
	if err != nil {
		t.Fatalf("LoadOwnerMap() error = %v", err)
	}

	collector := collector{ownerMap: ownerMap}
	got := collector.resolveOwnerRef(nil, jhlog.StableSymbolInNamespace(0x1234, ownerMap.SymbolNamespace))
	if got != "com.app.FeedRepository.refresh" {
		t.Fatalf("resolveOwnerRef() = %q", got)
	}
}

func TestLoadOwnerMapRequiresSupportedFormat(t *testing.T) {
	validNamespace := "aabb0000000000000000000000000000"
	tests := map[string]string{
		"missing format":       `{"kind":"metadata","symbolNamespace":"` + validNamespace + `"}`,
		"old format":           `{"format":3,"kind":"metadata","symbolNamespace":"` + validNamespace + `"}`,
		"missing namespace":    `{"format":4,"kind":"metadata"}`,
		"short namespace":      `{"format":4,"kind":"metadata","symbolNamespace":"aabb"}`,
		"uppercase namespace":  `{"format":4,"kind":"metadata","symbolNamespace":"AABB0000000000000000000000000000"}`,
		"whitespace namespace": `{"format":4,"kind":"metadata","symbolNamespace":" ` + validNamespace + `"}`,
		"namespace on entry": `{"format":4,"kind":"metadata","symbolNamespace":"` + validNamespace + `"}` + "\n" +
			`{"format":4,"kind":"entry","symbolNamespace":"` + validNamespace + `","id":"stable:0x0123456789abcdef","owner":"com.app.Owner.call"}`,
		"conflicting namespace": `{"format":4,"kind":"metadata","symbolNamespace":"` + validNamespace + `"}` + "\n" +
			`{"format":4,"kind":"metadata","symbolNamespace":"ccdd0000000000000000000000000000"}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			mapPath := filepath.Join(t.TempDir(), "owner-map.json")
			if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			if _, err := LoadOwnerMap(mapPath); err == nil {
				t.Fatalf("LoadOwnerMap() accepted %s owner map", name)
			}
		})
	}
}

func TestLoadOwnerMapReadsJSONLEntries(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "owner-map.json")
	data := `{"format":4,"kind":"metadata","variant":"debug","generatedOwners":true,"symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.FeedRepository.refresh","class":"com.app.FeedRepository","method":"refresh","descriptor":"()V"}` + "\n"
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ownerMap, err := LoadOwnerMap(mapPath)
	if err != nil {
		t.Fatalf("LoadOwnerMap() error = %v", err)
	}
	if got := ownerMap.Entries["stable:0x0123456789abcdef"]; got != "com.app.FeedRepository.refresh" {
		t.Fatalf("ownerMap[stable ID] = %q", got)
	}
	if len(ownerMap.Entries) != 1 {
		t.Fatalf("ownerMap contains unexpected aliases: %+v", ownerMap)
	}
}

func TestLoadOwnerMapsMergesModuleEntriesWithSharedNamespace(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app-owner-map.json")
	featurePath := filepath.Join(dir, "feature-owner-map.json")
	const namespace = "aabb0000000000000000000000000000"
	appData := `{"format":4,"kind":"metadata","symbolNamespace":"` + namespace + `"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0000000000000001","owner":"com.app.MainActivity.render"}` + "\n"
	featureData := `{"format":4,"kind":"metadata","symbolNamespace":"` + namespace + `"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0000000000000002","owner":"com.app.feed.FeedPresenter.load"}` + "\n"
	if err := os.WriteFile(appPath, []byte(appData), 0o600); err != nil {
		t.Fatalf("WriteFile(app) error = %v", err)
	}
	if err := os.WriteFile(featurePath, []byte(featureData), 0o600); err != nil {
		t.Fatalf("WriteFile(feature) error = %v", err)
	}

	ownerMap, err := LoadOwnerMaps([]string{appPath, featurePath})
	if err != nil {
		t.Fatalf("LoadOwnerMaps() error = %v", err)
	}
	if got := hexOrEmpty(ownerMap.SymbolNamespace); got != namespace {
		t.Fatalf("symbolNamespace = %q, want %q", got, namespace)
	}
	if got := ownerMap.Entries["stable:0x0000000000000001"]; got != "com.app.MainActivity.render" {
		t.Fatalf("app owner = %q", got)
	}
	if got := ownerMap.Entries["stable:0x0000000000000002"]; got != "com.app.feed.FeedPresenter.load" {
		t.Fatalf("feature owner = %q", got)
	}
	if len(ownerMap.Entries) != 2 {
		t.Fatalf("merged entries = %+v", ownerMap.Entries)
	}
}

func TestLoadOwnerMapsAllowsIdenticalCrossModuleEntries(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "app-owner-map.json")
	secondPath := filepath.Join(dir, "feature-owner-map.json")
	const data = `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.shared.Dispatcher.run"}` + "\n"
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	ownerMap, err := LoadOwnerMaps([]string{firstPath, secondPath})
	if err != nil {
		t.Fatalf("LoadOwnerMaps() error = %v", err)
	}
	if len(ownerMap.Entries) != 1 || ownerMap.Entries["stable:0x0123456789abcdef"] != "com.shared.Dispatcher.run" {
		t.Fatalf("ownerMap = %+v", ownerMap)
	}
}

func TestLoadOwnerMapsRejectsDifferentNamespaces(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app-owner-map.json")
	featurePath := filepath.Join(dir, "feature-owner-map.json")
	if err := os.WriteFile(appPath, []byte(`{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(app) error = %v", err)
	}
	if err := os.WriteFile(featurePath, []byte(`{"format":4,"kind":"metadata","symbolNamespace":"ccdd0000000000000000000000000000"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(feature) error = %v", err)
	}

	_, err := LoadOwnerMaps([]string{appPath, featurePath})
	if err == nil ||
		!strings.Contains(err.Error(), appPath) ||
		!strings.Contains(err.Error(), featurePath) ||
		!strings.Contains(err.Error(), "aabb") ||
		!strings.Contains(err.Error(), "ccdd") {
		t.Fatalf("LoadOwnerMaps() error = %v, want source-aware namespace mismatch", err)
	}
}

func TestLoadOwnerMapsRejectsConflictingStableIDs(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app-owner-map.json")
	featurePath := filepath.Join(dir, "feature-owner-map.json")
	const metadata = `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n"
	appData := metadata + `{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.First.call"}` + "\n"
	featureData := metadata + `{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.Second.call"}` + "\n"
	if err := os.WriteFile(appPath, []byte(appData), 0o600); err != nil {
		t.Fatalf("WriteFile(app) error = %v", err)
	}
	if err := os.WriteFile(featurePath, []byte(featureData), 0o600); err != nil {
		t.Fatalf("WriteFile(feature) error = %v", err)
	}

	_, err := LoadOwnerMaps([]string{appPath, featurePath})
	if err == nil ||
		!strings.Contains(err.Error(), appPath) ||
		!strings.Contains(err.Error(), featurePath) ||
		!strings.Contains(err.Error(), "stable:0x0123456789abcdef") ||
		!strings.Contains(err.Error(), "com.app.First.call") ||
		!strings.Contains(err.Error(), "com.app.Second.call") {
		t.Fatalf("LoadOwnerMaps() error = %v, want source-aware stable ID conflict", err)
	}
}

func TestLoadOwnerMapsRejectsInvalidMapWithoutPartialResult(t *testing.T) {
	dir := t.TempDir()
	validPath := filepath.Join(dir, "valid-owner-map.json")
	invalidPath := filepath.Join(dir, "invalid-owner-map.json")
	if err := os.WriteFile(validPath, []byte(`{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(valid) error = %v", err)
	}
	if err := os.WriteFile(invalidPath, []byte(`{"format":3,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(invalid) error = %v", err)
	}

	ownerMap, err := LoadOwnerMaps([]string{validPath, invalidPath})
	if err == nil || ownerMap != nil || !strings.Contains(err.Error(), invalidPath) || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("LoadOwnerMaps() = (%+v, %v), want fail-closed invalid-map error", ownerMap, err)
	}
}

func TestLoadOwnerMapRejectsNonCanonicalEntryID(t *testing.T) {
	mapPath := filepath.Join(t.TempDir(), "owner-map.json")
	data := `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789ABCDEF","owner":"com.app.FeedRepository.refresh"}` + "\n"
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadOwnerMap(mapPath)
	if err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("LoadOwnerMap() error = %v, want non-canonical ID error", err)
	}
}

func TestLoadOwnerMapRejectsConflictingStableIDs(t *testing.T) {
	mapPath := filepath.Join(t.TempDir(), "owner-map.json")
	data := `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.First.call"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.Second.call"}` + "\n"
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadOwnerMap(mapPath)
	if err == nil || !strings.Contains(err.Error(), "line 3") || !strings.Contains(err.Error(), "conflicting owner map entry") {
		t.Fatalf("LoadOwnerMap() error = %v, want line-aware conflict", err)
	}
}

func TestLoadOwnerMapAllowsIdenticalDuplicateStableIDs(t *testing.T) {
	mapPath := filepath.Join(t.TempDir(), "owner-map.json")
	data := `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.FeedRepository.refresh"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.FeedRepository.refresh"}` + "\n"
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ownerMap, err := LoadOwnerMap(mapPath)
	if err != nil {
		t.Fatalf("LoadOwnerMap() error = %v", err)
	}
	if len(ownerMap.Entries) != 1 || ownerMap.Entries["stable:0x0123456789abcdef"] != "com.app.FeedRepository.refresh" {
		t.Fatalf("ownerMap = %+v", ownerMap)
	}
}

func TestResolveOwnerAliasRequiresMatchingStableNamespace(t *testing.T) {
	ownerMap := &OwnerMap{
		Entries: map[string]string{
			"stable:0x0123456789abcdef": "com.app.FeedRepository.refresh",
			"manual":                    "manual owner",
		},
		SymbolNamespace: append([]byte{0xaa, 0xbb}, make([]byte, 14)...),
	}

	if got := ResolveOwnerAlias(ownerMap, "manual"); got != "manual owner" {
		t.Fatalf("manual alias = %q", got)
	}
	if got := ResolveOwnerAlias(ownerMap, "stable:aabb0000000000000000000000000000:0x0123456789abcdef"); got != "com.app.FeedRepository.refresh" {
		t.Fatalf("namespaced stable alias = %q", got)
	}
	if got := ResolveOwnerAlias(ownerMap, "stable:ccdd0000000000000000000000000000:0x0123456789abcdef"); got != "stable:ccdd0000000000000000000000000000:0x0123456789abcdef" {
		t.Fatalf("mismatched namespaced alias = %q", got)
	}
	if got := ResolveOwnerAlias(ownerMap, "unknown"); got != "unknown" {
		t.Fatalf("unknown alias = %q", got)
	}
}

func TestInspectFilesRejectsOwnerMapFromAnotherSymbolNamespace(t *testing.T) {
	mapPath := filepath.Join(t.TempDir(), "owner-map.json")
	data := `{"format":4,"kind":"metadata","symbolNamespace":"aabb0000000000000000000000000000"}` + "\n" +
		`{"format":4,"kind":"entry","id":"stable:0x0123456789abcdef","owner":"com.app.FeedRepository.refresh"}` + "\n"
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ownerMap, err := LoadOwnerMap(mapPath)
	if err != nil {
		t.Fatalf("LoadOwnerMap() error = %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "different-build.jhlog")
	header := jhlog.DefaultSegmentHeader()
	header.SymbolNamespace = append([]byte{0xcc, 0xdd}, make([]byte, 14)...)
	file, _, err := jhlog.CreateWithHeader(logPath, header)
	if err != nil {
		t.Fatalf("CreateWithHeader() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err = InspectFilesWithOptions("mismatch", []string{logPath}, Options{OwnerMap: ownerMap})
	if err == nil || !strings.Contains(err.Error(), "aabb") || !strings.Contains(err.Error(), "ccdd") {
		t.Fatalf("InspectFilesWithOptions() error = %v, want explicit namespace mismatch", err)
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
			t.Fatalf("embedded stable counter %q was not resolved without owner-map: %+v", name, summary.Counters)
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
		t.Fatalf("embedded runtime edge was not resolved without owner-map: %+v", summary.RuntimeCalls)
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
			Source:  source,
			Version: jhlog.FormatVersion,
			Header:  value,
			Status:  jhlog.SegmentStatusClosedClean,
			Sealed:  true,
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
		{Source: "first.jhlog", Version: jhlog.FormatVersion, Header: first, Status: jhlog.SegmentStatusClosedClean, Sealed: true},
		{Source: "third.jhlog", Version: jhlog.FormatVersion, Header: third, Status: jhlog.SegmentStatusClosedClean, Sealed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !warningsContain(issues, "разрыв segment chain: 0 → 2") {
		t.Fatalf("chain issues = %+v", issues)
	}
}

func TestCollectionQualityTreatsSizeLimitAsIncompleteWithApparentSuccessor(t *testing.T) {
	collector := newCollector("size limit", 2, Options{})
	quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
	collector.addStreamResult(jhlog.StreamResult{
		Source:        "size-limited.jhlog",
		Version:       jhlog.FormatVersion,
		Header:        collectionTestHeader(12, 0),
		Status:        jhlog.SegmentStatusClosedClean,
		Sealed:        true,
		LatestQuality: &quality,
		SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndSizeLimit},
	})
	collector.addStreamResult(jhlog.StreamResult{
		Source:        "apparent-successor.jhlog",
		Version:       jhlog.FormatVersion,
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
	if got.Level != "medium" || got.Complete {
		t.Fatalf("size-limited quality = %+v", got)
	}
	reasons := strings.Join(got.Reasons, "\n")
	if !strings.Contains(reasons, "достиг лимита размера") || !strings.Contains(reasons, "сбор завершён раньше") {
		t.Fatalf("size-limited reasons = %q", reasons)
	}
	for _, stale := range []string{"следующ", "продолж", "segment chain"} {
		if strings.Contains(strings.ToLower(reasons), stale) {
			t.Fatalf("size-limited reasons contain stale continuation wording %q: %q", stale, reasons)
		}
	}
}

func TestCollectionQualityCapsConfidenceForUnsealedAndLossyStreams(t *testing.T) {
	t.Run("live clean snapshot", func(t *testing.T) {
		collector := newCollector("live", 1, Options{})
		quality := jhlog.QualitySnapshot{Sequence: 1, Counters: map[uint64]uint64{}}
		collector.addStreamResult(jhlog.StreamResult{
			Source:        "active.jhlog",
			Version:       jhlog.FormatVersion,
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
			Version:       jhlog.FormatVersion,
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
			jhlog.QualityAcceptedEventTotal: 1_000,
			jhlog.QualityWrittenEventTotal:  900,
			jhlog.QualityQueueFullTotal:     10,
		}}
		collector.addStreamResult(jhlog.StreamResult{
			Source:        "lossy.jhlog",
			Version:       jhlog.FormatVersion,
			Header:        collectionTestHeader(11, 0),
			Status:        jhlog.SegmentStatusClosedClean,
			Sealed:        true,
			LatestQuality: &quality,
		})
		if err := collector.validateSegmentIdentityConsistency(); err != nil {
			t.Fatal(err)
		}
		collector.finalizeCollectionQuality()
		got := collector.summary.CollectionQuality
		if got.Level != "low" || got.KnownLostEvents != 110 || !warningsContain(got.Reasons, "потерю как минимум 110 событий") {
			t.Fatalf("lossy quality = %+v", got)
		}
	})
}

func collectionTestHeader(session byte, segmentIndex uint64) jhlog.SegmentHeader {
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = 1
	header.ProcessInstanceID[0] = 2
	header.SessionID[0] = session
	header.SegmentIndex = segmentIndex
	header.OSPID = 42
	header.CollectorStartElapsedUS = 1_000
	header.SegmentStartElapsedUS = 1_000 + segmentIndex*100
	header.SegmentStartUnixMS = 2_000 + segmentIndex*100
	header.IdentitySource = 1
	header.ProcessName = "main"
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
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictFlow, ID: 4, Value: "feed.refresh"}},
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictStep, ID: 5, Value: "network"}},
		{Type: jhlog.EventFlow, TimeMS: 1, Attribution: attributionForTest(2, 3, 4, 5), Flow: &jhlog.FlowEvent{ScreenID: 2, OwnerID: 3, FlowID: 4, StepID: 5}},
		{Type: jhlog.EventHTTP, TimeMS: 2, Attribution: attributionForTest(2, 3, 4, 5), HTTP: &jhlog.HTTPEvent{OwnerID: 3, RouteID: 1, DurationMS: 100, Status: jhlog.Status2xx}},
		{Type: jhlog.EventHTTP, TimeMS: 3, Attribution: attributionForTest(2, 3, 4, 5), HTTP: &jhlog.HTTPEvent{OwnerID: 3, RouteID: 1, DurationMS: 1000, Status: jhlog.Status2xx}},
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
	if len(summary.Flows) != 1 || summary.Flows[0].HTTPP95MS != 1000 {
		t.Fatalf("flow p95 = %+v, want 1000", summary.Flows)
	}
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
		{Type: jhlog.EventHTTP, TimeMS: 1, HTTP: &jhlog.HTTPEvent{OwnerID: 1, RouteID: 2, DurationMS: 100, Status: jhlog.Status2xx}},
		{Type: jhlog.EventStall, TimeMS: 2, Stall: &jhlog.StallEvent{OwnerID: 1, StackID: 3, DurationMS: 250}},
		{Type: jhlog.EventRetained, TimeMS: 3, Retained: &jhlog.RetainedEvent{ClassID: 4, AgeMS: 10_000, Count: 1}},
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
		{Type: jhlog.EventRetained, TimeMS: 1, Retained: &jhlog.RetainedEvent{ClassID: 1, OwnerID: 3, AgeMS: 10_000, Count: 1}},
		{Type: jhlog.EventRetained, TimeMS: 2, Retained: &jhlog.RetainedEvent{ClassID: 2, AgeMS: 12_000, Count: 1}},
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

func TestInspectFilesBoundsAggregateSamplesButKeepsCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bounded-aggregate.jhlog")
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
	const total = maxAggregateSamplesPerSignal + 25
	for i := 1; i <= total; i++ {
		value := uint64(i)
		if err := writer.WriteEvent(jhlog.Event{
			Type:   jhlog.EventHTTP,
			TimeMS: value,
			HTTP: &jhlog.HTTPEvent{
				OwnerID:    2,
				RouteID:    1,
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
				MetricID: 3,
				Value:    value,
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
	if route.Count != total || route.MaxMS != total || route.P95MS == 0 {
		t.Fatalf("route stats lost exact count/max or percentile: %+v", route)
	}
	if !route.P95Approximate || route.Sampled != maxAggregateSamplesPerSignal {
		t.Fatalf("route approximation was not marked: %+v", route)
	}
	if !summary.HTTPP95Approximate {
		t.Fatalf("HTTP p95 approximation was not marked")
	}
	if !warningsContain(summary.Warnings, "reservoir-сэмплу") {
		t.Fatalf("expected reservoir warning, got %+v", summary.Warnings)
	}
	if len(summary.Gauges) != 1 {
		t.Fatalf("Gauges = %+v, want one gauge", summary.Gauges)
	}
	expectedExtra := fmt.Sprintf("avg=%d max=%d samples=%d", uint64(total+1)/2, uint64(total), uint64(total))
	if summary.Gauges[0].Extra != expectedExtra {
		t.Fatalf("gauge Extra = %q, want %q", summary.Gauges[0].Extra, expectedExtra)
	}
}

func TestInspectFilesDoesNotCarryFlowContextAcrossEventsOrLogs(t *testing.T) {
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
		{Type: jhlog.EventFlow, TimeMS: 1, Attribution: attributionForTest(1, 2, 3, 4), Flow: &jhlog.FlowEvent{ScreenID: 1, OwnerID: 2, FlowID: 3, StepID: 4}},
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

	summary, err := inspectFilesForTest("sample", []string{first, second})
	if err != nil {
		t.Fatalf("inspectFilesForTest() error = %v", err)
	}
	if len(summary.Flows) != 2 {
		t.Fatalf("Flows = %+v, want a semantic flow transition and an unattributed HTTP flow", summary.Flows)
	}
	var httpFlow *FlowStats
	for index := range summary.Flows {
		if summary.Flows[index].HTTPCount > 0 {
			httpFlow = &summary.Flows[index]
			break
		}
	}
	if httpFlow == nil {
		t.Fatalf("HTTP flow missing: %+v", summary.Flows)
	}
	if httpFlow.Screen != "unknown" || httpFlow.Flow != "unknown" || httpFlow.Step != "unknown" || httpFlow.Owner != "unknown" {
		t.Fatalf("HTTP inherited stale flow context: %+v", httpFlow)
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

func TestLegacyProblemWindowsAreDeduplicatedPerLog(t *testing.T) {
	dict := map[uint64]string{
		1: "FeedScreen",
		2: "FeedOwner",
		3: "feed.open",
		4: "render",
		5: "main_thread_stall",
	}
	context := attributionForTest(1, 2, 3, 4)
	legacy := jhlog.Event{
		Type:        jhlog.EventProblem,
		TimeMS:      20,
		Attribution: context,
		Problem: &jhlog.ProblemEvent{
			ScreenID: 1,
			OwnerID:  2,
			FlowID:   3,
			StepID:   4,
			KindID:   5,
			WindowMS: 5_000,
			Count:    7,
			MaxMS:    80,
		},
	}
	logs := []jhlog.Log{
		{
			Dict: dict,
			Events: []jhlog.Event{
				{
					Type:        jhlog.EventStall,
					TimeMS:      10,
					Attribution: context,
					Stall:       &jhlog.StallEvent{OwnerID: 2, DurationMS: 80},
				},
				legacy,
			},
		},
		{
			Dict:   dict,
			Events: []jhlog.Event{legacy},
		},
	}

	summary := inspectLogsForTest("legacy problem dedup", logs)
	if len(summary.ProblemWindows) != 1 {
		t.Fatalf("problem windows = %+v", summary.ProblemWindows)
	}
	problem := summary.ProblemWindows[0]
	if problem.Kind != "main_thread_stall" || problem.Count != 8 || problem.Windows != 2 || problem.MaxMS != 80 {
		t.Fatalf("problem window = %+v, want canonical count 1 plus unmatched legacy count 7", problem)
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
		{name: "future", reason: 99, wantReason: "unknown(99)", warningFragment: "неизвестной причиной 99"},
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
		},
		Events: []jhlog.Event{
			{Type: jhlog.EventCounter, Metric: &jhlog.MetricEvent{MetricID: 1, Value: 3}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricID: 2, Value: 18}},
			{Type: jhlog.EventGauge, Metric: &jhlog.MetricEvent{MetricID: 2, Value: 22}},
		},
	}

	summary := inspectLogsForTest("jankstats", []jhlog.Log{log})
	if len(summary.JankStats) != 2 {
		t.Fatalf("unexpected jankstats metrics: %+v", summary.JankStats)
	}
}

func TestInspectResolvesStableCounterMetricThroughOwnerMap(t *testing.T) {
	const stableID = 0x0123456789abcdef
	log := jhlog.Log{
		Dict: map[uint64]string{
			1: "custom.metric",
		},
		Events: []jhlog.Event{
			{
				Type: jhlog.EventCounter,
				Metric: &jhlog.MetricEvent{
					MetricRef: jhlog.StableSymbolInNamespace(
						stableID,
						append([]byte{0xaa, 0xbb}, make([]byte, 14)...),
					),
					Value: 2,
				},
			},
			{
				Type: jhlog.EventCounter,
				Metric: &jhlog.MetricEvent{
					MetricID: 1,
					Value:    3,
				},
			},
		},
	}
	collector := newCollector("stable counter", 1, Options{OwnerMap: &OwnerMap{
		Entries: map[string]string{
			"stable:0x0123456789abcdef": "com.app.FeedRepository.refresh",
		},
		SymbolNamespace: append([]byte{0xaa, 0xbb}, make([]byte, 14)...),
	}})
	collector.startLog()
	for _, event := range log.Events {
		collector.add(log.Dict, event)
	}
	collector.finishLog()
	summary := collector.finish()

	counters := namedValuesByName(summary.Counters)
	if got := counters["com.app.FeedRepository.refresh"].Value; got != 2 {
		t.Fatalf("stable method counter = %d, want 2; counters = %+v", got, summary.Counters)
	}
	if got := counters["custom.metric"].Value; got != 3 {
		t.Fatalf("local counter = %d, want 3; counters = %+v", got, summary.Counters)
	}
}

func TestExternalStableLogRequiresExplicitCLIOptIn(t *testing.T) {
	const stableID = 0x0123456789abcdef
	namespace := append([]byte{0xaa, 0xbb}, make([]byte, 14)...)
	header := jhlog.DefaultSegmentHeader()
	header.SymbolNamespace = namespace
	path := filepath.Join(t.TempDir(), "external-symbols.jhlog")
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

	if _, err := InspectFilesWithOptions("external", []string{path}, Options{}); err == nil ||
		!strings.Contains(err.Error(), "--external-symbols") {
		t.Fatalf("missing owner-map error = %v", err)
	}
	ownerMap := &OwnerMap{
		Entries: map[string]string{
			"stable:0x0123456789abcdef": "com.app.FeedRepository.refresh",
		},
		SymbolNamespace: namespace,
	}
	if _, err := InspectFilesWithOptions("external", []string{path}, Options{
		OwnerMap:                       ownerMap,
		RequireExplicitExternalSymbols: true,
	}); err == nil || !strings.Contains(err.Error(), "--external-symbols") {
		t.Fatalf("implicit external mode error = %v", err)
	}
	summary, err := InspectFilesWithOptions("external", []string{path}, Options{
		OwnerMap:                       ownerMap,
		ExternalSymbols:                true,
		RequireExplicitExternalSymbols: true,
	})
	if err != nil {
		t.Fatalf("explicit external mode error = %v", err)
	}
	if got := namedValuesByName(summary.Counters)["com.app.FeedRepository.refresh"].Value; got != 2 {
		t.Fatalf("resolved external counter = %d, want 2", got)
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
					MetricID: 1,
					Value:    100,
					Count:    2,
					Sum:      200,
					Max:      140,
					Mode:     jhlog.MetricModeAverage,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricID: 1,
					Value:    200,
					Count:    4,
					Sum:      800,
					Max:      260,
					Mode:     jhlog.MetricModeAverage,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricID: 2,
					Value:    2,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricID: 2,
					Value:    5,
				},
			},
			{
				Type: jhlog.EventGauge,
				Metric: &jhlog.MetricEvent{
					MetricID: 3,
					Value:    50,
					Count:    2,
					Sum:      1,
					Max:      1,
					Mode:     jhlog.MetricModeBooleanRate,
				},
			},
		},
	}

	summary := inspectLogsForTest("metrics", []jhlog.Log{log})
	gauges := namedValuesByName(summary.Gauges)
	if got := gauges["memory.pss"]; got.Value != 166 || got.Extra != "avg=166 max=260 samples=6" {
		t.Fatalf("memory.pss = %+v", got)
	}
	if got := gauges["battery.status"]; got.Value != 5 || got.Extra != "state=5 samples=2" {
		t.Fatalf("battery.status = %+v", got)
	}
	if got := gauges["battery.charging"]; got.Value != 50 || got.Extra != "true_pct=50 true=1 samples=2" {
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

func TestCompareUsesRealSampleSizesAndDoesNotInventIntervals(t *testing.T) {
	comparison := Compare(
		Summary{
			LogCount:     5,
			EventCount:   500,
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
	if got := deltas["Problem windows"]; got.Baseline != "2 шт" || got.Candidate != "3 шт" {
		t.Fatalf("Problem windows delta = %+v", got)
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

func warningsContain(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func inspectLogsForTest(title string, logs []jhlog.Log) Summary {
	collector := newCollector(title, len(logs), Options{})
	for _, log := range logs {
		collector.startLog()
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
		Source:  path,
		Version: jhlog.FormatVersion,
		Dict:    map[uint64]string{},
		Kinds:   map[uint64]jhlog.DictKind{},
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
	log.Warnings = result.Warnings
	log.Result = result
	return log
}

func attributionForTest(screenID, ownerID, flowID, stepID uint64) jhlog.AttributionContext {
	return jhlog.AttributionContext{
		Present: true,
		Screen:  jhlog.LocalSymbol(screenID),
		Owner:   jhlog.LocalSymbol(ownerID),
		Flow:    jhlog.LocalSymbol(flowID),
		Step:    jhlog.LocalSymbol(stepID),
	}
}
