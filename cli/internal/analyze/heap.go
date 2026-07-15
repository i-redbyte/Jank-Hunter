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
	maxHprofObjects            = 250_000
	maxHprofEdges              = 1_500_000
	maxHprofTargets            = 2_000
	maxHprofPathElements       = 48
	maxRetainedTreeSample      = 8
	maxHprofExactTargets       = 512
	maxHprofEvidenceTargets    = 4_096
	maxHprofRetainedVisits     = 5_000_000
	maxHprofAlternativePaths   = 3
	maxHprofAlternativeStates  = 6_000
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
		return &HeapEvidence{
			Sources:  []string{path},
			Warnings: []string{"HPROF пропущен: в логе выполнения нет удержанных классов для связывания с дампом памяти."},
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
	path                string
	idSize              int
	strings             map[uint64]string
	classNames          map[uint64]string
	classes             map[uint64]*hprofClass
	nodes               map[uint64]*heapNode
	roots               []heapRoot
	targets             map[string]struct{}
	edgeCount           int
	stringBytes         uint64
	classFieldCount     int
	limits              hprofLimits
	degradationWarnings []string
	degradationKeys     map[string]struct{}
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

type heapIncomingEdge struct {
	from uint64
	edge heapEdge
}

type heapReverseStep struct {
	from uint64
	to   uint64
	edge heapEdge
}

type hprofReader struct {
	r     io.Reader
	read  uint64
	limit uint64
}

func newHprofParser(path string, targets map[string]struct{}) *hprofParser {
	return &hprofParser{
		path:            path,
		strings:         map[uint64]string{},
		classNames:      map[uint64]string{},
		classes:         map[uint64]*hprofClass{},
		nodes:           map[uint64]*heapNode{},
		targets:         targets,
		limits:          defaultHprofLimits(),
		degradationKeys: map[string]struct{}{},
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
		recordLength, err := checkedInt64(uint64(length), "HPROF record length")
		if err != nil {
			return err
		}
		limited := &hprofReader{
			r:     io.LimitReader(reader, recordLength),
			limit: uint64(length),
		}
		switch tag {
		case hprofTagString:
			if err := p.parseStringRecord(limited, length); err != nil {
				return fmt.Errorf("parse HPROF string record in %s: %w", p.path, err)
			}
		case hprofTagLoadClass:
			if err := p.parseLoadClassRecord(limited); err != nil {
				return fmt.Errorf("parse HPROF class mapping in %s: %w", p.path, err)
			}
		case hprofTagHeapDump, hprofTagHeapDumpSeg:
			if err := p.parseHeapDump(limited, length); err != nil {
				return fmt.Errorf("parse HPROF heap record in %s: %w", p.path, err)
			}
		}
		if err := limited.skip(limited.remaining()); err != nil {
			return fmt.Errorf("consume HPROF record 0x%02x in %s: %w", tag, p.path, err)
		}
	}
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
		return fmt.Errorf("%s не является Java HPROF дампом памяти", p.path)
	}
	idSizeRaw, err := readU4(reader)
	if err != nil {
		return fmt.Errorf("read HPROF id size: %w", err)
	}
	p.idSize, err = checkedInt(uint64(idSizeRaw), "HPROF id size")
	if err != nil {
		return err
	}
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
	idSize := uint64(p.idSize)
	if idSize > uint64(length) {
		return fmt.Errorf("invalid HPROF string record length %d", length)
	}
	id, err := reader.readID(p.idSize)
	if err != nil {
		return err
	}
	dataLength := uint64(length) - idSize
	if err := reader.require(dataLength); err != nil {
		return fmt.Errorf("invalid HPROF string payload: %w", err)
	}
	_, exists := p.strings[id]
	if !exists && len(p.strings) >= p.limits.strings {
		p.degrade("strings", fmt.Sprintf(
			"Достигнут лимит строк HPROF (%d): последующие строки прочитаны, но не сохранены.",
			p.limits.strings,
		))
		return reader.skip(dataLength)
	}
	if dataLength > p.limits.stringRecordBytes {
		p.degrade("string-record-bytes", fmt.Sprintf(
			"Строка HPROF превышает безопасный лимит одной записи (%d байт): значение прочитано, но не сохранено.",
			p.limits.stringRecordBytes,
		))
		return reader.skip(dataLength)
	}
	previousSize := uint64(len(p.strings[id]))
	totalWithoutPrevious := p.stringBytes
	if previousSize <= totalWithoutPrevious {
		totalWithoutPrevious -= previousSize
	}
	totalStringBytes, err := checkedAddUint64(totalWithoutPrevious, dataLength, "HPROF string storage")
	if err != nil || totalStringBytes > p.limits.stringBytes {
		p.degrade("string-bytes", fmt.Sprintf(
			"Достигнут общий лимит памяти строк HPROF (%d байт): последующие значения прочитаны, но не сохранены.",
			p.limits.stringBytes,
		))
		return reader.skip(dataLength)
	}
	dataSize, err := checkedInt(dataLength, "HPROF string payload length")
	if err != nil {
		return err
	}
	data := make([]byte, dataSize)
	if _, err := io.ReadFull(reader, data); err != nil {
		return fmt.Errorf("read HPROF string payload: %w", err)
	}
	p.strings[id] = strings.ReplaceAll(string(data), "/", ".")
	p.stringBytes = totalStringBytes
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
		if _, exists := p.classNames[classID]; !exists && len(p.classNames) >= p.limits.classes {
			p.degradeClassLimit()
			return nil
		}
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
				return fmt.Errorf("primitive array subrecord 0x%02x at offset %d: %w", subtag, reader.read, err)
			}
		case hprofSubPrimitiveArrNoData:
			if err := p.parsePrimitiveArrayNoDataDump(reader); err != nil {
				return fmt.Errorf("primitive array subrecord 0x%02x at offset %d: %w", subtag, reader.read, err)
			}
		case hprofSubHeapDumpInfo:
			if _, err := reader.readU4(); err != nil {
				return err
			}
			if _, err := reader.readID(p.idSize); err != nil {
				return err
			}
		default:
			handled, err := p.parseRoot(subtag, reader)
			if err != nil {
				return fmt.Errorf("root subrecord 0x%02x at offset %d: %w", subtag, reader.read, err)
			}
			if handled {
				continue
			}
			return fmt.Errorf("unsupported HPROF heap subrecord 0x%02x in %s", subtag, p.path)
		}
	}
	return nil
}

