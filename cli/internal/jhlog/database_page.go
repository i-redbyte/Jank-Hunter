package jhlog

import (
	"errors"
	"fmt"
	"math/bits"
)

var errDatabaseColumnInlineStableSymbol = errors.New("inline stable symbol is not columnar")

const databasePageSchema = 1

const (
	databaseMaskDefinition = iota
	databaseMaskFailure
	databaseMaskResult
	databaseMaskTransaction
	databaseMaskStatement
	databaseMaskPhases
	databaseMaskPoolWait
	databaseMaskLockWait
	databaseMaskExecute
	databaseMaskMaterialize
	databaseMaskCount
)

const (
	transactionMaskTerminal = iota
	transactionMaskParent
	transactionMaskMode
	transactionMaskOutcome
	transactionMaskFailure
	transactionMaskDuration
	transactionMaskStatements
	transactionMaskReads
	transactionMaskWrites
	transactionMaskCount
)

const (
	databaseColumnDescriptor = iota
	databaseColumnQuery
	databaseColumnSource
	databaseColumnFingerprint
	databaseColumnFramework
	databaseColumnOperation
	databaseColumnBoundary
	databaseColumnOutcome
	databaseColumnDuration
	databaseColumnFailure
	databaseColumnResultKind
	databaseColumnResultBucket
	databaseColumnTransaction
	databaseColumnStatement
	databaseColumnPhaseMask
	databaseColumnPoolWait
	databaseColumnLockWait
	databaseColumnExecute
	databaseColumnMaterialize
	databaseColumnCount
)

const (
	transactionColumnSource = iota
	transactionColumnID
	transactionColumnParent
	transactionColumnMode
	transactionColumnOutcome
	transactionColumnFailure
	transactionColumnDuration
	transactionColumnStatements
	transactionColumnReads
	transactionColumnWrites
	transactionColumnCount
)

const (
	packedColumnConstant uint64 = iota
	packedColumnFrameOfReference
	packedColumnUvarint
)

type databaseColumnPage struct {
	databaseRows      int
	transactionRows   int
	databaseMasks     [databaseMaskCount][(maxMicroPageRows + 7) / 8]byte
	transactionMasks  [transactionMaskCount][(maxMicroPageRows + 7) / 8]byte
	databaseValues    [databaseColumnCount][maxMicroPageRows]uint64
	databaseCounts    [databaseColumnCount]int
	transactionValues [transactionColumnCount][maxMicroPageRows]uint64
	transactionCounts [transactionColumnCount]int
}

func encodeDatabaseColumnSection(rows []microPageRow) ([]byte, bool, error) {
	page := databaseColumnPage{}
	originalBytes := 0
	for index := range rows {
		switch rows[index].eventType {
		case EventDatabase:
			originalBytes += len(rows[index].payload) + uvarintSize(uint64(len(rows[index].payload)))
			if err := page.appendDatabase(rows[index].payload); err != nil {
				if errors.Is(err, errDatabaseColumnInlineStableSymbol) {
					return nil, false, nil
				}
				return nil, false, fmt.Errorf("database row %d: %w", index, err)
			}
		case EventDatabaseTransaction:
			originalBytes += len(rows[index].payload) + uvarintSize(uint64(len(rows[index].payload)))
			if err := page.appendTransaction(rows[index].payload); err != nil {
				if errors.Is(err, errDatabaseColumnInlineStableSymbol) {
					return nil, false, nil
				}
				return nil, false, fmt.Errorf("database transaction row %d: %w", index, err)
			}
		}
	}
	if page.databaseRows+page.transactionRows == 0 {
		return nil, false, nil
	}
	encoded := page.encode(nil)
	columnBytes := len(encoded) + uvarintSize(uint64(len(encoded))) + page.databaseRows + page.transactionRows
	if columnBytes >= originalBytes+1 {
		return nil, false, nil
	}
	return encoded, true, nil
}

