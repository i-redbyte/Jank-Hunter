package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const (
	hprofTagString             = 0x01
	hprofTagLoadClass          = 0x02
	hprofTagHeapDump           = 0x0c
	hprofTagHeapDumpSeg        = 0x1c
	hprofSubClassDump          = 0x20
	hprofSubInstanceDump       = 0x21
	hprofSubObjectArray        = 0x22
	hprofSubPrimitiveArr       = 0x23
	hprofSubPrimitiveArrNoData = 0xc3
	hprofSubHeapDumpInfo       = 0xfe
	hprofTypeObject            = 2
	hprofTypeBoolean           = 4
	hprofTypeChar              = 5
	hprofTypeFloat             = 6
	hprofTypeDouble            = 7
	hprofTypeByte              = 8
	hprofTypeShort             = 9
	hprofTypeInt               = 10
	hprofTypeLong              = 11
	maxHprofStrings            = 500_000
	maxHprofStringBytes        = 64 << 20
	maxHprofStringRecordBytes  = 1 << 20
	maxHprofClasses            = 100_000
	maxHprofClassFields        = 1_500_000
	maxHprofRoots              = 500_000
	maxHprofObjects            = 2_000_000
	maxHprofEdges              = 4_000_000
	maxHprofDeferredBytes      = 64 << 20
	maxHprofTargets            = 2_000
	maxHprofPathElements       = 48
	maxRetainedTreeSample      = 8
	maxHprofEvidenceTargets    = 4_096
	// Counts edge inspections and scan/initialization work, not only newly visited objects.
	// Covers the measured full 2M-node/4M-edge graph while keeping adversarial work finite.
	maxHprofRetainedWork     = 48 * maxHprofObjects
	maxHprofAlternativePaths = 3
	maxHeapEvidenceJSONBytes = 64 << 20
)

func LoadHeapEvidenceFiles(paths []string, targetClasses []string) (*HeapEvidence, error) {
	var parts []*HeapEvidence
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		var (
			evidence *HeapEvidence
			err      error
		)
		switch strings.ToLower(filepath.Ext(path)) {
		case ".hprof":
			evidence, err = loadHprofHeapEvidence(path, targetClasses)
		default:
			evidence, err = loadJSONHeapEvidence(path)
		}
		if err != nil {
			return nil, err
		}
		parts = append(parts, evidence)
	}
	return MergeHeapEvidence(parts...), nil
}

func MergeHeapEvidence(parts ...*HeapEvidence) *HeapEvidence {
	merged := &HeapEvidence{DiagnosticsVersion: HeapDiagnosticsVersion}
	diagnosticSeen := map[HeapDiagnostic]struct{}{}
	sourceSeen := map[string]struct{}{}
	warningSeen := map[string]struct{}{}
	for _, part := range parts {
		if part == nil {
			continue
		}
		for _, d := range part.effectiveDiagnostics() {
			if _, seen := diagnosticSeen[d]; !seen {
				diagnosticSeen[d] = struct{}{}
				merged.Diagnostics = append(merged.Diagnostics, d)
				if !d.Informational() {
					if _, seen := warningSeen[d.Message]; !seen {
						warningSeen[d.Message] = struct{}{}
						merged.Warnings = append(merged.Warnings, d.Message)
					}
				}
			}
		}
		for _, source := range part.Sources {
			source = strings.TrimSpace(source)
			if source == "" {
				continue
			}
			if _, ok := sourceSeen[source]; ok {
				continue
			}
			sourceSeen[source] = struct{}{}
			merged.Sources = append(merged.Sources, source)
		}
		for _, leak := range part.Leaks {
			normalizeHeapLeak(&leak)
			if leak.ClassName == "" {
				continue
			}
			merged.Leaks = append(merged.Leaks, leak)
		}
		for _, warning := range part.Warnings {
			warning = strings.TrimSpace(warning)
			if warning == "" {
				continue
			}
			if _, ok := warningSeen[warning]; ok {
				continue
			}
			warningSeen[warning] = struct{}{}
			merged.Warnings = append(merged.Warnings, warning)
		}
	}
	if len(merged.Leaks) == 0 && len(merged.Warnings) == 0 && len(merged.Sources) == 0 && len(merged.Diagnostics) == 0 {
		return nil
	}
	sort.Strings(merged.Sources)
	return merged
}

