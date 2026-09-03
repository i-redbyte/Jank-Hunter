package jhlog

import (
	"fmt"
	"math"
)

func (encoder eventPayloadEncoder) encodeDictionary(payload *DictionaryEntry) error {
	if payload == nil {
		return fmt.Errorf("dictionary payload is nil")
	}
	data, err := dictionaryData(*payload)
	if err != nil {
		return err
	}
	prefix := uint64(0)
	if payload.frontReady {
		data = payload.frontData
		prefix = payload.frontPrefix
	}
	if prefix > uint64(len(data)) {
		return fmt.Errorf("dictionary prefix %d exceeds value length %d", prefix, len(data))
	}
	if err := encoder.writeValues(uint64(payload.Kind)); err != nil {
		return err
	}
	if payload.Kind == DictStableSymbol {
		if err := writeFixedUint64(encoder.writer, payload.ID); err != nil {
			return err
		}
	} else if err := encoder.writeValues(payload.ID); err != nil {
		return err
	}
	if payload.tokenReady {
		return writeAll(encoder.writer, payload.tokenData)
	}
	prefix <<= 1
	if err := encoder.writeValues(prefix, uint64(len(data))-payload.frontPrefix); err != nil {
		return err
	}
	return writeAll(encoder.writer, data[payload.frontPrefix:])
}

func (encoder eventPayloadEncoder) encodeSession(payload *SessionEvent) error {
	if payload == nil {
		return fmt.Errorf("session payload is nil")
	}
	if payload.CollectorFlags&^uint64(CollectorKnownMask) != 0 {
		return fmt.Errorf("unsupported collector flags 0x%x", payload.CollectorFlags)
	}
	if err := encoder.writeRefs(payload.AppVersionRef, payload.BuildRef, payload.DeviceRef); err != nil {
		return err
	}
	if err := encoder.writeValues(payload.SDKInt); err != nil {
		return err
	}
	if err := encoder.writeRefs(
		payload.AndroidReleaseRef,
		payload.SecurityPatchRef,
		payload.PrimaryABIRef,
		payload.SupportedABIsRef,
		payload.ManufacturerRef,
		payload.BrandRef,
		payload.HardwareRef,
		payload.BoardRef,
		payload.ProductRef,
	); err != nil {
		return err
	}
	return encoder.writeValues(payload.CollectorFlags)
}

func (encoder eventPayloadEncoder) encodeContext(payload *ContextEvent) error {
	if payload == nil {
		return fmt.Errorf("device context payload is nil")
	}
	return encoder.writeValues(
		uint64(payload.Network),
		payload.BatteryPct,
		payload.AvailMemoryKB,
		payload.BatteryState,
		encodeSVarint(payload.BatteryTempDeciC),
		payload.RxBytes,
		payload.TxBytes,
		payload.TotalMemoryKB,
		payload.FreeStorageKB,
		payload.TotalStorageKB,
	)
}

func (encoder eventPayloadEncoder) encodeUIWindow(payload *UIWindowEvent) error {
	if payload == nil {
		return fmt.Errorf("ui window payload is nil")
	}
	if err := validateUIWindow(payload); err != nil {
		return err
	}
	if err := encoder.writeValues(
		payload.WindowMS,
		payload.FrameCount,
		payload.JankCount,
		uint64(payload.Source),
		payload.FrameDeadlineUS,
	); err != nil {
		return err
	}
	return encoder.writeValues(payload.FrameDurationBuckets...)
}

func (encoder eventPayloadEncoder) encodeStall(payload *StallEvent) error {
	if payload == nil {
		return fmt.Errorf("stall payload is nil")
	}
	if err := writeSymbolRef(encoder.writer, payload.StackRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(payload.DurationMS)
}

func (encoder eventPayloadEncoder) encodeMemory(payload *MemoryEvent) error {
	if payload == nil {
		return fmt.Errorf("memory payload is nil")
	}
	return encoder.writeValues(payload.PSSKB, payload.JavaHeapKB, payload.NativeHeapKB)
}

func (encoder eventPayloadEncoder) encodeRetained(payload *RetainedEvent) error {
	if payload == nil {
		return fmt.Errorf("retained payload is nil")
	}
	if err := encoder.writeRefs(payload.ClassRef, payload.HolderRef); err != nil {
		return err
	}
	return encoder.writeValues(payload.AgeMS, payload.Count, uint64(payload.Evidence.Effective()))
}

func (encoder eventPayloadEncoder) encodeMetric(payload *MetricEvent) error {
	if payload == nil {
		return fmt.Errorf("metric payload is nil")
	}
	count := payload.Count
	if count == 0 {
		count = 1
	}
	sum := payload.Sum
	if sum == 0 {
		sum = payload.Value
	}
	maximum := payload.Max
	if maximum == 0 {
		maximum = payload.Value
	}
	if err := writeSymbolRef(encoder.writer, payload.MetricRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(payload.Value, count, sum, maximum, uint64(payload.Mode))
}

func (encoder eventPayloadEncoder) encodeLogSpam(payload *LogSpamEvent) error {
	if payload == nil {
		return fmt.Errorf("log spam payload is nil")
	}
	if err := writeSymbolRef(encoder.writer, payload.SourceRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(payload.Level, payload.Count)
}

func (encoder eventPayloadEncoder) encodeProblem(payload *ProblemEvent) error {
	if payload == nil {
		return fmt.Errorf("problem payload is nil")
	}
	if err := writeSymbolRef(encoder.writer, payload.KindRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(payload.WindowMS, payload.Count, payload.MaxMS)
}

func (encoder eventPayloadEncoder) encodeProcessExit(payload *ProcessExitEvent) error {
	if payload == nil {
		return fmt.Errorf("process exit payload is nil")
	}
	if payload.TimestampUnixMS == 0 || payload.TimestampUnixMS > math.MaxInt64 {
		return fmt.Errorf("process exit timestamp must be positive")
	}
	if err := encoder.writeValues(
		payload.Reason,
		payload.TimestampUnixMS,
		payload.Importance,
		payload.PSSKB,
		payload.RSSKB,
	); err != nil {
		return err
	}
	return writeSymbolRef(encoder.writer, payload.ProcessRef, encoder.stableAliases)
}

func (encoder eventPayloadEncoder) encodeProcessState(payload *ProcessStateEvent) error {
	if payload == nil {
		return fmt.Errorf("process state payload is nil")
	}
	if err := validateProcessStateEvent(payload); err != nil {
		return err
	}
	return encoder.writeValues(
		uint64(payload.UIVisibility),
		uint64(payload.Importance),
		uint64(payload.AndroidImportance),
		uint64(payload.Reason),
	)
}