func (page *databaseColumnPage) appendDatabase(payload []byte) error {
	row := page.databaseRows
	if row >= maxMicroPageRows {
		return fmt.Errorf("row capacity exceeded")
	}
	reader := recordReader{data: payload}
	descriptorToken, err := reader.readUvarint()
	if err != nil || descriptorToken == 0 {
		return fmt.Errorf("invalid descriptor token")
	}
	page.addDatabase(databaseColumnDescriptor, descriptorToken>>1)
	if descriptorToken&1 != 0 {
		page.setDatabaseMask(databaseMaskDefinition, row)
		for column := databaseColumnQuery; column <= databaseColumnSource; column++ {
			value, err := readColumnarSymbolToken(&reader)
			if err != nil {
				return err
			}
			page.addDatabase(column, value)
		}
		for column := databaseColumnFingerprint; column <= databaseColumnBoundary; column++ {
			if err := page.readDatabaseValue(&reader, column); err != nil {
				return err
			}
		}
	}
	if err := page.readDatabaseValue(&reader, databaseColumnOutcome); err != nil {
		return err
	}
	if err := page.readDatabaseValue(&reader, databaseColumnDuration); err != nil {
		return err
	}
	presence, err := reader.readUvarint()
	if err != nil {
		return fmt.Errorf("presence: %w", err)
	}
	const knownPresence = databasePresenceFailure | databasePresenceResult | databasePresenceTransaction |
		databasePresenceStatementToken | databasePresencePhases
	if presence&^knownPresence != 0 {
		return fmt.Errorf("unsupported presence 0x%x", presence)
	}
	if presence&databasePresenceFailure != 0 {
		page.setDatabaseMask(databaseMaskFailure, row)
		if err := page.readDatabaseValue(&reader, databaseColumnFailure); err != nil {
			return err
		}
	}
	if presence&databasePresenceResult != 0 {
		page.setDatabaseMask(databaseMaskResult, row)
		if err := page.readDatabaseValue(&reader, databaseColumnResultKind); err != nil {
			return err
		}
		if err := page.readDatabaseValue(&reader, databaseColumnResultBucket); err != nil {
			return err
		}
	}
	if presence&databasePresenceTransaction != 0 {
		page.setDatabaseMask(databaseMaskTransaction, row)
		if err := page.readDatabaseValue(&reader, databaseColumnTransaction); err != nil {
			return err
		}
	}
	if presence&databasePresenceStatementToken != 0 {
		page.setDatabaseMask(databaseMaskStatement, row)
		if err := page.readDatabaseValue(&reader, databaseColumnStatement); err != nil {
			return err
		}
	}
	if presence&databasePresencePhases != 0 {
		page.setDatabaseMask(databaseMaskPhases, row)
		phaseMask, err := reader.readUvarint()
		if err != nil {
			return fmt.Errorf("phase mask: %w", err)
		}
		page.addDatabase(databaseColumnPhaseMask, phaseMask)
		phaseColumns := [...]struct {
			phase  uint64
			mask   int
			column int
		}{
			{uint64(DatabasePhasePoolWait), databaseMaskPoolWait, databaseColumnPoolWait},
			{uint64(DatabasePhaseLockWait), databaseMaskLockWait, databaseColumnLockWait},
			{uint64(DatabasePhaseExecute), databaseMaskExecute, databaseColumnExecute},
			{uint64(DatabasePhaseMaterialize), databaseMaskMaterialize, databaseColumnMaterialize},
		}
		for _, field := range phaseColumns {
			if phaseMask&field.phase == 0 {
				continue
			}
			page.setDatabaseMask(field.mask, row)
			if err := page.readDatabaseValue(&reader, field.column); err != nil {
				return err
			}
		}
	}
	if reader.Len() != 0 {
		return fmt.Errorf("%d trailing bytes", reader.Len())
	}
	page.databaseRows++
	return nil
}

