package analyze

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	hprofTagString          = 0x01
	hprofTagLoadClass       = 0x02
	hprofTagHeapDump        = 0x0c
	hprofTagHeapDumpSeg     = 0x1c
	hprofSubClassDump       = 0x20
	hprofSubInstanceDump    = 0x21
	hprofSubObjectArray     = 0x22
	hprofSubPrimitiveArr    = 0x23
	hprofSubHeapDumpInfo    = 0xfe
	hprofTypeObject         = 2
	hprofTypeBoolean        = 4
	hprofTypeChar           = 5
	hprofTypeFloat          = 6
	hprofTypeDouble         = 7
	hprofTypeByte           = 8
	hprofTypeShort          = 9
	hprofTypeInt            = 10
	hprofTypeLong           = 11
	maxHprofObjects         = 250_000
	maxHprofEdges           = 1_500_000
	maxHprofTargets         = 2_000
	maxHprofPathElements    = 48
	maxRetainedTreeSample   = 8
	maxHprofExactTargets    = 512
	maxHprofEvidenceTargets = 4_096
	maxHprofRetainedVisits  = 5_000_000
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
	merged := &HeapEvidence{}
	sourceSeen := map[string]struct{}{}
	warningSeen := map[string]struct{}{}
	for _, part := range parts {
		if part == nil {
			continue
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
	if len(merged.Leaks) == 0 && len(merged.Warnings) == 0 && len(merged.Sources) == 0 {
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var evidence HeapEvidence
	if err := json.Unmarshal(data, &evidence); err == nil && (len(evidence.Leaks) > 0 || len(evidence.Sources) > 0 || len(evidence.Warnings) > 0) {
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
	leak.ClassName = strings.TrimSpace(leak.ClassName)
	leak.Holder = strings.TrimSpace(leak.Holder)
	leak.HolderField = strings.TrimSpace(leak.HolderField)
	leak.GCRoot = strings.TrimSpace(leak.GCRoot)
	leak.Source = strings.TrimSpace(leak.Source)
	leak.Confidence = strings.TrimSpace(leak.Confidence)
	if leak.RetainedSizeKB == 0 && leak.RetainedSizeBytes > 0 {
		leak.RetainedSizeKB = (leak.RetainedSizeBytes + 1023) / 1024
	}
	if leak.RetainedObjectCount == 0 && leak.RetainedSizeKB > 0 {
		leak.RetainedObjectCount = 1
	}
}

func loadHprofHeapEvidence(path string, targetClasses []string) (*HeapEvidence, error) {
	targets := targetClassSet(targetClasses)
	if len(targets) == 0 {
		return &HeapEvidence{
			Sources:  []string{path},
			Warnings: []string{"HPROF пропущен: в runtime-логе нет retained-классов для связывания с дампом памяти."},
		}, nil
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
	path       string
	idSize     int
	strings    map[uint64]string
	classNames map[uint64]string
	classes    map[uint64]*hprofClass
	nodes      map[uint64]*heapNode
	roots      []heapRoot
	targets    map[string]struct{}
	edgeCount  int
	truncated  bool
	warnings   []string
}

type hprofClass struct {
	id           uint64
	name         string
	superID      uint64
	instanceSize uint64
	fields       []hprofField
	staticEdges  []heapEdge
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
	edges       []heapEdge
}

type heapEdge struct {
	to    uint64
	label string
	kind  string
}

type heapRoot struct {
	id   uint64
	kind string
}

type hprofReader struct {
	r    io.Reader
	read uint64
}

func newHprofParser(path string, targets map[string]struct{}) *hprofParser {
	return &hprofParser{
		path:       path,
		strings:    map[uint64]string{},
		classNames: map[uint64]string{},
		classes:    map[uint64]*hprofClass{},
		nodes:      map[uint64]*heapNode{},
		targets:    targets,
	}
}

func (p *hprofParser) parse() error {
	file, err := os.Open(p.path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 128*1024)
	if err := p.readHeader(reader); err != nil {
		return err
	}
	for {
		tag, length, err := readHprofRecordHeader(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read HPROF record: %w", err)
		}
		limited := &hprofReader{r: io.LimitReader(reader, int64(length))}
		switch tag {
		case hprofTagString:
			if err := p.parseStringRecord(limited, length); err != nil {
				return err
			}
		case hprofTagLoadClass:
			if err := p.parseLoadClassRecord(limited); err != nil {
				return err
			}
		case hprofTagHeapDump, hprofTagHeapDumpSeg:
			if err := p.parseHeapDump(limited, length); err != nil {
				return err
			}
		default:
			if _, err := io.Copy(io.Discard, limited); err != nil {
				return err
			}
		}
		if limited.read < uint64(length) {
			if _, err := io.CopyN(io.Discard, reader, int64(uint64(length)-limited.read)); err != nil {
				return err
			}
		}
	}
	p.materializeClassStaticEdges()
	return nil
}

func (p *hprofParser) readHeader(reader *bufio.Reader) error {
	var header []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return fmt.Errorf("read HPROF header: %w", err)
		}
		if b == 0 {
			break
		}
		header = append(header, b)
		if len(header) > 128 {
			return fmt.Errorf("read HPROF header: missing terminator")
		}
	}
	if !strings.HasPrefix(string(header), "JAVA PROFILE ") {
		return fmt.Errorf("%s is not a Java HPROF heap dump", p.path)
	}
	idSizeRaw, err := readU4(reader)
	if err != nil {
		return fmt.Errorf("read HPROF id size: %w", err)
	}
	p.idSize = int(idSizeRaw)
	if p.idSize <= 0 || p.idSize > 8 {
		return fmt.Errorf("unsupported HPROF id size %d", p.idSize)
	}
	if _, err := readU8(reader); err != nil {
		return fmt.Errorf("read HPROF timestamp: %w", err)
	}
	return nil
}

func readHprofRecordHeader(reader *bufio.Reader) (byte, uint32, error) {
	tag, err := reader.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	if _, err := readU4(reader); err != nil {
		return 0, 0, err
	}
	length, err := readU4(reader)
	if err != nil {
		return 0, 0, err
	}
	return tag, length, nil
}

func (p *hprofParser) parseStringRecord(reader *hprofReader, length uint32) error {
	if uint32(p.idSize) > length {
		return fmt.Errorf("invalid HPROF string record length %d", length)
	}
	id, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	data := make([]byte, int(length)-p.idSize)
	if _, err := io.ReadFull(reader, data); err != nil {
		return err
	}
	p.strings[id] = strings.ReplaceAll(string(data), "/", ".")
	return nil
}

func (p *hprofParser) parseLoadClassRecord(reader *hprofReader) error {
	if _, err := reader.readU4(); err != nil {
		return err
	}
	classID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if _, err := reader.readU4(); err != nil {
		return err
	}
	nameID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if name := p.strings[nameID]; name != "" {
		p.classNames[classID] = name
	}
	return nil
}

func (p *hprofParser) parseHeapDump(reader *hprofReader, length uint32) error {
	for reader.read < uint64(length) {
		subtag, err := reader.readByte()
		if err != nil {
			return err
		}
		switch subtag {
		case hprofSubClassDump:
			if err := p.parseClassDump(reader); err != nil {
				return err
			}
		case hprofSubInstanceDump:
			if err := p.parseInstanceDump(reader); err != nil {
				return err
			}
		case hprofSubObjectArray:
			if err := p.parseObjectArrayDump(reader); err != nil {
				return err
			}
		case hprofSubPrimitiveArr:
			if err := p.parsePrimitiveArrayDump(reader); err != nil {
				return err
			}
		case hprofSubHeapDumpInfo:
			if _, err := reader.readU4(); err != nil {
				return err
			}
			if _, err := reader.readID(p.idSize); err != nil {
				return err
			}
		default:
			if p.parseRoot(subtag, reader) {
				continue
			}
			return fmt.Errorf("unsupported HPROF heap subrecord 0x%02x in %s", subtag, p.path)
		}
	}
	return nil
}

func (p *hprofParser) parseRoot(subtag byte, reader *hprofReader) bool {
	kind := hprofRootKind(subtag)
	if kind == "" {
		return false
	}
	id, err := reader.readID(p.idSize)
	if err != nil {
		return false
	}
	switch subtag {
	case 0x01:
		_, err = reader.readID(p.idSize)
	case 0x02, 0x03:
		_, err = reader.readU4()
		if err == nil {
			_, err = reader.readU4()
		}
	case 0x04, 0x06:
		_, err = reader.readU4()
	case 0x08:
		_, err = reader.readU4()
		if err == nil {
			_, err = reader.readU4()
		}
	}
	if err != nil {
		return false
	}
	if id != 0 {
		p.roots = append(p.roots, heapRoot{id: id, kind: kind})
	}
	return true
}

func hprofRootKind(subtag byte) string {
	switch subtag {
	case 0xff:
		return "unknown"
	case 0x01:
		return "JNI global"
	case 0x02:
		return "JNI local"
	case 0x03:
		return "Java frame"
	case 0x04:
		return "native stack"
	case 0x05:
		return "sticky class"
	case 0x06:
		return "thread block"
	case 0x07:
		return "monitor"
	case 0x08:
		return "thread object"
	case 0x89:
		return "interned string"
	case 0x8b:
		return "finalizing"
	case 0x8d:
		return "debugger"
	case 0x8e:
		return "reference cleanup"
	case 0x90:
		return "VM internal"
	case 0x8a:
		return "JNI monitor"
	default:
		return ""
	}
}

func (p *hprofParser) parseClassDump(reader *hprofReader) error {
	classID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if _, err := reader.readU4(); err != nil {
		return err
	}
	superID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		if _, err := reader.readID(p.idSize); err != nil {
			return err
		}
	}
	instanceSize, err := reader.readU4()
	if err != nil {
		return err
	}
	constantCount, err := reader.readU2()
	if err != nil {
		return err
	}
	for i := 0; i < int(constantCount); i++ {
		if _, err := reader.readU2(); err != nil {
			return err
		}
		typ, err := reader.readByte()
		if err != nil {
			return err
		}
		if err := reader.skip(p.valueSize(typ)); err != nil {
			return err
		}
	}
	name := p.className(classID)
	class := &hprofClass{
		id:           classID,
		name:         name,
		superID:      superID,
		instanceSize: uint64(instanceSize),
	}
	staticCount, err := reader.readU2()
	if err != nil {
		return err
	}
	for i := 0; i < int(staticCount); i++ {
		nameID, err := reader.readID(p.idSize)
		if err != nil {
			return err
		}
		fieldName := p.strings[nameID]
		typ, err := reader.readByte()
		if err != nil {
			return err
		}
		if typ == hprofTypeObject {
			value, err := reader.readID(p.idSize)
			if err != nil {
				return err
			}
			if value != 0 {
				class.staticEdges = append(class.staticEdges, heapEdge{
					to:    value,
					label: "static " + emptyFieldName(fieldName),
					kind:  "static",
				})
			}
			continue
		}
		if err := reader.skip(p.valueSize(typ)); err != nil {
			return err
		}
	}
	fieldCount, err := reader.readU2()
	if err != nil {
		return err
	}
	for i := 0; i < int(fieldCount); i++ {
		nameID, err := reader.readID(p.idSize)
		if err != nil {
			return err
		}
		typ, err := reader.readByte()
		if err != nil {
			return err
		}
		class.fields = append(class.fields, hprofField{
			name:  emptyFieldName(p.strings[nameID]),
			typ:   typ,
			owner: name,
		})
	}
	p.classes[classID] = class
	p.ensureNode(classID, class.name, uint64(p.idSize)*2)
	return nil
}

func (p *hprofParser) parseInstanceDump(reader *hprofReader) error {
	objectID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if _, err := reader.readU4(); err != nil {
		return err
	}
	classID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	dataLength, err := reader.readU4()
	if err != nil {
		return err
	}
	node := p.ensureNode(objectID, p.className(classID), p.instanceShallowSize(classID, dataLength))
	fields := p.instanceFields(classID)
	consumed := uint32(0)
	for _, field := range fields {
		size := uint32(p.valueSize(field.typ))
		if consumed+size > dataLength {
			break
		}
		if field.typ == hprofTypeObject {
			target, err := reader.readID(p.idSize)
			if err != nil {
				return err
			}
			if target != 0 {
				p.addEdge(node, target, field.name, "field")
			}
		} else if err := reader.skip(int(size)); err != nil {
			return err
		}
		consumed += size
	}
	if consumed < dataLength {
		if err := reader.skip(int(dataLength - consumed)); err != nil {
			return err
		}
	}
	return nil
}

func (p *hprofParser) parseObjectArrayDump(reader *hprofReader) error {
	arrayID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if _, err := reader.readU4(); err != nil {
		return err
	}
	length, err := reader.readU4()
	if err != nil {
		return err
	}
	classID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	node := p.ensureNode(arrayID, p.arrayClassName(classID), uint64(p.idSize)*uint64(length)+16)
	for i := 0; i < int(length); i++ {
		target, err := reader.readID(p.idSize)
		if err != nil {
			return err
		}
		if target != 0 {
			p.addEdge(node, target, fmt.Sprintf("[%d]", i), "array")
		}
	}
	return nil
}

func (p *hprofParser) parsePrimitiveArrayDump(reader *hprofReader) error {
	arrayID, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	if _, err := reader.readU4(); err != nil {
		return err
	}
	length, err := reader.readU4()
	if err != nil {
		return err
	}
	typ, err := reader.readByte()
	if err != nil {
		return err
	}
	size := uint64(p.valueSize(typ))*uint64(length) + 16
	p.ensureNode(arrayID, primitiveArrayName(typ), size)
	return reader.skip(int(uint64(p.valueSize(typ)) * uint64(length)))
}

func (p *hprofParser) materializeClassStaticEdges() {
	for _, class := range p.classes {
		node := p.ensureNode(class.id, class.name, uint64(p.idSize)*2)
		for _, edge := range class.staticEdges {
			p.addEdge(node, edge.to, edge.label, edge.kind)
		}
	}
}

func (p *hprofParser) ensureNode(id uint64, className string, shallowSize uint64) *heapNode {
	if node := p.nodes[id]; node != nil {
		if node.className == "" || strings.HasPrefix(node.className, "unknown") {
			node.className = className
		}
		if node.shallowSize == 0 {
			node.shallowSize = shallowSize
		}
		return node
	}
	if len(p.nodes) >= maxHprofObjects {
		p.truncated = true
		return &heapNode{id: id, className: className, shallowSize: shallowSize}
	}
	node := &heapNode{id: id, className: className, shallowSize: shallowSize}
	p.nodes[id] = node
	return node
}

func (p *hprofParser) addEdge(node *heapNode, to uint64, label, kind string) {
	if node == nil || p.truncated || node.id == 0 || to == 0 {
		return
	}
	if p.edgeCount >= maxHprofEdges {
		p.truncated = true
		return
	}
	node.edges = append(node.edges, heapEdge{to: to, label: label, kind: kind})
	p.edgeCount++
}

func (p *hprofParser) className(classID uint64) string {
	if name := p.classNames[classID]; name != "" {
		return name
	}
	if class := p.classes[classID]; class != nil && class.name != "" {
		return class.name
	}
	return fmt.Sprintf("unknown.class.%x", classID)
}

func (p *hprofParser) arrayClassName(classID uint64) string {
	name := p.className(classID)
	if strings.HasPrefix(name, "[") {
		return name
	}
	return name + "[]"
}

func (p *hprofParser) instanceShallowSize(classID uint64, dataLength uint32) uint64 {
	if class := p.classes[classID]; class != nil && class.instanceSize > 0 {
		return class.instanceSize
	}
	if dataLength > 0 {
		return uint64(dataLength)
	}
	return uint64(p.idSize) * 2
}

func (p *hprofParser) instanceFields(classID uint64) []hprofField {
	var out []hprofField
	seen := map[uint64]struct{}{}
	for classID != 0 {
		if _, ok := seen[classID]; ok {
			break
		}
		seen[classID] = struct{}{}
		class := p.classes[classID]
		if class == nil {
			break
		}
		out = append(out, class.fields...)
		classID = class.superID
	}
	return out
}

func (p *hprofParser) valueSize(typ byte) int {
	switch typ {
	case hprofTypeObject:
		return p.idSize
	case hprofTypeBoolean, hprofTypeByte:
		return 1
	case hprofTypeChar, hprofTypeShort:
		return 2
	case hprofTypeFloat, hprofTypeInt:
		return 4
	case hprofTypeDouble, hprofTypeLong:
		return 8
	default:
		return 0
	}
}

func (p *hprofParser) evidence() *HeapEvidence {
	evidence := &HeapEvidence{Sources: []string{p.path}}
	if p.truncated {
		evidence.Warnings = append(evidence.Warnings, fmt.Sprintf("HPROF %s разобран частично: достигнут безопасный лимит %d объектов или %d ссылок.", p.path, maxHprofObjects, maxHprofEdges))
	}
	if len(p.roots) == 0 {
		evidence.Warnings = append(evidence.Warnings, "HPROF не содержит распознанных GC roots.")
		return evidence
	}
	parent := p.rootBFS()
	targetsByClass := p.targetNodes(parent)
	rootIDs := p.rootIDs()
	exactBudget := &heapTraversalBudget{remaining: maxHprofRetainedVisits}
	exactTargets := 0
	evidenceTargets := 0
	skippedEvidenceTargets := 0
	limitedRetainedTargets := 0
	for className, ids := range targetsByClass {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if len(ids) > maxHprofTargets {
			ids = ids[:maxHprofTargets]
			evidence.Warnings = append(evidence.Warnings, fmt.Sprintf("Класс %s: для retained size использованы первые %d достижимых объектов.", className, maxHprofTargets))
		}
		best := HeapLeakEvidence{ClassName: className, Source: p.path}
		for _, id := range ids {
			if evidenceTargets >= maxHprofEvidenceTargets {
				skippedEvidenceTargets++
				continue
			}
			evidenceTargets++
			retainedSize, retainedCount, retainedSample, exact := p.retainedSizeForLimited(id, rootIDs, exactBudget, exactTargets < maxHprofExactTargets)
			if exact {
				exactTargets++
			} else {
				limitedRetainedTargets++
			}
			path := p.referencePath(parent, id)
			confidence := "высокое: путь найден в HPROF"
			if !exact {
				confidence = "среднее: путь найден в HPROF, retained size ограничен безопасным лимитом"
			}
			candidate := HeapLeakEvidence{
				ClassName:           className,
				Holder:              heapHolder(path, className),
				HolderField:         heapHolderField(path, className),
				GCRoot:              heapRootLabel(path),
				RetainedSizeKB:      (retainedSize + 1023) / 1024,
				RetainedSizeBytes:   retainedSize,
				RetainedObjectCount: retainedCount,
				ReferencePath:       path,
				DominatorTree:       retainedSample,
				Source:              p.path,
				Confidence:          confidence,
			}
			if betterHeapLeak(candidate, best) {
				best = candidate
			}
		}
		if best.RetainedObjectCount == 0 {
			best.RetainedObjectCount = uint64(len(ids))
		}
		normalizeHeapLeak(&best)
		evidence.Leaks = append(evidence.Leaks, best)
	}
	if limitedRetainedTargets > 0 {
		evidence.Warnings = append(evidence.Warnings, fmt.Sprintf("Точный retained size ограничен: для %d объектов использована безопасная оценка, чтобы большой HPROF не зависал.", limitedRetainedTargets))
	}
	if skippedEvidenceTargets > 0 {
		evidence.Warnings = append(evidence.Warnings, fmt.Sprintf("HPROF содержит слишком много retained-кандидатов: пропущено %d объектов после лимита %d.", skippedEvidenceTargets, maxHprofEvidenceTargets))
	}
	sort.Slice(evidence.Leaks, func(i, j int) bool {
		if evidence.Leaks[i].RetainedSizeKB == evidence.Leaks[j].RetainedSizeKB {
			return evidence.Leaks[i].ClassName < evidence.Leaks[j].ClassName
		}
		return evidence.Leaks[i].RetainedSizeKB > evidence.Leaks[j].RetainedSizeKB
	})
	return evidence
}

type heapTraversalBudget struct {
	remaining int
	exhausted bool
}

type heapParent struct {
	from   uint64
	root   string
	edge   heapEdge
	hasAny bool
}

func (p *hprofParser) rootBFS() map[uint64]heapParent {
	parent := map[uint64]heapParent{}
	queue := make([]uint64, 0, len(p.roots))
	for _, root := range p.roots {
		if root.id == 0 {
			continue
		}
		if _, ok := parent[root.id]; ok {
			continue
		}
		parent[root.id] = heapParent{root: root.kind, hasAny: true}
		queue = append(queue, root.id)
	}
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		node := p.nodes[id]
		if node == nil {
			continue
		}
		for _, edge := range node.edges {
			if edge.to == 0 {
				continue
			}
			if _, ok := parent[edge.to]; ok {
				continue
			}
			parent[edge.to] = heapParent{from: id, edge: edge, hasAny: true}
			queue = append(queue, edge.to)
		}
	}
	return parent
}

