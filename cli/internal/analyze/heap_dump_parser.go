package analyze

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

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
		p.classesByName[class.name] = append(p.classesByName[class.name], class)
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
	if p.resolveClass(classID) == nil && dataLength > 0 {
		return p.deferInstance(reader, objectID, classID, className, dataLength)
	}
	return p.parseInstancePayload(reader, node, objectID, classID, className, dataLength)
}

func (p *hprofParser) deferInstance(
	reader *hprofReader,
	objectID uint64,
	classID uint64,
	className string,
	dataLength uint32,
) error {
	nextBytes, err := checkedAddUint64(p.deferredBytes, uint64(dataLength), "HPROF deferred instance storage")
	if err != nil || nextBytes > maxHprofDeferredBytes {
		p.degrade("deferred-instances", fmt.Sprintf(
			"Достигнут лимит отложенных полей HPROF (%d байт): часть ссылок экземпляров не добавлена в runtime-граф.",
			maxHprofDeferredBytes,
		))
		return reader.skip(uint64(dataLength))
	}
	payloadSize, err := checkedInt(uint64(dataLength), "HPROF deferred instance payload")
	if err != nil {
		return err
	}
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return err
	}
	p.deferredBytes = nextBytes
	p.deferredInstances = append(p.deferredInstances, deferredHprofInstance{
		objectID:  objectID,
		classID:   classID,
		className: className,
		payload:   payload,
	})
	return nil
}

func (p *hprofParser) resolveDeferredInstances() error {
	unresolved := 0
	for _, instance := range p.deferredInstances {
		if p.resolveClass(instance.classID) == nil {
			unresolved++
			continue
		}
		node := p.ensureNode(
			instance.objectID,
			instance.className,
			p.instanceShallowSize(instance.classID, uint32(len(instance.payload))),
		)
		reader := &hprofReader{r: bytes.NewReader(instance.payload), limit: uint64(len(instance.payload))}
		if err := p.parseInstancePayload(
			reader,
			node,
			instance.objectID,
			instance.classID,
			instance.className,
			uint32(len(instance.payload)),
		); err != nil {
			return fmt.Errorf("parse deferred HPROF instance 0x%x: %w", instance.objectID, err)
		}
	}
	if unresolved > 0 {
		p.degrade("unresolved-instance-classes", fmt.Sprintf(
			"Для %d экземпляров HPROF не найдено однозначное описание класса: их ссылки не добавлены в runtime-граф.",
			unresolved,
		))
	}
	p.deferredInstances = nil
	p.deferredBytes = 0
	return nil
}

func (p *hprofParser) parseInstancePayload(
	reader *hprofReader,
	node *heapNode,
	objectID uint64,
	classID uint64,
	className string,
	dataLength uint32,
) error {
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
	if class := p.resolveClass(classID); class != nil && class.instanceSize > 0 {
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
		class := p.resolveClass(classID)
		if class == nil {
			break
		}
		out = append(out, class.fields...)
		classID = class.superID
	}
	return out
}

func (p *hprofParser) resolveClass(classID uint64) *hprofClass {
	if class := p.classes[classID]; class != nil {
		return class
	}
	candidates := p.classesByName[p.classNames[classID]]
	if len(candidates) == 1 {
		return candidates[0]
	}
	return nil
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
	if !p.hasGraphDegradation() {
		return
	}
	for i := range evidence.Leaks {
		evidence.Leaks[i].Confidence = lowerHprofConfidence(evidence.Leaks[i].Confidence)
	}
}

func (p *hprofParser) hasGraphDegradation() bool {
	for _, key := range []string{
		"strings",
		"string-record-bytes",
		"string-bytes",
		"roots",
		"class-fields",
		"deferred-instances",
		"unresolved-instance-classes",
		"nodes",
		"classes",
		"edges",
	} {
		if p.hasDegradation(key) {
			return true
		}
	}
	return false
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