func (page *databaseColumnPage) appendTransaction(payload []byte) error {
	row := page.transactionRows
	if row >= maxMicroPageRows {
		return fmt.Errorf("row capacity exceeded")
	}
	reader := recordReader{data: payload}
	source, err := readColumnarSymbolToken(&reader)
	if err != nil {
		return err
	}
	page.addTransaction(transactionColumnSource, source)
	transactionID, err := reader.readUvarint()
	if err != nil {
		return fmt.Errorf("mandatory column %d: %w", transactionColumnID, err)
	}
	page.addTransaction(transactionColumnID, transactionID)
	stage, err := reader.readUvarint()
	if err != nil || stage < uint64(DatabaseTransactionBegin) || stage > uint64(DatabaseTransactionTerminal) {
		return fmt.Errorf("invalid stage %d", stage)
	}
	if stage == uint64(DatabaseTransactionTerminal) {
		page.setTransactionMask(transactionMaskTerminal, row)
	}
	presence, err := reader.readUvarint()
	if err != nil {
		return fmt.Errorf("presence: %w", err)
	}
	const knownPresence = transactionPresenceParent | transactionPresenceMode | transactionPresenceOutcome |
		transactionPresenceFailure | transactionPresenceDuration | transactionPresenceStatements |
		transactionPresenceReads | transactionPresenceWrites
	if presence&^knownPresence != 0 {
		return fmt.Errorf("unsupported presence 0x%x", presence)
	}
	fields := [...]struct {
		presence uint64
		mask     int
		column   int
	}{
		{transactionPresenceParent, transactionMaskParent, transactionColumnParent},
		{transactionPresenceMode, transactionMaskMode, transactionColumnMode},
		{transactionPresenceOutcome, transactionMaskOutcome, transactionColumnOutcome},
		{transactionPresenceFailure, transactionMaskFailure, transactionColumnFailure},
		{transactionPresenceDuration, transactionMaskDuration, transactionColumnDuration},
		{transactionPresenceStatements, transactionMaskStatements, transactionColumnStatements},
		{transactionPresenceReads, transactionMaskReads, transactionColumnReads},
		{transactionPresenceWrites, transactionMaskWrites, transactionColumnWrites},
	}
	for _, field := range fields {
		if presence&field.presence == 0 {
			continue
		}
		page.setTransactionMask(field.mask, row)
		value, err := reader.readUvarint()
		if err != nil {
			return fmt.Errorf("optional column %d: %w", field.column, err)
		}
		page.addTransaction(field.column, value)
	}
	if reader.Len() != 0 {
		return fmt.Errorf("%d trailing bytes", reader.Len())
	}
	page.transactionRows++
	return nil
}

func readColumnarSymbolToken(reader *recordReader) (uint64, error) {
	token, err := reader.readUvarint()
	if err != nil {
		return 0, err
	}
	if token == 1 {
		return 0, errDatabaseColumnInlineStableSymbol
	}
	return token, nil
}

func (page *databaseColumnPage) readDatabaseValue(reader *recordReader, column int) error {
	value, err := reader.readUvarint()
	if err != nil {
		return fmt.Errorf("column %d: %w", column, err)
	}
	page.addDatabase(column, value)
	return nil
}

func (page *databaseColumnPage) addDatabase(column int, value uint64) {
	index := page.databaseCounts[column]
	page.databaseValues[column][index] = value
	page.databaseCounts[column] = index + 1
}

func (page *databaseColumnPage) addTransaction(column int, value uint64) {
	index := page.transactionCounts[column]
	page.transactionValues[column][index] = value
	page.transactionCounts[column] = index + 1
}

func (page *databaseColumnPage) setDatabaseMask(mask, row int) {
	page.databaseMasks[mask][row/8] |= 1 << (row % 8)
}

func (page *databaseColumnPage) setTransactionMask(mask, row int) {
	page.transactionMasks[mask][row/8] |= 1 << (row % 8)
}

func (page *databaseColumnPage) encode(destination []byte) []byte {
	destination = appendUvarint(destination, databasePageSchema)
	destination = appendUvarint(destination, uint64(page.databaseRows))
	destination = appendUvarint(destination, uint64(page.transactionRows))
	databaseMaskBytes := (page.databaseRows + 7) / 8
	for mask := range page.databaseMasks {
		destination = append(destination, page.databaseMasks[mask][:databaseMaskBytes]...)
	}
	transactionMaskBytes := (page.transactionRows + 7) / 8
	for mask := range page.transactionMasks {
		destination = append(destination, page.transactionMasks[mask][:transactionMaskBytes]...)
	}
	for column := range page.databaseValues {
		destination = appendPackedColumn(destination, page.databaseValues[column][:page.databaseCounts[column]])
	}
	for column := range page.transactionValues {
		destination = appendPackedColumn(destination, page.transactionValues[column][:page.transactionCounts[column]])
	}
	return destination
}

