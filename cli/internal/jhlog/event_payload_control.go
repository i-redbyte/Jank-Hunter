package jhlog

import (
	"fmt"
	"math"
	"sort"
)

func (encoder eventPayloadEncoder) encodeQuality(payload *QualitySnapshot) error {
	if payload == nil {
		return fmt.Errorf("quality snapshot payload is nil")
	}
	if payload.Sequence == 0 {
		return fmt.Errorf("quality sequence delta must be positive")
	}
	if payload.CapturedElapsedUS > math.MaxInt64 {
		return fmt.Errorf("quality captured time %d exceeds signed range", payload.CapturedElapsedUS)
	}
	ids := make([]uint64, 0, len(payload.Counters))
	for id := range payload.Counters {
		if !IsKnownQualityCounter(id) {
			return fmt.Errorf("unsupported quality counter id %d", id)
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err := encoder.writeValues(
		payload.Sequence,
		encodeSVarint(int64(payload.CapturedElapsedUS)),
		uint64(len(ids)),
	); err != nil {
		return err
	}
	previousID := uint64(0)
	for _, id := range ids {
		if err := encoder.writeValues(id-previousID, payload.Counters[id]); err != nil {
			return err
		}
		previousID = id
	}
	return nil
}

func (encoder eventPayloadEncoder) encodeSegmentEnd(payload *SegmentEndEvent) error {
	if payload == nil {
		return fmt.Errorf("segment end payload is nil")
	}
	if !payload.Reason.supported() {
		return fmt.Errorf("unsupported segment end reason %d", payload.Reason)
	}
	return encoder.writeValues(
		uint64(payload.Reason),
		payload.TotalEventRecords,
		payload.TotalDictionaryRecords,
		payload.LastQualitySequence,
	)
}

func (encoder eventPayloadEncoder) encodeLogGrowth(payload *LogGrowthRecord) error {
	if payload == nil {
		return fmt.Errorf("log-growth payload is nil")
	}
	prepared := *payload
	if !prepared.wireReady {
		var err error
		prepared, err = prepareLogGrowthDelta(prepared, nil)
		if err != nil {
			return err
		}
	}
	if err := encoder.writeValues(uint64(prepared.Kind), prepared.wireMode, uint64(len(prepared.Raw))); err != nil {
		return err
	}
	if prepared.wireMode == controlDeltaFull {
		return writeAll(encoder.writer, prepared.Raw)
	}
	if prepared.wireMode != controlDeltaPrefixSuffix ||
		prepared.wirePrefix+prepared.wireSuffix > uint64(len(prepared.Raw)) {
		return fmt.Errorf("invalid log-growth delta metadata")
	}
	if err := encoder.writeValues(prepared.wirePrefix, prepared.wireSuffix); err != nil {
		return err
	}
	middleEnd := len(prepared.Raw) - int(prepared.wireSuffix)
	return writeAll(encoder.writer, prepared.Raw[int(prepared.wirePrefix):middleEnd])
}
