package analyze

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestHeapTraversalRecordsStayCompact(t *testing.T) {
	if size := unsafe.Sizeof(storedHeapEdge{}); size > 24 {
		t.Fatalf("stored edge = %d bytes, want <=24", size)
	}
	if size := unsafe.Sizeof(heapParent{}); size > 32 {
		t.Fatalf("heap parent record = %d bytes, want <= 32", size)
	}
}

func TestInspectKeepsHeapClassEvidenceSeparate(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "sample.jhlog")
	if err := jhlog.WriteSample(logPath); err != nil {
		t.Fatalf("WriteSample() error = %v", err)
	}
	heap := &HeapEvidence{Leaks: []HeapLeakEvidence{{
		ClassName:           "com.app.checkout.CheckoutActivity",
		Holder:              "com.app.checkout.CheckoutPresenter",
		HolderField:         "com.app.checkout.CheckoutPresenter.activity",
		GCRoot:              "sticky class",
		RetainedSizeKB:      8192,
		RetainedObjectCount: 4,
		Source:              "checkout.hprof",
		ReferencePath: []HeapPathElement{
			{ClassName: "GC root: sticky class", Kind: "gc_root"},
			{ClassName: "com.app.checkout.CheckoutPresenter", Kind: "root_object"},
			{ClassName: "com.app.checkout.CheckoutActivity", FieldName: "activity", Kind: "field"},
		},
		DominatorTree: []string{"com.app.checkout.CheckoutActivity × 1", "android.view.View × 3"},
	}}}

	summary, err := InspectFilesWithOptions("sample", []string{logPath}, Options{HeapEvidence: heap})
	if err != nil {
		t.Fatalf("InspectFilesWithOptions() error = %v", err)
	}
	leak, ok := memoryLeakByClass(summary.MemoryLeaks, "com.app.checkout.CheckoutActivity")
	if !ok {
		t.Fatalf("heap-backed CheckoutActivity leak missing: %+v", summary.MemoryLeaks)
	}
	if leak.HeapEvidence || leak.WatchedObjectAssociation != EvidenceUnknown {
		t.Fatal("class match claimed runtime identity")
	}
	candidate := leak.HeapClassEvidence
	if candidate == nil || candidate.RetainedSizeKB != 8192 || candidate.RetainedObjectCount != 4 || candidate.GCRoot != "sticky class" || candidate.HolderField != "com.app.checkout.CheckoutPresenter.activity" {
		t.Fatalf("lost separate heap candidate: %+v", candidate)
	}
	if leak.EstimatedRetainedKB == 8192 || leak.RetainedObjectCount != 0 || leak.GCRoot != "" || leak.HolderField != "" {
		t.Fatal("candidate details entered watched-object fields")
	}
	if codeProblemsHaveSignal(summary.CodeProblems, "Подтвержденный путь удержания HPROF") {
		t.Fatal("code registry confirms class-only association")
	}

}

