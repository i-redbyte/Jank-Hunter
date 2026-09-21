package analyze

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestHprofInstanceSizeIndependentOfRecordOrder(t *testing.T) {
	for _, payload := range []bool{false, true} {
		for _, inherited := range []bool{false, true} {
			for _, order := range []string{"classes-first", "instances-first", "superclass-last"} {
				t.Run(fmt.Sprintf("payload=%t/inherited=%t/%s", payload, inherited, order), func(t *testing.T) {
					p := parseMiniHprof(t, instanceSizeHprof(order, payload, inherited, 1, 64), defaultHprofLimits())
					if got := p.nodeByID(0x1000).shallowSize; got != 64 {
						t.Fatalf("shallow size=%d, want CLASS_DUMP size 64", got)
					}
					if len(p.degradationWarnings) != 0 {
						t.Fatalf("complete reordered dump degraded: %v", p.degradationWarnings)
					}
					if p.pendingInstanceSizes != nil {
						t.Fatal("temporary size metadata retained after parse")
					}
					before := len(p.edges)
					if err := p.resolveDeferredInstances(); err != nil || len(p.edges) != before || p.nodeByID(0x1000).shallowSize != 64 {
						t.Fatalf("repeated resolution changed graph or size: %v", err)
					}
					leak := p.evidence().Leaks[0]
					if leak.RetainedSizeBytes != 64 || leak.RetainedSizeState != HeapSizeExact {
						t.Fatalf("retained size depends on record order: %+v", leak)
					}
				})
			}
		}
	}
}

func TestHprofRecordedZeroInstanceSizeIsNotPlaceholder(t *testing.T) {
	for _, order := range []string{"classes-first", "instances-first"} {
		p := parseMiniHprof(t, instanceSizeHprof(order, false, false, 1, 0), defaultHprofLimits())
		if got := p.nodeByID(0x1000).shallowSize; got != 0 {
			t.Errorf("%s: recorded zero replaced with %d", order, got)
		}
	}
}

func TestHprofMissingInstanceSizeNeverClaimsExactRetainedBytes(t *testing.T) {
	for _, payload := range []bool{false, true} {
		p := parseMiniHprof(t, instanceSizeHprof("missing-class", payload, false, 1, 64), defaultHprofLimits())
		if len(p.degradationWarnings) == 0 {
			t.Errorf("payload=%t: missing size metadata was silent", payload)
		}
		leak := p.evidence().Leaks[0]
		if leak.RetainedSizeState != HeapSizeUnknown || leak.Reachability != EvidencePositive {
			t.Errorf("payload=%t: missing size must not invent exact bytes or erase observed root: %+v", payload, leak)
		}
	}
}

func TestHprofLateSizeSurvivesMissingSuperclass(t *testing.T) {
	p := parseMiniHprof(t, instanceSizeHprof("missing-superclass", true, true, 1, 64), defaultHprofLimits())
	if got := p.nodeByID(0x1000).shallowSize; got != 64 {
		t.Fatalf("known direct class size=%d, want 64 despite unresolved fields", got)
	}
	if p.evidence().Leaks[0].RetainedSizeState != HeapSizeUnknown {
		t.Fatal("missing superclass still requires incomplete graph state")
	}
}

func TestHprofSizeDoesNotTrustClassNameAlias(t *testing.T) {
	b := newMiniHprof()
	name := b.string("com/app/LeakedActivity")
	b.loadClass(0x301, name)
	b.loadClass(0x302, name)
	var heap bytes.Buffer
	heap.WriteByte(0x05)
	writeU4(&heap, 0x1000)
	b.classDump(&heap, 0x301, 512, nil, nil)
	b.instanceDump(&heap, 0x1000, 0x302, nil)
	b.record(hprofTagHeapDump, heap.Bytes())
	p := parseMiniHprof(t, b.bytes(), defaultHprofLimits())
	leak := p.evidence().Leaks[0]
	if p.nodeByID(0x1000).shallowSize == 512 || leak.RetainedSizeState != HeapSizeUnknown {
		t.Fatalf("class-name alias supplied authoritative size: %+v", leak)
	}
}

func TestHprofLateSizeSurvivesPayloadAndNodeLimits(t *testing.T) {
	for _, limit := range []string{"payload", "nodes"} {
		p := newHprofParser(writeMiniHprof(t, instanceSizeHprof("instances-first", true, false, 3, 64)), nil)
		if limit == "payload" {
			p.deferredBytes = maxHprofDeferredBytes
		}
		if limit == "nodes" {
			p.limits.nodes = 1
		}
		if err := p.parse(); err != nil {
			t.Fatal(err)
		}
		if got := p.nodeByID(0x1000).shallowSize; got != 64 {
			t.Errorf("%s limit prevented size resolution: %d", limit, got)
		}
		if limit == "nodes" && len(p.nodes) != 1 {
			t.Fatal("resolution recreated discarded nodes")
		}
	}
}

func TestHprofDuplicateInstanceCannotAccumulatePendingMetadata(t *testing.T) {
	b := newMiniHprof()
	var heap bytes.Buffer
	b.instanceDump(&heap, 0x1000, 0x301, nil)
	b.instanceDump(&heap, 0x1000, 0x301, nil)
	b.record(hprofTagHeapDump, heap.Bytes())
	p := newHprofParser(writeMiniHprof(t, b.bytes()), nil)
	if err := p.parse(); err == nil || !strings.Contains(err.Error(), "duplicate HPROF object") {
		t.Fatalf("duplicate object must not grow pending metadata: %v", err)
	}
}