func (p *hprofParser) targetNodes(parent map[uint64]heapParent) map[string][]uint64 {
	out := map[string][]uint64{}
	for id, node := range p.nodes {
		if node == nil {
			continue
		}
		if _, ok := p.targets[node.className]; !ok {
			continue
		}
		if _, reachable := parent[id]; !reachable {
			continue
		}
		out[node.className] = append(out[node.className], id)
	}
	return out
}

func (p *hprofParser) retainedSizeForLimited(target uint64, rootIDs []uint64, budget *heapTraversalBudget, allowExact bool) (uint64, uint64, []string, bool) {
	if !allowExact || budget == nil || budget.exhausted {
		return p.shallowRetainedFallback(target)
	}
	fromTarget := p.reachableFromLimited([]uint64{target}, 0, budget)
	if budget.exhausted {
		return p.shallowRetainedFallback(target)
	}
	withoutTarget := p.reachableFromLimited(rootIDs, target, budget)
	if budget.exhausted {
		return p.shallowRetainedFallback(target)
	}
	var size uint64
	var count uint64
	classes := map[string]uint64{}
	for id := range fromTarget {
		if withoutTarget[id] {
			continue
		}
		node := p.nodes[id]
		if node == nil {
			continue
		}
		count++
		if node.shallowSize > 0 {
			size += node.shallowSize
		}
		classes[node.className]++
	}
	if size == 0 {
		if node := p.nodes[target]; node != nil && node.shallowSize > 0 {
			size = node.shallowSize
		}
	}
	return size, count, retainedClassSample(classes), true
}

