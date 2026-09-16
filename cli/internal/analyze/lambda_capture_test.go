package analyze

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadLambdaCaptureCatalogAndBuildRiskFindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lambda-captures.jsonl")
	fixture := `{"format":1,"class":"com.app.FeedScreen","captures":[` +
		`{"callsiteId":"stable:0x0000000000000001","owner":"com.app.FeedScreen.render()V","implementation":"com.app.FeedScreen.render$lambda$0","functionalInterface":"kotlin.jvm.functions.Function0","representation":"invokedynamic","values":[{"type":"android.app.Activity","role":"capture","strength":"strong"}],"sinks":["compose.remember"]},` +
		`{"callsiteId":"stable:0x0000000000000002","owner":"com.app.FeedViewModel.observe()V","implementation":"com.app.FeedViewModel.observe$lambda$0","functionalInterface":"kotlin.jvm.functions.Function2","representation":"invokedynamic","values":[{"type":"com.app.MainActivity","role":"capture","strength":"strong"}],"sinks":["flow.global_scope","flow.operator"]},` +
		`{"callsiteId":"stable:0x0000000000000003","owner":"com.app.Safe.bind()V","implementation":"com.app.Safe.bind$lambda$0","functionalInterface":"java.lang.Runnable","representation":"class","values":[{"type":"java.lang.ref.WeakReference","role":"capture:f$0","strength":"weak"}],"sinks":["handler.queue"]},` +
		`{"callsiteId":"stable:0x0000000000000004","owner":"com.app.Suppressed.bind()V","implementation":"com.app.Suppressed.bind$lambda$0","functionalInterface":"java.lang.Runnable","representation":"class","values":[{"type":"android.app.Activity","role":"capture:f$0","strength":"strong"}],"sinks":["static.field"],"suppressed":true,"suppressionReason":"intentional process owner"}` +
		`]}` + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := LoadLambdaCaptureCatalog(path)
	if err != nil {
		t.Fatalf("LoadLambdaCaptureCatalog() error = %v", err)
	}
	if !catalog.Available || len(catalog.Captures) != 4 {
		t.Fatalf("catalog = %+v", catalog)
	}

	report, err := buildProblemReportWithLambdaCaptures(Summary{}, DefaultProblemDetectorConfig(), nil, catalog)
	if err != nil {
		t.Fatalf("buildProblemReportWithLambdaCaptures() error = %v", err)
	}
	if got := countDetector(report.Problems, "memory.lambda_capture"); got != 2 {
		t.Fatalf("lambda findings = %d, want 2: %+v", got, report.Problems)
	}
	if report.Problems[0].Severity != "critical" || !strings.Contains(report.Problems[0].WhatHappened, "GlobalScope") {
		t.Fatalf("top finding = %+v", report.Problems[0])
	}
	for _, finding := range report.Problems {
		if strings.Contains(finding.Title, "Safe") || strings.Contains(finding.Title, "Suppressed") {
			t.Fatalf("safe/suppressed capture leaked into findings: %+v", finding)
		}
	}
}

func TestLoadLambdaCaptureCatalogRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lambda-captures.jsonl")
	fixture := `{"format":1,"class":"com.app.FeedScreen","captures":[],"capturez":[]}` + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLambdaCaptureCatalog(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadLambdaCaptureCatalog() error = %v, want strict schema rejection", err)
	}
}