type databaseColumnDecoder struct {
	page               databaseColumnPage
	databaseRow        int
	transactionRow     int
	databaseIndexes    [databaseColumnCount]int
	transactionIndexes [transactionColumnCount]int
}

func decodeDatabaseColumnSection(raw []byte, databaseRows, transactionRows int) (*databaseColumnDecoder, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if databaseRows+transactionRows == 0 {
		return nil, fmt.Errorf("database columns exist without database rows")
	}
	decoder := &databaseColumnDecoder{}
	reader := recordReader{data: raw}
	schema, err := reader.readUvarint()
	if err != nil {
		return nil, fmt.Errorf("database page schema: %w", err)
	}
	if schema != databasePageSchema {
		return nil, fmt.Errorf("database page schema %d is unsupported", schema)
	}
	declaredDatabaseRows, err := reader.readUvarint()
	if err != nil {
		return nil, fmt.Errorf("database row count: %w", err)
	}
	declaredTransactionRows, err := reader.readUvarint()
	if err != nil {
		return nil, fmt.Errorf("database transaction row count: %w", err)
	}
	if declaredDatabaseRows != uint64(databaseRows) || declaredTransactionRows != uint64(transactionRows) {
		return nil, fmt.Errorf(
			"database page rows %d/%d differ from type rows %d/%d",
			declaredDatabaseRows, declaredTransactionRows, databaseRows, transactionRows,
		)
	}
	decoder.page.databaseRows = databaseRows
	decoder.page.transactionRows = transactionRows
	if err := readDatabaseMasks(&reader, decoder); err != nil {
		return nil, err
	}
	databaseCounts := decoder.databaseColumnCounts()
	for column, count := range databaseCounts {
		decoder.page.databaseCounts[column] = count
		if err := decodePackedColumn(&reader, count, decoder.page.databaseValues[column][:]); err != nil {
			return nil, fmt.Errorf("database column %d: %w", column, err)
		}
	}
	transactionCounts := decoder.transactionColumnCounts()
	for column, count := range transactionCounts {
		decoder.page.transactionCounts[column] = count
		if err := decodePackedColumn(&reader, count, decoder.page.transactionValues[column][:]); err != nil {
			return nil, fmt.Errorf("database transaction column %d: %w", column, err)
		}
	}
	if reader.Len() != 0 {
		return nil, fmt.Errorf("database page leaves %d trailing bytes", reader.Len())
	}
	return decoder, nil
}

func readDatabaseMasks(reader *recordReader, decoder *databaseColumnDecoder) error {
	databaseBytes := (decoder.page.databaseRows + 7) / 8
	for mask := range decoder.page.databaseMasks {
		if reader.Len() < databaseBytes {
			return fmt.Errorf("database mask %d is truncated", mask)
		}
		copy(decoder.page.databaseMasks[mask][:], reader.data[reader.offset:reader.offset+databaseBytes])
		reader.offset += databaseBytes
		if err := validateMicroPagePadding(decoder.page.databaseMasks[mask][:databaseBytes], decoder.page.databaseRows); err != nil {
			return fmt.Errorf("database mask %d padding: %w", mask, err)
		}
	}
	transactionBytes := (decoder.page.transactionRows + 7) / 8
	for mask := range decoder.page.transactionMasks {
		if reader.Len() < transactionBytes {
			return fmt.Errorf("database transaction mask %d is truncated", mask)
		}
		copy(decoder.page.transactionMasks[mask][:], reader.data[reader.offset:reader.offset+transactionBytes])
		reader.offset += transactionBytes
		if err := validateMicroPagePadding(
			decoder.page.transactionMasks[mask][:transactionBytes],
			decoder.page.transactionRows,
		); err != nil {
			return fmt.Errorf("database transaction mask %d padding: %w", mask, err)
		}
	}
	return nil
}

