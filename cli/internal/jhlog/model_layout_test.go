package jhlog

import (
	"testing"
	"unsafe"
)

func TestCanonicalModelLayoutStaysCompact(t *testing.T) {
	limits := []struct {
		name  string
		size  uintptr
		limit uintptr
	}{
		{name: "symbol", size: unsafe.Sizeof(SymbolRef{}), limit: 32},
		{name: "attribution", size: unsafe.Sizeof(AttributionContext{}), limit: 144},
		{name: "event", size: unsafe.Sizeof(Event{}), limit: 416},
		{name: "session", size: unsafe.Sizeof(SessionEvent{}), limit: 424},
		{name: "http", size: unsafe.Sizeof(HTTPEvent{}), limit: 208},
		{name: "ui", size: unsafe.Sizeof(UIWindowEvent{}), limit: 88},
		{name: "stall", size: unsafe.Sizeof(StallEvent{}), limit: 40},
		{name: "retained", size: unsafe.Sizeof(RetainedEvent{}), limit: 88},
		{name: "metric", size: unsafe.Sizeof(MetricEvent{}), limit: 72},
		{name: "operation", size: unsafe.Sizeof(OperationEvent{}), limit: 152},
		{name: "log spam", size: unsafe.Sizeof(LogSpamEvent{}), limit: 48},
		{name: "problem", size: unsafe.Sizeof(ProblemEvent{}), limit: 56},
		{name: "runtime call", size: unsafe.Sizeof(RuntimeCallEvent{}), limit: 56},
		{name: "runtime row", size: unsafe.Sizeof(runtimeCallRow{}), limit: 184},
	}
	for _, item := range limits {
		t.Run(item.name, func(t *testing.T) {
			if item.size > item.limit {
				t.Fatalf("layout grew to %d bytes, limit %d", item.size, item.limit)
			}
		})
	}
}
