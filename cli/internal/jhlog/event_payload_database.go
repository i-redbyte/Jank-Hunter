package jhlog

import (
	"fmt"
	"math"
)

const (
	databasePresenceFailure uint64 = 1 << iota
	databasePresenceResult
	databasePresenceTransaction
	databasePresenceStatementToken
	databasePresencePhases
)

const (
	transactionPresenceParent uint64 = 1 << iota
	transactionPresenceMode
	transactionPresenceOutcome
	transactionPresenceFailure
	transactionPresenceDuration
	transactionPresenceStatements
	transactionPresenceReads
	transactionPresenceWrites
)

func (encoder eventPayloadEncoder) encodeDatabase(
	payload *DatabaseEvent,
	state *eventPayloadEncodeState,
) error {
	if payload == nil {
		return fmt.Errorf("database payload is nil")
	}
	if err := validateDatabaseEvent(payload); err != nil {
		return err
	}
	definition := !payload.descriptorPrepared || payload.descriptorDefinition
	if !definition && payload.descriptorID == 0 {
		return fmt.Errorf("database descriptor reference requires a non-zero ID")
	}
	descriptorToken := payload.descriptorID << 1
	if definition {
		descriptorToken |= 1
	}
	if err := encoder.writeValues(descriptorToken); err != nil {
		return err
	}
	if definition {
		if err := encoder.writeRefs(payload.QueryRef, payload.SourceRef); err != nil {
			return err
		}
		if err := encoder.writeValues(
			payload.StatementFingerprint,
			uint64(payload.Framework),
			uint64(payload.Operation),
			uint64(payload.Boundary),
		); err != nil {
			return err
		}
	}
	presence := uint64(0)
	if payload.FailureKind != DatabaseFailureNone {
		presence |= databasePresenceFailure
	}
	if payload.ResultKnown {
		presence |= databasePresenceResult
	}
	if payload.TransactionID != 0 {
		presence |= databasePresenceTransaction
	}
	if payload.StatementToken != 0 {
		presence |= databasePresenceStatementToken
	}
	if payload.PhaseMask != 0 {
		presence |= databasePresencePhases
	}
	if err := encoder.writeValues(uint64(payload.Outcome), payload.DurationUS, presence); err != nil {
		return err
	}
	if presence&databasePresenceFailure != 0 {
		if err := encoder.writeValues(uint64(payload.FailureKind)); err != nil {
			return err
		}
	}
	if presence&databasePresenceResult != 0 {
		if err := encoder.writeValues(uint64(payload.ResultKind), uint64(payload.ResultCountBucket)); err != nil {
			return err
		}
	}
	if presence&databasePresenceTransaction != 0 {
		if err := encoder.writeDatabaseTransactionID(payload.TransactionID, state); err != nil {
			return err
		}
	}
	if presence&databasePresenceStatementToken != 0 {
		if err := encoder.writeValues(payload.StatementToken); err != nil {
			return err
		}
	}
	if presence&databasePresencePhases != 0 {
		if err := encoder.writeValues(uint64(payload.PhaseMask)); err != nil {
			return err
		}
		if payload.PhaseMask&DatabasePhasePoolWait != 0 {
			if err := encoder.writeValues(payload.PoolWaitUS); err != nil {
				return err
			}
		}
		if payload.PhaseMask&DatabasePhaseLockWait != 0 {
			if err := encoder.writeValues(payload.LockWaitUS); err != nil {
				return err
			}
		}
		if payload.PhaseMask&DatabasePhaseExecute != 0 {
			if err := encoder.writeValues(payload.ExecuteUS); err != nil {
				return err
			}
		}
		if payload.PhaseMask&DatabasePhaseMaterialize != 0 {
			if err := encoder.writeValues(payload.MaterializeUS); err != nil {
				return err
			}
		}
	}
	return nil
}

func (encoder eventPayloadEncoder) encodeDatabaseTransaction(
	payload *DatabaseTransactionEvent,
	state *eventPayloadEncodeState,
) error {
	if payload == nil {
		return fmt.Errorf("database transaction payload is nil")
	}
	if err := validateDatabaseTransactionEvent(payload); err != nil {
		return err
	}
	if err := writeSymbolRef(encoder.writer, payload.SourceRef, encoder.stableAliases); err != nil {
		return err
	}
	presence := uint64(0)
	values := [...]struct {
		bit   uint64
		value uint64
	}{
		{transactionPresenceParent, payload.ParentID},
		{transactionPresenceMode, uint64(payload.Mode)},
		{transactionPresenceOutcome, uint64(payload.Outcome)},
		{transactionPresenceFailure, uint64(payload.FailureKind)},
		{transactionPresenceDuration, payload.DurationUS},
		{transactionPresenceStatements, payload.StatementCount},
		{transactionPresenceReads, payload.ReadCount},
		{transactionPresenceWrites, payload.WriteCount},
	}
	for _, field := range values {
		if field.value != 0 {
			presence |= field.bit
		}
	}
	if err := encoder.writeDatabaseTransactionID(payload.TransactionID, state); err != nil {
		return err
	}
	if err := encoder.writeValues(uint64(payload.Stage), presence); err != nil {
		return err
	}
	for index, field := range values {
		if presence&field.bit == 0 {
			continue
		}
		value := field.value
		if index == 0 {
			if value > math.MaxInt64 {
				return fmt.Errorf("database transaction parent ID %d exceeds signed delta range", value)
			}
			value = encodeSVarint(int64(value) - int64(payload.TransactionID))
		}
		if err := encoder.writeValues(value); err != nil {
			return err
		}
	}
	return nil
}

func (encoder eventPayloadEncoder) writeDatabaseTransactionID(
	transactionID uint64,
	state *eventPayloadEncodeState,
) error {
	if transactionID == 0 {
		return fmt.Errorf("database transaction ID must be non-zero")
	}
	if transactionID > math.MaxInt64 || state.lastDatabaseTransactionID > math.MaxInt64 {
		return fmt.Errorf("database transaction ID %d exceeds signed delta range", transactionID)
	}
	encoded := encodeSVarint(int64(transactionID) - int64(state.lastDatabaseTransactionID))
	if err := writeUvarint(encoder.writer, encoded); err != nil {
		return err
	}
	state.lastDatabaseTransactionID = transactionID
	return nil
}
