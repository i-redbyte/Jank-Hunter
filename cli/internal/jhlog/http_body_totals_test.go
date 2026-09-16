package jhlog

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestHTTPBodyARTFixturePreservesWholeCallContract(t *testing.T) {
	log, err := readLog("../../../wire/testdata/http-body-totals-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var rx, tx uint64
	for _, event := range log.Events {
		if event.HTTP == nil {
			continue
		}
		if event.Flags&uint64(FlagHTTPBodyTotals) == 0 {
			t.Fatalf("Android writer lost body totals marker: flags=0x%x", event.Flags)
		}
		if event.HTTP.Attempts != 2 {
			t.Fatalf("expected authentication/redirect exchange count=2, got %d", event.HTTP.Attempts)
		}
		rx += event.HTTP.RxBytes
		tx += event.HTTP.TxBytes
		count++
	}
	if count != 2 || rx != 260 || tx != 300 {
		t.Fatalf("HTTP fixture count/rx/tx = %d/%d/%d, want 2/260/300", count, rx, tx)
	}
}

func TestHTTPBodyTotalsMarkerRoundTrip(t *testing.T) {
	const bodyTotals = uint64(1 << 23)
	const flags = bodyTotals | uint64(FlagHTTPRequestBytesKnown|FlagHTTPResponseBytesKnown)
	for _, count := range []int{1, 128} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "http-body-totals.jhlog")
			events := make([]Event, count)
			for index := range events {
				events[index] = Event{Type: EventHTTP, TimeMS: uint64(index + 1), Flags: flags,
					HTTP: &HTTPEvent{DurationMS: 1, StatusCode: 200, Attempts: 2, TxBytes: 300, RxBytes: 130}}
			}
			writeClosedEvents(t, path, events)
			log, err := readLog(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(log.Events) != count {
				t.Fatalf("events=%d, want %d", len(log.Events), count)
			}
			for index, event := range log.Events {
				if event.Flags != flags || event.HTTP == nil || event.HTTP.TxBytes != 300 || event.HTTP.RxBytes != 130 {
					t.Fatalf("HTTP body contract lost at row %d: %+v", index, event)
				}
			}
		})
	}
}
