package jhlog

import (
	"bytes"
	"io"
	"math"
	"reflect"
	"testing"
)

func TestWriterDelegatesRuntimeBlockState(t *testing.T) {
	writerType := reflect.TypeOf(Writer{})
	if _, ok := writerType.FieldByName("runtimeBlock"); !ok {
		t.Fatal("Writer does not delegate runtime block state")
	}
	for _, obsolete := range []string{"runtimeEdges", "runtimeGraphLogicalCalls"} {
		if _, ok := writerType.FieldByName(obsolete); ok {
			t.Fatalf("Writer still owns %s", obsolete)
		}
	}
}

func TestRuntimeLogicalCountOverflowDoesNotMutateEdgeRegistry(t *testing.T) {
	var destination bytes.Buffer
	writer, err := NewWriter(&destination)
	if err != nil {
		t.Fatal(err)
	}
	writer.runtimeBlock.logicalCalls = math.MaxUint64
	event := Event{
		Type: EventRuntimeCall,
		RuntimeCall: &RuntimeCallEvent{
			Count: 1,
		},
	}
	if err := writer.WriteRuntimeCallBlock([]Event{event}); err == nil {
		t.Fatal("runtime logical count overflow was accepted")
	}
	if len(writer.runtimeBlock.edges) != 0 {
		t.Fatalf("overflow inserted %d runtime edges", len(writer.runtimeBlock.edges))
	}
}

func TestRuntimeBlockPreparationDoesNotAllocate(t *testing.T) {
	events := make([]Event, MaxRuntimeCallBlockRows)
	for index := range events {
		events[index] = Event{
			Type: EventRuntimeCall,
			Attribution: AttributionContext{
				Present: true, OperationID: uint64(index + 1),
			},
			RuntimeCall: &RuntimeCallEvent{Count: 1},
		}
	}
	encoder := newRuntimeBlockEncoder()
	if _, _, _, err := encoder.prepare(events); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		if _, _, _, err := encoder.prepare(events); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("runtimeBlockEncoder.prepare() allocations = %.2f, want 0", allocations)
	}
}

func TestColumnarRuntimeEdgeTupleEncodingDoesNotAllocate(t *testing.T) {
	rows := make([]runtimeCallRow, MaxRuntimeCallBlockRows)
	for index := range rows {
		rows[index] = runtimeCallRow{
			caller: StableSymbol(11), operationID: uint64(index), callee: StableSymbol(17),
			count: 1, edgeID: uint64(index + 1), edgeDefinition: true, edgeReady: true,
		}
	}
	var destination bytes.Buffer
	destination.Grow(4 * 1024)
	encoder := eventPayloadEncoder{writer: &destination, stableAliases: map[uint64]uint64{11: 1, 17: 2}}
	event := Event{Type: EventRuntimeCall, RuntimeCall: &RuntimeCallEvent{}, runtimeCalls: rows}
	if err := encoder.encodeRuntimeCall(event); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		destination.Reset()
		if err := encoder.encodeRuntimeCall(event); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("columnar runtime edge tuple encoding allocations = %.2f, want 0", allocations)
	}
}

func TestSemanticEventStagingDoesNotAllocate(t *testing.T) {
	var destination bytes.Buffer
	writer, err := NewWriter(&destination)
	if err != nil {
		t.Fatal(err)
	}
	event := Event{Type: EventMemory, Memory: &MemoryEvent{PSSKB: 1, JavaHeapKB: 2, NativeHeapKB: 3}}
	if err := writer.writeEventRecord(event, 1); err != nil {
		t.Fatal(err)
	}
	writer.microPage.reset()
	allocations := testing.AllocsPerRun(1_000, func() {
		if err := writer.writeEventRecord(event, 1); err != nil {
			panic(err)
		}
		writer.microPage.reset()
	})
	if allocations != 0 {
		t.Fatalf("semantic event staging allocations = %.2f, want 0", allocations)
	}
}

func BenchmarkSemanticEventStaging(b *testing.B) {
	var destination bytes.Buffer
	writer, err := NewWriter(&destination)
	if err != nil {
		b.Fatal(err)
	}
	event := Event{Type: EventMemory, Memory: &MemoryEvent{PSSKB: 1, JavaHeapKB: 2, NativeHeapKB: 3}}
	if err := writer.writeEventRecord(event, 1); err != nil {
		b.Fatal(err)
	}
	writer.microPage.reset()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := writer.writeEventRecord(event, 1); err != nil {
			b.Fatal(err)
		}
		writer.microPage.reset()
	}
}

