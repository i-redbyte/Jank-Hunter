package jhlog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type Writer struct {
	w      io.Writer
	lastMS uint64
}

func NewWriter(w io.Writer) (*Writer, error) {
	if _, err := w.Write(Magic); err != nil {
		return nil, err
	}
	return &Writer{w: w}, nil
}

func Create(path string) (*os.File, *Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	writer, err := NewWriter(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, writer, nil
}

func (w *Writer) WriteEvent(event Event) error {
	var payload bytes.Buffer
	if err := encodePayload(&payload, event); err != nil {
		return err
	}

	delta := event.TimeMS
	if event.TimeMS >= w.lastMS {
		delta = event.TimeMS - w.lastMS
	}
	w.lastMS = event.TimeMS

	flags := compactEventFlags(event)
	if err := writeCompactHeader(w.w, event.Type, delta, flags, needsPayloadLength(event.Type), uint64(payload.Len())); err != nil {
		return err
	}
	_, err := w.w.Write(payload.Bytes())
	return err
}

func compactEventFlags(event Event) uint64 {
	flags := event.Flags
	if event.Type == EventContext && event.Context != nil {
		if event.Context.LowMemory {
			flags |= uint64(FlagContextLowMemory)
		}
		if event.Context.NetworkMetered {
			flags |= uint64(FlagNetworkMetered)
		}
		if event.Context.NetworkValidated {
			flags |= uint64(FlagNetworkValidated)
		}
		if event.Context.NetworkVPN {
			flags |= uint64(FlagNetworkVPN)
		}
	}
	if event.Type == EventSession && event.Session != nil && event.Session.DeviceRooted {
		flags |= uint64(FlagDeviceRooted)
	}
	switch event.Type {
	case EventFlow:
		flags |= compactContextFlags(event.Flow)
	case EventLogSpam:
		flags |= compactContextFlags(event.LogSpam)
	case EventProblem:
		flags |= compactContextFlags(event.Problem)
	}
	return flags
}

type contextIDs interface {
	contextIDs() (screenID, ownerID, flowID, stepID uint64)
}

func compactContextFlags(context contextIDs) uint64 {
	if context == nil {
		return 0
	}
	screenID, ownerID, flowID, stepID := context.contextIDs()
	var flags uint64
	if screenID != 0 {
		flags |= uint64(FlagHasScreen)
	}
	if ownerID != 0 {
		flags |= uint64(FlagHasOwner)
	}
	if flowID != 0 {
		flags |= uint64(FlagHasFlow)
	}
	if stepID != 0 {
		flags |= uint64(FlagHasStep)
	}
	return flags
}

func encodePayload(w io.Writer, event Event) error {
	switch event.Type {
	case EventDictionary:
		if event.Dictionary == nil {
			return fmt.Errorf("dictionary payload is nil")
		}
		if err := writeUvarint(w, uint64(event.Dictionary.Kind)); err != nil {
			return err
		}
		if err := writeUvarint(w, event.Dictionary.ID); err != nil {
			return err
		}
		if err := writeDictionaryValue(w, event.Dictionary.Value); err != nil {
			return err
		}
	case EventSession:
		p := event.Session
		if p == nil {
			return fmt.Errorf("session payload is nil")
		}
		for _, value := range []uint64{p.AppVersionID, p.BuildID, p.DeviceID, p.SDKInt} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
		extra := []uint64{
			p.ProcessID,
			p.AndroidReleaseID,
			p.SecurityPatchID,
			p.PrimaryABIID,
			p.SupportedABIsID,
			p.ManufacturerID,
			p.BrandID,
			p.HardwareID,
			p.BoardID,
			p.ProductID,
		}
		if hasNonZero(extra) {
			for _, value := range extra {
				if err := writeUvarint(w, value); err != nil {
					return err
				}
			}
		}
	case EventContext:
		p := event.Context
		if p == nil {
			return fmt.Errorf("context payload is nil")
		}
		for _, value := range []uint64{
			uint64(p.Network),
			p.BatteryPct,
			p.AvailMemoryKB,
			p.BatteryState,
			p.BatteryTempDeciC,
			p.RxBytes,
			p.TxBytes,
			p.TotalMemoryKB,
			p.FreeStorageKB,
			p.TotalStorageKB,
		} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventHTTP:
		p := event.HTTP
		if p == nil {
			return fmt.Errorf("http payload is nil")
		}
		values := []uint64{
			p.OwnerID, p.RouteID, p.DurationMS, p.DNSMS, p.ConnectMS,
			p.TTFBMS, uint64(p.Status), p.RxBytes, p.TxBytes,
		}
		for _, value := range values {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventUIWindow:
		p := event.UIWindow
		if p == nil {
			return fmt.Errorf("ui window payload is nil")
		}
		values := []uint64{p.ScreenID, p.WindowMS, p.FrameCount, p.JankCount, p.P50MS, p.P95MS, p.P99MS}
		for _, value := range values {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventStall:
		p := event.Stall
		if p == nil {
			return fmt.Errorf("stall payload is nil")
		}
		for _, value := range []uint64{p.OwnerID, p.StackID, p.DurationMS} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventMemory:
		p := event.Memory
		if p == nil {
			return fmt.Errorf("memory payload is nil")
		}
		for _, value := range []uint64{p.PSSKB, p.JavaHeapKB, p.NativeHeapKB} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventRetained:
		p := event.Retained
		if p == nil {
			return fmt.Errorf("retained payload is nil")
		}
		for _, value := range []uint64{p.ClassID, p.AgeMS, p.Count} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventCounter, EventGauge:
		p := event.Metric
		if p == nil {
			return fmt.Errorf("metric payload is nil")
		}
		for _, value := range []uint64{p.MetricID, p.Value} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventFlow:
		p := event.Flow
		if p == nil {
			return fmt.Errorf("flow payload is nil")
		}
		if err := writeContextIDs(w, compactEventFlags(event), p); err != nil {
			return err
		}
	case EventLogSpam:
		p := event.LogSpam
		if p == nil {
			return fmt.Errorf("log spam payload is nil")
		}
		if err := writeContextIDs(w, compactEventFlags(event), p); err != nil {
			return err
		}
		for _, value := range []uint64{p.SourceID, p.Level, p.Count} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	case EventProblem:
		p := event.Problem
		if p == nil {
			return fmt.Errorf("problem payload is nil")
		}
		if err := writeContextIDs(w, compactEventFlags(event), p); err != nil {
			return err
		}
		for _, value := range []uint64{p.KindID, p.WindowMS, p.Count, p.MaxMS} {
			if err := writeUvarint(w, value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
	return nil
}

func (p *FlowEvent) contextIDs() (uint64, uint64, uint64, uint64) {
	if p == nil {
		return 0, 0, 0, 0
	}
	return p.ScreenID, p.OwnerID, p.FlowID, p.StepID
}

func (p *LogSpamEvent) contextIDs() (uint64, uint64, uint64, uint64) {
	if p == nil {
		return 0, 0, 0, 0
	}
	return p.ScreenID, p.OwnerID, p.FlowID, p.StepID
}

func (p *ProblemEvent) contextIDs() (uint64, uint64, uint64, uint64) {
	if p == nil {
		return 0, 0, 0, 0
	}
	return p.ScreenID, p.OwnerID, p.FlowID, p.StepID
}

func writeContextIDs(w io.Writer, flags uint64, context contextIDs) error {
	screenID, ownerID, flowID, stepID := context.contextIDs()
	values := []struct {
		flag  Flag
		value uint64
	}{
		{FlagHasScreen, screenID},
		{FlagHasOwner, ownerID},
		{FlagHasFlow, flowID},
		{FlagHasStep, stepID},
	}
	for _, item := range values {
		if flags&uint64(item.flag) == 0 {
			continue
		}
		if err := writeUvarint(w, item.value); err != nil {
			return err
		}
	}
	return nil
}

func hasNonZero(values []uint64) bool {
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}

func writeDictionaryValue(w io.Writer, value string) error {
	if value == "" {
		if err := writeUvarint(w, 0); err != nil {
			return err
		}
		if err := writeUvarint(w, dictValueCodecUTF8); err != nil {
			return err
		}
		return writeRawString(w, value)
	}
	if payload, codec, ok := encodedDictionaryValue(value); ok {
		utf8Size := uvarintSize(uint64(len(value))) + len(value)
		encodedSize := 1 + uvarintSize(codec) + len(payload)
		if encodedSize < utf8Size {
			if err := writeUvarint(w, 0); err != nil {
				return err
			}
			if err := writeUvarint(w, codec); err != nil {
				return err
			}
			_, err := w.Write(payload)
			return err
		}
	}
	return writeRawString(w, value)
}

func writeRawString(w io.Writer, value string) error {
	if err := writeUvarint(w, uint64(len(value))); err != nil {
		return err
	}
	_, err := io.WriteString(w, value)
	return err
}

func encodedDictionaryValue(value string) ([]byte, uint64, bool) {
	if isISODate(value) {
		return packBCDString(value[:4] + value[5:7] + value[8:10]), dictValueCodecBCDISODate, true
	}
	if isDecimalString(value) {
		var payload bytes.Buffer
		if err := writeUvarint(&payload, uint64(len(value))); err != nil {
			return nil, 0, false
		}
		payload.Write(packBCDString(value))
		return payload.Bytes(), dictValueCodecBCDDecimal, true
	}
	return nil, 0, false
}

func isDecimalString(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func isISODate(value string) bool {
	if len(value) != len("2000-01-01") || value[4] != '-' || value[7] != '-' {
		return false
	}
	for _, index := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func packBCDString(value string) []byte {
	out := make([]byte, (len(value)+1)/2)
	for i := range out {
		hi := value[i*2] - '0'
		lo := byte(0x0f)
		if i*2+1 < len(value) {
			lo = value[i*2+1] - '0'
		}
		out[i] = hi<<4 | lo
	}
	return out
}

func uvarintSize(value uint64) int {
	var buf [binary.MaxVarintLen64]byte
	return binary.PutUvarint(buf[:], value)
}

func writeUvarint(w io.Writer, value uint64) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], value)
	_, err := w.Write(buf[:n])
	return err
}

func writeCompactHeader(w io.Writer, eventType EventType, deltaMS, flags uint64, payloadLength bool, payloadSize uint64) error {
	deltaCode := compactDeltaCode(deltaMS)
	header := byte(eventType & compactEventTypeMask)
	if flags != 0 {
		header |= compactHeaderHasFlags
	}
	if payloadLength {
		header |= compactHeaderHasPayloadLen
	}
	header |= byte(deltaCode << compactHeaderDeltaShift)
	if _, err := w.Write([]byte{header}); err != nil {
		return err
	}
	if err := writeCompactDelta(w, deltaCode, deltaMS); err != nil {
		return err
	}
	if flags != 0 {
		if err := writeUvarint(w, flags); err != nil {
			return err
		}
	}
	if payloadLength {
		if err := writeUvarint(w, payloadSize); err != nil {
			return err
		}
	}
	return nil
}

func compactDeltaCode(deltaMS uint64) byte {
	switch {
	case deltaMS == 0:
		return compactDeltaZero
	case deltaMS <= 0xff:
		return compactDeltaUint8
	case deltaMS <= 0xffff:
		return compactDeltaUint16
	default:
		return compactDeltaUvarint
	}
}

func writeCompactDelta(w io.Writer, code byte, deltaMS uint64) error {
	switch code {
	case compactDeltaZero:
		return nil
	case compactDeltaUint8:
		_, err := w.Write([]byte{byte(deltaMS)})
		return err
	case compactDeltaUint16:
		_, err := w.Write([]byte{byte(deltaMS), byte(deltaMS >> 8)})
		return err
	default:
		return writeUvarint(w, deltaMS)
	}
}

func needsPayloadLength(eventType EventType) bool {
	switch eventType {
	case EventDictionary, EventSession, EventContext, EventFlow, EventLogSpam, EventProblem:
		return true
	default:
		return false
	}
}

const (
	compactEventTypeMask       EventType = 0x0f
	compactHeaderHasFlags                = 1 << 4
	compactHeaderHasPayloadLen           = 1 << 5
	compactHeaderDeltaShift              = 6
	compactDeltaZero           byte      = 0
	compactDeltaUint8          byte      = 1
	compactDeltaUint16         byte      = 2
	compactDeltaUvarint        byte      = 3
)

func WriteSample(path string) error {
	file, writer, err := Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	entries := []DictionaryEntry{
		{Kind: DictAppVersion, ID: 1, Value: "0.1.0-debug"},
		{Kind: DictBuild, ID: 2, Value: "100"},
		{Kind: DictDevice, ID: 3, Value: "Pixel 8 / API 35"},
		{Kind: DictProcess, ID: 4, Value: "main"},
		{Kind: DictOwner, ID: 10, Value: "FeedRepository.refresh"},
		{Kind: DictOwner, ID: 11, Value: "CheckoutPresenter.render"},
		{Kind: DictOwner, ID: 12, Value: "CheckoutButton.onClick"},
		{Kind: DictRoute, ID: 20, Value: "GET /feed"},
		{Kind: DictRoute, ID: 21, Value: "POST /checkout"},
		{Kind: DictScreen, ID: 30, Value: "FeedScreen"},
		{Kind: DictScreen, ID: 31, Value: "CheckoutScreen"},
		{Kind: DictClass, ID: 40, Value: "com.app.checkout.CheckoutActivity"},
		{Kind: DictStack, ID: 50, Value: "CheckoutPresenter.renderItems"},
		{Kind: DictMetric, ID: 60, Value: "logs.warn.count"},
		{Kind: DictMetric, ID: 61, Value: "ui.fps_x100"},
		{Kind: DictMetric, ID: 62, Value: "ui_jank"},
		{Kind: DictMetric, ID: 63, Value: "main_thread_stall"},
		{Kind: DictLogSource, ID: 64, Value: "android.util.Log.w"},
		{Kind: DictFlow, ID: 65, Value: "checkout.open"},
		{Kind: DictStep, ID: 66, Value: "render_list"},
		{Kind: DictStep, ID: 67, Value: "network"},
		{Kind: DictGeneric, ID: 70, Value: "15"},
		{Kind: DictGeneric, ID: 71, Value: "2025-05-05"},
		{Kind: DictGeneric, ID: 72, Value: "arm64-v8a"},
		{Kind: DictGeneric, ID: 73, Value: "arm64-v8a,armeabi-v7a,armeabi"},
		{Kind: DictGeneric, ID: 74, Value: "Google"},
		{Kind: DictGeneric, ID: 75, Value: "google"},
		{Kind: DictGeneric, ID: 76, Value: "shiba"},
		{Kind: DictGeneric, ID: 77, Value: "shiba"},
		{Kind: DictGeneric, ID: 78, Value: "shiba"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			return err
		}
	}

	events := []Event{
		{Type: EventSession, TimeMS: 1, Flags: uint64(FlagAppForeground), Session: &SessionEvent{AppVersionID: 1, BuildID: 2, DeviceID: 3, SDKInt: 35, ProcessID: 4, AndroidReleaseID: 70, SecurityPatchID: 71, PrimaryABIID: 72, SupportedABIsID: 73, ManufacturerID: 74, BrandID: 75, HardwareID: 76, BoardID: 77, ProductID: 78}},
		{Type: EventContext, TimeMS: 500, Flags: uint64(FlagAppForeground | FlagNetworkValidated), Context: &ContextEvent{Network: NetworkWiFi, BatteryPct: 82, AvailMemoryKB: 2018304, TotalMemoryKB: 8032000, BatteryState: 2, BatteryTempDeciC: 320, NetworkMetered: false, NetworkValidated: true, RxBytes: 1_204_000, TxBytes: 93_000, FreeStorageKB: 48_000_000, TotalStorageKB: 118_000_000, NetworkVPN: false}},
		{Type: EventHTTP, TimeMS: 1200, Flags: uint64(FlagHTTPReusedConnection | FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{OwnerID: 10, RouteID: 20, DurationMS: 184, DNSMS: 7, ConnectMS: 0, TTFBMS: 91, Status: Status2xx, RxBytes: 42120, TxBytes: 740}},
		{Type: EventHTTP, TimeMS: 2400, Flags: uint64(FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{OwnerID: 10, RouteID: 20, DurationMS: 612, DNSMS: 10, ConnectMS: 90, TTFBMS: 430, Status: Status2xx, RxBytes: 38900, TxBytes: 730}},
		{Type: EventUIWindow, TimeMS: 10000, Flags: uint64(FlagThreadMain | FlagAppForeground), UIWindow: &UIWindowEvent{ScreenID: 30, WindowMS: 10000, FrameCount: 580, JankCount: 28, P50MS: 12, P95MS: 33, P99MS: 72}},
		{Type: EventGauge, TimeMS: 10100, Metric: &MetricEvent{MetricID: 61, Value: 5800}},
		{Type: EventFlow, TimeMS: 12000, Flow: &FlowEvent{ScreenID: 31, OwnerID: 12, FlowID: 65, StepID: 67}},
		{Type: EventLogSpam, TimeMS: 12100, LogSpam: &LogSpamEvent{ScreenID: 31, OwnerID: 12, FlowID: 65, StepID: 67, SourceID: 64, Level: 5, Count: 12}},
		{Type: EventStall, TimeMS: 13200, Flags: uint64(FlagThreadMain | FlagAppForeground), Stall: &StallEvent{OwnerID: 11, StackID: 50, DurationMS: 1240}},
		{Type: EventProblem, TimeMS: 13201, Problem: &ProblemEvent{ScreenID: 31, OwnerID: 11, FlowID: 65, StepID: 66, KindID: 63, WindowMS: 1240, Count: 1, MaxMS: 1240}},
		{Type: EventMemory, TimeMS: 15000, Flags: uint64(FlagAppForeground), Memory: &MemoryEvent{PSSKB: 188240, JavaHeapKB: 90412, NativeHeapKB: 38112}},
		{Type: EventRetained, TimeMS: 21000, Retained: &RetainedEvent{ClassID: 40, AgeMS: 15000, Count: 2}},
		{Type: EventCounter, TimeMS: 22000, Metric: &MetricEvent{MetricID: 60, Value: 17}},
		{Type: EventHTTP, TimeMS: 23000, Flags: uint64(FlagHTTPFailed | FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{OwnerID: 11, RouteID: 21, DurationMS: 1320, DNSMS: 9, ConnectMS: 0, TTFBMS: 1140, Status: Status5xx, RxBytes: 1024, TxBytes: 1240}},
		{Type: EventUIWindow, TimeMS: 30000, Flags: uint64(FlagThreadMain | FlagAppForeground), UIWindow: &UIWindowEvent{ScreenID: 31, WindowMS: 10000, FrameCount: 542, JankCount: 62, P50MS: 14, P95MS: 48, P99MS: 108}},
		{Type: EventProblem, TimeMS: 30001, Problem: &ProblemEvent{ScreenID: 31, OwnerID: 11, FlowID: 65, StepID: 66, KindID: 62, WindowMS: 10000, Count: 62, MaxMS: 48}},
		{Type: EventGauge, TimeMS: 30100, Metric: &MetricEvent{MetricID: 61, Value: 5420}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			return err
		}
	}
	return nil
}
