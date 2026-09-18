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
	if payload.Origin > SymbolOriginRuntimeStack {
		return fmt.Errorf("invalid symbol origin %d", payload.Origin)
	}
	kind := uint64(payload.Kind)
	if encoder.symbolOrigins {
		kind = kind<<2 | uint64(payload.Origin)
	} else if payload.Origin != SymbolOriginUnknown {
		return fmt.Errorf("symbol origin requires feature31")
	}
	if err := encoder.writeValues(kind); err != nil {
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

func (encoder eventPayloadEncoder) encodeSession(payload *SessionEvent, legacy bool) error {
	if payload == nil {
		return fmt.Errorf("session payload is nil")
	}
	if payload.CollectorFlags&^uint64(CollectorKnownMask) != 0 || (legacy && payload.CollectorFlags&uint64(CollectorHTTP) != 0) {
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

func (encoder eventPayloadEncoder) encodeContext(payload *ContextEvent, legacy bool) error {
	if err := validateTrafficProvenance(payload, legacy); err != nil {
		return err
	}
	if err := encoder.writeValues(
		uint64(payload.Network), payload.BatteryPct, payload.AvailMemoryKB,
		payload.BatteryState, encodeSVarint(payload.BatteryTempDeciC), payload.RxBytes,
		payload.TxBytes, payload.TotalMemoryKB, payload.FreeStorageKB, payload.TotalStorageKB,
	); err != nil {
		return err
	}
	if legacy {
		return nil
	}
	return encoder.writeValues(uint64(payload.TrafficUIDPlusOne), uint64(payload.TrafficKnownFlags))
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
	if err := validateStallLifecycle(payload); err != nil {
		return err
	}
	if err := writeSymbolRef(encoder.writer, payload.StackRef, encoder.stableAliases); err != nil {
		return err
	}
	return encoder.writeValues(payload.DurationMS, payload.IncidentID, uint64(payload.State))
}

func validateStallLifecycle(payload *StallEvent) error {
	if payload.State > StallStateInterrupted {
		return fmt.Errorf("unsupported stall state %d", payload.State)
	}
	if payload.IncidentID == 0 && (payload.State == StallStateOngoing || payload.State == StallStateInterrupted) {
		return fmt.Errorf("stall state %d requires an incident ID", payload.State)
	}
	return nil
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

func (encoder eventPayloadEncoder) encodeMetric(payload *MetricEvent, gauge, legacy bool) error {
	if payload == nil {
		return fmt.Errorf("metric payload is nil")
	}
	count := payload.Count
	if count == 0 {
		count = 1
	}
	sum := payload.Sum
	if sum == 0 && payload.SumHigh == 0 {
		sum = payload.Value
	}
	maximum := payload.Max
	if maximum == 0 {
		maximum = payload.Value
	}
	if err := writeSymbolRef(encoder.writer, payload.MetricRef, encoder.stableAliases); err != nil {
		return err
	}
	if payload.SumHigh != 0 && (!gauge || legacy) {
		return fmt.Errorf("wide sum requires GAUGE_WIDE_SUM feature and gauge record")
	}
	if err := encoder.writeValues(payload.Value, count, sum, maximum, uint64(payload.Mode)); err != nil {
		return err
	}
	if gauge && !legacy {
		return encoder.writeValues(payload.SumHigh)
	}
	return nil
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