func (p *hprofParser) shallowRetainedFallback(target uint64) (uint64, uint64, []string, bool) {
	node := p.nodes[target]
	if node == nil {
		return 0, 0, nil, false
	}
	classes := map[string]uint64{node.className: 1}
	return node.shallowSize, 1, retainedClassSample(classes), false
}

func (p *hprofParser) reachableFromLimited(start []uint64, blocked uint64, budget *heapTraversalBudget) map[uint64]bool {
	seen := map[uint64]bool{}
	queue := make([]uint64, 0, len(start))
	for _, id := range start {
		if id == 0 || id == blocked || seen[id] {
			continue
		}
		if !budget.take() {
			return seen
		}
		seen[id] = true
		queue = append(queue, id)
	}
	for head := 0; head < len(queue); head++ {
		node := p.nodes[queue[head]]
		if node == nil {
			continue
		}
		for _, edge := range node.edges {
			if edge.to == 0 || edge.to == blocked || seen[edge.to] {
				continue
			}
			if !budget.take() {
				return seen
			}
			seen[edge.to] = true
			queue = append(queue, edge.to)
		}
	}
	return seen
}

func (b *heapTraversalBudget) take() bool {
	if b.remaining <= 0 {
		b.exhausted = true
		return false
	}
	b.remaining--
	return true
}