func TestHprofInstanceSizePermutationOracle(t *testing.T) {
	// Both retained algorithms must use the same final weights. All objects have
	// independent roots, so the reference result for each class is its recorded size.
	for seed := int64(0); seed < 32; seed++ {
		b := newMiniHprof()
		var records [][]byte
		targets := map[string]struct{}{}
		want := map[string]uint64{}
		for i := uint32(0); i < 9; i++ {
			name := fmt.Sprintf("app.Target%d", i)
			targets[name] = struct{}{}
			size := 8 + 8*i
			want[name] = uint64(size)
			b.loadClass(0x301+i, b.string(name))
			var class, instance bytes.Buffer
			b.classDump(&class, 0x301+i, size, nil, nil)
			instance.WriteByte(0x05)
			writeU4(&instance, 0x1000+i)
			b.instanceDump(&instance, 0x1000+i, 0x301+i, nil)
			records = append(records, class.Bytes(), instance.Bytes())
		}
		rng := rand.New(rand.NewSource(seed))
		rng.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })
		// Separate heap segments also exercise finalization across record boundaries.
		for _, record := range records {
			b.record(hprofTagHeapDumpSeg, record)
		}
		p := newHprofParser(writeMiniHprof(t, b.bytes()), targets)
		if err := p.parse(); err != nil {
			t.Fatal(err)
		}
		e := p.evidence()
		if len(e.Leaks) != len(want) {
			t.Fatal("missing target")
		}
		for _, leak := range e.Leaks {
			if leak.RetainedSizeBytes != want[leak.ClassName] || leak.RetainedSizeState != HeapSizeExact {
				t.Fatalf("seed=%d: independent size oracle mismatch: %+v", seed, leak)
			}
		}
	}
}

func TestHprofPendingSizeRecordsStayBoundedAndCompact(t *testing.T) {
	if unsafe.Sizeof(heapNode{}) != 40 || unsafe.Sizeof(pendingHprofInstanceSize{}) != 16 || unsafe.Sizeof(pendingHprofInstanceSizePage{}) != 4096 {
		t.Fatal("size resolution increased permanent graph storage")
	}
	b := newMiniHprof()
	var heap bytes.Buffer
	for i := uint32(1); i <= 1000; i++ {
		b.instanceDump(&heap, i, 0x301, nil)
	}
	p := newHprofParser("", nil)
	p.idSize = 4
	p.limits.nodes = 777
	reader := &hprofReader{r: bytes.NewReader(heap.Bytes()), limit: uint64(heap.Len())}
	if err := p.parseHeapDump(reader, uint32(heap.Len())); err != nil {
		t.Fatal(err)
	}
	count := 0
	for page := p.pendingInstanceSizes; page != nil; page = page.next {
		count += page.used
	}
	if count != 777 {
		t.Fatalf("pending size entries=%d, want admitted nodes 777", count)
	}
	p.classes[0x301] = &hprofClass{instanceSize: 64}
	if err := p.resolveDeferredInstances(); err != nil {
		t.Fatal(err)
	}
	if p.pendingInstanceSizes != nil || p.pendingInstanceSizesTail != nil {
		t.Fatal("pending storage retained")
	}
	for _, node := range p.nodes {
		if node.shallowSize != 64 {
			t.Fatal("size lost at page boundary")
		}
	}
}

func TestHprofInstanceSizeEndToEnd(t *testing.T) {
	dir := os.Getenv("JH_GM10_FIXTURES")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "retained.jhlog")
	file, writer, err := jhlog.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, event := range []jhlog.Event{
		{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictClass, ID: 1, Value: "com.app.LeakedActivity"}},
		{Type: jhlog.EventRetained, TimeMS: 100, Retained: &jhlog.RetainedEvent{ClassRef: jhlog.LocalSymbol(1), AgeMS: 60000, Count: 1, Evidence: jhlog.RetentionEvidenceAfterExplicitGC}},
	} {
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"classes-first", "instances-first", "missing-class"} {
		hp := filepath.Join(dir, order+".hprof")
		if err := os.WriteFile(hp, instanceSizeHprof(order, true, false, 1, 64), 0600); err != nil {
			t.Fatal(err)
		}
		heap, err := LoadHeapEvidenceFiles([]string{hp}, []string{"com.app.LeakedActivity"})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := InspectFilesWithOptions(order, []string{input}, Options{HeapEvidence: heap})
		if err != nil {
			t.Fatal(err)
		}
		if len(summary.MemoryLeaks) != 1 || summary.MemoryLeaks[0].HeapClassEvidence == nil {
			t.Fatalf("missing candidate: %+v", summary.MemoryLeaks)
		}
		candidate := summary.MemoryLeaks[0].HeapClassEvidence
		want := HeapSizeExact
		if order == "missing-class" {
			want = HeapSizeUnknown
		}
		if candidate.RetainedSizeState != want || want == HeapSizeExact && candidate.RetainedSizeBytes != 64 {
			t.Fatalf("%s: wrong CLI candidate size: %+v", order, candidate)
		}
	}
}