func (decoder *databaseColumnDecoder) databaseColumnCounts() [databaseColumnCount]int {
	rows := decoder.page.databaseRows
	definitions := decoder.databaseMaskPopulation(databaseMaskDefinition)
	result := [databaseColumnCount]int{
		rows, definitions, definitions, definitions, definitions, definitions, definitions,
		rows, rows,
		decoder.databaseMaskPopulation(databaseMaskFailure),
		decoder.databaseMaskPopulation(databaseMaskResult),
		decoder.databaseMaskPopulation(databaseMaskResult),
		decoder.databaseMaskPopulation(databaseMaskTransaction),
		decoder.databaseMaskPopulation(databaseMaskStatement),
		decoder.databaseMaskPopulation(databaseMaskPhases),
		decoder.databaseMaskPopulation(databaseMaskPoolWait),
		decoder.databaseMaskPopulation(databaseMaskLockWait),
		decoder.databaseMaskPopulation(databaseMaskExecute),
		decoder.databaseMaskPopulation(databaseMaskMaterialize),
	}
	return result
}

func (decoder *databaseColumnDecoder) transactionColumnCounts() [transactionColumnCount]int {
	return [transactionColumnCount]int{
		decoder.page.transactionRows,
		decoder.page.transactionRows,
		decoder.transactionMaskPopulation(transactionMaskParent),
		decoder.transactionMaskPopulation(transactionMaskMode),
		decoder.transactionMaskPopulation(transactionMaskOutcome),
		decoder.transactionMaskPopulation(transactionMaskFailure),
		decoder.transactionMaskPopulation(transactionMaskDuration),
		decoder.transactionMaskPopulation(transactionMaskStatements),
		decoder.transactionMaskPopulation(transactionMaskReads),
		decoder.transactionMaskPopulation(transactionMaskWrites),
	}
}

func (decoder *databaseColumnDecoder) databaseMaskPopulation(mask int) int {
	return maskPopulation(decoder.page.databaseMasks[mask][:], decoder.page.databaseRows)
}

func (decoder *databaseColumnDecoder) transactionMaskPopulation(mask int) int {
	return maskPopulation(decoder.page.transactionMasks[mask][:], decoder.page.transactionRows)
}

func maskPopulation(mask []byte, rows int) int {
	total := 0
	for _, value := range mask[:(rows+7)/8] {
		total += bits.OnesCount8(value)
	}
	return total
}

func (decoder *databaseColumnDecoder) nextPayload(eventType EventType, destination []byte) ([]byte, error) {
	switch eventType {
	case EventDatabase:
		return decoder.nextDatabasePayload(destination)
	case EventDatabaseTransaction:
		return decoder.nextTransactionPayload(destination)
	default:
		return nil, fmt.Errorf("event type %d is not a database row", eventType)
	}
}

func (decoder *databaseColumnDecoder) validateConsumed() error {
	if decoder.databaseRow != decoder.page.databaseRows || decoder.transactionRow != decoder.page.transactionRows {
		return fmt.Errorf(
			"database page consumed rows %d/%d of %d/%d",
			decoder.databaseRow,
			decoder.transactionRow,
			decoder.page.databaseRows,
			decoder.page.transactionRows,
		)
	}
	for column, count := range decoder.page.databaseCounts {
		if decoder.databaseIndexes[column] != count {
			return fmt.Errorf(
				"database column %d consumed %d of %d values",
				column,
				decoder.databaseIndexes[column],
				count,
			)
		}
	}
	for column, count := range decoder.page.transactionCounts {
		if decoder.transactionIndexes[column] != count {
			return fmt.Errorf(
				"database transaction column %d consumed %d of %d values",
				column,
				decoder.transactionIndexes[column],
				count,
			)
		}
	}
	return nil
}

