package analyze

import (
	"fmt"
	"math"
	"strings"
)

func (b *problemBuilder) detectNetwork() {
	locationsByRoute := networkProblemLocations(b.summary)
	for _, route := range b.summary.Routes {
		count := uint64(max(route.Count, 0))
		failures := uint64(max(route.Failures, 0))
		rate := ratePerSecond(count, b.summary.DurationMS)
		failureRate := problemRatio(failures, count)
		longPoll := isProbableLongPoll(route, b.summary.DurationMS, b.cfg)
		slow := route.P95MS >= b.cfg.HTTPSlowMS && !longPoll
		failed := count > 0 && failureRate >= b.cfg.HTTPFailureRate
		storm := count >= b.cfg.HTTPStormMinCount &&
			float64(route.PeakRequestsPerSecond) >= b.cfg.HTTPStormRate && !longPoll
		retried := route.Retries > 0 || route.ConnectFailures > 0 || route.TLSFailures > 0
		if !slow && !failed && !storm {
			continue
		}
		impact := 12
		if failed {
			impact += 10
		}
		if storm {
			impact += 6
		}
		impact = min(impact, 32)
		magnitude := 0
		if slow {
			magnitude = min(18, 7+int(route.P95MS/b.cfg.HTTPSlowMS)*4)
		}
		if failed {
			magnitude = max(magnitude, min(25, 8+int(failureRate/b.cfg.HTTPFailureRate)*4))
		}
		exposure := min(20, 4+int(math.Log2(float64(count)+1))*2)
		if storm {
			exposure = max(exposure, 16)
		}
		compound := 0
		symptoms := boolCount(slow, failed, storm, retried)
		if symptoms > 1 {
			compound = min(5, symptoms+1)
		}
		if count < b.cfg.HTTPMinSample && !storm {
			// A single failure is real, but it does not establish a typical failure rate. Keep the
			// route visible as a signal to verify without ranking it as a confirmed recurring defect.
			impact = min(impact, 16)
			magnitude = min(magnitude, 10)
			exposure = min(exposure, 4)
			compound = 0
		}
		where := locationsByRoute[route.Route]
		title := fmt.Sprintf("%s работает медленно", displayUnknown(route.Route, "Неизвестный маршрут"))
		if storm && slow {
			title = fmt.Sprintf("%s создаёт частый и медленный поток запросов", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if storm {
			title = fmt.Sprintf("%s создаёт шторм запросов", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if failed && count == 1 {
			title = fmt.Sprintf("%s завершился ошибкой в единственном наблюдении", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if failed && count < b.cfg.HTTPMinSample {
			title = fmt.Sprintf("%s завершался ошибкой в небольшой выборке", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if failed {
			title = fmt.Sprintf("%s часто завершается ошибкой", displayUnknown(route.Route, "Неизвестный маршрут"))
		}
		evidence := []ProblemEvidence{{Name: "Число запросов", Observed: fmt.Sprint(count), Unit: "requests", Sample: u64ptr(count), Source: "typed_http"}}
		if slow {
			evidence = append(evidence, ProblemEvidence{Name: upperFiveDurationLabel, Observed: fmt.Sprint(route.P95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.HTTPSlowMS), Sample: u64ptr(count), Source: "typed_http"})
		}
		if failed {
			evidence = append(evidence, ProblemEvidence{Name: "Доля ошибок", Observed: formatPercent(failureRate * 100), Unit: "%", ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.HTTPFailureRate*100), Numerator: u64ptr(failures), Denominator: u64ptr(count), Source: "typed_http"})
		}
		if storm {
			evidence = append(evidence, ProblemEvidence{Name: "Пиковая частота за 1 секунду", Observed: fmt.Sprint(route.PeakRequestsPerSecond), Unit: "requests/s", ExpectedOrThreshold: fmt.Sprintf("< %.2f requests/s", b.cfg.HTTPStormRate), Sample: u64ptr(count), Source: "typed_http_completion_window"})
		}
		if phase, ok := dominantHTTPPhase(route.Phases); ok {
			evidence = append(evidence, ProblemEvidence{Name: "Граница верхних 5% фазы «" + httpPhaseProblemLabel(phase.Name) + "»", Observed: fmt.Sprint(phase.P95MS), Unit: "ms", Sample: u64ptr(uint64(phase.SampleCount)), Source: "typed_http_phase"})
		}
		if route.Retries > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Повторы запросов", Observed: fmt.Sprint(route.Retries), Unit: "attempts", Source: "typed_http_attempts"})
		}
		if route.MaxConcurrency > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Максимальная одновременность", Observed: fmt.Sprint(route.MaxConcurrency), Unit: "requests", Source: "typed_http_intervals"})
		}
		wall := route.TotalDurationMS
		if wall == 0 {
			wall = uint64(route.Count) * route.P50MS
		}
		bytes := route.BytesRx + route.BytesTx
		confidence, reasons, limits := problemConfidence(b.summary, count, b.cfg.HTTPMinSample, true)
		if storm {
			limits = append(limits, "Пик частоты рассчитан по секундам завершения. Одновременность восстановлена отдельно из интервалов, но миллисекундная точность не позволяет определить порядок событий внутри одной миллисекунды.")
			if route.BurstEstimateStatus == "bounded_approximation" {
				limits = append(limits, "Пиковая частота оценена приближённо из-за длительного или неупорядоченного потока событий.")
			}
		}
		b.add(ProblemFinding{
			DetectorID: "network.route_health", DetectorVersion: b.cfg.Version, Category: ProblemCategoryNetwork, Subcategory: networkSubcategory(slow, failed, storm),
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: networkWhat(route, failures, count, slow, storm, b.cfg.HTTPMinSample), Where: where,
			Why:    ProblemWhy{ClaimLevel: "unknown", Summary: "Проблема маршрута измерена, а фазы и точные места вызова сужают поиск; причинность всё равно нужно подтвердить в указанном коде.", Factors: networkFactors(route, slow, failed, storm)},
			Impact: []string{"Задержка или ошибка пользовательского сценария", "Лишняя сетевая и серверная нагрузка при частых вызовах"}, Evidence: evidence,
			Frequency: &ProblemFrequency{Count: count, RatePerSec: rate}, Cost: &ProblemCost{WallTimeMS: nonZeroU64Ptr(wall), Bytes: nonZeroU64Ptr(bytes)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), compound, "задержка или ошибка для пользователя", "отклонение времени ответа", "частота в прогоне", "число контекстов", "сочетание сетевых симптомов"),
			Recommendations:   []ProblemRecommendation{{Action: "Проверить место вызова, убрать лишние повторы и сократить время ответа маршрута", Rationale: "Исправление уменьшит задержку пользователя, а при повторных запросах — ещё и сетевую нагрузку.", Verification: "Повторить тот же сценарий и сравнить число запросов, ошибки, задержку верхних 5% запросов и суммарное время ожидания."}},
			Limitations:       limits, Drilldowns: []ProblemDrilldown{{Label: "Сеть", Anchor: "network", Filter: route.Route}},
		})
	}
	b.detectWebSockets()
}

func isProbableLongPoll(route RouteStats, runDurationMS uint64, cfg ProblemDetectorConfig) bool {
	if uint64(max(route.Count, 0)) < cfg.HTTPMinSample || route.MaxConcurrency != 1 ||
		route.PeakRequestsPerSecond > 1 || runDurationMS == 0 {
		return false
	}
	count := uint64(route.Count)
	if runDurationMS/count < 4_000 {
		return false
	}
	minimumHoldMS := max(uint64(2_500), cfg.HTTPSlowMS*4)
	if route.P95MS < minimumHoldMS {
		return false
	}
	if isSustainedSerialRequest(route, runDurationMS, minimumHoldMS) {
		return true
	}
	return isLongPollRoute(route.Route) && isTTFBDominated(route)
}

func isSustainedSerialRequest(route RouteStats, runDurationMS, minimumHoldMS uint64) bool {
	if route.TotalDurationMS == 0 || route.P50MS < minimumHoldMS || route.P95MS < route.P50MS {
		return false
	}
	maximumSpreadMS := max(uint64(2_000), route.P50MS/2)
	if route.P95MS-route.P50MS > maximumSpreadMS {
		return false
	}
	return route.TotalDurationMS >= runDurationMS-runDurationMS/4
}

func isLongPollRoute(route string) bool {
	for _, marker := range [...]string{"longpoll", "long-poll", "polling", "fetchevents", "fetch_events"} {
		if containsASCIIFold(route, marker) {
			return true
		}
	}
	return false
}

func containsASCIIFold(value, lowerNeedle string) bool {
	for start := 0; start+len(lowerNeedle) <= len(value); start++ {
		matched := true
		for index := range lowerNeedle {
			char := value[start+index]
			if char >= 'A' && char <= 'Z' {
				char += 'a' - 'A'
			}
			if char != lowerNeedle[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func isTTFBDominated(route RouteStats) bool {
	minimumTTFB := route.P95MS - route.P95MS/5
	for _, phase := range route.Phases {
		if strings.EqualFold(phase.Name, "ttfb") && phase.SampleCount > 0 && phase.P95MS >= minimumTTFB {
			return true
		}
	}
	return false
}

func (b *problemBuilder) detectWebSockets() {
	analysis := b.summary.WebSocketAnalysis
	if analysis == nil {
		return
	}
	for _, connection := range analysis.Connections {
		terminals := saturatingUint64Sum(connection.Closed, connection.Failures)
		failureRate := problemRatio(connection.Failures, terminals)
		failureStorm := terminals >= 3 && failureRate >= 0.25
		reconnectStorm := connection.Reconnects >= 3
		slowConnect := connection.Opened >= 3 && connection.ConnectP95MS >= 1_500
		rapidChurn := terminals >= 5 && connection.LifetimeP95MS > 0 && connection.LifetimeP95MS <= 5_000
		if !failureStorm && !reconnectStorm && !slowConnect && !rapidChurn {
			continue
		}
		where := []ProblemLocation{{
			Screen: connection.Screen, Operation: connection.Operation,
			Route: connection.Route, Owner: connection.Owner,
		}}
		evidence := []ProblemEvidence{{
			Name: "Открытия соединения", Observed: fmt.Sprint(connection.Opened),
			Unit: "events", Sample: u64ptr(connection.Opened), Source: "typed_websocket_lifecycle",
		}}
		if failureStorm {
			evidence = append(evidence, ProblemEvidence{
				Name: "Доля обрывов", Observed: formatPercent(failureRate * 100), Unit: "%",
				ExpectedOrThreshold: "< 25%", Numerator: u64ptr(connection.Failures),
				Denominator: u64ptr(terminals), Source: "typed_websocket_lifecycle",
			})
		}
		if reconnectStorm {
			evidence = append(evidence, ProblemEvidence{
				Name: "Переподключения", Observed: fmt.Sprint(connection.Reconnects), Unit: "events",
				ExpectedOrThreshold: "< 3", Source: "typed_websocket_lifecycle",
			})
		}
		if slowConnect {
			evidence = append(evidence, ProblemEvidence{
				Name: "Граница верхних 5% времени подключения", Observed: fmt.Sprint(connection.ConnectP95MS), Unit: "ms",
				ExpectedOrThreshold: "< 1500 ms", Source: "typed_websocket_lifecycle",
			})
		}
		if rapidChurn {
			evidence = append(evidence, ProblemEvidence{
				Name: "Граница верхних 5% времени жизни", Observed: fmt.Sprint(connection.LifetimeP95MS), Unit: "ms",
				ExpectedOrThreshold: "> 5000 ms для устойчивого канала", Source: "typed_websocket_lifecycle",
			})
		}
		confidence, reasons, limits := problemConfidence(b.summary, connection.Opened, 3, true)
		b.add(ProblemFinding{
			DetectorID: "network.websocket_health", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryNetwork, Subcategory: "websocket_instability", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title: fmt.Sprintf("WebSocket %s нестабилен", displayUnknown(connection.Route, "без маршрута")),
			WhatHappened: fmt.Sprintf("Открытий: %d, сбоев: %d, переподключений: %d; 95%% подключений завершились не дольше чем за %d мс.",
				connection.Opened, connection.Failures, connection.Reconnects, connection.ConnectP95MS),
			Where:     where,
			Why:       ProblemWhy{ClaimLevel: "unknown", Summary: "Жизненный цикл показывает нестабильность канала, но причина требует проверки сети, сервера и клиентской политики переподключения."},
			Impact:    []string{"Потеря или задержка данных реального времени", "Лишние подключения, трафик и расход батареи"},
			Evidence:  evidence,
			Frequency: &ProblemFrequency{Count: connection.Opened, RatePerSec: ratePerSecond(connection.Opened, b.summary.DurationMS)},
			Cost:      &ProblemCost{Bytes: nonZeroU64Ptr(connection.ReceivedBytes)},
			PriorityBreakdown: priority(24, 17, min(20, 5+int(connection.Opened)), locationBreadth(where), boolCount(failureStorm, reconnectStorm, slowConnect, rapidChurn),
				"канал реального времени недоступен или запаздывает", "обрывы, повторные подключения или медленное соединение", "число открытий в прогоне", "маршрут и контекст известны", "сочетание симптомов WebSocket"),
			Recommendations: []ProblemRecommendation{{
				Action:       "Проверить причину закрытия и политику переподключения; добавить экспоненциальную задержку со случайным разбросом и ограничением числа попыток",
				Rationale:    "Это предотвращает синхронный шторм повторных подключений и снижает нагрузку при недоступности сети или сервера.",
				Verification: "Повторить сценарий и сравнить число обрывов, переподключений, границу верхних 5% времени подключения и время жизни соединения.",
			}},
			Limitations: limits, Drilldowns: []ProblemDrilldown{{Label: "WebSocket", Anchor: "network", Filter: connection.Route}},
		})
	}
}
