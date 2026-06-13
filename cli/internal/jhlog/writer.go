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

	if err := writeUvarint(w.w, uint64(event.Type)); err != nil {
		return err
	}
	if err := writeUvarint(w.w, delta); err != nil {
		return err
	}
	if err := writeUvarint(w.w, event.Flags); err != nil {
		return err
	}
	if err := writeUvarint(w.w, uint64(payload.Len())); err != nil {
		return err
	}
	_, err := w.w.Write(payload.Bytes())
	return err
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
		if err := writeString(w, event.Dictionary.Value); err != nil {
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
	case EventContext:
		p := event.Context
		if p == nil {
			return fmt.Errorf("context payload is nil")
		}
		for _, value := range []uint64{uint64(p.Network), p.BatteryPct, p.AvailMemoryKB} {
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
	default:
		return fmt.Errorf("unsupported event type %d", event.Type)
	}
	return nil
}

func writeString(w io.Writer, value string) error {
	if err := writeUvarint(w, uint64(len(value))); err != nil {
		return err
	}
	_, err := io.WriteString(w, value)
	return err
}

func writeUvarint(w io.Writer, value uint64) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], value)
	_, err := w.Write(buf[:n])
	return err
}

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
		{Kind: DictOwner, ID: 10, Value: "FeedRepository.refresh"},
		{Kind: DictOwner, ID: 11, Value: "CheckoutPresenter.render"},
		{Kind: DictRoute, ID: 20, Value: "GET /feed"},
		{Kind: DictRoute, ID: 21, Value: "POST /checkout"},
		{Kind: DictScreen, ID: 30, Value: "FeedScreen"},
		{Kind: DictScreen, ID: 31, Value: "CheckoutScreen"},
		{Kind: DictClass, ID: 40, Value: "com.app.checkout.CheckoutActivity"},
		{Kind: DictStack, ID: 50, Value: "CheckoutPresenter.renderItems"},
		{Kind: DictMetric, ID: 60, Value: "logs.warn.count"},
		{Kind: DictMetric, ID: 61, Value: "ui.fps_x100"},
	}
	for _, entry := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			return err
		}
	}

	events := []Event{
		{Type: EventSession, TimeMS: 1, Flags: uint64(FlagAppForeground), Session: &SessionEvent{AppVersionID: 1, BuildID: 2, DeviceID: 3, SDKInt: 35}},
		{Type: EventContext, TimeMS: 500, Flags: uint64(FlagAppForeground), Context: &ContextEvent{Network: NetworkWiFi, BatteryPct: 82, AvailMemoryKB: 2018304}},
		{Type: EventHTTP, TimeMS: 1200, Flags: uint64(FlagHTTPReusedConnection | FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{OwnerID: 10, RouteID: 20, DurationMS: 184, DNSMS: 7, ConnectMS: 0, TTFBMS: 91, Status: Status2xx, RxBytes: 42120, TxBytes: 740}},
		{Type: EventHTTP, TimeMS: 2400, Flags: uint64(FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{OwnerID: 10, RouteID: 20, DurationMS: 612, DNSMS: 10, ConnectMS: 90, TTFBMS: 430, Status: Status2xx, RxBytes: 38900, TxBytes: 730}},
		{Type: EventUIWindow, TimeMS: 10000, Flags: uint64(FlagThreadMain | FlagAppForeground), UIWindow: &UIWindowEvent{ScreenID: 30, WindowMS: 10000, FrameCount: 580, JankCount: 28, P50MS: 12, P95MS: 33, P99MS: 72}},
		{Type: EventGauge, TimeMS: 10100, Metric: &MetricEvent{MetricID: 61, Value: 5800}},
		{Type: EventStall, TimeMS: 13200, Flags: uint64(FlagThreadMain | FlagAppForeground), Stall: &StallEvent{OwnerID: 11, StackID: 50, DurationMS: 1240}},
		{Type: EventMemory, TimeMS: 15000, Flags: uint64(FlagAppForeground), Memory: &MemoryEvent{PSSKB: 188240, JavaHeapKB: 90412, NativeHeapKB: 38112}},
		{Type: EventRetained, TimeMS: 21000, Retained: &RetainedEvent{ClassID: 40, AgeMS: 15000, Count: 2}},
		{Type: EventCounter, TimeMS: 22000, Metric: &MetricEvent{MetricID: 60, Value: 17}},
		{Type: EventHTTP, TimeMS: 23000, Flags: uint64(FlagHTTPFailed | FlagHTTPTLS | FlagAppForeground), HTTP: &HTTPEvent{OwnerID: 11, RouteID: 21, DurationMS: 1320, DNSMS: 9, ConnectMS: 0, TTFBMS: 1140, Status: Status5xx, RxBytes: 1024, TxBytes: 1240}},
		{Type: EventUIWindow, TimeMS: 30000, Flags: uint64(FlagThreadMain | FlagAppForeground), UIWindow: &UIWindowEvent{ScreenID: 31, WindowMS: 10000, FrameCount: 542, JankCount: 62, P50MS: 14, P95MS: 48, P99MS: 108}},
		{Type: EventGauge, TimeMS: 30100, Metric: &MetricEvent{MetricID: 61, Value: 5420}},
	}
	for _, event := range events {
		if err := writer.WriteEvent(event); err != nil {
			return err
		}
	}
	return nil
}
