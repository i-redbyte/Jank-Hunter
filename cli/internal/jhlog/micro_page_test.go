package jhlog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMicroPageBuilderOwnsOneReusablePayloadArena(t *testing.T) {
	page := newMicroPageBuilder()
	payload := []byte{1, 2, 3}
	page.append(microPageRow{eventType: EventMemory, payload: payload})
	payload[0] = 9

	if got := page.rows[0].payload; !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("stored payload = %v, want owned copy", got)
	}
	if len(page.payloadArena) != len(payload) {
		t.Fatalf("payload arena length = %d, want %d", len(page.payloadArena), len(payload))
	}
	if cap(page.payloadArena) != maxMicroPagePayloadBytes {
		t.Fatalf("payload arena capacity = %d, want %d", cap(page.payloadArena), maxMicroPagePayloadBytes)
	}

	page.reset()
	if len(page.payloadArena) != 0 {
		t.Fatalf("payload arena length after reset = %d, want 0", len(page.payloadArena))
	}
	if cap(page.payloadArena) != maxMicroPagePayloadBytes {
		t.Fatalf("payload arena capacity after reset = %d, want %d", cap(page.payloadArena), maxMicroPagePayloadBytes)
	}
}

func TestColumnarMicroPageRoundTripsAndReducesEnvelopeBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "micro-page.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	events := make([]Event, maxMicroPageRows)
	for index := range events {
		value := uint64(index + 1)
		events[index] = Event{
			Type: EventCounter,
			Producer: ProducerMetadata{
				HasTime: true, ElapsedUS: 10_000 + value, HasThread: true, ThreadID: 7,
			},
			Attribution: AttributionContext{Present: true, Screen: LocalSymbol(3), OperationID: 9},
			Metric: &MetricEvent{
				MetricRef: LocalSymbol(1), Value: value, Count: 1, Sum: value, Max: value,
			},
		}
		if err := writer.WriteEvent(events[index]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	rawFile, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := firstChunkOffset(t, rawFile)
	metadata, err := parseChunkHeader(rawFile[offset:offset+chunkHeaderSize], 0)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RecordCount != 1 {
		t.Fatalf("micro-page physical records = %d, want 1", metadata.RecordCount)
	}

	state := recordEncodeState{}
	rowRecordBytes := 0
	for index := range events {
		record, next, _, err := encodeRecord(events[index], state)
		if err != nil {
			t.Fatal(err)
		}
		rowRecordBytes += len(record)
		state = next
	}
	if int(metadata.RawLen)*10 >= rowRecordBytes*9 {
		t.Fatalf("micro-page raw bytes = %d, row records = %d", metadata.RawLen, rowRecordBytes)
	}

	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != len(events) {
		t.Fatalf("decoded events = %d, want %d", len(log.Events), len(events))
	}
	for index := range events {
		if log.Events[index].Metric.Value != events[index].Metric.Value ||
			log.Events[index].Producer.ThreadID != 7 ||
			log.Events[index].Attribution.OperationID != 9 {
			t.Fatalf("decoded row %d = %+v", index, log.Events[index])
		}
	}
}

func TestColumnarMicroPageRejectsInvalidChangeMaskAndPadding(t *testing.T) {
	event := Event{
		Type:   EventCounter,
		Metric: &MetricEvent{MetricRef: LocalSymbol(1), Value: 1, Count: 1, Sum: 1, Max: 1},
	}
	var payload bytes.Buffer
	if err := encodeEventPayload(&payload, event, nil); err != nil {
		t.Fatal(err)
	}
	record, err := encodeMicroPageRecord(
		[]microPageRow{{eventType: event.Type, payload: payload.Bytes()}},
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	body := microPageRecordBody(t, record)

	t.Run("change without presence", func(t *testing.T) {
		corrupt := append([]byte(nil), body...)
		maskStart := microPageSectionStart(t, corrupt, 1)
		corrupt[maskStart+microPageMaskThreadChange] = 1
		_, _, err := decodeRecord(
			corrupt, recordDecodeState{}, "", "", "invalid", RecordPosition{}, nil,
		)
		if err == nil || !strings.Contains(err.Error(), "changes an absent thread") {
			t.Fatalf("change-mask error = %v", err)
		}
	})

	t.Run("nonzero type padding", func(t *testing.T) {
		corrupt := append([]byte(nil), body...)
		typeStart := microPageSectionStart(t, corrupt, 0)
		corrupt[typeStart] |= 0x80
		_, _, err := decodeRecord(
			corrupt, recordDecodeState{}, "", "", "invalid", RecordPosition{}, nil,
		)
		if err == nil || !strings.Contains(err.Error(), "non-zero unused bits") {
			t.Fatalf("padding error = %v", err)
		}
	})
}

func TestColumnarMicroPageRANSMaskRoundTripsWithoutOuterGZIP(t *testing.T) {
	event := Event{
		Type: EventCounter,
		Metric: &MetricEvent{
			MetricRef: LocalSymbol(1), Value: 1, Count: 1, Sum: 1, Max: 1,
		},
	}
	var payload bytes.Buffer
	if err := encodeEventPayload(&payload, event, nil); err != nil {
		t.Fatal(err)
	}
	rows := make([]microPageRow, maxMicroPageRows)
	for index := range rows {
		rows[index] = microPageRow{eventType: event.Type, payload: payload.Bytes()}
	}
	record, err := encodeMicroPageRecord(rows, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	body := microPageRecordBody(t, record)
	reader := recordReader{data: body}
	if _, err := reader.readUvarint(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.readUvarint(); err != nil {
		t.Fatal(err)
	}
	marker, err := reader.readUvarint()
	if err != nil {
		t.Fatal(err)
	}
	if marker != 0 {
		t.Fatalf("entropy marker = %d, want 0", marker)
	}
	if _, err := reader.readUvarint(); err != nil {
		t.Fatal(err)
	}
	codecMask, err := reader.readUvarint()
	if err != nil {
		t.Fatal(err)
	}
	if codecMask == 0 {
		t.Fatal("compressible page did not select any rANS section")
	}
	decoded, _, err := decodeRecord(
		body, recordDecodeState{}, "", "", "rans", RecordPosition{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.microPage == nil || len(decoded.microPage.events) != maxMicroPageRows {
		t.Fatalf("decoded micro-page = %+v", decoded.microPage)
	}
}

func microPageRecordBody(t *testing.T, record []byte) []byte {
	t.Helper()
	reader := recordReader{data: record}
	length, err := reader.readUvarint()
	if err != nil {
		t.Fatal(err)
	}
	if int(length) != reader.Len() {
		t.Fatalf("record length = %d, remaining = %d", length, reader.Len())
	}
	return append([]byte(nil), reader.data[reader.offset:]...)
}

func microPageSectionStart(t *testing.T, body []byte, wanted int) int {
	t.Helper()
	reader := recordReader{data: body}
	for range 3 { // physical type, envelope, page header
		if _, err := reader.readUvarint(); err != nil {
			t.Fatal(err)
		}
	}
	for section := 0; section <= wanted; section++ {
		length, err := reader.readUvarint()
		if err != nil {
			t.Fatal(err)
		}
		start := reader.offset
		if section == wanted {
			return start
		}
		reader.offset += int(length)
	}
	t.Fatal("section not found")
	return 0
}