func (decoder *databaseColumnDecoder) nextDatabasePayload(destination []byte) ([]byte, error) {
	row := decoder.databaseRow
	if row >= decoder.page.databaseRows {
		return nil, fmt.Errorf("database row overflow")
	}
	descriptorID := decoder.takeDatabase(databaseColumnDescriptor)
	definition := decoder.databaseMaskSet(databaseMaskDefinition, row)
	destination = appendUvarint(destination, descriptorID<<1|boolBit(definition))
	if definition {
		for column := databaseColumnQuery; column <= databaseColumnBoundary; column++ {
			destination = appendUvarint(destination, decoder.takeDatabase(column))
		}
	}
	destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnOutcome))
	destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnDuration))
	presence := uint64(0)
	if decoder.databaseMaskSet(databaseMaskFailure, row) {
		presence |= databasePresenceFailure
	}
	if decoder.databaseMaskSet(databaseMaskResult, row) {
		presence |= databasePresenceResult
	}
	if decoder.databaseMaskSet(databaseMaskTransaction, row) {
		presence |= databasePresenceTransaction
	}
	if decoder.databaseMaskSet(databaseMaskStatement, row) {
		presence |= databasePresenceStatementToken
	}
	if decoder.databaseMaskSet(databaseMaskPhases, row) {
		presence |= databasePresencePhases
	}
	destination = appendUvarint(destination, presence)
	if presence&databasePresenceFailure != 0 {
		destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnFailure))
	}
	if presence&databasePresenceResult != 0 {
		destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnResultKind))
		destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnResultBucket))
	}
	if presence&databasePresenceTransaction != 0 {
		destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnTransaction))
	}
	if presence&databasePresenceStatementToken != 0 {
		destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnStatement))
	}
	if presence&databasePresencePhases != 0 {
		phaseMask := decoder.takeDatabase(databaseColumnPhaseMask)
		destination = appendUvarint(destination, phaseMask)
		if decoder.databaseMaskSet(databaseMaskPoolWait, row) {
			destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnPoolWait))
		}
		if decoder.databaseMaskSet(databaseMaskLockWait, row) {
			destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnLockWait))
		}
		if decoder.databaseMaskSet(databaseMaskExecute, row) {
			destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnExecute))
		}
		if decoder.databaseMaskSet(databaseMaskMaterialize, row) {
			destination = appendUvarint(destination, decoder.takeDatabase(databaseColumnMaterialize))
		}
	}
	decoder.databaseRow++
	return destination, nil
}

func (decoder *databaseColumnDecoder) nextTransactionPayload(destination []byte) ([]byte, error) {
	row := decoder.transactionRow
	if row >= decoder.page.transactionRows {
		return nil, fmt.Errorf("database transaction row overflow")
	}
	destination = appendUvarint(destination, decoder.takeTransaction(transactionColumnSource))
	destination = appendUvarint(destination, decoder.takeTransaction(transactionColumnID))
	stage := uint64(DatabaseTransactionBegin)
	if decoder.transactionMaskSet(transactionMaskTerminal, row) {
		stage = uint64(DatabaseTransactionTerminal)
	}
	destination = appendUvarint(destination, stage)
	presence := uint64(0)
	for mask := transactionMaskParent; mask < transactionMaskCount; mask++ {
		if decoder.transactionMaskSet(mask, row) {
			presence |= 1 << (mask - 1)
		}
	}
	destination = appendUvarint(destination, presence)
	for column := transactionColumnParent; column < transactionColumnCount; column++ {
		mask := transactionMaskParent + column - transactionColumnParent
		if decoder.transactionMaskSet(mask, row) {
			destination = appendUvarint(destination, decoder.takeTransaction(column))
		}
	}
	decoder.transactionRow++
	return destination, nil
}

func (decoder *databaseColumnDecoder) takeDatabase(column int) uint64 {
	index := decoder.databaseIndexes[column]
	decoder.databaseIndexes[column] = index + 1
	return decoder.page.databaseValues[column][index]
}

func (decoder *databaseColumnDecoder) takeTransaction(column int) uint64 {
	index := decoder.transactionIndexes[column]
	decoder.transactionIndexes[column] = index + 1
	return decoder.page.transactionValues[column][index]
}

func (decoder *databaseColumnDecoder) databaseMaskSet(mask, row int) bool {
	return decoder.page.databaseMasks[mask][row/8]&(1<<(row%8)) != 0
}