func (p *hprofParser) parseRoot(subtag byte, reader *hprofReader) (bool, error) {
	if !isHprofRootSubtag(subtag) {
		return false, nil
	}
	id, err := reader.readID(p.idSize)
	if err != nil {
		return true, err
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
	case 0x8e:
		_, err = reader.readU4()
		if err == nil {
			_, err = reader.readU4()
		}
	}
	if err != nil {
		return true, err
	}
	if subtag == 0x90 || id == 0 {
		return true, nil
	}
	if len(p.roots) >= p.limits.roots {
		p.degrade("roots", fmt.Sprintf(
			"Достигнут лимит корней GC в HPROF (%d): последующие корни прочитаны, но не сохранены.",
			p.limits.roots,
		))
		return true, nil
	}
	p.roots = append(p.roots, heapRoot{id: id, kind: hprofRootKind(subtag)})
	return true, nil
}

func isHprofRootSubtag(subtag byte) bool {
	switch subtag {
	case 0xff, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x90:
		return true
	default:
		return false
	}
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
	case 0x8a:
		return "finalizing"
	case 0x8b:
		return "debugger"
	case 0x8c:
		return "reference cleanup"
	case 0x8d:
		return "VM internal"
	case 0x8e:
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
		valueSize, err := p.valueSize(typ)
		if err != nil {
			return fmt.Errorf("invalid constant pool value type: %w", err)
		}
		if err := reader.skip(valueSize); err != nil {
			return err
		}
	}
	name := p.className(classID)
	_, classExists := p.classes[classID]
	storeClass := classExists || len(p.classes) < p.limits.classes
	var class *hprofClass
	if storeClass {
		class = &hprofClass{
			id:           classID,
			name:         name,
			superID:      superID,
			instanceSize: uint64(instanceSize),
		}
	} else {
		p.degradeClassLimit()
	}
	classNode := p.ensureNode(classID, name, uint64(p.idSize)*2)
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
				p.addEdge(classNode, value, "static "+emptyFieldName(fieldName), "static")
			}
			continue
		}
		valueSize, err := p.valueSize(typ)
		if err != nil {
			return fmt.Errorf("invalid static field value type: %w", err)
		}
		if err := reader.skip(valueSize); err != nil {
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
		if _, err := p.valueSize(typ); err != nil {
			return fmt.Errorf("invalid instance field value type: %w", err)
		}
		if storeClass {
			if p.classFieldCount < p.limits.classFields {
				class.fields = append(class.fields, hprofField{
					name:  emptyFieldName(p.strings[nameID]),
					typ:   typ,
					owner: name,
				})
				p.classFieldCount++
			} else {
				p.degrade("class-fields", fmt.Sprintf(
					"Достигнут лимит полей классов HPROF (%d): последующие поля прочитаны, но не сохранены.",
					p.limits.classFields,
				))
			}
		}
	}
	if storeClass {
		p.classes[classID] = class
	}
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
	if err := reader.require(uint64(dataLength)); err != nil {
		return fmt.Errorf("invalid instance payload length %d: %w", dataLength, err)
	}
	className := p.className(classID)
	node := p.ensureNode(objectID, className, p.instanceShallowSize(classID, dataLength))
	fields := p.instanceFields(classID)
	consumed := uint64(0)
	for _, field := range fields {
		size, err := p.valueSize(field.typ)
		if err != nil {
			return fmt.Errorf("invalid field %s.%s: %w", field.owner, field.name, err)
		}
		next, err := checkedAddUint64(consumed, size, "HPROF instance field payload")
		if err != nil {
			return err
		}
		if next > uint64(dataLength) {
			return fmt.Errorf(
				"invalid HPROF instance payload for object 0x%x: field %s.%s requires %d bytes, only %d remain",
				objectID,
				field.owner,
				field.name,
				size,
				uint64(dataLength)-consumed,
			)
		}
		if field.typ == hprofTypeObject {
			target, err := reader.readID(p.idSize)
			if err != nil {
				return err
			}
			if target != 0 && !ignoredReferenceField(className, field.owner, field.name) {
				p.addEdge(node, target, field.name, "field")
			}
		} else if err := reader.skip(size); err != nil {
			return err
		}
		consumed = next
	}
	if consumed < uint64(dataLength) {
		if err := reader.skip(uint64(dataLength) - consumed); err != nil {
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
	payloadSize, err := checkedMulUint64(uint64(length), uint64(p.idSize), "HPROF object array payload")
	if err != nil {
		return err
	}
	if err := reader.require(payloadSize); err != nil {
		return fmt.Errorf("invalid object array length %d: %w", length, err)
	}
	shallowSize, err := checkedAddUint64(payloadSize, 16, "HPROF object array shallow size")
	if err != nil {
		return err
	}
	node := p.ensureNode(arrayID, p.arrayClassName(classID), shallowSize)
	if node == nil || p.hasDegradation("edges") {
		return reader.skip(payloadSize)
	}
	if length > 0 && p.edgeCount >= p.limits.edges {
		p.degradeEdgeLimit()
		return reader.skip(payloadSize)
	}
	for i := uint64(0); i < uint64(length); i++ {
		target, err := reader.readID(p.idSize)
		if err != nil {
			return err
		}
		if target != 0 {
			p.addEdge(node, target, fmt.Sprintf("[%d]", i), "array")
			if p.hasDegradation("edges") {
				remainingElements := uint64(length) - i - 1
				remainingBytes, err := checkedMulUint64(remainingElements, uint64(p.idSize), "remaining HPROF object array payload")
				if err != nil {
					return err
				}
				return reader.skip(remainingBytes)
			}
		}
	}
	return nil
}

func (p *hprofParser) parsePrimitiveArrayDump(reader *hprofReader) error {
	return p.parsePrimitiveArray(reader, true)
}

func (p *hprofParser) parsePrimitiveArrayNoDataDump(reader *hprofReader) error {
	return p.parsePrimitiveArray(reader, false)
}

func (p *hprofParser) parsePrimitiveArray(reader *hprofReader, hasData bool) error {
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
	elementSize, err := p.primitiveValueSize(typ)
	if err != nil {
		return err
	}
	payloadSize, err := checkedMulUint64(uint64(length), elementSize, "HPROF primitive array payload")
	if err != nil {
		return err
	}
	shallowSize, err := checkedAddUint64(payloadSize, 16, "HPROF primitive array shallow size")
	if err != nil {
		return err
	}
	if hasData {
		if err := reader.require(payloadSize); err != nil {
			return fmt.Errorf("invalid primitive array length %d for type 0x%02x: %w", length, typ, err)
		}
	}
	p.ensureNode(arrayID, primitiveArrayName(typ), shallowSize)
	if !hasData {
		return nil
	}
	return reader.skip(payloadSize)
}

func (p *hprofParser) ensureNode(id uint64, className string, shallowSize uint64) *heapNode {
	if id == 0 {
		return nil
	}
	if node := p.nodes[id]; node != nil {
		if node.className == "" || strings.HasPrefix(node.className, "unknown") {
			node.className = className
		}
		if node.shallowSize == 0 {
			node.shallowSize = shallowSize
		}
		return node
	}
	if len(p.nodes) >= p.limits.nodes {
		p.degrade("nodes", fmt.Sprintf(
			"Достигнут лимит объектов HPROF (%d): последующие объекты прочитаны, но не добавлены в runtime-граф.",
			p.limits.nodes,
		))
		return nil
	}
	node := &heapNode{id: id, className: className, shallowSize: shallowSize}
	p.nodes[id] = node
	return node
}

func (p *hprofParser) addEdge(node *heapNode, to uint64, label, kind string) {
	if node == nil || node.id == 0 || to == 0 {
		return
	}
	if p.edgeCount >= p.limits.edges {
		p.degradeEdgeLimit()
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

func (p *hprofParser) valueSize(typ byte) (uint64, error) {
	switch typ {
	case hprofTypeObject:
		return uint64(p.idSize), nil
	case hprofTypeBoolean, hprofTypeByte:
		return 1, nil
	case hprofTypeChar, hprofTypeShort:
		return 2, nil
	case hprofTypeFloat, hprofTypeInt:
		return 4, nil
	case hprofTypeDouble, hprofTypeLong:
		return 8, nil
	default:
		return 0, fmt.Errorf("unsupported HPROF value type 0x%02x", typ)
	}
}

func (p *hprofParser) primitiveValueSize(typ byte) (uint64, error) {
	if typ == hprofTypeObject {
		return 0, fmt.Errorf("object type is invalid for an HPROF primitive array")
	}
	return p.valueSize(typ)
}

func (p *hprofParser) degradeClassLimit() {
	p.degrade("classes", fmt.Sprintf(
		"Достигнут лимит классов HPROF (%d): последующие описания классов и сопоставления имен прочитаны, но не сохранены.",
		p.limits.classes,
	))
}

func (p *hprofParser) degradeEdgeLimit() {
	p.degrade("edges", fmt.Sprintf(
		"Достигнут лимит ссылок HPROF (%d): последующие ссылки прочитаны, но не добавлены в runtime-граф.",
		p.limits.edges,
	))
}

func (p *hprofParser) degrade(key, warning string) {
	if p.degradationKeys == nil {
		p.degradationKeys = map[string]struct{}{}
	}
	if _, exists := p.degradationKeys[key]; exists {
		return
	}
	p.degradationKeys[key] = struct{}{}
	p.degradationWarnings = append(p.degradationWarnings, warning)
}

func (p *hprofParser) hasDegradation(key string) bool {
	_, exists := p.degradationKeys[key]
	return exists
}

func (p *hprofParser) applyParseQuality(evidence *HeapEvidence) {
	if evidence == nil || len(p.degradationWarnings) == 0 {
		return
	}
	evidence.Warnings = append(append([]string(nil), p.degradationWarnings...), evidence.Warnings...)
	for i := range evidence.Leaks {
		evidence.Leaks[i].Confidence = lowerHprofConfidence(evidence.Leaks[i].Confidence)
	}
}

func lowerHprofConfidence(confidence string) string {
	const reason = "граф HPROF неполон из-за безопасных ограничений парсера"
	confidence = strings.TrimSpace(confidence)
	if confidence == "" {
		return "среднее: " + reason
	}
	if strings.Contains(confidence, reason) {
		return confidence
	}
	switch {
	case strings.HasPrefix(confidence, "высокое:"):
		confidence = "среднее:" + strings.TrimPrefix(confidence, "высокое:")
	case strings.HasPrefix(confidence, "среднее+:"):
		confidence = "низкое+:" + strings.TrimPrefix(confidence, "среднее+:")
	case strings.HasPrefix(confidence, "среднее:"):
		confidence = "низкое+:" + strings.TrimPrefix(confidence, "среднее:")
	}
	return confidence + "; " + reason
}

func (p *hprofParser) evidence() *HeapEvidence {
	evidence := &HeapEvidence{Sources: []string{p.path}}
	defer p.applyParseQuality(evidence)
	if len(p.roots) == 0 {
		evidence.Warnings = append(evidence.Warnings, "HPROF не содержит распознанных корней GC.")
		return evidence
	}
	parent := p.rootBFS()
	incoming := p.incomingEdges()
	rootKinds := p.rootKindByID()
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
			p.degrade("target-objects", fmt.Sprintf(
				"HPROF содержит больше %d достижимых target-объектов одного класса: размер и пути рассчитаны по ограниченной выборке.",
				maxHprofTargets,
			))
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
			alternativePaths := p.alternativeReferencePaths(incoming, rootKinds, id, path)
			root := heapRootLabel(path)
			holder := heapHolder(path, className)
			holderField := heapHolderField(path, className)
			rootCategory := heapRootCategory(root)
			referenceMatchers := heapReferenceMatchers(path)
			pattern := heapLeakPattern(className, holder, holderField, rootCategory, path)
			confidence := heapEvidenceConfidence(exact, referenceMatchers)
			candidate := HeapLeakEvidence{
				ClassName:           className,
				Holder:              holder,
				HolderField:         holderField,
				GCRoot:              root,
				GCRootCategory:      rootCategory,
				ChainFingerprint:    heapChainFingerprint(className, holder, holderField, rootCategory, path),
				RetainedSizeKB:      bytesToKB(retainedSize),
				RetainedSizeBytes:   retainedSize,
				RetainedObjectCount: retainedCount,
				ReferencePath:       path,
				AlternativePaths:    alternativePaths,
				DominatorTree:       retainedSample,
				LeakPattern:         pattern,
				ReferenceMatchers:   referenceMatchers,
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
		p.degrade("retained-traversal", fmt.Sprintf(
			"Точный удержанный размер ограничен: для %d объектов использована безопасная оценка, чтобы большой HPROF не зависал.",
			limitedRetainedTargets,
		))
	}
	if skippedEvidenceTargets > 0 {
		p.degrade("evidence-targets", fmt.Sprintf(
			"HPROF содержит слишком много кандидатов удержания: пропущено %d объектов после лимита %d.",
			skippedEvidenceTargets,
			maxHprofEvidenceTargets,
		))
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
		count = saturatingUint64Sum(count, 1)
		if node.shallowSize > 0 {
			size = saturatingUint64Sum(size, node.shallowSize)
		}
		classes[node.className] = saturatingUint64Sum(classes[node.className], 1)
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

func (p *hprofParser) rootKindByID() map[uint64]string {
	out := map[uint64]string{}
	for _, root := range p.roots {
		if root.id == 0 {
			continue
		}
		if _, ok := out[root.id]; !ok {
			out[root.id] = root.kind
		}
	}
	return out
}

func (p *hprofParser) incomingEdges() map[uint64][]heapIncomingEdge {
	out := map[uint64][]heapIncomingEdge{}
	for from, node := range p.nodes {
		if node == nil {
			continue
		}
		for _, edge := range node.edges {
			if edge.to == 0 {
				continue
			}
			out[edge.to] = append(out[edge.to], heapIncomingEdge{from: from, edge: edge})
		}
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool {
			left := out[id][i]
			right := out[id][j]
			if left.edge.kind == right.edge.kind {
				if left.edge.label == right.edge.label {
					return p.nodeClassName(left.from) < p.nodeClassName(right.from)
				}
				return left.edge.label < right.edge.label
			}
			return left.edge.kind < right.edge.kind
		})
	}
	return out
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
	if step, ok := parent[current]; ok && step.hasAny && step.from != 0 && len(reversed) >= maxHprofPathElements {
		p.degrade("reference-path-depth", fmt.Sprintf(
			"Цепочка HPROF превысила лимит глубины %d: показан только ограниченный фрагмент пути.",
			maxHprofPathElements,
		))
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

func (p *hprofParser) alternativeReferencePaths(
	incoming map[uint64][]heapIncomingEdge,
	rootKinds map[uint64]string,
	target uint64,
	primary []HeapPathElement,
) [][]HeapPathElement {
	type state struct {
		id    uint64
		steps []heapReverseStep
	}
	rootIDs := map[uint64]struct{}{}
	for id := range rootKinds {
		rootIDs[id] = struct{}{}
	}
	seenFingerprints := map[string]struct{}{}
	if fp := pathFingerprint(primary); fp != "" {
		seenFingerprints[fp] = struct{}{}
	}
	queue := []state{{id: target}}
	var out [][]HeapPathElement
	visitedStates := 0
	for head := 0; head < len(queue) && len(out) < maxHprofAlternativePaths && visitedStates < maxHprofAlternativeStates; head++ {
		current := queue[head]
		visitedStates++
		if len(current.steps) >= maxHprofPathElements {
			continue
		}
		for _, incomingEdge := range incoming[current.id] {
			if incomingEdge.from == 0 || reversePathContains(current.steps, incomingEdge.from) {
				continue
			}
			nextSteps := append(append([]heapReverseStep(nil), current.steps...), heapReverseStep{
				from: incomingEdge.from,
				to:   current.id,
				edge: incomingEdge.edge,
			})
			if _, isRoot := rootIDs[incomingEdge.from]; isRoot {
				path := p.pathFromReverseSteps(incomingEdge.from, rootKinds[incomingEdge.from], nextSteps)
				fp := pathFingerprint(path)
				if fp == "" {
					continue
				}
				if _, ok := seenFingerprints[fp]; ok {
					continue
				}
				seenFingerprints[fp] = struct{}{}
				out = append(out, path)
				continue
			}
			queue = append(queue, state{id: incomingEdge.from, steps: nextSteps})
		}
	}
	if visitedStates >= maxHprofAlternativeStates && len(out) < maxHprofAlternativePaths {
		p.degrade("alternative-path-search", fmt.Sprintf(
			"Поиск альтернативных HPROF-путей достиг лимита %d состояний: список путей может быть неполным.",
			maxHprofAlternativeStates,
		))
	}
	return out
}

func reversePathContains(steps []heapReverseStep, id uint64) bool {
	for _, step := range steps {
		if step.from == id || step.to == id {
			return true
		}
	}
	return false
}

func (p *hprofParser) pathFromReverseSteps(rootID uint64, rootKind string, steps []heapReverseStep) []HeapPathElement {
	path := []HeapPathElement{{
		ClassName: "GC root: " + rootKind,
		ObjectID:  fmt.Sprintf("0x%x", rootID),
		Kind:      "gc_root",
	}}
	if className := p.nodeClassName(rootID); className != "" {
		path = append(path, HeapPathElement{
			ClassName: className,
			ObjectID:  fmt.Sprintf("0x%x", rootID),
			Kind:      "root_object",
		})
	}
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		path = append(path, HeapPathElement{
			ClassName: p.nodeClassName(step.to),
			FieldName: step.edge.label,
			ObjectID:  fmt.Sprintf("0x%x", step.to),
			Kind:      step.edge.kind,
		})
	}
	return path
}

func (p *hprofParser) nodeClassName(id uint64) string {
	if node := p.nodes[id]; node != nil {
		return node.className
	}
	return ""
}

func betterHeapLeak(candidate, current HeapLeakEvidence) bool {
	if current.ClassName == "" {
		return true
	}
	if heapSizeDominates(candidate.RetainedSizeKB, current.RetainedSizeKB) {
		return true
	}
	if heapSizeDominates(current.RetainedSizeKB, candidate.RetainedSizeKB) {
		return false
	}
	candidateActionability := heapLeakActionabilityScore(candidate)
	currentActionability := heapLeakActionabilityScore(current)
	if candidateActionability != currentActionability {
		return candidateActionability > currentActionability
	}
	if candidate.RetainedSizeKB == current.RetainedSizeKB {
		if len(candidate.ReferencePath) == len(current.ReferencePath) {
			return candidate.Holder < current.Holder
		}
		return len(candidate.ReferencePath) < len(current.ReferencePath)
	}
	return candidate.RetainedSizeKB > current.RetainedSizeKB
}

func heapSizeDominates(left, right uint64) bool {
	if left == 0 || left <= right {
		return false
	}
	if right == 0 {
		return true
	}
	return left-right >= 4*1024 && left/right >= 2
}

func heapLeakActionabilityScore(leak HeapLeakEvidence) int {
	score := 0
	if isLikelyAppClass(leak.Holder) {
		score += 8
	}
	if leak.HolderField != "" {
		score += 5
		if isLikelyAppClass(leak.HolderField) {
			score += 2
		}
	}
	switch leak.GCRootCategory {
	case "class/static":
		score += 5
	case "thread":
		score += 4
	case "jni", "monitor":
		score += 2
	}
	if leak.LeakPattern != "" && leak.LeakPattern != "Сильная цепочка от корня GC удерживает объект" {
		score += 4
	}
	if len(leak.ReferenceMatchers) > 0 {
		score += 3
	}
	if heapPathContainsAppClass(leak.ReferencePath) {
		score += 3
	}
	if len(leak.AlternativePaths) > 0 {
		score += 1
	}
	if len(leak.ReferencePath) > 0 && len(leak.ReferencePath) <= 8 {
		score += 1
	}
	return score
}

func heapPathContainsAppClass(path []HeapPathElement) bool {
	for _, step := range path {
		if isLikelyAppClass(strings.TrimPrefix(step.ClassName, "GC root: ")) {
			return true
		}
	}
	return false
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

func heapRootCategory(root string) string {
	lower := strings.ToLower(strings.TrimSpace(root))
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "sticky class"):
		return "class/static"
	case strings.Contains(lower, "jni"):
		return "jni"
	case strings.Contains(lower, "thread") || strings.Contains(lower, "java frame") || strings.Contains(lower, "native stack"):
		return "thread"
	case strings.Contains(lower, "monitor"):
		return "monitor"
	case strings.Contains(lower, "reference") || strings.Contains(lower, "finalizing"):
		return "reference"
	case strings.Contains(lower, "vm") || strings.Contains(lower, "debugger"):
		return "vm/internal"
	default:
		return "unknown"
	}
}

func ignoredReferenceField(nodeClass, fieldOwner, fieldName string) bool {
	lowerField := strings.ToLower(strings.TrimSpace(fieldName))
	if lowerField != "referent" {
		return false
	}
	for _, owner := range []string{nodeClass, fieldOwner} {
		lowerOwner := strings.ToLower(strings.TrimSpace(owner))
		if lowerOwner == "java.lang.ref.reference" ||
			strings.HasPrefix(lowerOwner, "java.lang.ref.weakreference") ||
			strings.HasPrefix(lowerOwner, "java.lang.ref.softreference") ||
			strings.HasPrefix(lowerOwner, "java.lang.ref.phantomreference") {
			return true
		}
	}
	return false
}

func heapEvidenceConfidence(exact bool, matchers []string) string {
	parts := []string{"высокое: путь найден в HPROF по сильным ссылкам"}
	if !exact {
		parts[0] = "среднее+: путь найден в HPROF по сильным ссылкам, удержанный размер ограничен безопасным лимитом"
	}
	if len(matchers) > 0 {
		parts = append(parts, "совпавшие правила ссылок: "+strings.Join(matchers, ", "))
	}
	return strings.Join(parts, "; ")
}

func heapReferenceMatchers(path []HeapPathElement) []string {
	var out []string
	for _, step := range path {
		className := strings.ToLower(strings.TrimPrefix(step.ClassName, "GC root: "))
		fieldName := strings.ToLower(step.FieldName)
		if strings.Contains(className, "inputmethodmanager") {
			out = append(out, "android.input_method_manager")
		}
		if strings.Contains(className, "viewmodelstore") {
			out = append(out, "androidx.viewmodel_store")
		}
		if strings.Contains(className, "livedata") {
			out = append(out, "androidx.livedata_observer")
		}
		if strings.Contains(className, "recyclerview") {
			out = append(out, "androidx.recyclerview")
		}
		if strings.Contains(className, "compose") {
			out = append(out, "androidx.compose")
		}
		if strings.Contains(className, "kotlinx.coroutines") || strings.Contains(className, "job") ||
			strings.Contains(fieldName, "continuation") {
			out = append(out, "kotlin.coroutines")
		}
		if strings.Contains(className, "textline") {
			out = append(out, "android.text_line_pool")
		}
		if strings.Contains(className, "choreographer") || strings.Contains(className, "handler") || strings.Contains(className, "looper") {
			out = append(out, "android.main_thread_queue")
		}
		if strings.Contains(fieldName, "listener") || strings.Contains(fieldName, "callback") || strings.Contains(fieldName, "observer") {
			out = append(out, "listener_or_callback")
		}
		if strings.Contains(fieldName, "adapter") {
			out = append(out, "adapter_reference")
		}
		if strings.Contains(fieldName, "binding") {
			out = append(out, "view_binding_reference")
		}
		if strings.Contains(fieldName, "mcontext") || strings.HasSuffix(fieldName, ".context") {
			out = append(out, "context_reference")
		}
	}
	return uniqueStrings(out)
}

func heapLeakPattern(className, holder, holderField, rootCategory string, path []HeapPathElement) string {
	lowerClass := strings.ToLower(className)
	lowerHolder := strings.ToLower(holder)
	lowerField := strings.ToLower(holderField)
	switch {
	case strings.Contains(lowerClass, "activity") && strings.Contains(lowerField, "mcontext"):
		return "View/Context цепочка удерживает Activity"
	case strings.Contains(lowerClass, "activity") && rootCategory == "class/static":
		return "Activity удерживается цепочкой статического поля или одиночки"
	case strings.Contains(lowerClass, "fragment") && rootCategory == "class/static":
		return "Fragment удерживается цепочкой статического поля или одиночки"
	case strings.Contains(lowerClass, "viewmodel"):
		return "ViewModel живет после onCleared"
	case strings.Contains(lowerClass, "service"):
		return "Service удерживается после onDestroy"
	case strings.Contains(lowerClass, "dialog"):
		return "Dialog/window цепочка живет после dismiss или onStop"
	case strings.Contains(lowerClass, "viewholder"):
		return "RecyclerView ViewHolder удерживает view/binding после recycle"
	case strings.Contains(lowerClass, "adapter"):
		return "RecyclerView adapter удерживает экран после detach"
	case strings.Contains(lowerClass, "view") || strings.Contains(lowerClass, "binding"):
		return "View/binding живет после onDestroyView или detach"
	case rootCategory == "thread":
		return "Активный поток или очередь удерживает объект жизненного цикла"
	case strings.Contains(lowerHolder, "listener") || strings.Contains(lowerField, "listener") ||
		strings.Contains(lowerHolder, "callback") || strings.Contains(lowerField, "callback"):
		return "Слушатель или обратный вызов удерживает объект после очистки жизненного цикла"
	case pathContainsClass(path, "kotlinx.coroutines") || pathContainsClass(path, "java.util.concurrent"):
		return "Корутина или задача исполнителя удерживает объект"
	default:
		return "Сильная цепочка от корня GC удерживает объект"
	}
}

func pathContainsClass(path []HeapPathElement, needle string) bool {
	needle = strings.ToLower(needle)
	for _, step := range path {
		if strings.Contains(strings.ToLower(step.ClassName), needle) {
			return true
		}
	}
	return false
}

func heapChainFingerprint(className, holder, holderField, rootCategory string, path []HeapPathElement) string {
	parts := []string{
		normalizeLeakToken(className),
		normalizeLeakToken(holder),
		normalizeLeakToken(holderField),
		normalizeLeakToken(rootCategory),
	}
	for _, step := range path {
		classToken := normalizeLeakToken(strings.TrimPrefix(step.ClassName, "GC root: "))
		fieldToken := normalizeLeakToken(step.FieldName)
		kindToken := normalizeLeakToken(step.Kind)
		if classToken == "" && fieldToken == "" && kindToken == "" {
			continue
		}
		parts = append(parts, classToken+"."+fieldToken+"."+kindToken)
	}
	return strings.Join(parts, "|")
}

func pathFingerprint(path []HeapPathElement) string {
	if len(path) == 0 {
		return ""
	}
	return heapChainFingerprint("", "", "", heapRootCategory(heapRootLabel(path)), path)
}

func normalizeLeakToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "gc root: ")
	value = strings.ReplaceAll(value, "/", ".")
	value = strings.ReplaceAll(value, "$", ".")
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '\x00', '\x01', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	})
	return strings.Join(fields, " ")
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

func (r *hprofReader) remaining() uint64 {
	if r.read >= r.limit {
		return 0
	}
	return r.limit - r.read
}

func (r *hprofReader) require(n uint64) error {
	if n > r.remaining() {
		return fmt.Errorf("need %d bytes, only %d remain in the HPROF record", n, r.remaining())
	}
	return nil
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

func (r *hprofReader) skip(n uint64) error {
	if n == 0 {
		return nil
	}
	if err := r.require(n); err != nil {
		return err
	}
	count, err := checkedInt64(n, "HPROF skip length")
	if err != nil {
		return err
	}
	if _, err := io.CopyN(io.Discard, r, count); err != nil {
		return fmt.Errorf("skip %d HPROF bytes: %w", n, err)
	}
	return nil
}

func checkedInt(value uint64, description string) (int, error) {
	maxInt := uint64(^uint(0) >> 1)
	if value > maxInt {
		return 0, fmt.Errorf("%s %d overflows int", description, value)
	}
	return int(value), nil
}

func checkedInt64(value uint64, description string) (int64, error) {
	const maxInt64 = uint64(^uint64(0) >> 1)
	if value > maxInt64 {
		return 0, fmt.Errorf("%s %d overflows int64", description, value)
	}
	return int64(value), nil
}

func checkedAddUint64(left, right uint64, description string) (uint64, error) {
	if ^uint64(0)-left < right {
		return 0, fmt.Errorf("%s overflows uint64: %d + %d", description, left, right)
	}
	return left + right, nil
}

func checkedMulUint64(left, right uint64, description string) (uint64, error) {
	if left != 0 && right > ^uint64(0)/left {
		return 0, fmt.Errorf("%s overflows uint64: %d * %d", description, left, right)
	}
	return left * right, nil
}

func bytesToKB(value uint64) uint64 {
	kilobytes := value / 1024
	if value%1024 != 0 {
		kilobytes++
	}
	return kilobytes
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