func (p *hprofParser) rootIDs() []uint64 {
	ids := make([]uint64, 0, len(p.roots))
	for _, root := range p.roots {
		ids = append(ids, root.id)
	}
	return ids
}

func (p *hprofParser) referencePath(parent map[uint64]heapParent, target uint64) []HeapPathElement {
	var reversed []HeapPathElement
	current := target
	for i := 0; i < maxHprofPathElements; i++ {
		step, ok := parent[current]
		if !ok || !step.hasAny {
			break
		}
		node := p.nodes[current]
		className := ""
		if node != nil {
			className = node.className
		}
		if step.from == 0 {
			if className != "" {
				reversed = append(reversed, HeapPathElement{
					ClassName: className,
					ObjectID:  fmt.Sprintf("0x%x", current),
					Kind:      "root_object",
				})
			}
			reversed = append(reversed, HeapPathElement{
				ClassName: "GC root: " + step.root,
				ObjectID:  fmt.Sprintf("0x%x", current),
				Kind:      "gc_root",
			})
			break
		}
		reversed = append(reversed, HeapPathElement{
			ClassName: className,
			FieldName: step.edge.label,
			ObjectID:  fmt.Sprintf("0x%x", current),
			Kind:      step.edge.kind,
		})
		current = step.from
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

func betterHeapLeak(candidate, current HeapLeakEvidence) bool {
	if current.ClassName == "" {
		return true
	}
	if candidate.RetainedSizeKB == current.RetainedSizeKB {
		if len(candidate.ReferencePath) == len(current.ReferencePath) {
			return candidate.Holder < current.Holder
		}
		return len(candidate.ReferencePath) < len(current.ReferencePath)
	}
	return candidate.RetainedSizeKB > current.RetainedSizeKB
}

func retainedClassSample(classes map[string]uint64) []string {
	type row struct {
		name  string
		count uint64
	}
	rows := make([]row, 0, len(classes))
	for name, count := range classes {
		rows = append(rows, row{name: name, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count == rows[j].count {
			return rows[i].name < rows[j].name
		}
		return rows[i].count > rows[j].count
	})
	if len(rows) > maxRetainedTreeSample {
		rows = rows[:maxRetainedTreeSample]
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s × %d", row.name, row.count))
	}
	return out
}

func heapHolder(path []HeapPathElement, targetClass string) string {
	holder := ""
	for _, step := range path {
		className := strings.TrimPrefix(step.ClassName, "GC root: ")
		if className == targetClass || strings.HasPrefix(step.ClassName, "GC root: ") {
			continue
		}
		if isLikelyAppClass(className) {
			holder = className
		}
	}
	return holder
}

func heapHolderField(path []HeapPathElement, targetClass string) string {
	for i := len(path) - 1; i >= 0; i-- {
		step := path[i]
		if step.ClassName != targetClass || i == 0 {
			continue
		}
		prev := path[i-1]
		if prev.ClassName == "" || strings.HasPrefix(prev.ClassName, "GC root: ") || step.FieldName == "" {
			return ""
		}
		return prev.ClassName + "." + step.FieldName
	}
	return ""
}

func heapRootLabel(path []HeapPathElement) string {
	if len(path) == 0 {
		return ""
	}
	if strings.HasPrefix(path[0].ClassName, "GC root: ") {
		return strings.TrimPrefix(path[0].ClassName, "GC root: ")
	}
	return path[0].Kind
}

func emptyFieldName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "<field>"
	}
	return name
}

func primitiveArrayName(typ byte) string {
	switch typ {
	case hprofTypeBoolean:
		return "boolean[]"
	case hprofTypeChar:
		return "char[]"
	case hprofTypeFloat:
		return "float[]"
	case hprofTypeDouble:
		return "double[]"
	case hprofTypeByte:
		return "byte[]"
	case hprofTypeShort:
		return "short[]"
	case hprofTypeInt:
		return "int[]"
	case hprofTypeLong:
		return "long[]"
	default:
		return "primitive[]"
	}
}

func (r *hprofReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += uint64(n)
	return n, err
}

func (r *hprofReader) readByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}

func (r *hprofReader) readU2() (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func (r *hprofReader) readU4() (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func (r *hprofReader) readID(idSize int) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[8-idSize:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(buf[:]), nil
}

func (r *hprofReader) skip(n int) error {
	if n <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}

func readU4(reader io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(reader, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func readU8(reader io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(reader, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b[:]), nil
}