func TestLoadHprofHeapEvidenceFindsRootPathAndRetainedSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leak.hprof")
	if err := os.WriteFile(path, syntheticLeakHprof(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	evidence, err := LoadHeapEvidenceFiles([]string{path}, []string{"com.app.LeakedActivity"})
	if err != nil {
		t.Fatalf("LoadHeapEvidenceFiles() error = %v", err)
	}
	if evidence == nil || len(evidence.Leaks) != 1 {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
	leak := evidence.Leaks[0]
	if leak.ClassName != "com.app.LeakedActivity" {
		t.Fatalf("class = %q", leak.ClassName)
	}
	if leak.Holder != "com.app.LeakHolder" {
		t.Fatalf("holder = %q, want com.app.LeakHolder; leak=%+v", leak.Holder, leak)
	}
	if leak.HolderField != "com.app.LeakHolder.static leakedActivity" {
		t.Fatalf("holder field = %q; leak=%+v", leak.HolderField, leak)
	}
	if leak.GCRoot != "sticky class" {
		t.Fatalf("GC root = %q", leak.GCRoot)
	}
	if leak.GCRootCategory != "class/static" {
		t.Fatalf("GC root category = %q", leak.GCRootCategory)
	}
	if leak.ChainFingerprint == "" {
		t.Fatalf("expected chain fingerprint: %+v", leak)
	}
	if leak.LeakPattern != "Activity удерживается цепочкой статического поля или одиночки" {
		t.Fatalf("unexpected leak pattern: %+v", leak)
	}
	if !strings.Contains(leak.Confidence, "сильным ссылкам") {
		t.Fatalf("expected strong-reference confidence: %+v", leak)
	}
	if leak.RetainedObjectCount != 2 || leak.RetainedSizeBytes != 64 || leak.RetainedSizeKB != 1 {
		t.Fatalf("retained size/count mismatch: %+v", leak)
	}
	if len(leak.ReferencePath) < 3 || len(leak.DominatorTree) == 0 {
		t.Fatalf("expected reference path and dominator sample: %+v", leak)
	}
	if len(leak.AlternativePaths) == 0 {
		t.Fatalf("expected alternative reference path: %+v", leak)
	}
}

func TestNormalizeHprofStringReusesPayloadBuffer(t *testing.T) {
	payload := []byte("java/lang/String")

	if got := normalizeHprofString(payload); got != "java.lang.String" {
		t.Fatalf("normalizeHprofString() = %q, want java.lang.String", got)
	}
	if got := string(payload); got != "java.lang.String" {
		t.Fatalf("payload = %q, want in-place normalization before string allocation", got)
	}
}

func TestHprofClassHierarchyCachesOnlyBoundedLayoutSize(t *testing.T) {
	parser := newHprofParser("cached-fields.hprof", nil)
	parser.idSize = 4
	parser.classes[1] = &hprofClass{id: 1, fields: []hprofField{{name: "base", typ: hprofTypeObject}}}
	parser.classes[2] = &hprofClass{
		id:      2,
		superID: 1,
		fields:  []hprofField{{name: "child", typ: hprofTypeInt}},
	}

	if !parser.classHierarchyResolved(2) || !parser.classHierarchyResolved(2) {
		t.Fatal("complete class hierarchy was not resolved")
	}
	if got := parser.instanceLayoutBytes[2]; got != 8 {
		t.Fatalf("cached instance layout = %d bytes, want 8", got)
	}
	if len(parser.instanceLayoutBytes) != 1 {
		t.Fatalf("layout cache stores %d entries for one requested class", len(parser.instanceLayoutBytes))
	}
}

func TestHprofRejectsDuplicateClassDump(t *testing.T) {
	builder := newMiniHprof()
	var heap bytes.Buffer
	builder.classDump(&heap, 0x301, 16, nil, nil)
	builder.classDump(&heap, 0x301, 16, nil, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())

	path := writeMiniHprof(t, builder.bytes())
	parser := newHprofParser(path, map[string]struct{}{"com.app.Target": {}})
	err := parser.parse()

	if err == nil || !strings.Contains(err.Error(), "duplicate HPROF class dump") {
		t.Fatalf("parse() error = %v, want duplicate class diagnostic", err)
	}
}

func TestHprofDefersInstanceUntilCompleteClassHierarchy(t *testing.T) {
	builder := newMiniHprof()
	childName := builder.string("com/app/Child")
	baseName := builder.string("com/app/Base")
	targetName := builder.string("com/app/Target")
	fieldName := builder.string("target")
	builder.loadClass(0x301, childName)
	builder.loadClass(0x302, baseName)
	builder.loadClass(0x303, targetName)
	var heap bytes.Buffer
	builder.classDumpWithSuper(&heap, 0x301, 0x302, 16, nil, nil)
	builder.instanceDump(&heap, 0x401, 0x301, []uint32{0x402})
	builder.classDump(&heap, 0x302, 16, nil, []miniField{{nameID: fieldName, typ: hprofTypeObject}})
	builder.classDump(&heap, 0x303, 16, nil, nil)
	builder.instanceDump(&heap, 0x402, 0x303, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := parseMiniHprof(t, builder.bytes(), defaultHprofLimits())
	child := parser.nodeByID(0x401)
	edges := parser.nodeEdges(child)
	if child == nil || len(edges) != 1 || edges[0].to != 0x402 || edges[0].label != "target" {
		t.Fatalf("inherited reference was lost before superclass dump: %+v", child)
	}
	encoded, err := json.Marshal(parser.pathElement(0x402, edges[0]))
	if err != nil {
		t.Fatal(err)
	}
	var element map[string]any
	if err := json.Unmarshal(encoded, &element); err != nil {
		t.Fatal(err)
	}
	if element["declaring_class"] != "com.app.Base" {
		t.Fatalf("lost exact inherited field owner: %s", encoded)
	}
}

func TestHprofDoesNotResolveAliasBeforeAllClassDumpsAreKnown(t *testing.T) {
	builder := newMiniHprof()
	sharedName := builder.string("com/app/Shared")
	targetName := builder.string("com/app/Target")
	firstField := builder.string("first")
	secondField := builder.string("second")
	builder.loadClass(0x301, sharedName)
	builder.loadClass(0x302, sharedName)
	builder.loadClass(0x303, sharedName)
	builder.loadClass(0x304, targetName)
	var heap bytes.Buffer
	builder.classDump(&heap, 0x301, 16, nil, []miniField{{nameID: firstField, typ: hprofTypeObject}})
	builder.instanceDump(&heap, 0x401, 0x303, []uint32{0x402})
	builder.classDump(&heap, 0x302, 16, nil, []miniField{{nameID: secondField, typ: hprofTypeObject}})
	builder.classDump(&heap, 0x304, 16, nil, nil)
	builder.instanceDump(&heap, 0x402, 0x304, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := parseMiniHprof(t, builder.bytes(), defaultHprofLimits())
	alias := parser.nodeByID(0x401)
	if alias == nil || len(parser.nodeEdges(alias)) != 0 {
		t.Fatalf("ambiguous alias used a class layout before the dump was complete: %+v", alias)
	}
	if !warningContains(parser.degradationWarnings, "не найдено однозначное описание класса") {
		t.Fatalf("missing ambiguous alias warning: %+v", parser.degradationWarnings)
	}
}

func TestLoadHprofHeapEvidenceIgnoresWeakReferenceReferent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "weak.hprof")
	if err := os.WriteFile(path, syntheticWeakReferenceHprof(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	evidence, err := LoadHeapEvidenceFiles([]string{path}, []string{"com.app.LeakedActivity"})
	if err != nil {
		t.Fatalf("LoadHeapEvidenceFiles() error = %v", err)
	}
	if evidence == nil {
		t.Fatalf("expected empty evidence object")
	}
	if len(evidence.Leaks) != 1 || len(evidence.Leaks[0].ReferencePath) != 0 || evidence.Leaks[0].GCRoot != "" {
		t.Fatalf("weak referent should produce only negative class evidence, not a strong leak path: %+v", evidence.Leaks)
	}
}

func TestBetterHeapLeakPrefersActionablePathUnlessSizeDominates(t *testing.T) {
	actionable := HeapLeakEvidence{
		ClassName:      "com.app.LeakedActivity",
		Holder:         "com.app.CheckoutPresenter",
		HolderField:    "com.app.CheckoutPresenter.activity",
		GCRootCategory: "class/static",
		RetainedSizeKB: 6 * 1024,
		ReferencePath: []HeapPathElement{
			{ClassName: "GC root: sticky class", Kind: "gc_root"},
			{ClassName: "com.app.CheckoutPresenter", Kind: "root_object"},
			{ClassName: "com.app.LeakedActivity", FieldName: "activity", Kind: "field"},
		},
		LeakPattern: "Activity удерживается цепочкой статического поля или одиночки",
	}
	noisyLarge := HeapLeakEvidence{
		ClassName:      "com.app.LeakedActivity",
		GCRootCategory: "unknown",
		RetainedSizeKB: 8 * 1024,
		ReferencePath: []HeapPathElement{
			{ClassName: "GC root: unknown", Kind: "gc_root"},
			{ClassName: "java.lang.Object", Kind: "field"},
			{ClassName: "com.app.LeakedActivity", Kind: "field"},
		},
	}
	if !betterHeapLeak(actionable, noisyLarge) {
		t.Fatalf("actionable heap path should win when retained size is close")
	}

	huge := noisyLarge
	huge.RetainedSizeKB = 64 * 1024
	if betterHeapLeak(actionable, huge) {
		t.Fatalf("massively larger retained size should still win")
	}
}

func TestBestHeapEvidencePrefersActionablePathForSameRuntimeLeak(t *testing.T) {
	heap := &HeapEvidence{Leaks: []HeapLeakEvidence{
		{
			ClassName:      "com.app.LeakedActivity",
			GCRootCategory: "unknown",
			RetainedSizeKB: 8 * 1024,
			ReferencePath: []HeapPathElement{
				{ClassName: "GC root: unknown", Kind: "gc_root"},
				{ClassName: "java.lang.Object", Kind: "field"},
				{ClassName: "com.app.LeakedActivity", Kind: "field"},
			},
		},
		{
			ClassName:      "com.app.LeakedActivity",
			Holder:         "com.app.CheckoutPresenter",
			HolderField:    "com.app.CheckoutPresenter.activity",
			GCRootCategory: "class/static",
			RetainedSizeKB: 6 * 1024,
			ReferencePath: []HeapPathElement{
				{ClassName: "GC root: sticky class", Kind: "gc_root"},
				{ClassName: "com.app.CheckoutPresenter", Kind: "root_object"},
				{ClassName: "com.app.LeakedActivity", FieldName: "activity", Kind: "field"},
			},
			LeakPattern: "Activity удерживается цепочкой статического поля или одиночки",
		},
	}}

	best := bestHeapEvidence(memoryLeakStats{className: "com.app.LeakedActivity"}, heap)
	if best == nil || best.Holder != "com.app.CheckoutPresenter" {
		t.Fatalf("bestHeapEvidence() = %+v, want actionable app holder", best)
	}
}

func TestBestHeapEvidenceRejectsClassOnlyMatchWithDifferentKnownHolder(t *testing.T) {
	heap := &HeapEvidence{Leaks: []HeapLeakEvidence{{
		ClassName:   "android.widget.FrameLayout",
		Holder:      "com.app.registration.PreRegistrationActivity",
		HolderField: "com.app.registration.PreRegistrationActivity.contentView",
		ReferencePath: []HeapPathElement{
			{ClassName: "GC root: sticky class", Kind: "gc_root"},
			{ClassName: "com.app.registration.PreRegistrationActivity", Kind: "root_object"},
			{ClassName: "android.widget.FrameLayout", FieldName: "contentView", Kind: "field"},
		},
	}}}

	best := bestHeapEvidence(
		memoryLeakStats{
			className: "android.widget.FrameLayout",
			holder:    "com.app.chat.ChatFragment",
		},
		heap,
	)
	if best != nil {
		t.Fatalf("bestHeapEvidence() = %+v, want no cross-scenario class-only match", best)
	}
}

func TestBestHeapEvidenceMatchesUnreachableClassWithoutInventingHolder(t *testing.T) {
	heap := &HeapEvidence{Leaks: []HeapLeakEvidence{{
		ClassName:  "com.app.LeakedView",
		Source:     "sample.hprof",
		Confidence: "высокое: объект найден, но не достижим от распознанных корней GC",
	}}}

	best := bestHeapEvidence(memoryLeakStats{
		className: "com.app.LeakedView",
		holder:    "com.app.Screen.onDestroyView",
	}, heap)

	if best == nil || best.Source != "sample.hprof" {
		t.Fatalf("class-level negative HPROF evidence was lost: %+v", best)
	}
}

func TestHprofEvidenceLimitsExactRetainedSizeWork(t *testing.T) {
	parser := newHprofParser("large.hprof", map[string]struct{}{"com.app.LeakedActivity": struct{}{}})
	rootID := uint64(1)
	parser.roots = []heapRoot{{id: rootID, kind: "sticky class"}}
	root := parser.ensureNode(rootID, "java.lang.Class", 16)
	for i := 0; i < 512+8; i++ {
		id := uint64(100 + i)
		shallowSize := uint64(48)
		if i >= 512 {
			shallowSize = 4096
		}
		parser.ensureNode(id, "com.app.LeakedActivity", shallowSize)
		parser.addEdge(root, id, "static leaked", "static")
	}

	evidence := parser.evidenceWithBudget(&heapTraversalBudget{remaining: 32})

	if evidence == nil || len(evidence.Leaks) != 1 {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
	if !warningContains(evidence.Warnings, "Точный удержанный размер ограничен") {
		t.Fatalf("expected retained-size limit warning: %+v", evidence.Warnings)
	}
	if !strings.Contains(evidence.Leaks[0].Confidence, "удержанный размер ограничен") {
		t.Fatalf("expected limited confidence on best leak: %+v", evidence.Leaks[0])
	}
}

func TestRetainedSizeReusesRootReachabilityAndNeedsOnlyOneBoundedTraversal(t *testing.T) {
	parser := newHprofParser("single-pass.hprof", nil)
	root := parser.ensureNode(1, "java.lang.Class", 16)
	target := parser.ensureNode(2, "com.app.LeakedActivity", 32)
	parser.ensureNode(3, "android.view.View", 64)
	parser.ensureNode(4, "com.app.Unrelated", 128)
	parser.roots = []heapRoot{{id: root.id, kind: "sticky class"}}
	parser.addEdge(root, target.id, "static leak", "static")
	parser.addEdge(target, 3, "content", "field")
	parser.addEdge(root, 4, "unrelated", "static")

	rootReachability := parser.rootBFS()
	budget := &heapTraversalBudget{remaining: 15}
	scratch := newHeapReachabilityScratch(len(parser.nodes))
	size, count, _, exact := parser.retainedSizeForLimited(
		target.id,
		parser.rootIDs(),
		rootReachability,
		scratch,
		budget,
	)

	if !exact {
		t.Fatalf("retained size unexpectedly exhausted the single-traversal budget")
	}
	if size != 96 || count != 2 {
		t.Fatalf("retained size/count = %d/%d, want 96/2", size, count)
	}
}

func TestRetainedSizeScratchDoesNotLeakReachabilityBetweenTargets(t *testing.T) {
	parser := newHprofParser("scratch.hprof", nil)
	root := parser.ensureNode(1, "java.lang.Class", 16)
	first := parser.ensureNode(2, "com.app.FirstActivity", 32)
	parser.ensureNode(3, "android.view.FirstView", 64)
	second := parser.ensureNode(4, "com.app.SecondActivity", 128)
	parser.ensureNode(5, "android.view.SecondView", 256)
	parser.roots = []heapRoot{{id: root.id, kind: "sticky class"}}
	parser.addEdge(root, first.id, "first", "static")
	parser.addEdge(first, 3, "content", "field")
	parser.addEdge(root, second.id, "second", "static")
	parser.addEdge(second, 5, "content", "field")

	reachability := parser.rootBFS()
	scratch := newHeapReachabilityScratch(len(parser.nodes))
	firstSize, firstCount, _, firstExact := parser.retainedSizeForLimited(
		first.id, parser.rootIDs(), reachability, scratch, &heapTraversalBudget{remaining: 18},
	)
	secondSize, secondCount, _, secondExact := parser.retainedSizeForLimited(
		second.id, parser.rootIDs(), reachability, scratch, &heapTraversalBudget{remaining: 18},
	)

	if !firstExact || firstSize != 96 || firstCount != 2 {
		t.Fatalf("first retained size/count/exact = %d/%d/%t, want 96/2/true", firstSize, firstCount, firstExact)
	}
	if !secondExact || secondSize != 384 || secondCount != 2 {
		t.Fatalf("second retained size/count/exact = %d/%d/%t, want 384/2/true", secondSize, secondCount, secondExact)
	}
}

func TestHprofEvidenceDoesNotSearchAlternativePathsForDiscardedCandidate(t *testing.T) {
	const pathologicalFanIn = 6_001
	const targetClass = "com.app.LeakedActivity"
	parser := newHprofParser("branching.hprof", map[string]struct{}{targetClass: {}})
	root := parser.ensureNode(1, "java.lang.Class", 16)
	discarded := parser.ensureNode(2, targetClass, 32)
	selected := parser.ensureNode(3, targetClass, 32)
	holder := parser.ensureNode(4, "com.app.LeakHolder", 32)
	parser.roots = []heapRoot{{id: root.id, kind: "sticky class"}}
	parser.addEdge(root, discarded.id, "discarded", "static")
	parser.addEdge(root, holder.id, "holder", "static")
	parser.addEdge(holder, selected.id, "activityListener", "field")
	for index := 0; index < pathologicalFanIn; index++ {
		noise := parser.ensureNode(uint64(10+index), "java.lang.Object", 8)
		parser.addEdge(noise, discarded.id, "noise", "field")
	}

	evidence := parser.evidence()

	if evidence == nil || len(evidence.Leaks) != 1 || evidence.Leaks[0].Holder != "com.app.LeakHolder" {
		t.Fatalf("unexpected selected evidence: %+v", evidence)
	}
	if warningContains(evidence.Warnings, "Поиск альтернативных HPROF-путей достиг лимита") {
		t.Fatalf("discarded candidate degraded selected evidence: %+v", evidence.Warnings)
	}
}

func TestHprofEvidenceFindsRepresentativePathsWithoutTraversingUnreachableFanIn(t *testing.T) {
	const pathologicalFanIn = 6_001
	const targetClass = "com.app.LeakedActivity"
	parser := newHprofParser("fan-in.hprof", map[string]struct{}{targetClass: {}})
	root := parser.ensureNode(1, "java.lang.Class", 16)
	firstHolder := parser.ensureNode(2, "com.app.FirstHolder", 32)
	secondHolder := parser.ensureNode(3, "com.app.SecondHolder", 32)
	thirdHolder := parser.ensureNode(4, "com.app.ThirdHolder", 32)
	target := parser.ensureNode(5, targetClass, 64)
	parser.roots = []heapRoot{{id: root.id, kind: "sticky class"}}
	parser.addEdge(root, firstHolder.id, "first", "static")
	parser.addEdge(root, secondHolder.id, "second", "static")
	parser.addEdge(root, thirdHolder.id, "third", "static")
	parser.addEdge(firstHolder, target.id, "listenerA", "field")
	parser.addEdge(secondHolder, target.id, "listenerB", "field")
	parser.addEdge(thirdHolder, target.id, "listenerC", "field")
	for index := 0; index < pathologicalFanIn; index++ {
		noise := parser.ensureNode(uint64(10+index), "java.lang.Object", 8)
		parser.addEdge(noise, target.id, "a-noise", "field")
	}

	evidence := parser.evidence()

	if evidence == nil || len(evidence.Leaks) != 1 {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
	if len(evidence.Leaks[0].AlternativePaths) != 2 {
		t.Fatalf("alternative paths = %d, want 2", len(evidence.Leaks[0].AlternativePaths))
	}
	if warningContains(evidence.Warnings, "Поиск альтернативных HPROF-путей достиг лимита") {
		t.Fatalf("unreachable fan-in degraded representative paths: %+v", evidence.Warnings)
	}
}

func TestHprofAndroidRootTagsAreConsumedAndClassified(t *testing.T) {
	builder := newMiniHprof()
	var heap bytes.Buffer
	expected := map[uint64]string{
		0x101: "finalizing",
		0x102: "debugger",
		0x103: "reference cleanup",
		0x104: "VM internal",
		0x105: "JNI monitor",
		0x106: "sticky class",
	}
	for index, subtag := range []byte{0x8a, 0x8b, 0x8c, 0x8d, 0x8e} {
		heap.WriteByte(subtag)
		writeU4(&heap, uint32(0x101+index))
		if subtag == 0x8e {
			writeU4(&heap, 17)
			writeU4(&heap, 23)
		}
	}
	// A root after JNI monitor proves that both trailing u4 fields were consumed.
	heap.WriteByte(0x05)
	writeU4(&heap, 0x106)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := parseMiniHprof(t, builder.bytes(), defaultHprofLimits())
	if len(parser.roots) != len(expected) {
		t.Fatalf("roots = %+v, want %d roots", parser.roots, len(expected))
	}
	for _, root := range parser.roots {
		want, exists := expected[root.id]
		if !exists || root.kind != want {
			t.Fatalf("root 0x%x kind = %q, want %q; roots=%+v", root.id, root.kind, want, parser.roots)
		}
	}
}

func TestHprofUnreachableRootIsConsumedButNotRetained(t *testing.T) {
	builder := newMiniHprof()
	var heap bytes.Buffer
	heap.WriteByte(0x90)
	writeU4(&heap, 0x201)
	heap.WriteByte(0x05)
	writeU4(&heap, 0x202)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := parseMiniHprof(t, builder.bytes(), defaultHprofLimits())
	if len(parser.roots) != 1 || parser.roots[0].id != 0x202 || parser.roots[0].kind != "sticky class" {
		t.Fatalf("unreachable object must not become a GC root: %+v", parser.roots)
	}
}

func TestHprofReportsTargetThatIsPresentButUnreachableFromGCRoots(t *testing.T) {
	builder := newMiniHprof()
	targetName := builder.string("com/app/Target")
	const (
		targetClassID = uint32(0x301)
		targetID      = uint32(0x401)
		rootID        = uint32(0x501)
	)
	builder.loadClass(targetClassID, targetName)
	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, rootID)
	builder.classDump(&heap, targetClassID, 16, nil, nil)
	builder.instanceDump(&heap, targetID, targetClassID, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := newHprofParser(writeMiniHprof(t, builder.bytes()), map[string]struct{}{"com.app.Target": {}})
	if err := parser.parse(); err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	evidence := parser.evidence()

	if evidence == nil || len(evidence.Leaks) != 1 {
		t.Fatalf("unreachable target must remain distinguishable from an absent target: %+v", evidence)
	}
	target := evidence.Leaks[0]
	if target.Reachability != EvidenceNegative {
		t.Fatalf("complete graph lost explicit negative class reachability: %s", target.Reachability)
	}
	if target.ClassName != "com.app.Target" || len(target.ReferencePath) != 0 || target.GCRoot != "" {
		t.Fatalf("unreachable target was reported as a reachable leak: %+v", target)
	}
	if !strings.Contains(target.Confidence, "не достижим") {
		t.Fatalf("unreachable target confidence is ambiguous: %q", target.Confidence)
	}
}

func TestHprofWithoutRecognizedGCRootsDoesNotClaimTargetIsUnreachable(t *testing.T) {
	parser := newHprofParser("rootless.hprof", map[string]struct{}{"com.app.Target": {}})
	parser.ensureNode(1, "com.app.Target", 16)

	evidence := parser.evidence()

	if evidence == nil || len(evidence.Leaks) != 1 {
		t.Fatalf("rootless target evidence = %+v", evidence)
	}
	confidence := evidence.Leaks[0].Confidence
	if evidence.Leaks[0].Reachability != EvidenceUnknown {
		t.Fatalf("rootless graph must have unknown reachability: %s", evidence.Leaks[0].Reachability)
	}
	if strings.Contains(confidence, "не достижим") || !strings.Contains(confidence, "нет распознанных GC root") {
		t.Fatalf("rootless HPROF produced a false reachability conclusion: %q", confidence)
	}
}

func TestHprofTargetLimitPrefersReachableObjects(t *testing.T) {
	parser := newHprofParser("many-targets.hprof", map[string]struct{}{"com.app.Target": {}})
	root := parser.ensureNode(1, "com.app.Root", 16)
	parser.roots = append(parser.roots, heapRoot{id: root.id, kind: "ROOT UNKNOWN"})
	for id := uint64(2); id < uint64(maxHprofTargets)+2; id++ {
		parser.ensureNode(id, "com.app.Target", 16)
	}
	reachable := parser.ensureNode(uint64(maxHprofTargets)+2, "com.app.Target", 16)
	parser.addEdge(root, reachable.id, "retained", "field")

	evidence := parser.evidence()

	if evidence == nil || len(evidence.Leaks) != 1 || len(evidence.Leaks[0].ReferencePath) == 0 {
		t.Fatalf("reachable target after bounded unreachable objects was lost: %+v", evidence)
	}
}

func TestHprofPrimitiveArrayNoDataKeepsNextSubrecordAligned(t *testing.T) {
	builder := newMiniHprof()
	var heap bytes.Buffer
	heap.WriteByte(hprofSubPrimitiveArrNoData)
	writeU4(&heap, 0x301)
	writeU4(&heap, 0)
	writeU4(&heap, 3)
	heap.WriteByte(hprofTypeInt)
	heap.WriteByte(0x05)
	writeU4(&heap, 0x302)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := parseMiniHprof(t, builder.bytes(), defaultHprofLimits())
	node := parser.nodeByID(0x301)
	if node == nil || node.className != "int[]" || node.shallowSize != 28 {
		t.Fatalf("primitive array without data = %+v, want int[3] with 28-byte shallow size", node)
	}
	if len(parser.roots) != 1 || parser.roots[0].id != 0x302 {
		t.Fatalf("subrecord after primitive array without data is misaligned: %+v", parser.roots)
	}
}

func TestHprofTruncatedRootReturnsRootDiagnostic(t *testing.T) {
	builder := newMiniHprof()
	var heap bytes.Buffer
	heap.WriteByte(0x8e)
	writeU4(&heap, 0x401)
	// JNI monitor also requires thread serial and stack depth.
	builder.record(hprofTagHeapDump, heap.Bytes())

	path := writeMiniHprof(t, builder.bytes())
	parser := newHprofParser(path, map[string]struct{}{"com.app.Target": {}})
	err := parser.parse()
	if err == nil {
		t.Fatal("parse() error = nil, want truncated JNI monitor diagnostic")
	}
	if !strings.Contains(err.Error(), "root subrecord 0x8e") || strings.Contains(err.Error(), "unsupported HPROF heap subrecord") {
		t.Fatalf("parse() error = %q, want root-specific truncation diagnostic", err)
	}
}

func TestHprofIndependentCapsPreserveAlignmentAndLowerConfidence(t *testing.T) {
	tests := []struct {
		name        string
		warningPart string
		limit       func(*hprofLimits)
	}{
		{
			name:        "strings",
			warningPart: "лимит строк HPROF",
			limit:       func(limits *hprofLimits) { limits.strings = 5 },
		},
		{
			name:        "classes",
			warningPart: "лимит классов HPROF",
			limit:       func(limits *hprofLimits) { limits.classes = 3 },
		},
		{
			name:        "roots",
			warningPart: "лимит корней GC",
			limit:       func(limits *hprofLimits) { limits.roots = 1 },
		},
		{
			name:        "nodes",
			warningPart: "лимит объектов HPROF",
			limit:       func(limits *hprofLimits) { limits.nodes = 5 },
		},
		{
			name:        "edges",
			warningPart: "лимит ссылок HPROF",
			limit:       func(limits *hprofLimits) { limits.edges = 2 },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := defaultHprofLimits()
			test.limit(&limits)
			parser := parseMiniHprof(t, syntheticLeakHprof(), limits)
			evidence := parser.evidence()
			if evidence == nil || len(evidence.Leaks) != 1 {
				t.Fatalf("cap must preserve enough aligned data for the leak finding: %+v", evidence)
			}
			if !warningContains(evidence.Warnings, test.warningPart) {
				t.Fatalf("warnings = %+v, want %q", evidence.Warnings, test.warningPart)
			}
			confidence := evidence.Leaks[0].Confidence
			if strings.HasPrefix(confidence, "высокое:") || !strings.Contains(confidence, "граф HPROF неполон") {
				t.Fatalf("confidence was not lowered after %s cap: %q", test.name, confidence)
			}
		})
	}
}

func TestHprofInvalidArrayLengthFailsWithoutAllocation(t *testing.T) {
	builder := newMiniHprof()
	var heap bytes.Buffer
	heap.WriteByte(hprofSubPrimitiveArr)
	writeU4(&heap, 0x501)
	writeU4(&heap, 0)
	writeU4(&heap, ^uint32(0))
	heap.WriteByte(hprofTypeLong)
	builder.record(hprofTagHeapDump, heap.Bytes())

	path := writeMiniHprof(t, builder.bytes())
	parser := newHprofParser(path, map[string]struct{}{"com.app.Target": {}})
	err := parser.parse()
	if err == nil {
		t.Fatal("parse() error = nil, want invalid primitive array length")
	}
	if !strings.Contains(err.Error(), "invalid primitive array length") || !strings.Contains(err.Error(), "need 34359738360 bytes") {
		t.Fatalf("parse() error = %q, want bounded length diagnostic", err)
	}
	if _, err := checkedMulUint64(^uint64(0), 2, "test payload"); err == nil {
		t.Fatal("checkedMulUint64() error = nil, want overflow diagnostic")
	}
	if _, err := checkedAddUint64(^uint64(0), 1, "test shallow size"); err == nil {
		t.Fatal("checkedAddUint64() error = nil, want overflow diagnostic")
	}
}

func TestHprofResolvesUniqueClassDescriptionForAliasID(t *testing.T) {
	builder := newMiniHprof()
	holderName := builder.string("com.app.Holder")
	containerName := builder.string("com.app.Container")
	targetName := builder.string("com.app.Target")
	containerField := builder.string("container")
	targetField := builder.string("target")

	const (
		holderClassID         = uint32(0x100)
		containerClassID      = uint32(0x200)
		containerAliasClassID = uint32(0x201)
		targetClassID         = uint32(0x300)
		containerID           = uint32(0x1001)
		targetID              = uint32(0x1002)
	)
	builder.loadClass(holderClassID, holderName)
	builder.loadClass(containerClassID, containerName)
	builder.loadClass(containerAliasClassID, containerName)
	builder.loadClass(targetClassID, targetName)

	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, holderClassID)
	builder.classDump(&heap, holderClassID, 16, []miniStaticField{{nameID: containerField, valueID: containerID}}, nil)
	builder.instanceDump(&heap, containerID, containerAliasClassID, []uint32{targetID})
	builder.instanceDump(&heap, targetID, targetClassID, nil)
	builder.classDump(&heap, containerClassID, 16, nil, []miniField{{nameID: targetField, typ: hprofTypeObject}})
	builder.classDump(&heap, targetClassID, 16, nil, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := newHprofParser(writeMiniHprof(t, builder.bytes()), map[string]struct{}{"com.app.Target": {}})
	if err := parser.parse(); err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	evidence := parser.evidence()
	if len(evidence.Leaks) != 1 || evidence.Leaks[0].ClassName != "com.app.Target" {
		t.Fatalf("alias class fields did not preserve target path: %+v", evidence)
	}
	if len(evidence.Leaks[0].ReferencePath) < 3 {
		t.Fatalf("alias class path is incomplete: %+v", evidence.Leaks[0].ReferencePath)
	}
}

func TestHprofRetainedSizeApproximationDoesNotClaimIncompleteGraph(t *testing.T) {
	parser := newHprofParser("sample.hprof", map[string]struct{}{})
	parser.degrade("retained-traversal", "retained size is approximate")
	evidence := &HeapEvidence{Leaks: []HeapLeakEvidence{{Confidence: "среднее+: путь найден"}}}

	parser.applyParseQuality(evidence)

	if evidence.Leaks[0].Confidence != "среднее+: путь найден" {
		t.Fatalf("retained-size approximation changed graph confidence: %q", evidence.Leaks[0].Confidence)
	}
	if !warningContains(evidence.Warnings, "retained size is approximate") {
		t.Fatalf("retained-size warning missing: %+v", evidence.Warnings)
	}
}

func TestHprofClassMirrorIsNotReportedAsRetainedInstance(t *testing.T) {
	builder := newMiniHprof()
	targetName := builder.string("com.app.Target")
	const targetClassID = uint32(0x200)
	builder.loadClass(targetClassID, targetName)

	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, targetClassID)
	builder.classDump(&heap, targetClassID, 16, nil, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())

	parser := newHprofParser(writeMiniHprof(t, builder.bytes()), map[string]struct{}{"com.app.Target": {}})
	if err := parser.parse(); err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if evidence := parser.evidence(); len(evidence.Leaks) != 0 {
		t.Fatalf("class mirror was reported as an instance: %+v", evidence.Leaks)
	}
}

func syntheticLeakHprof() []byte {
	builder := newMiniHprof()
	holderName := builder.string("com.app.LeakHolder")
	activityName := builder.string("com.app.LeakedActivity")
	childName := builder.string("com.app.Child")
	leakedField := builder.string("leakedActivity")
	alternateField := builder.string("alternateActivity")
	childField := builder.string("child")

	const (
		holderClassID    = uint32(0x100)
		alternateClassID = uint32(0x180)
		activityClassID  = uint32(0x200)
		childClassID     = uint32(0x300)
		activityID       = uint32(0x1001)
		childID          = uint32(0x1002)
	)
	builder.loadClass(holderClassID, holderName)
	builder.loadClass(alternateClassID, holderName)
	builder.loadClass(activityClassID, activityName)
	builder.loadClass(childClassID, childName)

	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, holderClassID)
	heap.WriteByte(0x05)
	writeU4(&heap, alternateClassID)
	builder.classDump(&heap, holderClassID, 16, []miniStaticField{{nameID: leakedField, valueID: activityID}}, nil)
	builder.classDump(&heap, alternateClassID, 16, []miniStaticField{{nameID: alternateField, valueID: activityID}}, nil)
	builder.classDump(&heap, activityClassID, 48, nil, []miniField{{nameID: childField, typ: hprofTypeObject}})
	builder.classDump(&heap, childClassID, 16, nil, nil)
	builder.instanceDump(&heap, activityID, activityClassID, []uint32{childID})
	builder.instanceDump(&heap, childID, childClassID, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())
	return builder.bytes()
}

func syntheticWeakReferenceHprof() []byte {
	builder := newMiniHprof()
	holderName := builder.string("com.app.LeakHolder")
	weakName := builder.string("java.lang.ref.WeakReference")
	activityName := builder.string("com.app.LeakedActivity")
	weakField := builder.string("weakActivity")
	referentField := builder.string("referent")

	const (
		holderClassID   = uint32(0x100)
		weakClassID     = uint32(0x180)
		activityClassID = uint32(0x200)
		weakID          = uint32(0x1001)
		activityID      = uint32(0x1002)
	)
	builder.loadClass(holderClassID, holderName)
	builder.loadClass(weakClassID, weakName)
	builder.loadClass(activityClassID, activityName)

	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, holderClassID)
	builder.classDump(&heap, holderClassID, 16, []miniStaticField{{nameID: weakField, valueID: weakID}}, nil)
	builder.classDump(&heap, weakClassID, 16, nil, []miniField{{nameID: referentField, typ: hprofTypeObject}})
	builder.classDump(&heap, activityClassID, 48, nil, nil)
	builder.instanceDump(&heap, weakID, weakClassID, []uint32{activityID})
	builder.instanceDump(&heap, activityID, activityClassID, nil)
	builder.record(hprofTagHeapDump, heap.Bytes())
	return builder.bytes()
}

type miniHprof struct {
	out       bytes.Buffer
	nextStrID uint32
}

type miniStaticField struct {
	nameID  uint32
	valueID uint32
}

type miniField struct {
	nameID uint32
	typ    byte
}

func newMiniHprof() *miniHprof {
	builder := &miniHprof{nextStrID: 1}
	builder.out.WriteString("JAVA PROFILE 1.0.3")
	builder.out.WriteByte(0)
	writeU4(&builder.out, 4)
	writeU8(&builder.out, 0)
	return builder
}

func writeMiniHprof(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mini.hprof")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func parseMiniHprof(t *testing.T, data []byte, limits hprofLimits) *hprofParser {
	t.Helper()
	parser := newHprofParser(writeMiniHprof(t, data), map[string]struct{}{"com.app.LeakedActivity": {}})
	parser.limits = limits
	if err := parser.parse(); err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	return parser
}

func (b *miniHprof) bytes() []byte {
	return b.out.Bytes()
}

func (b *miniHprof) string(value string) uint32 {
	id := b.nextStrID
	b.nextStrID++
	var body bytes.Buffer
	writeU4(&body, id)
	body.WriteString(value)
	b.record(hprofTagString, body.Bytes())
	return id
}

func (b *miniHprof) loadClass(classID, nameID uint32) {
	var body bytes.Buffer
	writeU4(&body, classID)
	writeU4(&body, classID)
	writeU4(&body, 0)
	writeU4(&body, nameID)
	b.record(hprofTagLoadClass, body.Bytes())
}

func (b *miniHprof) record(tag byte, body []byte) {
	b.out.WriteByte(tag)
	writeU4(&b.out, 0)
	writeU4(&b.out, uint32(len(body)))
	b.out.Write(body)
}

func (b *miniHprof) classDump(out *bytes.Buffer, classID uint32, instanceSize uint32, staticFields []miniStaticField, fields []miniField) {
	b.classDumpWithSuper(out, classID, 0, instanceSize, staticFields, fields)
}

func (b *miniHprof) classDumpWithSuper(out *bytes.Buffer, classID, superID uint32, instanceSize uint32, staticFields []miniStaticField, fields []miniField) {
	out.WriteByte(hprofSubClassDump)
	writeU4(out, classID)
	writeU4(out, 0)
	writeU4(out, superID)
	for i := 0; i < 5; i++ {
		writeU4(out, 0)
	}
	writeU4(out, instanceSize)
	writeU2(out, 0)
	writeU2(out, uint16(len(staticFields)))
	for _, field := range staticFields {
		writeU4(out, field.nameID)
		out.WriteByte(hprofTypeObject)
		writeU4(out, field.valueID)
	}
	writeU2(out, uint16(len(fields)))
	for _, field := range fields {
		writeU4(out, field.nameID)
		out.WriteByte(field.typ)
	}
}

func (b *miniHprof) instanceDump(out *bytes.Buffer, objectID, classID uint32, objectFields []uint32) {
	out.WriteByte(hprofSubInstanceDump)
	writeU4(out, objectID)
	writeU4(out, 0)
	writeU4(out, classID)
	writeU4(out, uint32(len(objectFields))*4)
	for _, value := range objectFields {
		writeU4(out, value)
	}
}

func writeU2(out *bytes.Buffer, value uint16) {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], value)
	out.Write(buf[:])
}

func writeU4(out *bytes.Buffer, value uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	out.Write(buf[:])
}

func writeU8(out *bytes.Buffer, value uint64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], value)
	out.Write(buf[:])
}

func warningContains(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}
