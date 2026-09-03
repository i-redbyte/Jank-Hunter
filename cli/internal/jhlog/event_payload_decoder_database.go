package jhlog

import "fmt"

func decodeDatabasePayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	descriptorToken, err := readPayloadValue(reader, "database descriptor token")
	if err != nil {
		return err
	}
	if descriptorToken == 0 {
		return fmt.Errorf("database descriptor token must be non-zero")
	}
	descriptorID := descriptorToken >> 1
	definition := descriptorToken&1 != 0
	var descriptor databaseDescriptorKey
	if definition {
		descriptor.query, err = readPayloadRef(reader, symbolNamespace, segmentState, "database query")
		if err != nil {
			return err
		}
		descriptor.source, err = readPayloadRef(reader, symbolNamespace, segmentState, "database source")
		if err != nil {
			return err
		}
		staticValues, readErr := readPayloadValues(reader, valueScratch,
			"database statement fingerprint",
			"database framework",
			"database operation",
			"database boundary",
		)
		if readErr != nil {
			return readErr
		}
		descriptor.fingerprint = staticValues[0]
		descriptor.framework = DatabaseFramework(staticValues[1])
		descriptor.operation = DatabaseOperation(staticValues[2])
		descriptor.boundary = DatabaseBoundary(staticValues[3])
		if descriptorID != 0 {
			if descriptorID > maxDatabaseDescriptors {
				return fmt.Errorf("database descriptor ID %d exceeds %d", descriptorID, maxDatabaseDescriptors)
			}
			if descriptorID != uint64(len(segmentState.databaseDescriptors))+1 {
				return fmt.Errorf("database descriptor ID %d is not sequential", descriptorID)
			}
			segmentState.databaseDescriptors[descriptorID] = descriptor
		}
	} else {
		descriptor = segmentState.databaseDescriptors[descriptorID]
		if descriptor.source.IsUnknown() {
			return fmt.Errorf("undefined database descriptor %d", descriptorID)
		}
	}
	dynamicValues, err := readPayloadValues(reader, valueScratch, "database outcome", "database total duration", "database presence mask")
	if err != nil {
		return err
	}
	presence := dynamicValues[2]
	const knownDatabasePresence = databasePresenceFailure | databasePresenceResult |
		databasePresenceTransaction | databasePresenceStatementToken | databasePresencePhases
	if presence&^knownDatabasePresence != 0 {
		return fmt.Errorf("unsupported database presence bits 0x%x", presence&^knownDatabasePresence)
	}
	database := &DatabaseEvent{
		QueryRef: descriptor.query, SourceRef: descriptor.source,
		StatementFingerprint: descriptor.fingerprint, Framework: descriptor.framework,
		Operation: descriptor.operation, Boundary: descriptor.boundary,
		Outcome: DatabaseOutcome(dynamicValues[0]), DurationUS: dynamicValues[1],
	}
	if presence&databasePresenceFailure != 0 {
		value, readErr := readPayloadValue(reader, "database failure kind")
		if readErr != nil {
			return readErr
		}
		database.FailureKind = DatabaseFailureKind(value)
	}
	if presence&databasePresenceResult != 0 {
		values, readErr := readPayloadValues(reader, valueScratch, "database result kind", "database result count bucket")
		if readErr != nil {
			return readErr
		}
		database.ResultKnown = true
		database.ResultKind = DatabaseResultKind(values[0])
		database.ResultCountBucket = DatabaseCountBucket(values[1])
	}
	if presence&databasePresenceTransaction != 0 {
		database.TransactionID, err = readDatabaseTransactionID(reader, segmentState)
		if err != nil {
			return err
		}
	}
	if presence&databasePresenceStatementToken != 0 {
		database.StatementToken, err = readPayloadValue(reader, "database statement token")
		if err != nil {
			return err
		}
	}
	if presence&databasePresencePhases != 0 {
		phaseMask, readErr := readPayloadValue(reader, "database phase mask")
		if readErr != nil {
			return readErr
		}
		database.PhaseMask = DatabasePhase(phaseMask)
		if database.PhaseMask&DatabasePhasePoolWait != 0 {
			database.PoolWaitUS, err = readPayloadValue(reader, "database pool wait duration")
			if err != nil {
				return err
			}
		}
		if database.PhaseMask&DatabasePhaseLockWait != 0 {
			database.LockWaitUS, err = readPayloadValue(reader, "database lock wait duration")
			if err != nil {
				return err
			}
		}
		if database.PhaseMask&DatabasePhaseExecute != 0 {
			database.ExecuteUS, err = readPayloadValue(reader, "database execute duration")
			if err != nil {
				return err
			}
		}
		if database.PhaseMask&DatabasePhaseMaterialize != 0 {
			database.MaterializeUS, err = readPayloadValue(reader, "database materialize duration")
			if err != nil {
				return err
			}
		}
	}
	if err := validateDatabaseEvent(database); err != nil {
		return err
	}
	event.Database = database
	return nil
}

