package jhlog

import (
	"bytes"
	"testing"
)

func TestDatabaseColumnPageDecodesCanonicalTenMaskWireLayout(t *testing.T) {
	section := []byte{
		1, 1, 0, // schema, database rows, transaction rows
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // ten one-byte database masks
		0, 7, // constant descriptor ID
		0, 1, // constant outcome
		0, 10, // constant duration
	}

	decoder, err := decodeDatabaseColumnSection(section, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decoder.nextPayload(EventDatabase, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{14, 1, 10, 0}; !bytes.Equal(payload, want) {
		t.Fatalf("database payload = %x, want %x", payload, want)
	}
	if err := decoder.validateConsumed(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseColumnPageRoundTripsWirePayloads(t *testing.T) {
	aliases := map[uint64]uint64{0x51: 1}
	state := eventPayloadEncodeState{}
	rows := make([]microPageRow, 0, maxMicroPageRows)
	originalBytes := 0
	for index := 0; index < maxMicroPageRows/2; index++ {
		transactionID := uint64(index + 1)
		database := DatabaseEvent{
			QueryRef: LocalSymbol(1), SourceRef: StableSymbol(0x51),
			Framework: DatabaseFrameworkRoom, Operation: DatabaseOperationQuery,
			Outcome: DatabaseOutcomeSuccess, Boundary: DatabaseBoundaryExecute,
			StatementFingerprint: 0x7123, DurationUS: 1_000 + uint64(index),
			TransactionID: transactionID, StatementToken: uint64(index + 10),
			ResultKnown: true, ResultKind: DatabaseResultRows,
			ResultCountBucket: DatabaseCountTwoToTen,
			PhaseMask:         DatabasePhaseLockWait | DatabasePhaseExecute,
			LockWaitUS:        10, ExecuteUS: 500 + uint64(index),
			descriptorID: 1, descriptorDefinition: index == 0, descriptorPrepared: true,
		}
		rows = appendDatabaseTestRow(t, rows, Event{Type: EventDatabase, Database: &database}, aliases, &state)

		stage := DatabaseTransactionBegin
		outcome := DatabaseTransactionOutcomeUnknown
		duration := uint64(0)
		if index&1 != 0 {
			stage = DatabaseTransactionTerminal
			outcome = DatabaseTransactionSuccess
			duration = 2_000 + uint64(index)
		}
		transaction := DatabaseTransactionEvent{
			SourceRef: StableSymbol(0x51), TransactionID: transactionID,
			Stage: stage, Mode: DatabaseTransactionImmediate,
			Outcome: outcome, DurationUS: duration,
		}
		if stage == DatabaseTransactionTerminal {
			transaction.StatementCount = 8
			transaction.ReadCount = uint64(index & 3)
			transaction.WriteCount = uint64(index & 1)
		}
		if index > 0 {
			transaction.ParentID = transactionID - 1
		}
		rows = appendDatabaseTestRow(
			t, rows, Event{Type: EventDatabaseTransaction, DatabaseTransaction: &transaction}, aliases, &state,
		)
	}
	for index := range rows {
		originalBytes += len(rows[index].payload) + uvarintSize(uint64(len(rows[index].payload)))
	}

	section, encoded, err := encodeDatabaseColumnSection(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !encoded {
		t.Fatal("compressible database rows stayed row-oriented")
	}
	if len(section)+len(rows) >= originalBytes {
		t.Fatalf("column bytes = %d, row bytes = %d", len(section)+len(rows), originalBytes)
	}
	decoder, err := decodeDatabaseColumnSection(section, maxMicroPageRows/2, maxMicroPageRows/2)
	if err != nil {
		t.Fatal(err)
	}
	var scratch [256]byte
	for index := range rows {
		decoded, err := decoder.nextPayload(rows[index].eventType, scratch[:0])
		if err != nil {
			t.Fatalf("row %d: %v", index, err)
		}
		if !bytes.Equal(decoded, rows[index].payload) {
			t.Fatalf("row %d payload = %x, want %x", index, decoded, rows[index].payload)
		}
	}
	if err := decoder.validateConsumed(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseColumnPageFallsBackForInlineStableSymbol(t *testing.T) {
	payload := appendUvarint(nil, 3)
	payload = appendUvarint(payload, 2)
	payload = appendUvarint(payload, 1)
	payload = append(payload, make([]byte, 8)...)
	for range 7 {
		payload = appendUvarint(payload, 1)
	}
	_, encoded, err := encodeDatabaseColumnSection([]microPageRow{{eventType: EventDatabase, payload: payload}})
	if err != nil {
		t.Fatal(err)
	}
	if encoded {
		t.Fatal("inline stable symbol must retain its fixed-width row payload")
	}
}

func appendDatabaseTestRow(
	t *testing.T,
	rows []microPageRow,
	event Event,
	aliases map[uint64]uint64,
	state *eventPayloadEncodeState,
) []microPageRow {
	t.Helper()
	var payload bytes.Buffer
	if err := encodeEventPayloadWithState(&payload, event, aliases, state); err != nil {
		t.Fatal(err)
	}
	return append(rows, microPageRow{eventType: event.Type, payload: append([]byte(nil), payload.Bytes()...)})
}
