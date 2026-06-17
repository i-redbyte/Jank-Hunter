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
	if len(summary.MemoryLeaks) != 1 {
		t.Fatalf("unexpected memory leaks: %+v", summary.MemoryLeaks)
	}
	leak := summary.MemoryLeaks[0]
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
	if len(summary.CodeProblems) == 0 || !codeProblemsHaveSignal(summary.CodeProblems, "Подозрение утечки памяти") {
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
	if leak.RetainedObjectCount != 2 || leak.RetainedSizeBytes != 64 || leak.RetainedSizeKB != 1 {
		t.Fatalf("retained size/count mismatch: %+v", leak)
	}
	if len(leak.ReferencePath) < 3 || len(leak.DominatorTree) == 0 {
		t.Fatalf("expected reference path and dominator sample: %+v", leak)
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
	if !warningContains(evidence.Warnings, "Точный retained size ограничен") {
		t.Fatalf("expected retained-size limit warning: %+v", evidence.Warnings)
	}
	if !strings.Contains(evidence.Leaks[0].Confidence, "retained size ограничен") {
		t.Fatalf("expected limited confidence on best leak: %+v", evidence.Leaks[0])
	}
}

func syntheticLeakHprof() []byte {
	builder := newMiniHprof()
	holderName := builder.string("com.app.LeakHolder")
	activityName := builder.string("com.app.LeakedActivity")
	childName := builder.string("com.app.Child")
	leakedField := builder.string("leakedActivity")
	childField := builder.string("child")

	const (
		holderClassID   = uint32(0x100)
		activityClassID = uint32(0x200)
		childClassID    = uint32(0x300)
		activityID      = uint32(0x1001)
		childID         = uint32(0x1002)
	)
	builder.loadClass(holderClassID, holderName)
	builder.loadClass(activityClassID, activityName)
	builder.loadClass(childClassID, childName)

	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, holderClassID)
	builder.classDump(&heap, holderClassID, 16, []miniStaticField{{nameID: leakedField, valueID: activityID}}, nil)
	builder.classDump(&heap, activityClassID, 48, nil, []miniField{{nameID: childField, typ: hprofTypeObject}})
	builder.classDump(&heap, childClassID, 16, nil, nil)
	builder.instanceDump(&heap, activityID, activityClassID, []uint32{childID})
	builder.instanceDump(&heap, childID, childClassID, nil)
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
