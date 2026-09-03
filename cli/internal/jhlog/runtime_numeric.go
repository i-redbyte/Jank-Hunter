package jhlog

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"math/bits"
)

const (
	runtimeColumnConstant uint64 = iota
	runtimeColumnFrameOfReference
	runtimeColumnDelta
	runtimeColumnRLE
	runtimeColumnUvarint
	runtimeColumnDefault
	runtimeColumnSparse
)

type runtimeNumericPlan struct {
	mode       uint64
	base       uint64
	width      int
	runCount   int
	encodedLen int
}

func writeRuntimeNumericColumn(w io.Writer, values []uint64, allowDelta, allowSparse bool, defaultValue uint64) error {
	if len(values) == 0 || len(values) > MaxRuntimeCallBlockRows {
		return fmt.Errorf("runtime numeric column row count %d is outside 1..%d", len(values), MaxRuntimeCallBlockRows)
	}
	plan := planRuntimeNumericColumn(values, allowDelta, allowSparse, defaultValue)
	if err := writeUvarint(w, plan.mode); err != nil {
		return err
	}
	switch plan.mode {
	case runtimeColumnConstant:
		return writeUvarint(w, plan.base)
	case runtimeColumnFrameOfReference:
		if err := writeUvarint(w, plan.base); err != nil {
			return err
		}
		if err := writeUvarint(w, uint64(plan.width)); err != nil {
			return err
		}
		return writeRuntimeFramePacked(w, values, plan)
	case runtimeColumnDelta:
		if err := writeUvarint(w, values[0]); err != nil {
			return err
		}
		if err := writeUvarint(w, uint64(plan.width)); err != nil {
			return err
		}
		return writeRuntimeDeltaPacked(w, values, plan.width)
	case runtimeColumnRLE:
		if err := writeUvarint(w, uint64(plan.runCount)); err != nil {
			return err
		}
		for start := 0; start < len(values); {
			end := start + 1
			for end < len(values) && values[end] == values[start] {
				end++
			}
			if err := writeUvarint(w, values[start]); err != nil {
				return err
			}
			if err := writeUvarint(w, uint64(end-start)); err != nil {
				return err
			}
			start = end
		}
		return nil
	case runtimeColumnUvarint:
		for _, value := range values {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
		return nil
	case runtimeColumnDefault:
		return nil
	case runtimeColumnSparse:
		bitmapBytes := (len(values) + 7) / 8
		if err := writeRuntimeSparseBitmap(w, values, defaultValue, bitmapBytes); err != nil {
			return err
		}
		for _, value := range values {
			if value != defaultValue {
				if err := writeUvarint(w, value); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		panic("invalid runtime numeric mode")
	}
}

func writeRuntimeFramePacked(w io.Writer, values []uint64, plan runtimeNumericPlan) error {
	byteCount := (len(values)*plan.width + 7) / 8
	if buffer, ok := w.(*bytes.Buffer); ok {
		var packed [MaxRuntimeCallBlockRows * 8]byte
		packRuntimeFrame(packed[:byteCount], values, plan)
		_, err := buffer.Write(packed[:byteCount])
		return err
	}
	var packed [MaxRuntimeCallBlockRows * 8]byte
	packRuntimeFrame(packed[:byteCount], values, plan)
	return writeAll(w, packed[:byteCount])
}

func packRuntimeFrame(destination []byte, values []uint64, plan runtimeNumericPlan) {
	for index, value := range values {
		packColumnValue(destination, index*plan.width, plan.width, value-plan.base)
	}
}

func writeRuntimeDeltaPacked(w io.Writer, values []uint64, width int) error {
	byteCount := ((len(values)-1)*width + 7) / 8
	if buffer, ok := w.(*bytes.Buffer); ok {
		var packed [MaxRuntimeCallBlockRows * 8]byte
		packRuntimeDelta(packed[:byteCount], values, width)
		_, err := buffer.Write(packed[:byteCount])
		return err
	}
	var packed [MaxRuntimeCallBlockRows * 8]byte
	packRuntimeDelta(packed[:byteCount], values, width)
	return writeAll(w, packed[:byteCount])
}

func packRuntimeDelta(destination []byte, values []uint64, width int) {
	for index := 1; index < len(values); index++ {
		delta := int64(values[index]) - int64(values[index-1])
		packColumnValue(destination, (index-1)*width, width, encodeSVarint(delta))
	}
}

func writeRuntimeSparseBitmap(w io.Writer, values []uint64, defaultValue uint64, byteCount int) error {
	if buffer, ok := w.(*bytes.Buffer); ok {
		var bitmap [(MaxRuntimeCallBlockRows + 7) / 8]byte
		fillRuntimeSparseBitmap(bitmap[:byteCount], values, defaultValue)
		_, err := buffer.Write(bitmap[:byteCount])
		return err
	}
	var bitmap [(MaxRuntimeCallBlockRows + 7) / 8]byte
	fillRuntimeSparseBitmap(bitmap[:byteCount], values, defaultValue)
	return writeAll(w, bitmap[:byteCount])
}

func fillRuntimeSparseBitmap(destination []byte, values []uint64, defaultValue uint64) {
	for index, value := range values {
		if value != defaultValue {
			destination[index/8] |= 1 << (index % 8)
		}
	}
}

func planRuntimeNumericColumn(values []uint64, allowDelta, allowSparse bool, defaultValue uint64) runtimeNumericPlan {
	minimum := values[0]
	maximum := values[0]
	rawBytes := 1
	runs := 1
	rleBytes := 0
	runStart := 0
	deltaWidth := 0
	deltaAllowed := allowDelta && values[0] <= math.MaxInt64
	nonDefault := 0
	sparseBytes := 1 + (len(values)+7)/8
	for index, value := range values {
		minimum = min(minimum, value)
		maximum = max(maximum, value)
		rawBytes += uvarintSize(value)
		if value != defaultValue {
			nonDefault++
			sparseBytes += uvarintSize(value)
		}
		if value > math.MaxInt64 {
			deltaAllowed = false
		}
		if index > 0 {
			if values[index-1] != value {
				rleBytes += uvarintSize(values[runStart]) + uvarintSize(uint64(index-runStart))
				runs++
				runStart = index
			}
			if deltaAllowed {
				deltaWidth = max(deltaWidth, bits.Len64(encodeSVarint(int64(value)-int64(values[index-1]))))
			}
		}
	}
	rleBytes += uvarintSize(values[runStart]) + uvarintSize(uint64(len(values)-runStart))
	if allowSparse && nonDefault == 0 {
		return runtimeNumericPlan{mode: runtimeColumnDefault, encodedLen: 1}
	}
	if minimum == maximum {
		return runtimeNumericPlan{
			mode: runtimeColumnConstant, base: minimum,
			encodedLen: 1 + uvarintSize(minimum),
		}
	}
	best := runtimeNumericPlan{mode: runtimeColumnUvarint, encodedLen: rawBytes}
	width := bits.Len64(maximum - minimum)
	frameBytes := 1 + uvarintSize(minimum) + uvarintSize(uint64(width)) + (len(values)*width+7)/8
	if frameBytes < best.encodedLen {
		best = runtimeNumericPlan{
			mode: runtimeColumnFrameOfReference, base: minimum, width: width, encodedLen: frameBytes,
		}
	}
	if deltaAllowed && deltaWidth != 0 {
		deltaBytes := 1 + uvarintSize(values[0]) + uvarintSize(uint64(deltaWidth)) +
			((len(values)-1)*deltaWidth+7)/8
		if deltaBytes < best.encodedLen {
			best = runtimeNumericPlan{
				mode: runtimeColumnDelta, base: values[0], width: deltaWidth, encodedLen: deltaBytes,
			}
		}
	}
	rleBytes += 1 + uvarintSize(uint64(runs))
	if rleBytes < best.encodedLen {
		best = runtimeNumericPlan{mode: runtimeColumnRLE, runCount: runs, encodedLen: rleBytes}
	}
	if allowSparse && sparseBytes < best.encodedLen {
		best = runtimeNumericPlan{mode: runtimeColumnSparse, encodedLen: sparseBytes}
	}
	return best
}

func readRuntimeNumericColumn(
	reader *recordReader,
	destination []uint64,
	allowDelta, allowSparse bool,
	defaultValue uint64,
) error {
	if len(destination) == 0 || len(destination) > MaxRuntimeCallBlockRows {
		return fmt.Errorf("runtime numeric column row count %d is outside 1..%d", len(destination), MaxRuntimeCallBlockRows)
	}
	mode, err := reader.readUvarint()
	if err != nil {
		return fmt.Errorf("mode: %w", err)
	}
	switch mode {
	case runtimeColumnConstant:
		value, err := reader.readUvarint()
		if err != nil {
			return fmt.Errorf("constant: %w", err)
		}
		for index := range destination {
			destination[index] = value
		}
	case runtimeColumnFrameOfReference:
		minimum, err := reader.readUvarint()
		if err != nil {
			return fmt.Errorf("minimum: %w", err)
		}
		width, packed, err := readRuntimePackedData(reader, len(destination))
		if err != nil {
			return err
		}
		for index := range destination {
			delta := unpackColumnValue(packed, index*width, width)
			if delta > math.MaxUint64-minimum {
				return fmt.Errorf("frame-of-reference value overflows uint64")
			}
			destination[index] = minimum + delta
		}
	case runtimeColumnDelta:
		if !allowDelta {
			return fmt.Errorf("delta mode is not allowed")
		}
		if len(destination) < 2 {
			return fmt.Errorf("delta mode requires at least two rows")
		}
		first, err := reader.readUvarint()
		if err != nil || first > math.MaxInt64 {
			return fmt.Errorf("invalid first delta value %d", first)
		}
		destination[0] = first
		width, packed, err := readRuntimePackedData(reader, len(destination)-1)
		if err != nil {
			return err
		}
		previous := int64(first)
		for index := 1; index < len(destination); index++ {
			delta := decodeSVarint(unpackColumnValue(packed, (index-1)*width, width))
			current, err := addSignedTimestamp(previous, delta)
			if err != nil || current < 0 {
				return fmt.Errorf("delta value %d overflows non-negative int64", index)
			}
			destination[index] = uint64(current)
			previous = current
		}
	case runtimeColumnRLE:
		runCount, err := reader.readUvarint()
		if err != nil || runCount == 0 || runCount > uint64(len(destination)) {
			return fmt.Errorf("invalid run count %d", runCount)
		}
		offset := 0
		var previous uint64
		for run := uint64(0); run < runCount; run++ {
			value, err := reader.readUvarint()
			if err != nil {
				return fmt.Errorf("run %d value: %w", run, err)
			}
			length, err := reader.readUvarint()
			if err != nil || length == 0 || length > uint64(len(destination)-offset) {
				return fmt.Errorf("run %d has invalid length %d", run, length)
			}
			if run != 0 && value == previous {
				return fmt.Errorf("run %d redundantly repeats value %d", run, value)
			}
			for end := offset + int(length); offset < end; offset++ {
				destination[offset] = value
			}
			previous = value
		}
		if offset != len(destination) {
			return fmt.Errorf("runs populate %d of %d rows", offset, len(destination))
		}
	case runtimeColumnUvarint:
		for index := range destination {
			value, err := reader.readUvarint()
			if err != nil {
				return fmt.Errorf("value %d: %w", index, err)
			}
			destination[index] = value
		}
	case runtimeColumnDefault:
		if !allowSparse {
			return fmt.Errorf("default mode is not allowed")
		}
		for index := range destination {
			destination[index] = defaultValue
		}
	case runtimeColumnSparse:
		if !allowSparse {
			return fmt.Errorf("sparse mode is not allowed")
		}
		bitmapBytes := (len(destination) + 7) / 8
		if bitmapBytes > reader.Len() {
			return fmt.Errorf("sparse bitmap needs %d bytes, has %d", bitmapBytes, reader.Len())
		}
		bitmap := reader.data[reader.offset : reader.offset+bitmapBytes]
		reader.offset += bitmapBytes
		if err := validateMicroPagePadding(bitmap, len(destination)); err != nil {
			return fmt.Errorf("sparse bitmap padding: %w", err)
		}
		nonDefault := 0
		for index := range destination {
			destination[index] = defaultValue
			if bitmap[index/8]&(1<<(index%8)) == 0 {
				continue
			}
			value, err := reader.readUvarint()
			if err != nil {
				return fmt.Errorf("sparse value %d: %w", index, err)
			}
			if value == defaultValue {
				return fmt.Errorf("sparse value %d redundantly stores the default", index)
			}
			destination[index] = value
			nonDefault++
		}
		if nonDefault == 0 {
			return fmt.Errorf("sparse bitmap contains no values")
		}
	default:
		return fmt.Errorf("unsupported runtime numeric mode %d", mode)
	}
	return nil
}

func readRuntimePackedData(reader *recordReader, valueCount int) (int, []byte, error) {
	width, err := reader.readUvarint()
	if err != nil || width == 0 || width > 64 {
		return 0, nil, fmt.Errorf("invalid bit width %d", width)
	}
	byteCount := (valueCount*int(width) + 7) / 8
	if byteCount > reader.Len() {
		return 0, nil, fmt.Errorf("packed data needs %d bytes, has %d", byteCount, reader.Len())
	}
	packed := reader.data[reader.offset : reader.offset+byteCount]
	reader.offset += byteCount
	if err := validateMicroPagePadding(packed, valueCount*int(width)); err != nil {
		return 0, nil, fmt.Errorf("packed padding: %w", err)
	}
	return int(width), packed, nil
}