func TestRawChunkFlushDoesNotCopyPayload(t *testing.T) {
	writer, err := NewWriterWithOptions(io.Discard, WriterOptions{
		Header: DefaultSegmentHeader(),
		GZIP:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 32*1024)
	writer.raw.Grow(len(payload))
	flush := func() {
		if _, err := writer.raw.Write(payload); err != nil {
			panic(err)
		}
		writer.recordCount = 1
		if err := writer.flushRawChunk(); err != nil {
			panic(err)
		}
	}
	flush()
	allocations := testing.AllocsPerRun(1_000, flush)
	if allocations != 0 {
		t.Fatalf("raw chunk flush allocations = %.2f, want 0", allocations)
	}
}

func BenchmarkRawChunkFlush(b *testing.B) {
	writer, err := NewWriterWithOptions(io.Discard, WriterOptions{
		Header: DefaultSegmentHeader(),
		GZIP:   false,
	})
	if err != nil {
		b.Fatal(err)
	}
	payload := make([]byte, 32*1024)
	writer.raw.Grow(len(payload))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for range b.N {
		if _, err := writer.raw.Write(payload); err != nil {
			b.Fatal(err)
		}
		writer.recordCount = 1
		if err := writer.flushRawChunk(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestWriteUvarintToByteBufferDoesNotAllocate(t *testing.T) {
	var destination bytes.Buffer
	destination.Grow(16)
	allocations := testing.AllocsPerRun(1_000, func() {
		destination.Reset()
		if err := writeUvarint(&destination, math.MaxUint64); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("writeUvarint() allocations = %.2f, want 0", allocations)
	}
}

func BenchmarkWriteUvarintToByteBuffer(b *testing.B) {
	var destination bytes.Buffer
	destination.Grow(16)
	b.ReportAllocs()
	for range b.N {
		destination.Reset()
		if err := writeUvarint(&destination, math.MaxUint64); err != nil {
			b.Fatal(err)
		}
	}
}

func TestInlineStableSymbolRefDoesNotAllocate(t *testing.T) {
	var destination bytes.Buffer
	destination.Grow(16)
	allocations := testing.AllocsPerRun(1_000, func() {
		destination.Reset()
		if err := writeSymbolRef(&destination, StableSymbol(math.MaxUint64), nil); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("inline stable symbol allocations = %.2f, want 0", allocations)
	}
}

func BenchmarkInlineStableSymbolRef(b *testing.B) {
	var destination bytes.Buffer
	destination.Grow(16)
	b.ReportAllocs()
	for range b.N {
		destination.Reset()
		if err := writeSymbolRef(&destination, StableSymbol(math.MaxUint64), nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRuntimeNumericColumnToByteBufferDoesNotAllocate(t *testing.T) {
	tests := []struct {
		name        string
		values      []uint64
		allowSparse bool
	}{
		{name: "delta", values: runtimeNumericSequence(func(index int) uint64 { return 10_000 + uint64(index) })},
		{name: "frame", values: runtimeNumericSequence(func(index int) uint64 { return 10_000 + uint64(index&7) })},
		{name: "sparse", values: runtimeNumericSequence(func(index int) uint64 {
			if index&15 == 0 {
				return 12
			}
			return 0
		}), allowSparse: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var destination bytes.Buffer
			destination.Grow(2_048)
			allocations := testing.AllocsPerRun(1_000, func() {
				destination.Reset()
				if err := writeRuntimeNumericColumn(
					&destination, test.values, true, test.allowSparse, 0,
				); err != nil {
					panic(err)
				}
			})
			if allocations != 0 {
				t.Fatalf("writeRuntimeNumericColumn() allocations = %.2f, want 0", allocations)
			}
		})
	}
}

func BenchmarkRuntimeNumericColumnToByteBuffer(b *testing.B) {
	values := runtimeNumericSequence(func(index int) uint64 { return 10_000 + uint64(index) })
	var destination bytes.Buffer
	destination.Grow(256)
	b.ReportAllocs()
	for range b.N {
		destination.Reset()
		if err := writeRuntimeNumericColumn(&destination, values, true, false, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func runtimeNumericSequence(valueAt func(int) uint64) []uint64 {
	values := make([]uint64, MaxRuntimeCallBlockRows)
	for index := range values {
		values[index] = valueAt(index)
	}
	return values
}