func (decoder *databaseColumnDecoder) transactionMaskSet(mask, row int) bool {
	return decoder.page.transactionMasks[mask][row/8]&(1<<(row%8)) != 0
}

func boolBit(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func appendPackedColumn(destination []byte, values []uint64) []byte {
	if len(values) == 0 {
		return destination
	}
	minimum := values[0]
	maximum := values[0]
	uvarintBytes := 0
	for _, value := range values {
		minimum = min(minimum, value)
		maximum = max(maximum, value)
		uvarintBytes += uvarintSize(value)
	}
	if minimum == maximum {
		destination = appendUvarint(destination, packedColumnConstant)
		return appendUvarint(destination, minimum)
	}
	width := bits.Len64(maximum - minimum)
	packedBytes := (len(values)*width + 7) / 8
	frameBytes := 1 + uvarintSize(minimum) + 1 + packedBytes
	if frameBytes < 1+uvarintBytes {
		destination = appendUvarint(destination, packedColumnFrameOfReference)
		destination = appendUvarint(destination, minimum)
		destination = appendUvarint(destination, uint64(width))
		start := len(destination)
		destination = append(destination, make([]byte, packedBytes)...)
		for index, value := range values {
			packColumnValue(destination[start:], index*width, width, value-minimum)
		}
		return destination
	}
	destination = appendUvarint(destination, packedColumnUvarint)
	for _, value := range values {
		destination = appendUvarint(destination, value)
	}
	return destination
}

func decodePackedColumn(reader *recordReader, count int, destination []uint64) error {
	if count == 0 {
		return nil
	}
	mode, err := reader.readUvarint()
	if err != nil {
		return fmt.Errorf("mode: %w", err)
	}
	switch mode {
	case packedColumnConstant:
		value, err := reader.readUvarint()
		if err != nil {
			return fmt.Errorf("constant: %w", err)
		}
		for index := 0; index < count; index++ {
			destination[index] = value
		}
	case packedColumnFrameOfReference:
		minimum, err := reader.readUvarint()
		if err != nil {
			return fmt.Errorf("minimum: %w", err)
		}
		width, err := reader.readUvarint()
		if err != nil || width == 0 || width > 64 {
			return fmt.Errorf("invalid bit width %d", width)
		}
		byteCount := (count*int(width) + 7) / 8
		if byteCount > reader.Len() {
			return fmt.Errorf("packed data needs %d bytes, has %d", byteCount, reader.Len())
		}
		packed := reader.data[reader.offset : reader.offset+byteCount]
		reader.offset += byteCount
		for index := 0; index < count; index++ {
			delta := unpackColumnValue(packed, index*int(width), int(width))
			if delta > ^uint64(0)-minimum {
				return fmt.Errorf("frame-of-reference value overflows uint64")
			}
			destination[index] = minimum + delta
		}
		if err := validateMicroPagePadding(packed, count*int(width)); err != nil {
			return fmt.Errorf("packed padding: %w", err)
		}
	case packedColumnUvarint:
		for index := 0; index < count; index++ {
			value, err := reader.readUvarint()
			if err != nil {
				return fmt.Errorf("value %d: %w", index, err)
			}
			destination[index] = value
		}
	default:
		return fmt.Errorf("unsupported mode %d", mode)
	}
	return nil
}

func packColumnValue(destination []byte, bitOffset, width int, value uint64) {
	remaining := width
	for remaining > 0 {
		byteOffset := bitOffset / 8
		shift := bitOffset % 8
		take := min(remaining, 8-shift)
		mask := uint64(1<<take) - 1
		destination[byteOffset] |= byte(value&mask) << shift
		value >>= take
		bitOffset += take
		remaining -= take
	}
}

func unpackColumnValue(source []byte, bitOffset, width int) uint64 {
	var value uint64
	written := 0
	remaining := width
	for remaining > 0 {
		byteOffset := bitOffset / 8
		shift := bitOffset % 8
		take := min(remaining, 8-shift)
		mask := byte((1 << take) - 1)
		value |= uint64((source[byteOffset]>>shift)&mask) << written
		bitOffset += take
		written += take
		remaining -= take
	}
	return value
}
