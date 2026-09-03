package jhlog

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

func decodeCorePayload(
	reader *recordReader,
	event *Event,
	processName string,
	symbolNamespace string,
	segmentState *segmentDecodeState,
	valueScratch []uint64,
) error {
	switch event.Type {
	case EventDictionary:
		kindValue, err := readPayloadValue(reader, "dictionary kind")
		if err != nil {
			return err
		}
		kind := DictKind(kindValue)
		if kind > DictAttributeValue {
			return fmt.Errorf("unsupported dictionary kind %d", kind)
		}
		var id uint64
		var alias uint64
		if kind == DictStableSymbol {
			alias = uint64(len(segmentState.stableAliases)) + 1
			var rawID [8]byte
			if err := reader.readFull(rawID[:]); err != nil {
				return fmt.Errorf("stable dictionary ID: %w", err)
			}
			id = binary.LittleEndian.Uint64(rawID[:])
			if existing, ok := segmentState.stableIDs[id]; ok {
				return fmt.Errorf("stable symbol %d is already defined as alias %d", id, existing)
			}
		} else {
			id, err = readPayloadValue(reader, "dictionary local id")
			if err != nil {
				return err
			}
		}
		layout, err := readPayloadValue(reader, "dictionary value layout")
		if err != nil {
			return err
		}
		var data []byte
		if layout&1 != 0 {
			data, err = decodeDictionaryTokens(reader, kind, layout>>1, segmentState)
			if err != nil {
				return err
			}
		} else {
			prefixLength := layout >> 1
			previous := segmentState.dictionaryPrevious[kind]
			if prefixLength > uint64(len(previous)) {
				return fmt.Errorf("dictionary prefix length %d exceeds previous kind %d length %d", prefixLength, kind, len(previous))
			}
			if prefixLength < uint64(len(previous)) && previous[prefixLength]&0xc0 == 0x80 {
				return fmt.Errorf("dictionary prefix length %d splits a UTF-8 code point", prefixLength)
			}
			suffixLength, suffixErr := readPayloadValue(reader, "dictionary suffix length")
			if suffixErr != nil {
				return suffixErr
			}
			if suffixLength > uint64(reader.Len()) || prefixLength+suffixLength > maxRawChunkSize {
				return fmt.Errorf("dictionary suffix length %d exceeds remaining or maximum value", suffixLength)
			}
			data = make([]byte, int(prefixLength+suffixLength))
			copy(data, previous[:int(prefixLength)])
			if err := reader.readFull(data[int(prefixLength):]); err != nil {
				return fmt.Errorf("dictionary suffix: %w", err)
			}
			if !utf8.Valid(data) {
				return fmt.Errorf("dictionary value %d is not valid UTF-8", id)
			}
			if canonical := commonUTF8Prefix(previous, data); canonical != int(prefixLength) {
				return fmt.Errorf("dictionary prefix length %d is non-canonical; expected %d", prefixLength, canonical)
			}
		}
		segmentState.dictionaryPrevious[kind] = data
		entry := &DictionaryEntry{Kind: kind, ID: id, Alias: alias, Data: data}
		entry.Value = string(data)
		if kind == DictStableSymbol {
			segmentState.stableAliases[alias] = id
			segmentState.stableIDs[id] = alias
		}
		event.Dictionary = entry
	case EventSession:
		refs := make([]SymbolRef, 12)
		for i, name := range []string{
			"app version", "build", "device",
		} {
			ref, err := readPayloadRef(reader, symbolNamespace, segmentState, name)
			if err != nil {
				return err
			}
			refs[i] = ref
		}
		sdk, err := readPayloadValue(reader, "SDK")
		if err != nil {
			return err
		}
		for i, name := range []string{
			"Android release", "security patch", "primary ABI", "supported ABIs",
			"manufacturer", "brand", "hardware", "board", "product",
		} {
			ref, err := readPayloadRef(reader, symbolNamespace, segmentState, name)
			if err != nil {
				return err
			}
			refs[i+3] = ref
		}
		collectorFlags, err := readPayloadValue(reader, "collector flags")
		if err != nil {
			return err
		}
		if collectorFlags&^uint64(CollectorKnownMask) != 0 {
			return fmt.Errorf("unsupported collector flags 0x%x", collectorFlags)
		}
		event.Session = &SessionEvent{
			AppVersionRef: refs[0],
			BuildRef:      refs[1],
			DeviceRef:     refs[2],
			SDKInt:        sdk, CollectorFlags: collectorFlags, ProcessName: processName,
			AndroidReleaseRef: refs[3],
			SecurityPatchRef:  refs[4],
			PrimaryABIRef:     refs[5],
			SupportedABIsRef:  refs[6],
			ManufacturerRef:   refs[7],
			BrandRef:          refs[8],
			HardwareRef:       refs[9],
			BoardRef:          refs[10],
			ProductRef:        refs[11],
			DeviceRooted:      event.Flags&uint64(FlagDeviceRooted) != 0,
		}
	case EventContext:
		values, err := readPayloadValues(reader, valueScratch, "network", "battery percent", "available memory", "battery state", "battery temperature", "rx bytes", "tx bytes", "total memory", "free storage", "total storage")
		if err != nil {
			return err
		}
		if values[0] > uint64(NetworkVPN) {
			return fmt.Errorf("unsupported network kind %d", values[0])
		}
		if values[1] > 100 {
			return fmt.Errorf("battery percent %d exceeds 100", values[1])
		}
		if values[7] > 0 && values[2] > values[7] {
			return fmt.Errorf("available memory %d exceeds total memory %d", values[2], values[7])
		}
		if values[9] > 0 && values[8] > values[9] {
			return fmt.Errorf("free storage %d exceeds total storage %d", values[8], values[9])
		}
		event.Context = &ContextEvent{
			Network: NetworkKind(values[0]), BatteryPct: values[1], AvailMemoryKB: values[2],
			BatteryState: values[3], BatteryTempDeciC: decodeSVarint(values[4]),
			RxBytes: values[5], TxBytes: values[6], TotalMemoryKB: values[7],
			FreeStorageKB: values[8], TotalStorageKB: values[9],
			LowMemory:        event.Flags&uint64(FlagContextLowMemory) != 0,
			NetworkMetered:   event.Flags&uint64(FlagNetworkMetered) != 0,
			NetworkValidated: event.Flags&uint64(FlagNetworkValidated) != 0,
			NetworkVPN:       event.Flags&uint64(FlagNetworkVPN) != 0,
		}
	case EventUIWindow:
		values, err := readPayloadValues(reader, valueScratch, "window", "frames", "jank", "source", "frame deadline")
		if err != nil {
			return err
		}
		windowMS := values[0]
		frameCount := values[1]
		jankCount := values[2]
		source := UIFrameSource(values[3])
		frameDeadlineUS := values[4]
		bucketValues, err := readPayloadValues(reader, valueScratch,
			"frames <=8ms", "frames <=12ms", "frames <=16ms", "frames <=20ms", "frames <=24ms",
			"frames <=32ms", "frames <=40ms", "frames <=50ms", "frames <=67ms", "frames <=100ms",
			"frames <=250ms", "frames <=1000ms", "frames >1000ms",
		)
		if err != nil {
			return err
		}
		buckets := append([]uint64(nil), bucketValues...)
		window := &UIWindowEvent{
			WindowMS: windowMS, FrameCount: frameCount, JankCount: jankCount, Source: source,
			FrameDeadlineUS: frameDeadlineUS, FrameDurationBuckets: buckets,
		}
		if err := validateUIWindow(window); err != nil {
			return err
		}
		window.P50MS = UIFrameHistogramQuantileMS(buckets, 50)
		window.P95MS = UIFrameHistogramQuantileMS(buckets, 95)
		window.P99MS = UIFrameHistogramQuantileMS(buckets, 99)
		event.UIWindow = window
	case EventStall:
		stack, err := readPayloadRef(reader, symbolNamespace, segmentState, "stack")
		if err != nil {
			return err
		}
		duration, err := readPayloadValue(reader, "duration")
		if err != nil {
			return err
		}
		event.Stall = &StallEvent{
			StackRef: stack, DurationMS: duration,
		}
	case EventMemory:
		values, err := readPayloadValues(reader, valueScratch, "PSS", "Java heap", "native heap")
		if err != nil {
			return err
		}
		event.Memory = &MemoryEvent{PSSKB: values[0], JavaHeapKB: values[1], NativeHeapKB: values[2]}
	case EventRetained:
		classRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "retained class")
		if err != nil {
			return err
		}
		holderRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "holder")
		if err != nil {
			return err
		}
		values, err := readPayloadValues(reader, valueScratch, "age", "count")
		if err != nil {
			return err
		}
		value, err := readPayloadValue(reader, "retention evidence")
		if err != nil {
			return err
		}
		evidence := RetentionEvidence(value)
		if evidence != RetentionEvidenceTimeOnly && evidence != RetentionEvidenceAfterExplicitGC {
			return fmt.Errorf("unsupported retention evidence %d", value)
		}
		event.Retained = &RetainedEvent{
			ClassRef: classRef, HolderRef: holderRef,
			AgeMS: values[0], Count: values[1], Evidence: evidence,
		}
	case EventCounter, EventGauge:
		metricRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "metric")
		if err != nil {
			return err
		}
		values, err := readPayloadValues(reader, valueScratch, "value", "count", "sum", "max", "mode")
		if err != nil {
			return err
		}
		if values[1] == 0 {
			return fmt.Errorf("metric count must be positive")
		}
		if values[4] > uint64(MetricModeBooleanRate) {
			return fmt.Errorf("unsupported metric mode %d", values[4])
		}
		if values[3] > values[2] {
			return fmt.Errorf("metric max %d exceeds sum %d", values[3], values[2])
		}
		event.Metric = &MetricEvent{MetricRef: metricRef, Value: values[0], Count: values[1], Sum: values[2], Max: values[3], Mode: MetricMode(values[4])}
	case EventLogSpam:
		sourceRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "log source")
		if err != nil {
			return err
		}
		values, err := readPayloadValues(reader, valueScratch, "level", "count")
		if err != nil {
			return err
		}
		event.LogSpam = &LogSpamEvent{
			SourceRef: sourceRef, Level: values[0], Count: values[1],
		}
	case EventProblem:
		kindRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "problem kind")
		if err != nil {
			return err
		}
		values, err := readPayloadValues(reader, valueScratch, "window", "count", "max")
		if err != nil {
			return err
		}
		event.Problem = &ProblemEvent{
			KindRef: kindRef, WindowMS: values[0], Count: values[1], MaxMS: values[2],
		}
	case EventProcessExit:
		values, err := readPayloadValues(reader, valueScratch, "exit reason", "exit timestamp", "exit importance", "exit PSS", "exit RSS")
		if err != nil {
			return err
		}
		processRef, err := readPayloadRef(reader, symbolNamespace, segmentState, "exit process")
		if err != nil {
			return err
		}
		if values[1] == 0 || values[1] > math.MaxInt64 {
			return fmt.Errorf("process exit timestamp must be positive")
		}
		event.ProcessExit = &ProcessExitEvent{
			Reason: values[0], TimestampUnixMS: values[1], Importance: values[2],
			PSSKB: values[3], RSSKB: values[4], ProcessRef: processRef,
		}
	case EventProcessState:
		values, err := readPayloadValues(reader, valueScratch,
			"process UI visibility",
			"process importance",
			"Android process importance",
			"process state reason",
		)
		if err != nil {
			return err
		}
		if values[2] > math.MaxUint32 {
			return fmt.Errorf("android process importance exceeds %d", uint64(math.MaxUint32))
		}
		processState := &ProcessStateEvent{
			UIVisibility: ProcessUIVisibility(values[0]), Importance: ProcessImportance(values[1]),
			AndroidImportance: uint32(values[2]), Reason: ProcessStateReason(values[3]),
		}
		if err := validateProcessStateEvent(processState); err != nil {
			return err
		}
		event.ProcessState = processState
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
	return nil
}
