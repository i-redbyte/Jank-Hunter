package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const ProblemSchemaVersion = "jankhunter.problems/v1"

const (
	ProblemCategoryStability = "stability"
	ProblemCategoryUI        = "ui_main_thread"
	ProblemCategoryNetwork   = "network"
	ProblemCategoryMemory    = "memory_gc"
	ProblemCategoryIO        = "io_storage"
	ProblemCategoryCPU       = "cpu_tasks"
	ProblemCategoryPower     = "power_thermal"
	ProblemCategoryLogs      = "logs"
)

type ProblemFinding struct {
	ID                string                  `json:"id"`
	Fingerprint       string                  `json:"fingerprint"`
	DetectorID        string                  `json:"detector_id"`
	DetectorVersion   string                  `json:"detector_version"`
	Category          string                  `json:"category"`
	Subcategory       string                  `json:"subcategory"`
	Severity          string                  `json:"severity"`
	Status            string                  `json:"status"`
	RiskScore         int                     `json:"risk_score"`
	Confidence        string                  `json:"confidence"`
	ConfidenceReasons []string                `json:"confidence_reasons"`
	Title             string                  `json:"title"`
	WhatHappened      string                  `json:"what_happened"`
	Where             []ProblemLocation       `json:"where"`
	Why               ProblemWhy              `json:"why"`
	Impact            []string                `json:"impact"`
	Evidence          []ProblemEvidence       `json:"evidence"`
	Frequency         *ProblemFrequency       `json:"frequency,omitempty"`
	Cost              *ProblemCost            `json:"cost,omitempty"`
	RankBreakdown     []ProblemRiskComponent  `json:"rank_breakdown"`
	Recommendations   []ProblemRecommendation `json:"recommendations"`
	Limitations       []string                `json:"limitations,omitempty"`
	Drilldowns        []ProblemDrilldown      `json:"drilldowns,omitempty"`
	RelatedFindings   []string                `json:"related_findings,omitempty"`
	RelatedCategories []string                `json:"related_categories,omitempty"`
}

