package analyze

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestInspectAppliesHeapEvidence(t *testing.T) {
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
	if !leak.HeapEvidence {
		t.Fatalf("expected heap evidence: %+v", leak)
	}
	if leak.EstimatedRetainedKB != 8192 || leak.RetainedObjectCount != 4 {
		t.Fatalf("heap retained size/count did not apply: %+v", leak)
	}
	if leak.GCRoot != "sticky class" || leak.HolderField != "com.app.checkout.CheckoutPresenter.activity" {
		t.Fatalf("heap root/holder field did not apply: %+v", leak)
	}
	if leak.DominatorTreeConfidence == "" || leak.LeakChainConfidence == "" {
		t.Fatalf("expected heap confidence strings: %+v", leak)
	}
	if len(summary.CodeProblems) == 0 || !codeProblemsHaveSignal(summary.CodeProblems, "Подтвержденный путь удержания HPROF") {
		t.Fatalf("expected code registry to include heap-backed leak: %+v", summary.CodeProblems)
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
	if len(evidence.Leaks) != 0 {
		t.Fatalf("weak referent should not create strong leak path: %+v", evidence.Leaks)
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

func TestHprofEvidenceLimitsExactRetainedSizeWork(t *testing.T) {
	parser := newHprofParser("large.hprof", map[string]struct{}{"com.app.LeakedActivity": struct{}{}})
	rootID := uint64(1)
	parser.roots = []heapRoot{{id: rootID, kind: "sticky class"}}
	root := parser.ensureNode(rootID, "java.lang.Class", 16)
	for i := 0; i < maxHprofExactTargets+8; i++ {
		id := uint64(100 + i)
		shallowSize := uint64(48)
		if i >= maxHprofExactTargets {
			shallowSize = 4096
		}
		parser.nodes[id] = &heapNode{id: id, className: "com.app.LeakedActivity", shallowSize: shallowSize}
		parser.addEdge(root, id, "static leaked", "static")
	}

	evidence := parser.evidence()

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
	node := parser.nodes[0x301]
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
	out.WriteByte(hprofSubClassDump)
	writeU4(out, classID)
	writeU4(out, 0)
	writeU4(out, 0)
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