func HeapTargetClasses(summary Summary) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, leak := range summary.MemoryLeaks {
		name := strings.TrimSpace(leak.ClassName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func loadJSONHeapEvidence(path string) (*HeapEvidence, error) {
	data, err := readBoundedFile(path, "heap evidence", maxHeapEvidenceJSONBytes)
	if err != nil {
		return nil, err
	}
	var evidence HeapEvidence
	if err := json.Unmarshal(data, &evidence); err == nil && (len(evidence.Leaks) > 0 || len(evidence.Sources) > 0 || len(evidence.Warnings) > 0 || evidence.DiagnosticsVersion != 0 || len(evidence.Diagnostics) > 0) {
		if len(evidence.Sources) == 0 {
			evidence.Sources = []string{path}
		}
		for i := range evidence.Leaks {
			if evidence.Leaks[i].Source == "" {
				evidence.Leaks[i].Source = path
			}
			normalizeHeapLeak(&evidence.Leaks[i])
		}
		return &evidence, nil
	}
	var leaks []HeapLeakEvidence
	if err := json.Unmarshal(data, &leaks); err != nil {
		return nil, fmt.Errorf("read heap evidence %s: %w", path, err)
	}
	evidence = HeapEvidence{Sources: []string{path}, Leaks: leaks}
	for i := range evidence.Leaks {
		if evidence.Leaks[i].Source == "" {
			evidence.Leaks[i].Source = path
		}
		normalizeHeapLeak(&evidence.Leaks[i])
	}
	return &evidence, nil
}

func normalizeHeapLeak(leak *HeapLeakEvidence) {
	if leak.RetainedSizeState != HeapSizeExact && leak.RetainedSizeState != HeapSizeEstimated {
		leak.RetainedSizeState = HeapSizeUnknown
	}
	observedPathState := heapReferencePathState(leak.ReferencePath)
	if observedPathState == HeapPathTruncated {
		leak.ReferencePathState = HeapPathTruncated
	} else if (leak.ReferencePathState != HeapPathComplete && leak.ReferencePathState != HeapPathTruncated) || observedPathState == HeapPathUnknown {
		// Older evidence did not record whether a display fragment was truncated.
		leak.ReferencePathState = HeapPathUnknown
	}
	if leak.GCRootObjectID == "" && len(leak.ReferencePath) > 0 && heapRootLabel(leak.ReferencePath) != "" {
		leak.GCRootObjectID = leak.ReferencePath[0].ObjectID
	}
	if leak.Reachability == "" {
		leak.Reachability = EvidenceUnknown
		if confirmedHeapReferencePath(leak) {
			leak.Reachability = EvidencePositive
		}
	}
	leak.ClassName = strings.TrimSpace(leak.ClassName)
	leak.Holder = strings.TrimSpace(leak.Holder)
	leak.HolderField = strings.TrimSpace(leak.HolderField)
	leak.GCRoot = strings.TrimSpace(leak.GCRoot)
	leak.GCRootCategory = firstNonEmpty(strings.TrimSpace(leak.GCRootCategory), heapRootCategory(leak.GCRoot))
	leak.LeakPattern = strings.TrimSpace(leak.LeakPattern)
	leak.Source = strings.TrimSpace(leak.Source)
	leak.Confidence = strings.TrimSpace(leak.Confidence)
	leak.ReferenceMatchers = uniqueStrings(leak.ReferenceMatchers)
	leak.ChainFingerprint = firstNonEmpty(
		strings.TrimSpace(leak.ChainFingerprint),
		heapChainFingerprint(leak.ClassName, leak.Holder, leak.HolderField, leak.GCRootCategory, leak.ReferencePath),
	)
	if leak.RetainedSizeKB == 0 && leak.RetainedSizeBytes > 0 {
		leak.RetainedSizeKB = bytesToKB(leak.RetainedSizeBytes)
	}
	if leak.RetainedObjectCount == 0 && leak.RetainedSizeKB > 0 {
		leak.RetainedObjectCount = 1
	}
}

func loadHprofHeapEvidence(path string, targetClasses []string) (*HeapEvidence, error) {
	targets := targetClassSet(targetClasses)
	if len(targets) == 0 {
		evidence := &HeapEvidence{Sources: []string{path}}
		evidence.AddDiagnostic(HeapDiagnostic{Code: "no_targets", Severity: HeapDiagnosticInfo, Impact: HeapImpactNone, Source: path, Message: "HPROF пропущен: в логе выполнения нет удержанных классов для связывания с дампом памяти."})
		return evidence, nil
	}
	parser := newHprofParser(path, targets)
	if err := parser.parse(); err != nil {
		return nil, err
	}
	return parser.evidence(), nil
}

func targetClassSet(targetClasses []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, className := range targetClasses {
		className = strings.TrimSpace(className)
		if className == "" {
			continue
		}
		out[className] = struct{}{}
	}
	return out
}

type hprofParser struct {
	path                   string
	idSize                 int
	strings                map[uint64]string
	classNames             map[uint64]string
	classes                map[uint64]*hprofClass
	classesByName          map[string][]*hprofClass
	instanceLayoutBytes    map[uint64]uint64
	nodeIndexes            map[uint64]uint32
	nodes                  []heapNode
	edges                  []storedHeapEdge
	edgeLabels             []string
	edgeLabelIDs           map[string]uint32
	arrayEdgeLabels        []string
	roots                  []heapRoot
	targets                map[string]struct{}
	stringBytes            uint64
	classFieldCount        int
	deferredBytes          uint64
	deferredInstances      []deferredHprofInstance
	limits                 hprofLimits
	degradationDiagnostics []HeapDiagnostic
	degradationWarnings    []string
	degradationKeys        map[string]struct{}

	pendingInstanceSizes     *pendingHprofInstanceSizePage
	pendingInstanceSizesTail *pendingHprofInstanceSizePage
}

type hprofLimits struct {
	strings           int
	stringBytes       uint64
	stringRecordBytes uint64
	classes           int
	classFields       int
	roots             int
	nodes             int
	edges             int
}

func defaultHprofLimits() hprofLimits {
	return hprofLimits{
		strings:           maxHprofStrings,
		stringBytes:       maxHprofStringBytes,
		stringRecordBytes: maxHprofStringRecordBytes,
		classes:           maxHprofClasses,
		classFields:       maxHprofClassFields,
		roots:             maxHprofRoots,
		nodes:             maxHprofObjects,
		edges:             maxHprofEdges,
	}
}

type hprofClass struct {
	id           uint64
	name         string
	superID      uint64
	instanceSize uint64
	fields       []hprofField
}

type deferredHprofInstance struct {
	objectID  uint64
	classID   uint64
	className string
	payload   []byte
}

// Only instances whose exact CLASS_DUMP has not arrived need this temporary record.
// Slots remain valid when nodes grows; duplicate definitions cannot add another entry.
type pendingHprofInstanceSize struct {
	classID  uint64
	nodeSlot uint32
}

// 4096 bytes on 64-bit hosts, including the link and count. Pages avoid repeated
// copying and excess allocation of a growing flat slice when most classes arrive late.
type pendingHprofInstanceSizePage struct {
	next    *pendingHprofInstanceSizePage
	used    int
	entries [255]pendingHprofInstanceSize
}

type hprofField struct {
	name  string
	typ   byte
	owner string
}

type heapNode struct {
	id          uint64
	className   string
	shallowSize uint64
	firstEdge   uint32
	lastEdge    uint32
}

type heapEdge struct {
	owner string
	to    uint64
	label string
	kind  string
}

type storedHeapEdge struct {
	to      uint64
	ownerID uint32
	labelID uint32
	next    uint32
	kind    uint8
}

type heapRoot struct {
	id   uint64
	kind string
}

type heapIncomingEdge struct {
	from uint64
	edge heapEdge
}

type hprofReader struct {
	r     io.Reader
	read  uint64
	limit uint64
}

func newHprofParser(path string, targets map[string]struct{}) *hprofParser {
	return &hprofParser{
		path:                path,
		strings:             map[uint64]string{},
		classNames:          map[uint64]string{},
		classes:             map[uint64]*hprofClass{},
		classesByName:       map[string][]*hprofClass{},
		instanceLayoutBytes: map[uint64]uint64{},
		nodeIndexes:         map[uint64]uint32{},
		edgeLabelIDs:        map[string]uint32{},
		targets:             targets,
		limits:              defaultHprofLimits(),
		degradationKeys:     map[string]struct{}{},
	}
}
