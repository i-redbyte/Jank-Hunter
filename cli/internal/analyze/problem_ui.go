package analyze

import (
	"fmt"
	"math"
)

func (b *problemBuilder) detectUI() {
	for _, screen := range b.summary.Screens {
		if screen.Frames < b.cfg.UIMinFrames {
			continue
		}
		tailThresholdMS := b.cfg.UIFrameTailMS
		if screen.FrameDeadlineStatus == "consistent" && screen.FrameDeadlineUS > 0 {
			tailThresholdMS = maxUint64(1, (screen.FrameDeadlineUS*2+999)/1_000)
		}
		badRate := screen.JankRatePct >= b.cfg.UIJankRate
		badTail := screen.FrameP95MS >= tailThresholdMS || screen.FrameP99MS >= tailThresholdMS*2
		if !badRate && !badTail {
			continue
		}
		magnitude := min(25, 8+int(screen.JankRatePct/b.cfg.UIJankRate)*4)
		if badTail {
			magnitude = max(magnitude, min(25, 8+int(screen.FrameP95MS/tailThresholdMS)*4))
		}
		exposure := min(20, 6+int(math.Log2(float64(screen.Frames))))
		confidence, reasons, limits := problemConfidence(b.summary, screen.Frames, b.cfg.UIMinFrames, true)
		if screen.FrameDeadlineStatus != "consistent" || screen.FrameDeadlineUS == 0 {
			limits = append(limits, "Бюджет кадра различается между окнами; применён фиксированный порог детектора.")
		}
		if screen.FrameSource != "jankstats" {
			limits = append(limits, "Источник кадров — "+problemFrameSourceLabel(screen.FrameSource)+"; интервал обратных вызовов Choreographer не равен длительности кадра JankStats.")
			confidence = capProblemConfidence(confidence, "medium")
		}
		where := []ProblemLocation{{Screen: screen.Screen}}
		title := fmt.Sprintf("Экран %s заметно дёргается", displayUnknown(screen.Screen, "без атрибуции"))
		what := fmt.Sprintf("Подтормаживали %.1f%% кадров (%d из %d). Задержка верхних 5%% кадров — %d мс, отдельных худших кадров — до %d мс.", screen.JankRatePct, screen.JankyFrames, screen.Frames, screen.FrameP95MS, screen.FrameP99MS)
		why := "Подтормаживания измерены на указанном экране. Связанные работы из того же сценария показаны в разделе «Сценарии и причины»."
		evidence := []ProblemEvidence{{Name: "Доля медленных кадров", Observed: formatPercent(screen.JankRatePct), Unit: "%", ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.UIJankRate), Sample: u64ptr(screen.Frames), Numerator: u64ptr(screen.JankyFrames), Denominator: u64ptr(screen.Frames), Source: "ui_window"}, {Name: "Задержка верхних 5% кадров", Observed: fmt.Sprint(screen.FrameP95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", tailThresholdMS), Sample: u64ptr(screen.Frames), Source: "ui_frame_histogram"}, {Name: "Целевое время кадра", Observed: formatFrameDeadline(screen.FrameDeadlineUS), Unit: "ms", Source: "ui_window_deadline"}}
		var frequency *ProblemFrequency
		if screen.JankyFrames > 0 {
			frequency = &ProblemFrequency{Count: screen.JankyFrames}
		} else if badTail {
			title = fmt.Sprintf("Кадры экрана %s выходят за целевое время", displayUnknown(screen.Screen, "без атрибуции"))
			what = fmt.Sprintf("Системный признак jank не сработал, однако верхние 5%% из %d кадров занимали до %d мс, а отдельные худшие — до %d мс. Поэтому значение 0%% нельзя считать нормой.", screen.Frames, screen.FrameP95MS, screen.FrameP99MS)
			why = "Длинные кадры подтверждены распределением их длительности. Связанные работы из того же сценария показаны в разделе «Сценарии и причины»."
			evidence[0] = ProblemEvidence{Name: "Кадры с системным признаком jank", Observed: "не отмечены", Sample: u64ptr(screen.Frames), Source: "ui_window"}
		}
		b.add(ProblemFinding{
			DetectorID: "ui.jank_tail", DetectorVersion: b.cfg.Version, Category: ProblemCategoryUI, Subcategory: "jank",
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons,
			Title:        title,
			WhatHappened: what,
			Where:        where, Why: ProblemWhy{ClaimLevel: "unknown", Summary: why},
			Impact:    []string{"Видимые рывки и задержка реакции интерфейса"},
			Evidence:  evidence,
			Frequency: frequency, PriorityBreakdown: priority(28, magnitude, exposure, 2, boolScore(badRate && badTail, 4), "деградация UI", "доля медленных и худшие кадры", "размер выборки кадров", "один экран", "доля и задержка кадров согласованы"),
			Recommendations: []ProblemRecommendation{{Action: "Профилировать длинные кадры на этом экране и убрать тяжёлую работу с главного потока", Rationale: "Доля медленных кадров и верхние значения задержки показывают деградацию, которую может скрывать средний FPS.", Verification: "Повторить сценарий минимум на 120 кадрах и сравнить долю медленных кадров и распределение их длительности с бюджетом дисплея."}},
			Limitations:     limits, Drilldowns: []ProblemDrilldown{{Label: "Стабильность и UI", Anchor: "stability-ui", Filter: screen.Screen}},
		})
	}
}

func (b *problemBuilder) detectStallsAndIO() {
	b.detectTypedIO()
	for _, window := range b.summary.ProblemWindows {
		stall := window.Kind == "main_thread_stall" || window.Kind == "main_thread_dispatch"
		if !stall {
			continue
		}
		threshold := b.cfg.StallMS
		if window.MaxMS < threshold {
			continue
		}
		category, subcategory := ProblemCategoryStability, "main_thread_stall"
		title, impact := fmt.Sprintf("Главный поток останавливался до %d мс", window.MaxMS), 32
		magnitude := min(25, 8+int(window.MaxMS/threshold)*4)
		exposure := min(20, 5+int(math.Log2(float64(window.Count)+1))*3)
		confidence, reasons, limits := problemConfidence(b.summary, window.Count, 3, true)
		stack := BestMainThreadStallStack(b.summary.Owners, window.Owner)
		diagnosis := DiagnoseMainThreadStall(window.Owner, stack)
		if stack != "" {
			title = fmt.Sprintf("%s: до %d мс", diagnosis.Title, window.MaxMS)
		}
		where := []ProblemLocation{{Screen: window.Screen, Operation: window.Operation, Owner: window.Owner, Method: stack}}
		claim := "unknown"
		why := "Зафиксирована остановка главного потока; источник внутри интервала не связан строгим идентификатором."
		if stack != "" {
			claim = "correlated"
			why = "Остановка главного потока измерена напрямую; связанный с тем же владельцем снимок стека указывает на " + stack + ". Снимок локализует место выполнения, но не доказывает, что вся длительность паузы потрачена в конечном методе стека."
		}
		b.add(ProblemFinding{
			DetectorID: "stability.main_thread_stall", DetectorVersion: b.cfg.Version,
			Category: category, Subcategory: subcategory, Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: fmt.Sprintf("%d событий в %d окнах, максимум %d мс, суммарное окно %d мс.", window.Count, window.Windows, window.MaxMS, window.TotalWindowMS), Where: where,
			Why: ProblemWhy{ClaimLevel: claim, Summary: why}, Impact: []string{"Задержка ввода, пропуски кадров и риск ANR при повторении"},
			Evidence:  []ProblemEvidence{{Name: "Максимальная блокировка", Observed: fmt.Sprint(window.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", threshold), Sample: u64ptr(window.Count), Source: "problem_window"}, {Name: "Число событий", Observed: fmt.Sprint(window.Count), Unit: "events", Source: "problem_window"}},
			Frequency: &ProblemFrequency{Count: window.Count}, Cost: &ProblemCost{MainThreadBlockedMS: nonZeroU64Ptr(window.TotalWindowMS)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), 0, "блокировка UI", "длительность", "повторяемость", "контекст", "связанный источник отсутствует"),
			Recommendations:   []ProblemRecommendation{{Action: diagnosis.Action, Rationale: diagnosis.Explanation, Verification: "Повторить тот же сценарий и сравнить максимальное и суммарное время блокировки; временную связь с медленными кадрами подтвердить общей трассой."}},
			Limitations:       limits, Drilldowns: []ProblemDrilldown{{Label: "Таймлайн", Anchor: "timeline", Filter: window.Owner}},
		})
	}
}

func (b *problemBuilder) detectTypedIO() {
	if b.summary.IOAnalysis != nil {
		for _, operation := range b.summary.IOAnalysis.Calls {
			b.detectCriticalIO(operation)
		}
	}
}
