package jhlog

import (
	"bytes"
	"fmt"
	"math"
)

const (
	microPageTypeBits  = 5
	microPageMaskCount = 6
)

const (
	microPageMaskTimePresence = iota
	microPageMaskThreadPresence
	microPageMaskThreadChange
	microPageMaskContextPresence
	microPageMaskContextChange
	microPageMaskAttributesPresence
)

type microPageRow struct {
	eventType  EventType
	producer   ProducerMetadata
	context    AttributionContext
	attributes uint64
	payload    []byte
	elapsedUS  uint64
	hasTime    bool
}

type microPageBuilder struct {
	rows         []microPageRow
	payloadArena []byte
	payloadBytes int
}

type decodedMicroPage struct {
	events  []Event
	weights []uint64
}

func newMicroPageBuilder() microPageBuilder {
	return microPageBuilder{
		rows:         make([]microPageRow, 0, maxMicroPageRows),
		payloadArena: make([]byte, 0, maxMicroPagePayloadBytes),
	}
}

func (page *microPageBuilder) canAppend(payloadBytes int) bool {
	return len(page.rows) < maxMicroPageRows && payloadBytes >= 0 &&
		page.payloadBytes+payloadBytes <= maxMicroPagePayloadBytes
}

func (page *microPageBuilder) append(row microPageRow) {
	start := len(page.payloadArena)
	page.payloadArena = append(page.payloadArena, row.payload...)
	row.payload = page.payloadArena[start:]
	page.rows = append(page.rows, row)
	page.payloadBytes = len(page.payloadArena)
}

func (page *microPageBuilder) reset() {
	clear(page.rows)
	page.rows = page.rows[:0]
	page.payloadArena = page.payloadArena[:0]
	page.payloadBytes = 0
}