func TestLoadLambdaCaptureCatalogRejectsEmptyCaptureRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lambda-captures.jsonl")
	if err := os.WriteFile(path, []byte(`{"format":1,"class":"com.example.Empty","captures":[]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLambdaCaptureCatalog(path)
	if err == nil || !strings.Contains(err.Error(), "captures count") {
		t.Fatalf("error = %v, want empty record rejection", err)
	}
}

func TestWeakForceUnwrapProducesLifetimeMismatchInsteadOfLeak(t *testing.T) {
	catalog := &LambdaCaptureCatalog{Available: true, Captures: []LambdaCapture{{
		CallsiteID: "stable:0x0000000000000001", Owner: "com.app.Screen.bind()V",
		Implementation: "com.app.Screen.bind$lambda$0", Representation: "invokedynamic",
		Values: []LambdaCapturedValue{{Type: "java.lang.ref.WeakReference", Role: "capture", Strength: "weak"}},
		Sinks:  []string{"handler.queue"}, WeakDereference: "force_unwrap",
	}}}

	report, err := buildProblemReportWithLambdaCaptures(Summary{}, DefaultProblemDetectorConfig(), nil, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if countDetector(report.Problems, "stability.lambda_lifetime_mismatch") != 1 || countDetector(report.Problems, "memory.lambda_capture") != 0 {
		t.Fatalf("problems = %+v", report.Problems)
	}
}

func TestHeapMatchersRecognizeKotlinCaptureFields(t *testing.T) {
	path := []HeapPathElement{
		{ClassName: "com.app.Screen$load$1"},
		{ClassName: "android.app.Activity", FieldName: "this$0"},
		{ClassName: "android.view.View", FieldName: "L$0"},
	}

	matchers := strings.Join(heapReferenceMatchers(path), ",")
	if !strings.Contains(matchers, "kotlin.lambda_capture") || !strings.Contains(matchers, "kotlin.coroutine_spill") {
		t.Fatalf("matchers = %q", matchers)
	}
}

func TestHeapMatchersDoNotTreatEveryInnerClassAsLambda(t *testing.T) {
	path := []HeapPathElement{
		{ClassName: "com.app.Screen$Inner"},
		{ClassName: "com.app.Screen", FieldName: "this$0"},
	}

	if matchers := heapReferenceMatchers(path); slices.Contains(matchers, "kotlin.lambda_capture") {
		t.Fatalf("ordinary inner-class outer reference classified as lambda capture: %v", matchers)
	}
}

func TestLifecycleCaptureClassifierDoesNotMistakePreviewForView(t *testing.T) {
	if isLifecycleCaptureType("com.app.HeavyPreview") {
		t.Fatal("ordinary Preview class was classified as an Android View")
	}
	if isLifecycleCaptureType("com.app.ApiService") {
		t.Fatal("ordinary application service was classified as an Android Service")
	}
	if !isLifecycleCaptureType("com.app.CustomView") || !isLifecycleCaptureType("com.app.MainActivity") {
		t.Fatal("real lifecycle class suffix was not recognized")
	}
	if !isLifecycleCaptureType("android.app.Service") {
		t.Fatal("Android Service was not recognized")
	}
	if !isLifecycleCaptureType("com.app.MainActivity[]") {
		t.Fatal("array of lifecycle objects was not recognized")
	}
}

func TestLambdaCaptureHeapConfirmationRequiresExactImplementationFieldPath(t *testing.T) {
	capture := LambdaCapture{
		CallsiteID: "stable:0x0000000000000001", Owner: "com.app.Screen.bind()V",
		Implementation: "com.app.Screen$bind$1", Representation: "class",
		Values: []LambdaCapturedValue{{Type: "com.app.MainActivity", Role: "capture:this$0", Strength: "strong"}},
	}
	unrelated := Summary{MemoryLeaks: []MemoryLeakSuspect{{
		ClassName: "com.app.MainActivity", HeapEvidence: true,
		ReferenceMatchers: []string{"kotlin.lambda_capture"},
		ReferencePath: []HeapPathElement{
			{ClassName: "com.app.Other$bind$1"},
			{ClassName: "com.app.MainActivity", FieldName: "this$0"},
		},
	}}}
	if risks := lambdaCaptureRisks(capture, buildLambdaHeapEvidenceIndex(unrelated.MemoryLeaks)); len(risks) != 0 {
		t.Fatalf("class-based capture without sink or exact heap evidence became a finding: %+v", risks)
	}

	exact := Summary{MemoryLeaks: []MemoryLeakSuspect{{
		ClassName: "com.app.MainActivity", HeapEvidence: true,
		ReferencePath: []HeapPathElement{
			{ClassName: "com.app.Screen$bind$1"},
			{ClassName: "com.app.MainActivity", FieldName: "this$0"},
		},
	}}}
	heapEvidence := buildLambdaHeapEvidenceIndex(exact.MemoryLeaks)
	if risks := lambdaCaptureRisks(capture, heapEvidence); len(risks) != 1 || risks[0].confidence != "high" || risks[0].claim != "linked" {
		t.Fatalf("exact heap path did not upgrade lambda confidence: %+v", risks)
	}
	capture.Values[0].Type = "android.app.Activity"
	if risks := lambdaCaptureRisks(capture, heapEvidence); len(risks) != 1 || risks[0].confidence != "high" {
		t.Fatalf("heap subtype did not confirm its exact declared capture field: %+v", risks)
	}
	capture.Values[0].Type = "com.app.MainActivity"
	capture.Sinks = []string{"flow.lifecycle_scope"}
	if risks := lambdaCaptureRisks(capture, heapEvidence); len(risks) != 1 || risks[0].confidence != "high" {
		t.Fatalf("exact heap evidence was hidden by a statically safe sink: %+v", risks)
	}
	capture.Sinks = nil

	registry := BuildCodeProblemRegistryWithLambdaCaptures(exact, &LambdaCaptureCatalog{Available: true, Captures: []LambdaCapture{capture}})
	var lambdaSignal *CodeProblemSignal
	for rowIndex := range registry {
		for signalIndex := range registry[rowIndex].Signals {
			if strings.Contains(registry[rowIndex].Signals[signalIndex].Name, capture.Implementation) {
				lambdaSignal = &registry[rowIndex].Signals[signalIndex]
			}
		}
	}
	if lambdaSignal == nil || !strings.Contains(lambdaSignal.Detail, "Дамп памяти") {
		t.Fatalf("code registry lost heap-confirmed evidence: %+v", registry)
	}
	report, err := buildProblemReportWithLambdaCaptures(exact, DefaultProblemDetectorConfig(), nil, &LambdaCaptureCatalog{
		Available: true,
		Captures:  []LambdaCapture{capture},
	})
	if err != nil || len(report.Problems) != 1 {
		t.Fatalf("heap-confirmed problem report = %+v, error = %v", report, err)
	}
	finding := report.Problems[0]
	if len(finding.ConfidenceReasons) != 1 || !strings.Contains(finding.ConfidenceReasons[0], "Дамп памяти") {
		t.Fatalf("heap-confirmed confidence reason is inaccurate: %+v", finding.ConfidenceReasons)
	}
	for _, limitation := range finding.Limitations {
		if strings.Contains(limitation, "без данных выполнения") {
			t.Fatalf("heap-confirmed finding retained static-only limitation: %+v", finding.Limitations)
		}
	}

	capture.Representation = "invokedynamic"
	if risks := lambdaCaptureRisks(capture, heapEvidence); len(risks) != 0 {
		t.Fatalf("invokedynamic capture without a lifetime sink unexpectedly became heap-confirmed: %+v", risks)
	}
}

func TestCoroutineCaptureWithoutLifetimeEvidenceIsNotReportedAsLeak(t *testing.T) {
	capture := LambdaCapture{
		CallsiteID: "stable:0x0000000000000001", Owner: "com.app.Screen.load()V",
		Implementation: "com.app.Screen$load$1", Representation: "coroutine",
		Values: []LambdaCapturedValue{{Type: "com.app.MainActivity", Role: "capture:this$0", Strength: "strong"}},
	}

	if risks := lambdaCaptureRisks(capture, nil); len(risks) != 0 {
		t.Fatalf("ordinary coroutine state was reported as a leak without lifetime evidence: %+v", risks)
	}
}

func TestLoadLambdaCaptureCatalogAcceptsJvmMaximumCapturedArguments(t *testing.T) {
	values := make([]LambdaCapturedValue, 255)
	for index := range values {
		values[index] = LambdaCapturedValue{Type: "java.lang.Object", Role: "capture", Strength: "strong"}
	}
	record := lambdaCaptureRecord{Format: LambdaCaptureFormat, Class: "com.app.LargeLambda", Captures: []LambdaCapture{{
		CallsiteID: "stable:0x0000000000000001", Owner: "com.app.LargeLambda.bind()V",
		Implementation: "com.app.LargeLambda.bind$lambda$0", FunctionalInterface: "kotlin.jvm.functions.Function0",
		Representation: "invokedynamic", Values: values,
	}}}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lambda-captures.jsonl")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := LoadLambdaCaptureCatalog(path)
	if err != nil || len(catalog.Captures) != 1 || len(catalog.Captures[0].Values) != 255 {
		t.Fatalf("catalog = %+v, error = %v", catalog, err)
	}
}

func TestLoadLambdaCaptureCatalogAllowsCoroutineSpillsBeyondJvmArgumentLimit(t *testing.T) {
	values := make([]LambdaCapturedValue, 256)
	for index := range values {
		values[index] = LambdaCapturedValue{Type: "java.lang.Object", Role: "spill:L$0", Strength: "strong"}
	}
	capture := LambdaCapture{
		CallsiteID: "stable:0x0000000000000001", Owner: "com.app.Work$run$1.invokeSuspend",
		Implementation: "com.app.Work$run$1", FunctionalInterface: "kotlin.jvm.functions.Function2",
		Representation: "coroutine", Values: values,
	}

	if err := validateLambdaCapture(capture); err != nil {
		t.Fatalf("valid coroutine spill catalog was rejected: %v", err)
	}
}

func TestLoadLambdaCaptureCatalogRejectsNonCanonicalCallsiteID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lambda-captures.jsonl")
	fixture := `{"format":1,"class":"com.app.Feed","captures":[` +
		`{"callsiteId":"unstable:1","owner":"com.app.Feed.bind()V","implementation":"com.app.Feed.bind$lambda$0","functionalInterface":"java.lang.Runnable","representation":"invokedynamic","values":[{"type":"android.app.Activity","role":"capture","strength":"strong"}],"sinks":[]}` +
		`]}` + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLambdaCaptureCatalog(path)
	if err == nil || !strings.Contains(err.Error(), "callsiteId") {
		t.Fatalf("error = %v, want canonical callsiteId validation", err)
	}
}

func TestLoadLambdaCaptureCatalogRejectsOversizedArtifactBeforeScanning(t *testing.T) {
	path := sparseAnalyzeInput(t, "lambda-captures.jsonl", lambdaCaptureMaxFileBytes+1)

	_, err := LoadLambdaCaptureCatalog(path)

	if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("error = %v, want bounded-file rejection", err)
	}
}

func countDetector(findings []ProblemFinding, detector string) int {
	count := 0
	for _, finding := range findings {
		if finding.DetectorID == detector {
			count++
		}
	}
	return count
}