type ProblemLocation struct {
	Process string `json:"process,omitempty"`
	Screen  string `json:"screen,omitempty"`
	Flow    string `json:"flow,omitempty"`
	Step    string `json:"step,omitempty"`
	Route   string `json:"route,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Class   string `json:"class,omitempty"`
	Method  string `json:"method,omitempty"`
}

type ProblemWhy struct {
	ClaimLevel string   `json:"claim_level"`
	Summary    string   `json:"summary"`
	Factors    []string `json:"factors,omitempty"`
}

type ProblemEvidence struct {
	Name                string  `json:"name"`
	Observed            string  `json:"observed"`
	Unit                string  `json:"unit,omitempty"`
	ExpectedOrThreshold string  `json:"expected_or_threshold,omitempty"`
	Sample              *uint64 `json:"sample,omitempty"`
	Numerator           *uint64 `json:"numerator,omitempty"`
	Denominator         *uint64 `json:"denominator,omitempty"`
	Source              string  `json:"source"`
}

type ProblemFrequency struct {
	Count       uint64   `json:"count"`
	RatePerSec  *float64 `json:"rate_per_sec,omitempty"`
	RunCoverage *float64 `json:"run_coverage,omitempty"`
}

type ProblemCost struct {
	WallTimeMS          *uint64 `json:"wall_time_ms,omitempty"`
	MainThreadBlockedMS *uint64 `json:"main_thread_blocked_ms,omitempty"`
	Bytes               *uint64 `json:"bytes,omitempty"`
	MemoryKB            *uint64 `json:"memory_kb,omitempty"`
}

type ProblemRiskComponent struct {
	Component   string `json:"component"`
	Score       int    `json:"score"`
	Maximum     int    `json:"maximum"`
	Explanation string `json:"explanation"`
}

type ProblemRecommendation struct {
	Action       string `json:"action"`
	Rationale    string `json:"rationale"`
	Verification string `json:"verification"`
}

type ProblemDrilldown struct {
	Label  string `json:"label"`
	Anchor string `json:"anchor"`
	Filter string `json:"filter,omitempty"`
}

type CategoryCoverage struct {
	Category          string   `json:"category"`
	Label             string   `json:"label"`
	Status            string   `json:"status"`
	WorstSeverity     string   `json:"worst_severity,omitempty"`
	FindingCount      int      `json:"finding_count"`
	RequiredEvidence  []string `json:"required_evidence"`
	AvailableEvidence []string `json:"available_evidence,omitempty"`
	MissingEvidence   []string `json:"missing_evidence,omitempty"`
	Explanation       string   `json:"explanation"`
	NextAction        string   `json:"next_action,omitempty"`
}

type ProblemSummary struct {
	SchemaVersion string         `json:"schema_version"`
	Verdict       string         `json:"verdict"`
	Headline      string         `json:"headline"`
	Total         int            `json:"total"`
	SignalTotal   int            `json:"signal_total"`
	Critical      int            `json:"critical"`
	High          int            `json:"high"`
	Medium        int            `json:"medium"`
	Low           int            `json:"low"`
	Info          int            `json:"info"`
	ByCategory    []ProblemCount `json:"by_category"`
	Unchecked     int            `json:"unchecked_categories"`
}

type ProblemCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type DetectorMetadata struct {
	ID              string              `json:"id"`
	Version         string              `json:"version"`
	Category        string              `json:"category"`
	Title           string              `json:"title"`
	MinimumSample   uint64              `json:"minimum_sample"`
	RequiredSignals []string            `json:"required_signals"`
	Thresholds      []DetectorThreshold `json:"thresholds"`
}

type DetectorThreshold struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type ProblemDetectorConfig struct {
	Version             string
	HTTPSlowMS          uint64
	HTTPHighMS          uint64
	HTTPMinSample       uint64
	HTTPFailureRate     float64
	HTTPStormRate       float64
	HTTPStormMinCount   uint64
	UIJankRate          float64
	UIHighJankRate      float64
	UIMinFrames         uint64
	UIFrameTailMS       uint64
	StallMS             uint64
	StallHighMS         uint64
	IOMainThreadMS      uint64
	IOBackgroundMS      uint64
	IOStormMinCount     uint64
	IOStormRate         float64
	LogSpamMinCount     uint64
	LogSpamRate         float64
	ProcessCPUPercent   float64
	ThermalSevereStatus uint64
}

func DefaultProblemDetectorConfig() ProblemDetectorConfig {
	return ProblemDetectorConfig{
		Version: "2.0.0", HTTPSlowMS: 700, HTTPHighMS: 1500, HTTPMinSample: 20,
		HTTPFailureRate: 0.05, HTTPStormRate: 1, HTTPStormMinCount: 20,
		UIJankRate: 3, UIHighJankRate: 10, UIMinFrames: 120, UIFrameTailMS: 32,
		StallMS: 250, StallHighMS: 1000, IOMainThreadMS: 16, IOBackgroundMS: 500,
		IOStormMinCount: 50, IOStormRate: 5, LogSpamMinCount: 100, LogSpamRate: 5,
		ProcessCPUPercent: 80, ThermalSevereStatus: 3,
	}
}

type ProblemReport struct {
	Summary   ProblemSummary
	Problems  []ProblemFinding
	Incidents []ProblemFinding
	Coverage  []CategoryCoverage
	Registry  []DetectorMetadata
}

func BuildProblemReport(summary Summary) (ProblemReport, error) {
	return BuildProblemReportWithConfig(summary, DefaultProblemDetectorConfig())
}

func BuildProblemReportWithConfig(summary Summary, cfg ProblemDetectorConfig) (ProblemReport, error) {
	if err := validateProblemDetectorConfig(cfg); err != nil {
		return ProblemReport{}, err
	}
	b := problemBuilder{summary: summary, cfg: cfg}
	b.detectProcessExit()
	b.detectStallsAndIO()
	b.detectUI()
	b.detectSemanticWork()
	b.detectNetwork()
	b.detectMemory()
	b.detectCPU()
	b.detectPower()
	b.detectLogSpam()
	b.finishFindings()
	coverage := b.coverage()
	incidents := buildProblemIncidents(b.findings)
	problemSummary := summarizeProblems(incidents, coverage)
	problemSummary.SignalTotal = len(b.findings)
	problemSummary.Headline = problemSummaryHeadline(problemSummary)
	report := ProblemReport{
		Summary:   problemSummary,
		Problems:  b.findings,
		Incidents: incidents,
		Coverage:  coverage,
		Registry:  detectorRegistry(cfg),
	}
	if err := validateProblemReport(report); err != nil {
		return ProblemReport{}, err
	}
	return report, nil
}

func CompareProblems(baseline, candidate Summary, cohortsComparable bool) ProblemComparison {
	baselineProblems := problemIncidentsOrFindings(baseline)
	candidateProblems := problemIncidentsOrFindings(candidate)
	before := make(map[string]ProblemFinding, len(baselineProblems))
	after := make(map[string]ProblemFinding, len(candidateProblems))
	for _, finding := range baselineProblems {
		before[finding.Fingerprint] = finding
	}
	for _, finding := range candidateProblems {
		after[finding.Fingerprint] = finding
	}
	keys := make([]string, 0, len(before)+len(after))
	seen := map[string]struct{}{}
	for key := range before {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range after {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	deltas := make([]ProblemDelta, 0, len(keys))
	current := make([]ProblemFinding, 0, len(after))
	for _, key := range keys {
		baselineFinding, hasBaseline := before[key]
		candidateFinding, hasCandidate := after[key]
		delta := ProblemDelta{Fingerprint: key, Comparable: cohortsComparable}
		switch {
		case !hasBaseline:
			delta.Status = "new"
			candidateFinding.Status = "new"
		case !hasCandidate:
			delta.Status = "resolved"
			baselineFinding.Status = "resolved"
		case candidateFinding.RiskScore >= baselineFinding.RiskScore+10 || problemSeverityRank(candidateFinding.Severity) > problemSeverityRank(baselineFinding.Severity):
			delta.Status = "regressed"
			candidateFinding.Status = "regressed"
		case candidateFinding.RiskScore <= baselineFinding.RiskScore-10 || problemSeverityRank(candidateFinding.Severity) < problemSeverityRank(baselineFinding.Severity):
			delta.Status = "improved"
			candidateFinding.Status = "improved"
		default:
			delta.Status = "persistent"
			candidateFinding.Status = "persistent"
		}
		if !cohortsComparable {
			delta.Note = "Состав прогонов различается; статус показан как наблюдаемое изменение, но не является доказанной регрессией."
		}
		if hasBaseline {
			copy := baselineFinding
			delta.Baseline = &copy
		}
		if hasCandidate {
			copy := candidateFinding
			delta.Candidate = &copy
			current = append(current, candidateFinding)
		}
		deltas = append(deltas, delta)
	}
	sort.Slice(deltas, func(i, j int) bool {
		left, right := problemDeltaRank(deltas[i].Status), problemDeltaRank(deltas[j].Status)
		if left != right {
			return left > right
		}
		leftRisk, rightRisk := deltaRisk(deltas[i]), deltaRisk(deltas[j])
		if leftRisk != rightRisk {
			return leftRisk > rightRisk
		}
		return deltas[i].Fingerprint < deltas[j].Fingerprint
	})
	sort.Slice(current, func(i, j int) bool {
		if problemDeltaRank(current[i].Status) != problemDeltaRank(current[j].Status) {
			return problemDeltaRank(current[i].Status) > problemDeltaRank(current[j].Status)
		}
		if current[i].RiskScore != current[j].RiskScore {
			return current[i].RiskScore > current[j].RiskScore
		}
		return current[i].Fingerprint < current[j].Fingerprint
	})
	summary := summarizeProblems(current, candidate.CategoryCoverage)
	return ProblemComparison{SchemaVersion: ProblemSchemaVersion, Summary: summary, Deltas: deltas}
}

func problemDeltaRank(status string) int {
	return map[string]int{"resolved": 0, "improved": 1, "persistent": 2, "new": 3, "regressed": 4}[status]
}
func deltaRisk(delta ProblemDelta) int {
	if delta.Candidate != nil {
		return delta.Candidate.RiskScore
	}
	if delta.Baseline != nil {
		return delta.Baseline.RiskScore
	}
	return 0
}

func validateProblemDetectorConfig(cfg ProblemDetectorConfig) error {
	if cfg.Version == "" || cfg.HTTPSlowMS == 0 || cfg.HTTPHighMS < cfg.HTTPSlowMS ||
		cfg.HTTPMinSample == 0 || cfg.HTTPFailureRate <= 0 || cfg.HTTPFailureRate > 1 ||
		cfg.HTTPStormRate <= 0 || cfg.HTTPStormMinCount == 0 || cfg.UIJankRate <= 0 ||
		cfg.UIHighJankRate < cfg.UIJankRate || cfg.UIMinFrames == 0 || cfg.UIFrameTailMS == 0 ||
		cfg.StallMS == 0 || cfg.StallHighMS < cfg.StallMS || cfg.IOMainThreadMS == 0 ||
		cfg.IOBackgroundMS < cfg.IOMainThreadMS || cfg.IOStormMinCount == 0 || cfg.IOStormRate <= 0 || cfg.LogSpamMinCount == 0 ||
		cfg.LogSpamRate <= 0 || cfg.ProcessCPUPercent <= 0 || cfg.ProcessCPUPercent > 100 {
		return fmt.Errorf("invalid problem detector config %q", cfg.Version)
	}
	return nil
}

type problemBuilder struct {
	summary  Summary
	cfg      ProblemDetectorConfig
	findings []ProblemFinding
}

func (b *problemBuilder) detectNetwork() {
	for _, route := range b.summary.Routes {
		count := uint64(max(route.Count, 0))
		failures := uint64(max(route.Failures, 0))
		rate := ratePerSecond(count, b.summary.DurationMS)
		failureRate := problemRatio(failures, count)
		slow := route.P95MS >= b.cfg.HTTPSlowMS
		failed := count > 0 && failureRate >= b.cfg.HTTPFailureRate
		storm := count >= b.cfg.HTTPStormMinCount && float64(route.PeakRequestsPerSecond) >= b.cfg.HTTPStormRate
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
		symptoms := boolCount(slow, failed, storm)
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
		where := []ProblemLocation{{Route: route.Route, Owner: route.OwnerSample}}
		for _, flow := range b.summary.Flows {
			if flow.RouteSample == route.Route {
				where = appendUniqueLocation(where, ProblemLocation{Screen: flow.Screen, Flow: flow.Flow, Step: flow.Step, Route: route.Route, Owner: flow.Owner})
			}
		}
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
			evidence = append(evidence, ProblemEvidence{Name: "p95 длительности", Observed: fmt.Sprint(route.P95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.HTTPSlowMS), Sample: u64ptr(count), Source: "typed_http"})
		}
		if failed {
			evidence = append(evidence, ProblemEvidence{Name: "Доля ошибок", Observed: formatPercent(failureRate * 100), Unit: "%", ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.HTTPFailureRate*100), Numerator: u64ptr(failures), Denominator: u64ptr(count), Source: "typed_http"})
		}
		if storm {
			evidence = append(evidence, ProblemEvidence{Name: "Пиковая частота за 1 секунду", Observed: fmt.Sprint(route.PeakRequestsPerSecond), Unit: "requests/s", ExpectedOrThreshold: fmt.Sprintf("< %.2f requests/s", b.cfg.HTTPStormRate), Sample: u64ptr(count), Source: "typed_http_completion_window"})
		}
		wall := uint64(route.Count) * route.P50MS
		bytes := route.BytesRx + route.BytesTx
		confidence, reasons, limits := problemConfidence(b.summary, count, b.cfg.HTTPMinSample, true)
		if storm {
			limits = append(limits, "Пик рассчитан по секундам завершения запросов; без идентификатора жизненного цикла нельзя точно восстановить одновременность и цепочку повторов.")
			if route.BurstEstimateStatus == "bounded_approximation" {
				limits = append(limits, "Пиковая частота оценена приближённо из-за длительного или неупорядоченного потока событий.")
			}
		}
		b.add(ProblemFinding{
			DetectorID: "network.route_health", DetectorVersion: b.cfg.Version, Category: ProblemCategoryNetwork, Subcategory: networkSubcategory(slow, failed, storm),
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: networkWhat(route, failures, count, slow, storm, b.cfg.HTTPMinSample), Where: where,
			Why:    ProblemWhy{ClaimLevel: "unknown", Summary: "Проблема маршрута измерена, но для поиска причины нужно проверить указанные экран, сценарий и вызывающий код.", Factors: networkFactors(slow, failed, storm)},
			Impact: []string{"Задержка или ошибка пользовательского сценария", "Лишняя сетевая и серверная нагрузка при частых вызовах"}, Evidence: evidence,
			Frequency: &ProblemFrequency{Count: count, RatePerSec: rate}, Cost: &ProblemCost{WallTimeMS: nonZeroU64Ptr(wall), Bytes: nonZeroU64Ptr(bytes)},
			RankBreakdown:   risk(impact, magnitude, exposure, locationBreadth(where), compound, "задержка или ошибка для пользователя", "отклонение времени ответа", "частота в прогоне", "число контекстов", "сочетание сетевых симптомов"),
			Recommendations: []ProblemRecommendation{{Action: "Проверить место вызова, убрать лишние повторы и сократить время ответа маршрута", Rationale: "Исправление уменьшит задержку пользователя, а при повторных запросах — ещё и сетевую нагрузку.", Verification: "Повторить тот же сценарий и сравнить число запросов, ошибки, задержку верхних 5% запросов и суммарное время ожидания."}},
			Limitations:     limits, Drilldowns: []ProblemDrilldown{{Label: "Сеть", Anchor: "network", Filter: route.Route}},
		})
	}
}

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
			limits = append(limits, "Источник кадров — "+screen.FrameSource+"; интервал обратных вызовов Choreographer не равен длительности кадра JankStats.")
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
			Frequency: frequency, RankBreakdown: risk(28, magnitude, exposure, 2, boolScore(badRate && badTail, 4), "деградация UI", "доля медленных и худшие кадры", "размер выборки кадров", "один экран", "доля и задержка кадров согласованы"),
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
		where := []ProblemLocation{{Screen: window.Screen, Flow: window.Flow, Step: window.Step, Owner: window.Owner}}
		claim := "unknown"
		why := "Зафиксирована остановка главного потока; источник внутри интервала не связан строгим идентификатором."
		b.add(ProblemFinding{
			DetectorID: "stability.main_thread_stall", DetectorVersion: b.cfg.Version,
			Category: category, Subcategory: subcategory, Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: fmt.Sprintf("%d событий в %d окнах, максимум %d мс, суммарное окно %d мс.", window.Count, window.Windows, window.MaxMS, window.TotalWindowMS), Where: where,
			Why: ProblemWhy{ClaimLevel: claim, Summary: why}, Impact: []string{"Задержка ввода, пропуски кадров и риск ANR при повторении"},
			Evidence:  []ProblemEvidence{{Name: "Максимальная блокировка", Observed: fmt.Sprint(window.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", threshold), Sample: u64ptr(window.Count), Source: "problem_window"}, {Name: "Число событий", Observed: fmt.Sprint(window.Count), Unit: "events", Source: "problem_window"}},
			Frequency: &ProblemFrequency{Count: window.Count}, Cost: &ProblemCost{MainThreadBlockedMS: nonZeroU64Ptr(window.TotalWindowMS)},
			RankBreakdown:   risk(impact, magnitude, exposure, locationBreadth(where), 0, "блокировка UI", "длительность", "повторяемость", "контекст", "связанный источник отсутствует"),
			Recommendations: []ProblemRecommendation{{Action: "Разбить или перенести долгую работу с главного потока", Rationale: "Сокращение времени блокировки напрямую уменьшает риск зависания и ANR.", Verification: "Повторить тот же сценарий и сравнить максимальное и суммарное время блокировки с подтормаживаниями в том же контексте."}},
			Limitations:     limits, Drilldowns: []ProblemDrilldown{{Label: "Таймлайн", Anchor: "timeline", Filter: window.Owner}},
		})
	}
}

func (b *problemBuilder) detectTypedIO() {
	for _, operation := range b.summary.IOOperations {
		maxMS := microsecondsToMillisecondsCeil(operation.MaxDurationUS)
		totalMS := microsecondsToMillisecondsCeil(operation.TotalDurationUS)
		rate := ratePerSecond(operation.Count, b.summary.DurationMS)
		storm := operation.Count >= b.cfg.IOStormMinCount && rate != nil && *rate >= b.cfg.IOStormRate
		slowMain := operation.MainThread && maxMS >= b.cfg.IOMainThreadMS
		slowBackground := !operation.MainThread && maxMS >= b.cfg.IOBackgroundMS
		if !slowMain && !slowBackground && !storm {
			continue
		}
		where := []ProblemLocation{{Screen: operation.Screen, Flow: operation.Flow, Step: operation.Step, Owner: operation.Owner}}
		title := fmt.Sprintf("%s выполняется слишком долго", ioOperationLabel(operation.Operation))
		subcategory := "slow_background_io"
		impact := 16
		if slowMain {
			title = fmt.Sprintf("%s блокирует главный поток", ioOperationLabel(operation.Operation))
			subcategory, impact = "main_thread_io", 32
		} else if storm {
			title = fmt.Sprintf("%s создаёт поток мелких I/O операций", ioOperationLabel(operation.Operation))
			subcategory, impact = "io_storm", 20
		}
		subcategory += "_" + operation.Operation
		confidence, reasons, limits := problemConfidence(b.summary, operation.Count, 3, true)
		threshold := b.cfg.IOBackgroundMS
		if operation.MainThread {
			threshold = b.cfg.IOMainThreadMS
		}
		evidence := []ProblemEvidence{
			{Name: "Максимальная длительность", Observed: fmt.Sprint(maxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", threshold), Sample: u64ptr(operation.Count), Source: "typed_io"},
			{Name: "Суммарная длительность", Observed: fmt.Sprint(totalMS), Unit: "ms", Source: "typed_io"},
			{Name: "Объём", Observed: fmt.Sprint(operation.Bytes), Unit: "bytes", Source: "typed_io"},
		}
		if storm {
			evidence = append(evidence, ProblemEvidence{Name: "Средняя частота", Observed: fmt.Sprintf("%.2f", *rate), Unit: "events/s", ExpectedOrThreshold: fmt.Sprintf("< %.2f events/s", b.cfg.IOStormRate), Source: "typed_io"})
		}
		b.add(ProblemFinding{
			DetectorID:      map[bool]string{true: "io.main_thread", false: "io.operation_pressure"}[operation.MainThread],
			DetectorVersion: b.cfg.Version, Category: ProblemCategoryIO, Subcategory: subcategory,
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: fmt.Sprintf("%s: %d операций, максимум %d мс, суммарно %d мс, %d байт.", ioOperationLabel(operation.Operation), operation.Count, maxMS, totalMS, operation.Bytes),
			Where:        where, Why: ProblemWhy{ClaimLevel: "linked", Summary: "Типизированное событие атомарно связывает операцию, поток выполнения и контекст приложения."},
			Impact:   []string{map[bool]string{true: "Блокировка ввода, пропуски кадров и рост риска ANR", false: "Рост задержки, нагрузки на хранилище и конкуренция за I/O"}[operation.MainThread]},
			Evidence: evidence, Frequency: &ProblemFrequency{Count: operation.Count, RatePerSec: rate},
			Cost:            &ProblemCost{MainThreadBlockedMS: map[bool]*uint64{true: nonZeroU64Ptr(totalMS), false: nil}[operation.MainThread], Bytes: nonZeroU64Ptr(operation.Bytes)},
			RankBreakdown:   risk(impact, min(25, 8+int(maxMS/maxUint64(threshold, 1))*4), min(20, 5+int(math.Log2(float64(operation.Count)+1))*3), locationBreadth(where), boolScore(storm && slowMain, 5), "задержка I/O", "длительность", "число и частота", "контекст", "поток и операция"),
			Recommendations: []ProblemRecommendation{{Action: map[bool]string{true: "Перенести операцию с главного потока и объединить мелкие обращения", false: "Сократить критический путь, объединить мелкие обращения и уменьшить объём операции"}[operation.MainThread], Rationale: "Тип операции и контекст уже локализованы; снижение числа и длительности обращений уменьшает измеренную I/O-нагрузку.", Verification: "Повторить тот же сценарий и сравнить число операций, максимальную и суммарную длительность и подтормаживания главного потока."}},
			Limitations:     limits, Drilldowns: []ProblemDrilldown{{Label: "Таймлайн", Anchor: "timeline", Filter: operation.Owner}},
		})
	}
}

func (b *problemBuilder) detectMemory() {
	for _, leak := range b.summary.MemoryLeaks {
		if leak.Count == 0 {
			continue
		}
		impact := 18
		if leak.HeapEvidence {
			impact = 32
		}
		if leak.EstimatedRetainedKB >= 16*1024 {
			impact = min(40, impact+6)
		}
		magnitude := min(25, 6+int(math.Log2(float64(leak.EstimatedRetainedKB/1024)+1))*4)
		exposure := min(20, 5+int(leak.Count)*3)
		confidence, reasons, limits := problemConfidence(b.summary, leak.Count, 2, true)
		claim := "hypothesis"
		why := "Объект удерживался дольше ожидаемого; без heap path это сигнал, а не доказанная утечка."
		if leak.HeapEvidence {
			confidence, claim, why = "high", "linked", "HPROF подтвердил путь удержания до GC root."
			reasons = append(reasons, "Есть HPROF path и retained-size evidence.")
		}
		where := []ProblemLocation{{Screen: leak.Screen, Flow: leak.Flow, Step: leak.Step, Owner: leak.Holder, Class: leak.ClassName}}
		b.add(ProblemFinding{
			DetectorID: "memory.retention", DetectorVersion: b.cfg.Version, Category: ProblemCategoryMemory, Subcategory: map[bool]string{true: "confirmed_leak", false: "retained_object"}[leak.HeapEvidence],
			Status: "observed", Confidence: confidence, ConfidenceReasons: uniqueStrings(reasons), Title: memoryTitle(leak),
			WhatHappened: fmt.Sprintf("%s удерживался до %d мс; оценка retained size — %d КБ; наблюдений — %d.", leak.ClassName, leak.MaxAgeMS, leak.EstimatedRetainedKB, leak.Count), Where: where,
			Why:       ProblemWhy{ClaimLevel: claim, Summary: why, Factors: nonEmptyStrings(leak.GCRoot, leak.HolderField, leak.LeakPattern)},
			Impact:    []string{"Рост памяти, давление GC и риск OOM при накоплении"},
			Evidence:  []ProblemEvidence{{Name: "Возраст удержания", Observed: fmt.Sprint(leak.MaxAgeMS), Unit: "ms", Sample: u64ptr(leak.Count), Source: "retention"}, {Name: "Retained size", Observed: fmt.Sprint(leak.EstimatedRetainedKB), Unit: "KB", Source: map[bool]string{true: "hprof", false: "runtime_estimate"}[leak.HeapEvidence]}},
			Frequency: &ProblemFrequency{Count: leak.Count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(leak.EstimatedRetainedKB)},
			RankBreakdown:   risk(impact, magnitude, exposure, locationBreadth(where), boolScore(leak.HeapEvidence && leak.EstimatedRetainedKB > 0, 5), "memory/OOM risk", "retained size", "повторяемость", "контекст", "heap path + size"),
			Recommendations: []ProblemRecommendation{{Action: firstNonEmpty(leak.Recommendation, "Разорвать путь удержания и ограничить lifetime владельца"), Rationale: "Устранение GC-root path освобождает весь dominator subtree.", Verification: "Повторить сценарий, вызвать GC и подтвердить отсутствие объекта/пути в новом HPROF."}},
			Limitations:     append(limits, leak.QualityWarnings...), Drilldowns: []ProblemDrilldown{{Label: "Память", Anchor: "memory-resources", Filter: leak.ClassName}},
		})
	}
	if b.summary.LowMemoryCount > 0 {
		count := uint64(b.summary.LowMemoryCount)
		confidence, reasons, limits := problemConfidence(b.summary, count, 3, true)
		b.add(ProblemFinding{DetectorID: "memory.pressure", DetectorVersion: b.cfg.Version, Category: ProblemCategoryMemory, Subcategory: "low_memory", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Приложение работало при дефиците памяти", WhatHappened: fmt.Sprintf("Low-memory состояние отмечено в %d samples; max PSS — %d КБ, минимум доступной памяти — %d КБ.", count, b.summary.MemoryMaxKB, b.summary.AvailMemoryMinKB), Why: ProblemWhy{ClaimLevel: "correlated", Summary: "Memory pressure и PSS наблюдались в одном прогоне; конкретный allocator не связан."}, Impact: []string{"Более частые GC, выгрузка компонентов и риск OOM"}, Evidence: []ProblemEvidence{{Name: "Low-memory samples", Observed: fmt.Sprint(count), Unit: "samples", Source: "memory_context"}, {Name: "Max PSS", Observed: fmt.Sprint(b.summary.MemoryMaxKB), Unit: "KB", Source: "memory_sample"}}, Frequency: &ProblemFrequency{Count: count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(b.summary.MemoryMaxKB)}, RankBreakdown: risk(26, 14, min(20, int(count)*4), 2, 3, "memory pressure", "PSS/headroom proxy", "samples", "run-level", "pressure + PSS"), Recommendations: []ProblemRecommendation{{Action: "Снять heap trend/allocations и сократить крупные кеши и буферы", Rationale: "Абсолютный PSS без тренда недостаточен для локализации.", Verification: "Сравнить PSS/heap slope, GC pauses и headroom в длинном повторе."}}, Limitations: append(limits, "Max PSS не доказывает рост; нужен временной trend.")})
	}
}

func (b *problemBuilder) detectProcessExit() {
	for _, exit := range b.summary.ProcessExits {
		name, severe := processExitReason(exit.Reason)
		if !severe {
			continue
		}
		when := time.UnixMilli(int64(exit.LatestTimestampUnixMS)).UTC().Format(time.RFC3339)
		confidence := "medium"
		reasons := []string{"ApplicationExitInfo содержит типизированные reason, timestamp, process и memory evidence.", "Событие историческое: оно относится к предыдущему process instance."}
		where := []ProblemLocation{{Process: exit.Process}}
		b.add(ProblemFinding{
			DetectorID: "stability.historical_process_exit", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryStability, Subcategory: fmt.Sprintf("historical_exit_%d", exit.Reason), Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        "Android подтвердил предыдущее завершение: " + name,
			WhatHappened: fmt.Sprintf("%s завершился с причиной %s; последнее событие — %s, наблюдений — %d.", displayUnknown(exit.Process, "Процесс"), name, when, exit.Count),
			Where:        where,
			Why:          ProblemWhy{ClaimLevel: "linked", Summary: "Android связал системную причину с конкретным историческим процессом; связь с текущим пользовательским действием не доказана."},
			Impact:       []string{"Потеря пользовательского сценария; для ANR/crash/OOM возможна потеря несохранённых данных"},
			Evidence: []ProblemEvidence{
				{Name: "Exit reason", Observed: name, Sample: u64ptr(exit.Count), Source: "typed_application_exit_info"},
				{Name: "Последний timestamp", Observed: when, Source: "typed_application_exit_info"},
				{Name: "PSS при завершении", Observed: fmt.Sprint(exit.MaxPSSKB), Unit: "KB", Source: "typed_application_exit_info"},
				{Name: "RSS при завершении", Observed: fmt.Sprint(exit.MaxRSSKB), Unit: "KB", Source: "typed_application_exit_info"},
			},
			Frequency: &ProblemFrequency{Count: exit.Count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(exit.MaxPSSKB)},
			RankBreakdown:   risk(40, 20, min(20, 6+int(exit.Count)*3), locationBreadth(where), 3, "crash/ANR/OOM", "системная причина", "исторические события", "process", "reason + timestamp + memory"),
			Recommendations: []ProblemRecommendation{{Action: "Сопоставить timestamp с crash/ANR trace и воспроизвести соответствующий flow", Rationale: "Типизированный exit подтверждает класс сбоя, но не называет текущую строку кода.", Verification: "Проверить отсутствие новой записи той же причины после исправления и повторного сценария."}},
			Limitations:     []string{"ApplicationExitInfo описывает предыдущий процесс и не доказывает, что сбой произошёл в анализируемом run."},
		})
	}
}

func (b *problemBuilder) detectCPU() {
	value, ok := namedValue(b.summary.Gauges, "process.cpu.core_percent_x100")
	if !ok {
		return
	}
	percent := float64(value) / 100
	if percent < b.cfg.ProcessCPUPercent {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, 1, 4, true)
	b.add(ProblemFinding{DetectorID: "cpu.process_saturation", DetectorVersion: b.cfg.Version, Category: ProblemCategoryCPU, Subcategory: "process_cpu", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Процесс длительно нагружает CPU", WhatHappened: fmt.Sprintf("Агрегированная загрузка процесса — %.1f%% одного ядра.", percent), Why: ProblemWhy{ClaimLevel: "unknown", Summary: "Process CPU не связывает нагрузку с конкретным методом или task."}, Impact: []string{"Конкуренция за CPU, задержки UI, нагрев и расход батареи"}, Evidence: []ProblemEvidence{{Name: "Process CPU", Observed: fmt.Sprintf("%.1f", percent), Unit: "% core", ExpectedOrThreshold: fmt.Sprintf("< %.0f%%", b.cfg.ProcessCPUPercent), Source: "system_sampler_gauge"}}, RankBreakdown: risk(22, min(25, int(percent/10)+8), 10, 2, 0, "resource contention", "CPU level", "aggregate samples", "run-level", "нет linked task"), Recommendations: []ProblemRecommendation{{Action: "Связать CPU spike с runtime task/method trace", Rationale: "Process-level сигнал не называет виновный код.", Verification: "Повторить с task/runtime evidence и проверить CPU вместе с UI tail."}}, Limitations: append(limits, "Gauge агрегирован; длительность и per-thread linkage недоступны.")})
}

func (b *problemBuilder) detectPower() {
	status, ok := namedValue(b.summary.Gauges, "device.thermal.status")
	if !ok || status < b.cfg.ThermalSevereStatus {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, 1, 3, true)
	b.add(ProblemFinding{DetectorID: "power.thermal_pressure", DetectorVersion: b.cfg.Version, Category: ProblemCategoryPower, Subcategory: "thermal", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Устройство работало при сильном thermal pressure", WhatHappened: fmt.Sprintf("Android thermal status достиг %d (порог severe: %d).", status, b.cfg.ThermalSevereStatus), Why: ProblemWhy{ClaimLevel: "unknown", Summary: "Thermal status — confounder; он не доказывает, что приложение вызвало нагрев."}, Impact: []string{"Thermal throttling может усиливать CPU и UI деградацию"}, Evidence: []ProblemEvidence{{Name: "Thermal status", Observed: fmt.Sprint(status), Unit: "Android status", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.ThermalSevereStatus), Source: "system_sampler_gauge"}}, RankBreakdown: risk(18, min(25, int(status)*4), 8, 2, 0, "throttling", "thermal status", "run snapshot", "device-level", "причина не связана"), Recommendations: []ProblemRecommendation{{Action: "Повторить сценарий на холодном устройстве и сопоставить CPU/UI", Rationale: "Так отделяется дефект приложения от внешнего thermal confounder.", Verification: "Сравнить одинаковый flow при normal и severe thermal status."}}, Limitations: append(limits, "Короткий thermal snapshot не доказывает длительность или источник нагрева.")})
}

func (b *problemBuilder) detectLogSpam() {
	for _, row := range b.summary.LogSpam {
		rate := ratePerSecond(row.Count, b.summary.DurationMS)
		if row.Count < b.cfg.LogSpamMinCount && (rate == nil || *rate < b.cfg.LogSpamRate) {
			continue
		}
		confidence, reasons, limits := problemConfidence(b.summary, row.Count, b.cfg.LogSpamMinCount, true)
		where := []ProblemLocation{{Screen: row.Screen, Flow: row.Flow, Step: row.Step, Owner: row.Owner}}
		b.add(ProblemFinding{DetectorID: "logs.spam", DetectorVersion: b.cfg.Version, Category: ProblemCategoryLogs, Subcategory: "log_spam", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: fmt.Sprintf("%s создаёт поток повторяющихся логов", displayUnknown(row.Owner, row.Source)), WhatHappened: fmt.Sprintf("%s.%s записан %d раз%s.", row.Source, row.Level, row.Count, formatOptionalRate(rate)), Where: where, Why: ProblemWhy{ClaimLevel: "linked", Summary: "Автоматический hook напрямую связал вызовы логирования с этим owner/context."}, Impact: []string{"Лишние allocations, форматирование и I/O; полезные сообщения теряются в шуме"}, Evidence: []ProblemEvidence{{Name: "Число сообщений", Observed: fmt.Sprint(row.Count), Unit: "logs", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.LogSpamMinCount), Sample: u64ptr(row.Count), Source: "log_hook"}}, Frequency: &ProblemFrequency{Count: row.Count, RatePerSec: rate}, RankBreakdown: risk(12, min(25, 6+int(math.Log2(float64(row.Count)))), min(20, 6+int(math.Log2(float64(row.Count)))), locationBreadth(where), 0, "diagnostic overhead", "count", "rate/count", "context", "single symptom"), Recommendations: []ProblemRecommendation{{Action: "Удалить hot-path log или добавить rate limit/aggregation", Rationale: "Сокращает overhead и повышает диагностическую ценность логов.", Verification: "Повторить flow и проверить count/rate этого owner."}}, Limitations: limits})
	}
}

func (b *problemBuilder) add(f ProblemFinding) {
	f.RiskScore = riskScore(f.RankBreakdown)
	f.Severity = severityForRisk(f.RiskScore)
	f.Fingerprint = findingFingerprint(f)
	f.ID = "problem-" + f.Fingerprint[:16]
	if f.Status == "" {
		f.Status = "observed"
	}
	b.findings = append(b.findings, f)
}

func (b *problemBuilder) finishFindings() {
	b.findings = uniqueProblemFindings(b.findings)
	sort.Slice(b.findings, func(i, j int) bool {
		if problemSeverityRank(b.findings[i].Severity) != problemSeverityRank(b.findings[j].Severity) {
			return problemSeverityRank(b.findings[i].Severity) > problemSeverityRank(b.findings[j].Severity)
		}
		if b.findings[i].RiskScore != b.findings[j].RiskScore {
			return b.findings[i].RiskScore > b.findings[j].RiskScore
		}
		if problemConfidenceRank(b.findings[i].Confidence) != problemConfidenceRank(b.findings[j].Confidence) {
			return problemConfidenceRank(b.findings[i].Confidence) > problemConfidenceRank(b.findings[j].Confidence)
		}
		return b.findings[i].Fingerprint < b.findings[j].Fingerprint
	})
}

// uniqueProblemFindings is the last safety boundary for the report. A detector target has one
// fingerprint by design, so overlapping instrumentation must never make the complete problem
// section disappear because two observations resolved to that same target.
func uniqueProblemFindings(findings []ProblemFinding) []ProblemFinding {
	if len(findings) == 0 {
		return findings
	}
	result := make([]ProblemFinding, 0, len(findings))
	positions := make(map[string]int, len(findings))
	for _, finding := range findings {
		index, exists := positions[finding.Fingerprint]
		if !exists {
			positions[finding.Fingerprint] = len(result)
			result = append(result, finding)
			continue
		}
		if preferProblemFinding(finding, result[index]) {
			result[index] = finding
		}
	}
	return result
}

func preferProblemFinding(candidate, current ProblemFinding) bool {
	if candidate.RiskScore != current.RiskScore {
		return candidate.RiskScore > current.RiskScore
	}
	if problemConfidenceRank(candidate.Confidence) != problemConfidenceRank(current.Confidence) {
		return problemConfidenceRank(candidate.Confidence) > problemConfidenceRank(current.Confidence)
	}
	if len(candidate.Evidence) != len(current.Evidence) {
		return len(candidate.Evidence) > len(current.Evidence)
	}
	return candidate.Title < current.Title
}

func (b *problemBuilder) coverage() []CategoryCoverage {
	definitions := []struct {
		id, label  string
		required   []string
		configured bool
		sufficient bool
		available  []string
		action     string
	}{
		{ProblemCategoryStability, "Стабильность", []string{"паузы главного потока", "причины завершения процессов"}, collectorConfigured(b.summary, jhlog.CollectorMainThreadStalls) || collectorConfigured(b.summary, jhlog.CollectorProcessExit) || b.summary.StallCount > 0 || len(b.summary.ProcessExits) > 0, true, []string{"паузы главного потока", "причины завершения процессов"}, "Включить сбор пауз и завершений процессов, затем записать активный пользовательский сценарий длительностью не менее 30 секунд."},
		{ProblemCategoryUI, "UI и главный поток", []string{"интервалы наблюдения UI", "целевое время и источник кадров", "Compose-работа"}, collectorConfigured(b.summary, jhlog.CollectorFPS) || collectorConfigured(b.summary, jhlog.CollectorCompose) || b.summary.UIFrames > 0, b.summary.UIFrames >= b.cfg.UIMinFrames, []string{"интервалы наблюдения UI и Compose-границы"}, "Включить сбор JankStats/FPS и записать не менее 120 кадров."},
		{ProblemCategoryNetwork, "Сеть", []string{"жизненный цикл HTTP-вызовов"}, b.summary.HTTPCount > 0, uint64(b.summary.HTTPCount) >= b.cfg.HTTPMinSample, []string{"завершённые HTTP-вызовы"}, "Подключить jankhunter-okhttp3 и повторить сетевой сценарий."},
		{ProblemCategoryMemory, "Память и GC", []string{"снимки памяти", "удержания объектов или HPROF"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || collectorConfigured(b.summary, jhlog.CollectorRetainedObjects) || b.summary.MemoryCount > 0 || len(b.summary.MemoryLeaks) > 0, b.summary.MemoryCount >= 3 || len(b.summary.MemoryLeaks) > 0, []string{"события памяти и удержаний"}, "Включить сбор памяти; подозрение на утечку проверить с помощью HPROF."},
		{ProblemCategoryIO, "I/O и хранилище", []string{"I/O-операции и Room DAO с привязкой к коду"}, collectorConfigured(b.summary, jhlog.CollectorIOTracing) || collectorConfigured(b.summary, jhlog.CollectorRoom) || len(b.summary.IOOperations) > 0, len(b.summary.IOOperations) > 0 || hasSemanticDomain(b.summary, SemanticDomainRoom), []string{"I/O-операции и Room DAO с привязкой к коду"}, "Включить ioTracingEnabled/roomTracingEnabled и выполнить сценарий с БД или хранилищем."},
		{ProblemCategoryCPU, "CPU и задачи", []string{"нагрузка CPU процесса", "выполнения Worker с привязкой к коду"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || collectorConfigured(b.summary, jhlog.CollectorWorker) || hasNamedPrefix(b.summary.Gauges, "process.cpu."), hasNamedPrefix(b.summary.Gauges, "process.cpu.") || hasSemanticDomain(b.summary, SemanticDomainWorker), []string{"нагрузка CPU и Worker-задачи"}, "Включить системный сборщик и workerTracingEnabled, затем выполнить фоновую задачу."},
		{ProblemCategoryPower, "Энергия и нагрев", []string{"температура и расход батареи", "зарядка и активность приложения"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || hasNamedPrefix(b.summary.Gauges, "device.thermal.") || hasNamedPrefix(b.summary.Gauges, "battery."), hasNamedPrefix(b.summary.Gauges, "device.thermal.") || hasNamedPrefix(b.summary.Gauges, "battery."), []string{"температура и расход батареи"}, "Записать длинный сценарий без зарядки с включённым системным сборщиком."},
		{ProblemCategoryLogs, "Логи", []string{"число вызовов логирования"}, len(b.summary.LogSpam) > 0, len(b.summary.LogSpam) > 0, []string{"число вызовов логирования"}, "Включить запись частого логирования и выполнить соответствующий сценарий."},
	}
	out := make([]CategoryCoverage, 0, len(definitions))
	for _, definition := range definitions {
		findings := findingsForCategory(b.findings, definition.id)
		status := "insufficient_data"
		explanation := "Источник данных доступен частично, но наблюдений пока недостаточно для оценки."
		if len(findings) > 0 {
			status, explanation = "problems_found", "Найдены проблемы. Ниже они отсортированы по опасности."
			if collectionEvidenceDegraded(b.summary.CollectionQuality) {
				explanation += " Зарегистрированные потери данных могут влиять на точность оценки."
			}
		} else if !definition.configured {
			status, explanation = "not_measured", "Обязательный источник данных не подключён, поэтому состояние категории неизвестно."
		} else if collectionEvidenceDegraded(b.summary.CollectionQuality) {
			status, explanation = "collection_degraded", "Сбор зарегистрировал потерю или повреждение событий, поэтому состояние категории неизвестно."
		} else if !definition.sufficient {
			status, explanation = "insufficient_data", "Сбор включён, но наблюдений пока недостаточно для оценки."
		} else if b.summary.DurationMS >= 30_000 {
			status, explanation = "healthy", "В записанном сценарии опасные пороги не превышены."
		}
		coverage := CategoryCoverage{Category: definition.id, Label: definition.label, Status: status, FindingCount: len(findings), RequiredEvidence: definition.required, Explanation: explanation}
		if definition.configured {
			coverage.AvailableEvidence = definition.available
		} else {
			coverage.MissingEvidence = definition.required
			coverage.NextAction = definition.action
		}
		if len(findings) > 0 {
			coverage.WorstSeverity = findings[0].Severity
		}
		out = append(out, coverage)
	}
	return out
}

func collectionEvidenceDegraded(quality CollectionQuality) bool {
	if quality.Complete {
		return false
	}
	if quality.SegmentsWithQuality == 0 &&
		quality.AcceptedEvents == 0 &&
		quality.WrittenEvents == 0 &&
		quality.DamagedSegments == 0 {
		return false
	}
	return !quality.ExactAdmission ||
		!quality.ChainValid ||
		!quality.CounterInvariantsValid ||
		!quality.QualityProgressionValid ||
		quality.DamagedSegments > 0 ||
		quality.KnownLostEvents > 0 ||
		quality.ControlFailures > 0 ||
		quality.BoundedEvidenceLoss > 0 ||
		quality.OtherEvidenceLoss > 0
}

func summarizeProblems(findings []ProblemFinding, coverage []CategoryCoverage) ProblemSummary {
	s := ProblemSummary{SchemaVersion: ProblemSchemaVersion, Total: len(findings), SignalTotal: len(findings)}
	counts := map[string]int{}
	for _, finding := range findings {
		counts[finding.Category]++
		switch finding.Severity {
		case "critical":
			s.Critical++
		case "high":
			s.High++
		case "medium":
			s.Medium++
		case "low":
			s.Low++
		default:
			s.Info++
		}
	}
	for _, category := range coverage {
		s.ByCategory = append(s.ByCategory, ProblemCount{Category: category.Category, Count: counts[category.Category]})
		if category.Status != "healthy" && category.Status != "problems_found" {
			s.Unchecked++
		}
	}
	if s.Total > 0 {
		s.Verdict = "problems_found"
	} else if s.Unchecked > 0 {
		s.Verdict = "incomplete"
	} else {
		s.Verdict = "clean"
	}
	s.Headline = problemSummaryHeadline(s)
	return s
}

func problemSummaryHeadline(summary ProblemSummary) string {
	if summary.Total > 0 {
		if summary.SignalTotal > summary.Total {
			return fmt.Sprintf(
				"Найдено инцидентов: %d; объединено связанных сигналов: %d. Максимальный риск — %s.",
				summary.Total,
				summary.SignalTotal,
				problemSummaryWorstSeverity(summary),
			)
		}
		return fmt.Sprintf("Найдено проблем: %d. Максимальный риск — %s.", summary.Total, problemSummaryWorstSeverity(summary))
	}
	if summary.Unchecked > 0 {
		return fmt.Sprintf("Проблем не найдено, но %d категорий не проверены полностью.", summary.Unchecked)
	}
	return "В записанном сценарии значимых проблем не обнаружено."
}

func problemSummaryWorstSeverity(summary ProblemSummary) string {
	switch {
	case summary.Critical > 0:
		return "критический"
	case summary.High > 0:
		return "высокий"
	case summary.Medium > 0:
		return "средний"
	case summary.Low > 0:
		return "низкий"
	default:
		return "информационный"
	}
}

func detectorRegistry(cfg ProblemDetectorConfig) []DetectorMetadata {
	return []DetectorMetadata{
		{ID: "stability.historical_process_exit", Version: cfg.Version, Category: ProblemCategoryStability, Title: "Историческое завершение процесса", MinimumSample: 1, RequiredSignals: []string{"ApplicationExitInfo"}},
		{ID: "stability.main_thread_stall", Version: cfg.Version, Category: ProblemCategoryStability, Title: "Остановка главного потока", MinimumSample: 1, RequiredSignals: []string{"main-thread stall"}, Thresholds: []DetectorThreshold{{"stall", float64(cfg.StallMS), "ms"}, {"high", float64(cfg.StallHighMS), "ms"}}},
		{ID: "ui.jank_tail", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Jank и длинный tail кадров", MinimumSample: cfg.UIMinFrames, RequiredSignals: []string{"UI window"}, Thresholds: []DetectorThreshold{{"jank_rate", cfg.UIJankRate, "%"}, {"frame_tail", float64(cfg.UIFrameTailMS), "ms"}}},
		{ID: "ui.compose_work", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Тяжёлая или частая Compose-работа", MinimumSample: 3, RequiredSignals: []string{"Compose boundary"}, Thresholds: []DetectorThreshold{{"frame_budget", float64(composeDefaultFrameBudgetMS), "ms"}, {"frequent", float64(composeFrequentMinCount), "events"}}},
		{ID: "network.route_health", Version: cfg.Version, Category: ProblemCategoryNetwork, Title: "Медленный/ошибочный/частый route", MinimumSample: cfg.HTTPMinSample, RequiredSignals: []string{"typed HTTP"}, Thresholds: []DetectorThreshold{{"slow_p95", float64(cfg.HTTPSlowMS), "ms"}, {"failure_rate", cfg.HTTPFailureRate * 100, "%"}, {"storm_rate", cfg.HTTPStormRate, "requests/s"}}},
		{ID: "memory.retention", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Удержание памяти", MinimumSample: 1, RequiredSignals: []string{"retention or HPROF"}},
		{ID: "memory.pressure", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Дефицит памяти", MinimumSample: 3, RequiredSignals: []string{"memory/context samples"}},
		{ID: "io.main_thread", Version: cfg.Version, Category: ProblemCategoryIO, Title: "I/O на главном потоке", MinimumSample: 1, RequiredSignals: []string{"attributed I/O"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.IOMainThreadMS), "ms"}}},
		{ID: "io.operation_pressure", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный или частый I/O", MinimumSample: 1, RequiredSignals: []string{"attributed I/O"}, Thresholds: []DetectorThreshold{{"background_slow", float64(cfg.IOBackgroundMS), "ms"}, {"storm_rate", cfg.IOStormRate, "events/s"}}},
		{ID: "io.room_main_thread", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Room DAO на главном потоке", MinimumSample: 1, RequiredSignals: []string{"Room DAO boundary"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.IOMainThreadMS), "ms"}}},
		{ID: "io.room_pressure", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный или частый Room DAO", MinimumSample: 1, RequiredSignals: []string{"Room DAO boundary"}, Thresholds: []DetectorThreshold{{"background_slow", float64(cfg.IOBackgroundMS), "ms"}, {"storm_rate", cfg.IOStormRate, "events/s"}}},
		{ID: "cpu.process_saturation", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Высокая загрузка CPU", MinimumSample: 4, RequiredSignals: []string{"process CPU"}, Thresholds: []DetectorThreshold{{"process_cpu", cfg.ProcessCPUPercent, "% core"}}},
		{ID: "cpu.worker_execution", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Долгая, повторная или неуспешная Worker-задача", MinimumSample: 1, RequiredSignals: []string{"Worker boundary"}, Thresholds: []DetectorThreshold{{"long_execution", float64(workerLongExecutionMS), "ms"}, {"repeated", float64(workerRepeatedMinCount), "events"}}},
		{ID: "power.thermal_pressure", Version: cfg.Version, Category: ProblemCategoryPower, Title: "Thermal pressure", MinimumSample: 3, RequiredSignals: []string{"thermal status"}, Thresholds: []DetectorThreshold{{"severe", float64(cfg.ThermalSevereStatus), "Android status"}}},
		{ID: "logs.spam", Version: cfg.Version, Category: ProblemCategoryLogs, Title: "Спам логами", MinimumSample: cfg.LogSpamMinCount, RequiredSignals: []string{"log hook"}, Thresholds: []DetectorThreshold{{"count", float64(cfg.LogSpamMinCount), "logs"}, {"rate", cfg.LogSpamRate, "logs/s"}}},
	}
}

func validateProblemReport(report ProblemReport) error {
	categories := map[string]struct{}{
		ProblemCategoryStability: {}, ProblemCategoryUI: {}, ProblemCategoryNetwork: {}, ProblemCategoryMemory: {},
		ProblemCategoryIO: {}, ProblemCategoryCPU: {}, ProblemCategoryPower: {}, ProblemCategoryLogs: {},
	}
	detectors := map[string]struct{}{}
	for _, detector := range report.Registry {
		if _, ok := categories[detector.Category]; !ok {
			return fmt.Errorf("detector %q has unknown category %q", detector.ID, detector.Category)
		}
		if detector.ID == "" || detector.Version == "" {
			return fmt.Errorf("detector metadata is incomplete")
		}
		if _, exists := detectors[detector.ID]; exists {
			return fmt.Errorf("duplicate detector %q", detector.ID)
		}
		detectors[detector.ID] = struct{}{}
	}
	knownSeverity := map[string]struct{}{"info": {}, "low": {}, "medium": {}, "high": {}, "critical": {}}
	knownUnits := map[string]struct{}{"": {}, "requests": {}, "ms": {}, "%": {}, "requests/s": {}, "events": {}, "events/s": {}, "bytes": {}, "KB": {}, "samples": {}, "% core": {}, "Android status": {}, "logs": {}}
	fingerprints := map[string]struct{}{}
	for _, finding := range report.Problems {
		if _, ok := detectors[finding.DetectorID]; !ok {
			return fmt.Errorf("finding %q uses unknown detector %q", finding.ID, finding.DetectorID)
		}
		if _, ok := categories[finding.Category]; !ok {
			return fmt.Errorf("finding %q has unknown category %q", finding.ID, finding.Category)
		}
		if _, ok := knownSeverity[finding.Severity]; !ok {
			return fmt.Errorf("finding %q has invalid severity %q", finding.ID, finding.Severity)
		}
		if finding.Confidence != "low" && finding.Confidence != "medium" && finding.Confidence != "high" {
			return fmt.Errorf("finding %q has invalid confidence %q", finding.ID, finding.Confidence)
		}
		if finding.Why.ClaimLevel != "linked" && finding.Why.ClaimLevel != "correlated" && finding.Why.ClaimLevel != "hypothesis" && finding.Why.ClaimLevel != "unknown" {
			return fmt.Errorf("finding %q has invalid claim level %q", finding.ID, finding.Why.ClaimLevel)
		}
		if finding.RiskScore < 0 || finding.RiskScore > 100 || len(finding.RankBreakdown) != 5 {
			return fmt.Errorf("finding %q has invalid risk breakdown", finding.ID)
		}
		for _, evidence := range finding.Evidence {
			if _, ok := knownUnits[evidence.Unit]; !ok {
				return fmt.Errorf("finding %q has unknown unit %q", finding.ID, evidence.Unit)
			}
		}
		if _, exists := fingerprints[finding.Fingerprint]; exists {
			return fmt.Errorf("duplicate finding fingerprint %q", finding.Fingerprint)
		}
		fingerprints[finding.Fingerprint] = struct{}{}
	}
	return nil
}

func risk(impact, magnitude, exposure, breadth, compounding int, impactWhy, magnitudeWhy, exposureWhy, breadthWhy, compoundWhy string) []ProblemRiskComponent {
	return []ProblemRiskComponent{{"impact", clamp(impact, 0, 40), 40, impactWhy}, {"magnitude", clamp(magnitude, 0, 25), 25, magnitudeWhy}, {"exposure", clamp(exposure, 0, 20), 20, exposureWhy}, {"breadth", clamp(breadth, 0, 10), 10, breadthWhy}, {"compounding", clamp(compounding, 0, 5), 5, compoundWhy}}
}

func riskScore(parts []ProblemRiskComponent) int {
	total := 0
	for _, part := range parts {
		total += part.Score
	}
	return clamp(total, 0, 100)
}
func severityForRisk(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 35:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "info"
	}
}
func problemSeverityRank(value string) int {
	return map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}[value]
}
func problemConfidenceRank(value string) int {
	return map[string]int{"low": 1, "medium": 2, "high": 3}[value]
}

func capProblemConfidence(value, maximum string) string {
	if problemConfidenceRank(value) > problemConfidenceRank(maximum) {
		return maximum
	}
	return value
}

func problemConfidence(summary Summary, sample, minimum uint64, direct bool) (string, []string, []string) {
	confidence := "high"
	reasons := []string{}
	limits := []string{}
	if direct {
		reasons = append(reasons, "Симптом записан типизированным событием во время выполнения приложения.")
	}
	if sample < minimum {
		confidence = "low"
		reasons = append(reasons, fmt.Sprintf("Выборка %d меньше минимума %d.", sample, minimum))
		limits = append(limits, "Малая выборка: пока нельзя надёжно оценить типичную задержку и частоту вызовов.")
	} else {
		reasons = append(reasons, fmt.Sprintf("Выборка %d достигает минимума %d.", sample, minimum))
	}
	if collectionEvidenceDegraded(summary.CollectionQuality) {
		confidence = "low"
		reasons = append(reasons, "Сбор зарегистрировал потерю или повреждение событий.")
		limits = append(limits, summary.CollectionQuality.Reasons...)
	} else if confidence == "high" && !summary.AnalysisInputs.Complete {
		confidence = "medium"
		reasons = append(reasons, "Не все дополнительные материалы для анализа доступны.")
	}
	return confidence, uniqueStrings(reasons), uniqueStrings(limits)
}

func findingFingerprint(f ProblemFinding) string {
	// Fingerprints identify the underlying detector target. Subcategory and detector version are
	// deliberately excluded so a slow route can become a compound storm without looking "new".
	parts := []string{f.DetectorID, f.Category}
	if f.DetectorID == "stability.historical_process_exit" || strings.HasPrefix(f.DetectorID, "io.") {
		parts = append(parts, f.Subcategory)
	}
	locations := append([]ProblemLocation(nil), f.Where...)
	sort.Slice(locations, func(i, j int) bool { return locationKey(locations[i]) < locationKey(locations[j]) })
	for _, location := range locations {
		parts = append(parts, locationKey(location))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func locationKey(v ProblemLocation) string {
	return strings.ToLower(strings.Join([]string{v.Process, v.Screen, v.Flow, v.Step, v.Route, v.Owner, v.Class, v.Method}, "\x1f"))
}
func locationBreadth(where []ProblemLocation) int {
	seen := map[string]struct{}{}
	for _, item := range where {
		seen[locationKey(item)] = struct{}{}
	}
	return min(10, max(1, len(seen)*2))
}
func appendUniqueLocation(values []ProblemLocation, value ProblemLocation) []ProblemLocation {
	key := locationKey(value)
	for _, item := range values {
		if locationKey(item) == key {
			return values
		}
	}
	return append(values, value)
}
func findingsForCategory(values []ProblemFinding, category string) []ProblemFinding {
	out := []ProblemFinding{}
	for _, value := range values {
		if value.Category == category {
			out = append(out, value)
		}
	}
	return out
}
func ratePerSecond(count, durationMS uint64) *float64 {
	if durationMS == 0 {
		return nil
	}
	value := float64(count) * 1000 / float64(durationMS)
	return &value
}
func problemRatio(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func u64ptr(v uint64) *uint64 { return &v }
func nonZeroU64Ptr(v uint64) *uint64 {
	if v == 0 {
		return nil
	}
	return &v
}
func formatPercent(v float64) string { return fmt.Sprintf("%.1f", v) }
func formatOptionalRate(rate *float64) string {
	if rate == nil {
		return ""
	}
	return fmt.Sprintf(" (%.2f/с)", *rate)
}
func displayUnknown(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
func boolCount(values ...bool) int {
	total := 0
	for _, value := range values {
		if value {
			total++
		}
	}
	return total
}
func boolScore(value bool, score int) int {
	if value {
		return score
	}
	return 0
}
func clamp(v, low, high int) int { return min(high, max(low, v)) }
func nonEmptyStrings(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
func namedValue(values []NamedValue, name string) (uint64, bool) {
	for _, value := range values {
		if value.Name == name {
			return value.Value, true
		}
	}
	return 0, false
}
func hasNamedPrefix(values []NamedValue, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value.Name, prefix) {
			return true
		}
	}
	return false
}

func collectorConfigured(summary Summary, flag jhlog.CollectorFlag) bool {
	return summary.CollectorSessions > 0 && summary.CollectorFlagsAll&uint64(flag) != 0
}

func microsecondsToMillisecondsCeil(value uint64) uint64 {
	return value/1_000 + boolToUint64(value%1_000 != 0)
}

func boolToUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func formatFrameDeadline(valueUS uint64) string {
	if valueUS == 0 {
		return "mixed/unknown"
	}
	return fmt.Sprintf("%.3f", float64(valueUS)/1_000)
}

func ioOperationLabel(operation string) string {
	label := map[string]string{
		"file_read": "Чтение файла", "file_write": "Запись файла", "file_sync": "Синхронизация файла",
		"database_read": "Чтение БД", "database_write": "Запись БД",
		"content_read": "Чтение ContentProvider", "content_write": "Запись ContentProvider",
	}[operation]
	return firstNonEmpty(label, operation, "I/O операция")
}
func processExitReason(reason uint64) (string, bool) {
	switch reason {
	case 3:
		return "low memory / OOM pressure", true
	case 4:
		return "Java crash", true
	case 5:
		return "native crash", true
	case 6:
		return "ANR", true
	case 7:
		return "initialization failure", true
	case 9:
		return "excessive resource usage", true
	default:
		return fmt.Sprintf("reason %d", reason), false
	}
}
func memoryTitle(leak MemoryLeakSuspect) string {
	if leak.HeapEvidence {
		return fmt.Sprintf("%s подтверждённо удерживается до GC root", displayUnknown(leak.ClassName, "Объект"))
	}
	return fmt.Sprintf("%s подозрительно долго удерживается", displayUnknown(leak.ClassName, "Объект"))
}
func networkSubcategory(slow, failed, storm bool) string {
	if storm && slow {
		return "slow_storm"
	}
	if storm {
		return "request_storm"
	}
	if failed {
		return "failure_rate"
	}
	return "slow_route"
}
func networkFactors(slow, failed, storm bool) []string {
	out := []string{}
	if slow {
		out = append(out, "Высокая задержка среди самых медленных вызовов")
	}
	if failed {
		out = append(out, "Высокая доля ошибок")
	}
	if storm {
		out = append(out, "Высокий секундный пик завершений")
	}
	return out
}
func networkWhat(route RouteStats, failures, count uint64, slow, storm bool, minimumSample uint64) string {
	parts := []string{fmt.Sprintf("Запрос выполнился %s", russianTimes(count))}
	if slow {
		switch {
		case count == 1:
			parts = append(parts, fmt.Sprintf("единственный вызов занял %d мс", route.P95MS))
		case count < minimumSample:
			parts = append(parts, fmt.Sprintf("задержка в верхней части небольшой выборки достигла %d мс", route.P95MS))
		default:
			parts = append(parts, fmt.Sprintf("95%% вызовов завершились не дольше %d мс", route.P95MS))
		}
	}
	if failures > 0 {
		parts = append(parts, fmt.Sprintf("с ошибкой завершилось %s", russianRequestCount(failures)))
	}
	if storm {
		parts = append(parts, fmt.Sprintf("в пике завершалось %d запросов в секунду — это выше допустимого уровня", route.PeakRequestsPerSecond))
	}
	return strings.Join(parts, "; ") + "."
}

func russianTimes(count uint64) string {
	if count%10 == 1 && count%100 != 11 {
		return fmt.Sprintf("%d раз", count)
	}
	if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		return fmt.Sprintf("%d раза", count)
	}
	return fmt.Sprintf("%d раз", count)
}

func russianRequestCount(count uint64) string {
	word := "запросов"
	if count%10 == 1 && count%100 != 11 {
		word = "запрос"
	} else if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		word = "запроса"
	}
	return fmt.Sprintf("%d %s", count, word)
}