func encodeMicroPageRecord(
	rows []microPageRow,
	stableAliases map[uint64]uint64,
	entropyEnabled bool,
) ([]byte, error) {
	if len(rows) == 0 || len(rows) > maxMicroPageRows {
		return nil, fmt.Errorf("micro-page row count %d is outside 1..%d", len(rows), maxMicroPageRows)
	}
	maskBytes := (len(rows) + 7) / 8
	sections := [9]bytes.Buffer{}
	const sectionCount = 9
	masks := make([]byte, maskBytes*microPageMaskCount)
	var databaseSection []byte
	databaseColumns := false
	var err error
	databaseSection, databaseColumns, err = encodeDatabaseColumnSection(rows)
	if err != nil {
		return nil, err
	}

	var typeBuffer uint64
	typeBits := 0
	var lastElapsedUS uint64
	hasElapsed := false
	var lastThreadID uint64
	hasThread := false
	var lastContext AttributionContext
	hasContext := false
	for index := range rows {
		row := rows[index]
		if !row.eventType.IsSemanticData() {
			return nil, fmt.Errorf("micro-page row %d has non-semantic type %d", index, row.eventType)
		}
		typeBuffer |= (uint64(row.eventType) - 1) << typeBits
		typeBits += microPageTypeBits
		for typeBits >= 8 {
			_ = sections[0].WriteByte(byte(typeBuffer))
			typeBuffer >>= 8
			typeBits -= 8
		}

		if rows[index].hasTime {
			setMicroPageMask(masks, maskBytes, microPageMaskTimePresence, index)
			if !hasElapsed {
				if err := writeUvarint(&sections[2], rows[index].elapsedUS); err != nil {
					return nil, err
				}
			} else {
				delta := int64(rows[index].elapsedUS) - int64(lastElapsedUS)
				if err := writeUvarint(&sections[2], encodeSVarint(delta)); err != nil {
					return nil, err
				}
			}
			lastElapsedUS = rows[index].elapsedUS
			hasElapsed = true
		}

		hasEventThread := row.producer.HasThread || row.producer.ThreadID != 0
		if hasEventThread {
			setMicroPageMask(masks, maskBytes, microPageMaskThreadPresence, index)
			if !hasThread || row.producer.ThreadID != lastThreadID {
				setMicroPageMask(masks, maskBytes, microPageMaskThreadChange, index)
				if err := writeUvarint(&sections[3], row.producer.ThreadID); err != nil {
					return nil, err
				}
				lastThreadID = row.producer.ThreadID
				hasThread = true
			}
		}

		context := row.context
		if context.Present {
			setMicroPageMask(masks, maskBytes, microPageMaskContextPresence, index)
			if !hasContext || !equalAttribution(lastContext, context) {
				setMicroPageMask(masks, maskBytes, microPageMaskContextChange, index)
				if err := writeAttribution(&sections[4], context, stableAliases); err != nil {
					return nil, err
				}
				lastContext = context
				hasContext = true
			}
		}

		attributes := row.attributes
		if attributes != 0 {
			setMicroPageMask(masks, maskBytes, microPageMaskAttributesPresence, index)
			if err := writeUvarint(&sections[5], attributes); err != nil {
				return nil, err
			}
		}
		payloadLength := len(rows[index].payload)
		if databaseColumns && (row.eventType == EventDatabase || row.eventType == EventDatabaseTransaction) {
			payloadLength = 0
		}
		if err := writeUvarint(&sections[6], uint64(payloadLength)); err != nil {
			return nil, err
		}
		if payloadLength != 0 {
			if _, err := sections[7].Write(rows[index].payload); err != nil {
				return nil, err
			}
		}
	}
	if databaseColumns {
		if _, err := sections[8].Write(databaseSection); err != nil {
			return nil, err
		}
	}
	if typeBits != 0 {
		_ = sections[0].WriteByte(byte(typeBuffer))
	}
	if _, err := sections[1].Write(masks); err != nil {
		return nil, err
	}

	var page bytes.Buffer
	var entropy ransEncoder
	var encodedSections [9][]byte
	codecMask := uint64(0)
	if entropyEnabled {
		for index := 0; index < sectionCount; index++ {
			if encoded, compressed := entropy.encode(sections[index].Bytes()); compressed {
				encodedSections[index] = append(encodedSections[index], encoded...)
				codecMask |= 1 << index
			}
		}
	}
	if codecMask == 0 {
		if err := writeUvarint(&page, uint64(len(rows))); err != nil {
			return nil, err
		}
	} else {
		if err := writeUvarint(&page, 0); err != nil {
			return nil, err
		}
		if err := writeUvarint(&page, uint64(len(rows))); err != nil {
			return nil, err
		}
		if err := writeUvarint(&page, codecMask); err != nil {
			return nil, err
		}
	}
	for index := 0; index < sectionCount; index++ {
		raw := sections[index].Bytes()
		if err := writeUvarint(&page, uint64(len(raw))); err != nil {
			return nil, err
		}
		if codecMask&(1<<index) != 0 {
			encoded := encodedSections[index]
			if err := writeUvarint(&page, uint64(len(encoded))); err != nil {
				return nil, err
			}
			raw = encoded
		}
		if _, err := page.Write(raw); err != nil {
			return nil, err
		}
	}
	var body bytes.Buffer
	if err := writeUvarint(&body, uint64(eventMicroPage)); err != nil {
		return nil, err
	}
	if err := writeUvarint(&body, 0); err != nil {
		return nil, err
	}
	if _, err := body.Write(page.Bytes()); err != nil {
		return nil, err
	}
	var record bytes.Buffer
	if err := writeUvarint(&record, uint64(body.Len())); err != nil {
		return nil, err
	}
	if _, err := record.Write(body.Bytes()); err != nil {
		return nil, err
	}
	return record.Bytes(), nil
}