func decodeDatabaseTransactionPayload(
	reader *recordReader,
	event *Event,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	sourceRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "database transaction source")
	if err != nil {
		return err
	}
	transactionID, err := readDatabaseTransactionID(reader, segmentState)
	if err != nil {
		return err
	}
	values, err := readPayloadValues(reader, valueScratch, "database transaction stage", "database transaction presence mask")
	if err != nil {
		return err
	}
	presence := values[1]
	const knownTransactionPresence = transactionPresenceParent | transactionPresenceMode |
		transactionPresenceOutcome | transactionPresenceFailure | transactionPresenceDuration |
		transactionPresenceStatements | transactionPresenceReads | transactionPresenceWrites
	if presence&^knownTransactionPresence != 0 {
		return fmt.Errorf("unsupported database transaction presence bits 0x%x", presence&^knownTransactionPresence)
	}
	transaction := &DatabaseTransactionEvent{
		SourceRef: sourceRef, TransactionID: transactionID, Stage: DatabaseTransactionStage(values[0]),
	}
	if presence&transactionPresenceParent != 0 {
		encodedParentDelta, readErr := readPayloadValue(reader, "database transaction parent ID delta")
		if readErr != nil {
			return readErr
		}
		transaction.ParentID, err = addSignedDatabaseTransactionID(
			transaction.TransactionID,
			decodeSVarint(encodedParentDelta),
		)
		if err != nil {
			return err
		}
	}
	if presence&transactionPresenceMode != 0 {
		value, readErr := readPayloadValue(reader, "database transaction mode")
		if readErr != nil {
			return readErr
		}
		transaction.Mode = DatabaseTransactionMode(value)
	}
	if presence&transactionPresenceOutcome != 0 {
		value, readErr := readPayloadValue(reader, "database transaction outcome")
		if readErr != nil {
			return readErr
		}
		transaction.Outcome = DatabaseTransactionOutcome(value)
	}
	if presence&transactionPresenceFailure != 0 {
		value, readErr := readPayloadValue(reader, "database transaction failure kind")
		if readErr != nil {
			return readErr
		}
		transaction.FailureKind = DatabaseFailureKind(value)
	}
	if presence&transactionPresenceDuration != 0 {
		transaction.DurationUS, err = readPayloadValue(reader, "database transaction duration")
		if err != nil {
			return err
		}
	}
	if presence&transactionPresenceStatements != 0 {
		transaction.StatementCount, err = readPayloadValue(reader, "database transaction statement count")
		if err != nil {
			return err
		}
	}
	if presence&transactionPresenceReads != 0 {
		transaction.ReadCount, err = readPayloadValue(reader, "database transaction read count")
		if err != nil {
			return err
		}
	}
	if presence&transactionPresenceWrites != 0 {
		transaction.WriteCount, err = readPayloadValue(reader, "database transaction write count")
		if err != nil {
			return err
		}
	}
	if err := validateDatabaseTransactionEvent(transaction); err != nil {
		return err
	}
	event.DatabaseTransaction = transaction
	return nil
}
