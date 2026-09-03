package jhlog

import (
	"fmt"
	"math"
)

func decodeQualityPayload(
	reader *recordReader,
	event *Event,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	values, err := readPayloadValues(reader, valueScratch, "quality sequence delta", "quality captured delta", "quality changed entry count")
	if err != nil {
		return err
	}
	if values[0] == 0 || math.MaxUint64-segmentState.qualitySequence < values[0] {
		return fmt.Errorf("quality sequence delta %d is invalid", values[0])
	}
	sequence := segmentState.qualitySequence + values[0]
	if segmentState.qualityCapturedUS > math.MaxInt64 {
		return fmt.Errorf("prior quality captured time exceeds signed range")
	}
	captured, err := addSignedTimestamp(int64(segmentState.qualityCapturedUS), decodeSVarint(values[1]))
	if err != nil {
		return fmt.Errorf("quality captured delta: %w", err)
	}
	entryCount := values[2]
	if entryCount > uint64(reader.Len()/2) {
		return fmt.Errorf("quality entry count %d exceeds remaining payload", entryCount)
	}
	previousID := uint64(0)
	for i := uint64(0); i < entryCount; i++ {
		entry, readErr := readPayloadValues(reader, valueScratch, "quality counter id delta", "quality counter value delta")
		if readErr != nil {
			return readErr
		}
		if entry[0] == 0 || math.MaxUint64-previousID < entry[0] {
			return fmt.Errorf("quality counter id delta %d is invalid", entry[0])
		}
		counterID := previousID + entry[0]
		if !IsKnownQualityCounter(counterID) {
			return fmt.Errorf("unsupported quality counter id %d", counterID)
		}
		previousValue := segmentState.qualityCounters[counterID]
		if math.MaxUint64-previousValue < entry[1] {
			return fmt.Errorf("quality counter %d delta overflows", counterID)
		}
		segmentState.qualityCounters[counterID] = previousValue + entry[1]
		previousID = counterID
	}
	segmentState.qualitySequence = sequence
	segmentState.qualityCapturedUS = uint64(captured)
	quality := &QualitySnapshot{
		Sequence: sequence, CapturedElapsedUS: uint64(captured),
		Counters: make(map[uint64]uint64, len(segmentState.qualityCounters)),
	}
	for counterID, value := range segmentState.qualityCounters {
		quality.Counters[counterID] = value
	}
	event.Quality = quality
	return nil
}

func decodeSegmentEndPayload(reader *recordReader, event *Event, valueScratch []uint64) error {
	values, err := readPayloadValues(reader, valueScratch,
		"segment end reason",
		"total event records",
		"total dictionary records",
		"last quality sequence",
	)
	if err != nil {
		return err
	}
	reason := SegmentEndReason(values[0])
	if !reason.supported() {
		return fmt.Errorf("unsupported segment end reason %d", reason)
	}
	event.SegmentEnd = &SegmentEndEvent{
		Reason: reason, TotalEventRecords: values[1],
		TotalDictionaryRecords: values[2], LastQualitySequence: values[3],
	}
	return nil
}

func decodeLogGrowthPayload(
	reader *recordReader,
	event *Event,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	values, err := readPayloadValues(reader, valueScratch, "log-growth kind", "log-growth delta mode", "log-growth decoded length")
	if err != nil {
		return err
	}
	kind := LogGrowthRecordKind(values[0])
	if kind != LogGrowthHistory && kind != LogGrowthLive {
		return fmt.Errorf("unsupported log-growth record kind %d", kind)
	}
	if values[2] == 0 || values[2] > maxRawChunkSize {
		return fmt.Errorf("log-growth decoded length %d is invalid", values[2])
	}
	var raw []byte
	switch values[1] {
	case controlDeltaFull:
		if values[2] != uint64(reader.Len()) {
			return fmt.Errorf("full log-growth length %d differs from remaining %d", values[2], reader.Len())
		}
		raw = make([]byte, int(values[2]))
		if err := reader.readFull(raw); err != nil {
			return fmt.Errorf("log-growth payload: %w", err)
		}
	case controlDeltaPrefixSuffix:
		previous := segmentState.logGrowthPrevious[kind]
		if len(previous) == 0 {
			return fmt.Errorf("log-growth delta has no prior kind %d state", kind)
		}
		prefix, readErr := readPayloadValue(reader, "log-growth prefix length")
		if readErr != nil {
			return readErr
		}
		suffix, readErr := readPayloadValue(reader, "log-growth suffix length")
		if readErr != nil {
			return readErr
		}
		if prefix > uint64(len(previous)) || suffix > uint64(len(previous))-prefix ||
			prefix > values[2] || suffix > values[2]-prefix {
			return fmt.Errorf("log-growth prefix/suffix exceed prior or decoded state")
		}
		middleLength := values[2] - prefix - suffix
		if middleLength != uint64(reader.Len()) {
			return fmt.Errorf("log-growth middle length %d differs from remaining %d", middleLength, reader.Len())
		}
		if uvarintSize(prefix)+uvarintSize(suffix)+int(middleLength) >= int(values[2]) {
			return fmt.Errorf("log-growth delta is not smaller than full encoding")
		}
		raw = make([]byte, int(values[2]))
		copy(raw, previous[:int(prefix)])
		if err := reader.readFull(raw[int(prefix):int(prefix+middleLength)]); err != nil {
			return fmt.Errorf("log-growth delta middle: %w", err)
		}
		copy(raw[int(prefix+middleLength):], previous[len(previous)-int(suffix):])
	default:
		return fmt.Errorf("unsupported log-growth delta mode %d", values[1])
	}
	segmentState.logGrowthPrevious[kind] = raw
	record, err := decodeLogGrowthRecord(kind, raw)
	if err != nil {
		return err
	}
	event.LogGrowth = record
	return nil
}