func decodeMicroPage(
	reader *recordReader,
	outer Event,
	state recordDecodeState,
	processName string,
	symbolNamespace string,
	segmentState *segmentDecodeState,
) (*decodedMicroPage, recordDecodeState, error) {
	pageHeader, err := reader.readUvarint()
	if err != nil {
		return nil, state, fmt.Errorf("micro-page header: %w", err)
	}
	rowCount := pageHeader
	codecMask := uint64(0)
	const sectionCount = 9
	if pageHeader == 0 {
		rowCount, err = reader.readUvarint()
		if err != nil {
			return nil, state, fmt.Errorf("micro-page entropy row count: %w", err)
		}
		codecMask, err = reader.readUvarint()
		if err != nil {
			return nil, state, fmt.Errorf("micro-page codec mask: %w", err)
		}
		if codecMask == 0 || codecMask >= 1<<sectionCount {
			return nil, state, fmt.Errorf("micro-page codec mask 0x%x is invalid", codecMask)
		}
	}
	if rowCount == 0 || rowCount > maxMicroPageRows {
		return nil, state, fmt.Errorf("micro-page row count %d is outside 1..%d", rowCount, maxMicroPageRows)
	}
	var sections [9][]byte
	for index := 0; index < sectionCount; index++ {
		decodedLength, err := reader.readUvarint()
		if err != nil {
			return nil, state, fmt.Errorf("micro-page section %d decoded length: %w", index, err)
		}
		limit := microPageSectionLimit(index, int(rowCount))
		if decodedLength > uint64(limit) {
			return nil, state, fmt.Errorf("micro-page section %d decoded length %d exceeds %d", index, decodedLength, limit)
		}
		if codecMask&(1<<index) == 0 {
			if decodedLength > uint64(reader.Len()) {
				return nil, state, fmt.Errorf("micro-page section %d length %d exceeds remaining %d", index, decodedLength, reader.Len())
			}
			start := reader.offset
			reader.offset += int(decodedLength)
			sections[index] = reader.data[start:reader.offset]
		} else {
			encodedLength, err := reader.readUvarint()
			if err != nil {
				return nil, state, fmt.Errorf("micro-page section %d rANS length: %w", index, err)
			}
			if encodedLength > uint64(reader.Len()) ||
				uvarintSize(encodedLength)+int(encodedLength) >= int(decodedLength) {
				return nil, state, fmt.Errorf("micro-page section %d has invalid rANS length %d", index, encodedLength)
			}
			start := reader.offset
			reader.offset += int(encodedLength)
			decoded, err := decodeRANS(reader.data[start:reader.offset], int(decodedLength))
			if err != nil {
				return nil, state, fmt.Errorf("micro-page section %d: %w", index, err)
			}
			sections[index] = decoded
		}
	}
	if reader.Len() != 0 {
		return nil, state, fmt.Errorf("micro-page leaves %d trailing bytes", reader.Len())
	}
	rows := int(rowCount)
	typeBytes := (rows*microPageTypeBits + 7) / 8
	maskBytes := (rows + 7) / 8
	if len(sections[0]) != typeBytes {
		return nil, state, fmt.Errorf("micro-page type section has %d bytes, expected %d", len(sections[0]), typeBytes)
	}
	if len(sections[1]) != maskBytes*microPageMaskCount {
		return nil, state, fmt.Errorf("micro-page mask section has %d bytes, expected %d", len(sections[1]), maskBytes*microPageMaskCount)
	}
	if err := validateMicroPagePadding(sections[0], rows*microPageTypeBits); err != nil {
		return nil, state, fmt.Errorf("micro-page type padding: %w", err)
	}
	for mask := 0; mask < microPageMaskCount; mask++ {
		if err := validateMicroPagePadding(sections[1][mask*maskBytes:(mask+1)*maskBytes], rows); err != nil {
			return nil, state, fmt.Errorf("micro-page mask %d padding: %w", mask, err)
		}
	}
	databaseRows := 0
	transactionRows := 0
	for index := 0; index < rows; index++ {
		switch EventType(unpackMicroPageType(sections[0], index) + 1) {
		case EventDatabase:
			databaseRows++
		case EventDatabaseTransaction:
			transactionRows++
		}
	}
	databaseColumns, err := decodeDatabaseColumnSection(sections[8], databaseRows, transactionRows)
	if err != nil {
		return nil, state, err
	}

	timeReader := recordReader{data: sections[2]}
	threadReader := recordReader{data: sections[3]}
	contextReader := recordReader{data: sections[4]}
	attributesReader := recordReader{data: sections[5]}
	lengthsReader := recordReader{data: sections[6]}
	pageEvents := make([]Event, rows)
	pageWeights := make([]uint64, rows)
	var payloadLengths [maxMicroPageRows]int
	payloadTotal := 0
	for index := 0; index < rows; index++ {
		length, err := lengthsReader.readUvarint()
		if err != nil {
			return nil, state, fmt.Errorf("micro-page row %d payload length: %w", index, err)
		}
		if length > maxRawChunkSize || payloadTotal > len(sections[7])-int(length) {
			return nil, state, fmt.Errorf("micro-page row %d payload length %d exceeds payload section", index, length)
		}
		payloadLengths[index] = int(length)
		eventType := EventType(unpackMicroPageType(sections[0], index) + 1)
		if databaseColumns != nil && (eventType == EventDatabase || eventType == EventDatabaseTransaction) && length != 0 {
			return nil, state, fmt.Errorf("micro-page row %d has row and column database payloads", index)
		}
		payloadTotal += int(length)
	}
	if lengthsReader.Len() != 0 || payloadTotal != len(sections[7]) {
		return nil, state, fmt.Errorf("micro-page payload lengths consume %d of %d bytes", payloadTotal, len(sections[7]))
	}

	nextState := state
	var pageElapsedUS uint64
	hasPageElapsed := false
	var pageThreadID uint64
	hasPageThread := false
	var pageContext AttributionContext
	hasPageContext := false
	payloadOffset := 0
	for index := 0; index < rows; index++ {
		typeValue := unpackMicroPageType(sections[0], index)
		eventType := EventType(typeValue + 1)
		if !eventType.IsSemanticData() {
			return nil, state, fmt.Errorf("micro-page row %d has non-semantic type %d", index, eventType)
		}
		event := Event{Type: eventType, Source: outer.Source, Position: outer.Position}
		if microPageMaskSet(sections[1], maskBytes, microPageMaskTimePresence, index) {
			var elapsedUS uint64
			if !hasPageElapsed {
				elapsedUS, err = timeReader.readUvarint()
			} else {
				var encoded uint64
				encoded, err = timeReader.readUvarint()
				if err == nil {
					delta := decodeSVarint(encoded)
					if pageElapsedUS > math.MaxInt64 {
						err = fmt.Errorf("prior page timestamp exceeds signed range")
					} else {
						var decoded int64
						decoded, err = addSignedTimestamp(int64(pageElapsedUS), delta)
						elapsedUS = uint64(decoded)
					}
				}
			}
			if err != nil {
				return nil, state, fmt.Errorf("micro-page row %d timestamp: %w", index, err)
			}
			if elapsedUS > math.MaxInt64 {
				return nil, state, fmt.Errorf("micro-page row %d timestamp %d exceeds signed range", index, elapsedUS)
			}
			delta := int64(elapsedUS) - nextState.lastElapsedUS
			event.DeltaUS = delta
			if delta >= 0 {
				event.DeltaMS = uint64(delta) / 1000
			}
			event.Producer.HasTime = true
			event.Producer.ElapsedUS = elapsedUS
			nextState.lastElapsedUS = int64(elapsedUS)
			pageElapsedUS = elapsedUS
			hasPageElapsed = true
		}
		event.TimeUS = uint64(nextState.lastElapsedUS)
		event.TimeMS = event.TimeUS / 1000

		hasThread := microPageMaskSet(sections[1], maskBytes, microPageMaskThreadPresence, index)
		threadChanged := microPageMaskSet(sections[1], maskBytes, microPageMaskThreadChange, index)
		if threadChanged && !hasThread {
			return nil, state, fmt.Errorf("micro-page row %d changes an absent thread", index)
		}
		if hasThread {
			if threadChanged {
				pageThreadID, err = threadReader.readUvarint()
				if err != nil {
					return nil, state, fmt.Errorf("micro-page row %d thread: %w", index, err)
				}
				hasPageThread = true
			} else if !hasPageThread {
				return nil, state, fmt.Errorf("micro-page row %d reuses a missing thread", index)
			}
			event.Producer.HasThread = true
			event.Producer.ThreadID = pageThreadID
		}

		hasContext := microPageMaskSet(sections[1], maskBytes, microPageMaskContextPresence, index)
		contextChanged := microPageMaskSet(sections[1], maskBytes, microPageMaskContextChange, index)
		if contextChanged && !hasContext {
			return nil, state, fmt.Errorf("micro-page row %d changes an absent context", index)
		}
		if hasContext {
			if contextChanged {
				pageContext, err = readAttribution(&contextReader, symbolNamespace, segmentState.stableAliases)
				if err != nil {
					return nil, state, fmt.Errorf("micro-page row %d context: %w", index, err)
				}
				hasPageContext = true
			} else if !hasPageContext {
				return nil, state, fmt.Errorf("micro-page row %d reuses a missing context", index)
			}
			event.Attribution = pageContext
			event.Attribution.Present = true
			nextState.lastContext = pageContext
			nextState.hasContext = true
		}

		if microPageMaskSet(sections[1], maskBytes, microPageMaskAttributesPresence, index) {
			attributes, err := attributesReader.readUvarint()
			if err != nil {
				return nil, state, fmt.Errorf("micro-page row %d attributes: %w", index, err)
			}
			if attributes == 0 || attributes&^semanticAttributeMask != 0 {
				return nil, state, fmt.Errorf("micro-page row %d has invalid attributes 0x%x", index, attributes)
			}
			event.Flags = attributes
		}

		payloadEnd := payloadOffset + payloadLengths[index]
		payload := sections[7][payloadOffset:payloadEnd]
		var databasePayload [256]byte
		if databaseColumns != nil && (eventType == EventDatabase || eventType == EventDatabaseTransaction) {
			payload, err = databaseColumns.nextPayload(eventType, databasePayload[:0])
			if err != nil {
				return nil, state, fmt.Errorf("micro-page row %d database columns: %w", index, err)
			}
		}
		payloadReader := recordReader{data: payload}
		if err := decodeEventPayload(&payloadReader, &event, processName, symbolNamespace, nil, segmentState); err != nil {
			return nil, state, fmt.Errorf("micro-page row %d payload: %w", index, err)
		}
		if payloadReader.Len() != 0 {
			return nil, state, fmt.Errorf("micro-page row %d leaves %d payload bytes", index, payloadReader.Len())
		}
		pageEvents[index] = event
		pageWeights[index] = uint64(len(payload) + 1)
		payloadOffset = payloadEnd
	}
	if databaseColumns != nil {
		if err := databaseColumns.validateConsumed(); err != nil {
			return nil, state, err
		}
	}
	for index, sectionReader := range []*recordReader{&timeReader, &threadReader, &contextReader, &attributesReader} {
		if sectionReader.Len() != 0 {
			return nil, state, fmt.Errorf("micro-page metadata section %d leaves %d bytes", index+2, sectionReader.Len())
		}
	}
	return &decodedMicroPage{events: pageEvents, weights: pageWeights}, nextState, nil
}

