package jhlog

import (
	"bytes"
	"math"
	"reflect"
	"testing"
)

func TestRuntimeNumericColumnsSelectModesAndRoundTrip(t *testing.T) {
	constant := make([]uint64, MaxRuntimeCallBlockRows)
	frame := make([]uint64, MaxRuntimeCallBlockRows)
	delta := make([]uint64, MaxRuntimeCallBlockRows)
	rle := make([]uint64, MaxRuntimeCallBlockRows)
	raw := make([]uint64, MaxRuntimeCallBlockRows)
	for index := range constant {
		constant[index] = 1
		frame[index] = 1_000 + uint64(index&7)
		delta[index] = 1_000_000 + uint64(index)
		if index < MaxRuntimeCallBlockRows/2 {
			rle[index] = 7
		} else {
			rle[index] = 9
		}
		if index&1 == 0 {
			raw[index] = math.MaxUint64
		}
	}
	for _, test := range []struct {
		name   string
		values []uint64
		mode   uint64
	}{
		{name: "constant", values: constant, mode: runtimeColumnConstant},
		{name: "frame", values: frame, mode: runtimeColumnFrameOfReference},
		{name: "delta", values: delta, mode: runtimeColumnDelta},
		{name: "rle", values: rle, mode: runtimeColumnRLE},
		{name: "uvarint", values: raw, mode: runtimeColumnUvarint},
	} {
		t.Run(test.name, func(t *testing.T) {
			if plan := planRuntimeNumericColumn(test.values, true, false, 0); plan.mode != test.mode {
				t.Fatalf("mode = %d, want %d (size %d)", plan.mode, test.mode, plan.encodedLen)
			}
			var encoded bytes.Buffer
			if err := writeRuntimeNumericColumn(&encoded, test.values, true, false, 0); err != nil {
				t.Fatal(err)
			}
			decoded := make([]uint64, len(test.values))
			reader := recordReader{data: encoded.Bytes()}
			if err := readRuntimeNumericColumn(&reader, decoded, true, false, 0); err != nil {
				t.Fatal(err)
			}
			if reader.Len() != 0 || !reflect.DeepEqual(decoded, test.values) {
				t.Fatalf("decoded = %v trailing=%d", decoded, reader.Len())
			}
		})
	}
}

func TestRuntimeNumericColumnsRejectNonCanonicalRLE(t *testing.T) {
	reader := recordReader{data: []byte{
		byte(runtimeColumnRLE), 2,
		7, 1,
		7, 1,
	}}
	err := readRuntimeNumericColumn(&reader, make([]uint64, 2), true, false, 0)
	if err == nil {
		t.Fatal("redundant adjacent runs were accepted")
	}
}

func TestRuntimeNumericColumnsPreserveSparseDefaults(t *testing.T) {
	for _, test := range []struct {
		name       string
		values     []uint64
		defaultVal uint64
		mode       uint64
	}{
		{name: "all default", values: make([]uint64, MaxRuntimeCallBlockRows), mode: runtimeColumnDefault},
		{name: "scattered non-default", values: func() []uint64 {
			values := make([]uint64, MaxRuntimeCallBlockRows)
			for index := 0; index < len(values); index += 16 {
				values[index] = 12
			}
			return values
		}(), mode: runtimeColumnSparse},
		{name: "count default", values: func() []uint64 {
			values := make([]uint64, MaxRuntimeCallBlockRows)
			for index := range values {
				values[index] = 1
			}
			return values
		}(), defaultVal: 1, mode: runtimeColumnDefault},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := planRuntimeNumericColumn(test.values, true, true, test.defaultVal)
			if plan.mode != test.mode {
				t.Fatalf("mode = %d, want %d", plan.mode, test.mode)
			}
			var encoded bytes.Buffer
			if err := writeRuntimeNumericColumn(&encoded, test.values, true, true, test.defaultVal); err != nil {
				t.Fatal(err)
			}
			decoded := make([]uint64, len(test.values))
			reader := recordReader{data: encoded.Bytes()}
			if err := readRuntimeNumericColumn(&reader, decoded, true, true, test.defaultVal); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, test.values) {
				t.Fatalf("decoded = %v", decoded)
			}
		})
	}
}
