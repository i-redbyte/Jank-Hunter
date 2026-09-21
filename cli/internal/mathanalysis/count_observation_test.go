package mathanalysis

import "testing"

func TestEventCountSeriesDoNotTurnUnobservedHTTPIntervalsIntoZero(t *testing.T) {
	busy := TimelineBucket{HasObservation: true, HTTPCount: 1, HTTPFailed: 1, DNSCount: 1, ConnectCount: 1}
	for _, gap := range []TimelineBucket{{}, {HasObservation: true, HasMemoryPSS: true, MemoryPSSKB: 100}} {
		timeline := []TimelineBucket{busy, gap, busy}
		for _, series := range timelineSeries(timeline, 1000) {
			if series.Name != "HTTP запросы" && series.Name != "HTTP ошибки" && series.Name != "DNS количество" && series.Name != "Количество соединений" {
				continue
			}
			if len(series.Present) != 3 || !series.Present[0] || series.Present[1] || !series.Present[2] {
				t.Errorf("%s: unrelated/missing observation became measured zero: %v", series.Name, series.Present)
			}
		}
		for _, definition := range timelinePeriodicDefinitions(timeline) {
			if definition.name != "HTTP запросы" && definition.name != "HTTP ошибки" && definition.name != "DNS количество" && definition.name != "Количество соединений" {
				continue
			}
			if len(definition.present) != 3 || !definition.present[0] || definition.present[1] || !definition.present[2] {
				t.Errorf("%s: spectral mask fabricates quiet interval: %v", definition.name, definition.present)
			}
		}
	}
}