func microPageSectionLimit(section, rows int) int {
	switch section {
	case 0:
		return (rows*microPageTypeBits + 7) / 8
	case 1:
		return ((rows + 7) / 8) * microPageMaskCount
	case 2, 3, 5:
		return rows * 10
	case 4:
		return rows * 29
	case 6:
		return rows * 3
	case 7:
		return maxMicroPagePayloadBytes
	case 8:
		return maxMicroPagePayloadBytes + rows*64
	default:
		panic("invalid micro-page section")
	}
}

func setMicroPageMask(masks []byte, maskBytes, mask, row int) {
	masks[mask*maskBytes+row/8] |= 1 << (row % 8)
}

func microPageMaskSet(masks []byte, maskBytes, mask, row int) bool {
	return masks[mask*maskBytes+row/8]&(1<<(row%8)) != 0
}

func unpackMicroPageType(types []byte, row int) uint64 {
	bitOffset := row * microPageTypeBits
	byteOffset := bitOffset / 8
	shift := bitOffset % 8
	value := uint64(types[byteOffset]) >> shift
	if shift > 3 && byteOffset+1 < len(types) {
		value |= uint64(types[byteOffset+1]) << (8 - shift)
	}
	return value & 0x1f
}

func validateMicroPagePadding(data []byte, usedBits int) error {
	if len(data) == 0 || usedBits%8 == 0 {
		return nil
	}
	usedInLast := usedBits % 8
	if data[len(data)-1]&byte(0xff<<usedInLast) != 0 {
		return fmt.Errorf("non-zero unused bits")
	}
	return nil
}
