package report

import (
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

type routeCompareRow struct {
	Route             string
	BaselineCount     int
	CandidateCount    int
	BaselineFailures  int
	CandidateFailures int
	BaselineP95MS     uint64
	CandidateP95MS    uint64
	DeltaP95MS        int64
	BaselineOwner     string
	CandidateOwner    string
	Severity          string
	Comparable        bool
	ComparisonNote    string
	BaselinePresent   bool
	CandidatePresent  bool
}

func routeCompareRows(baseline, candidate analyze.Summary) []routeCompareRow {
	base := map[string]analyze.RouteStats{}
	cand := map[string]analyze.RouteStats{}
	names := map[string]struct{}{}
	for _, route := range baseline.Routes {
		base[route.Route] = route
		names[route.Route] = struct{}{}
	}
	for _, route := range candidate.Routes {
		cand[route.Route] = route
		names[route.Route] = struct{}{}
	}
	rows := make([]routeCompareRow, 0, len(names))
	for name := range names {
		b, hasBaseline := base[name]
		c, hasCandidate := cand[name]
		delta := saturatingSignedDelta(c.P95MS, b.P95MS)
		comparable := hasBaseline && hasCandidate && b.Count > 0 && c.Count > 0
		severity := "ok"
		note := comparePresenceNote(hasBaseline, hasCandidate, "маршрут")
		if comparable {
			severity = latencyDeltaSeverity(b.P95MS, c.P95MS)
			if min(b.Count, c.Count) < 3 {
				severity = capSeverity(severity, "medium")
				note = "меньше трех запросов хотя бы в одном прогоне"
			}
		}
		rows = append(rows, routeCompareRow{
			Route:             name,
			BaselineCount:     b.Count,
			CandidateCount:    c.Count,
			BaselineFailures:  b.Failures,
			CandidateFailures: c.Failures,
			BaselineP95MS:     b.P95MS,
			CandidateP95MS:    c.P95MS,
			DeltaP95MS:        delta,
			BaselineOwner:     b.OwnerSample,
			CandidateOwner:    c.OwnerSample,
			Severity:          severity,
			Comparable:        comparable,
			ComparisonNote:    note,
			BaselinePresent:   hasBaseline,
			CandidatePresent:  hasCandidate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if int64Magnitude(rows[i].DeltaP95MS) != int64Magnitude(rows[j].DeltaP95MS) {
			return int64Magnitude(rows[i].DeltaP95MS) > int64Magnitude(rows[j].DeltaP95MS)
		}
		return rows[i].Route < rows[j].Route
	})
	return rows
}

type screenCompareRow struct {
	Screen                       string
	BaselineFrames               uint64
	CandidateFrames              uint64
	BaselineJankPct              float64
	CandidateJankPct             float64
	DeltaJankPct                 float64
	BaselineAvgFPS               float64
	CandidateAvgFPS              float64
	DeltaFPS                     float64
	BaselineFPSAvailable         bool
	CandidateFPSAvailable        bool
	FPSComparable                bool
	BaselineFrameP95MS           uint64
	CandidateFrameP95MS          uint64
	FrameTailComparisonAvailable bool
	Severity                     string
	Comparable                   bool
	ComparisonNote               string
	BaselinePresent              bool
	CandidatePresent             bool
}

func screenCompareRows(baseline, candidate analyze.Summary) []screenCompareRow {
	base := map[string]analyze.ScreenStats{}
	cand := map[string]analyze.ScreenStats{}
	names := map[string]struct{}{}
	for _, screen := range baseline.Screens {
		base[screen.Screen] = screen
		names[screen.Screen] = struct{}{}
	}
	for _, screen := range candidate.Screens {
		cand[screen.Screen] = screen
		names[screen.Screen] = struct{}{}
	}
	rows := make([]screenCompareRow, 0, len(names))
	for name := range names {
		b, hasBaseline := base[name]
		c, hasCandidate := cand[name]
		deltaJank := c.JankRatePct - b.JankRatePct
		baselineFPSAvailable := b.AvgFPS > 0
		candidateFPSAvailable := c.AvgFPS > 0
		fpsComparable := baselineFPSAvailable && candidateFPSAvailable
		deltaFPS := 0.0
		if fpsComparable {
			deltaFPS = c.AvgFPS - b.AvgFPS
		}
		comparable := hasBaseline && hasCandidate && b.Frames > 0 && c.Frames > 0
		severity := "ok"
		note := comparePresenceNote(hasBaseline, hasCandidate, "экран")
		if comparable {
			severity = screenDeltaSeverity(deltaJank, deltaFPS)
			if min(b.Frames, c.Frames) < 120 {
				severity = capSeverity(severity, "medium")
				note = "меньше 120 кадров хотя бы в одном прогоне"
			}
		}
		rows = append(rows, screenCompareRow{
			Screen:                       name,
			BaselineFrames:               b.Frames,
			CandidateFrames:              c.Frames,
			BaselineJankPct:              b.JankRatePct,
			CandidateJankPct:             c.JankRatePct,
			DeltaJankPct:                 deltaJank,
			BaselineAvgFPS:               b.AvgFPS,
			CandidateAvgFPS:              c.AvgFPS,
			DeltaFPS:                     deltaFPS,
			BaselineFPSAvailable:         baselineFPSAvailable,
			CandidateFPSAvailable:        candidateFPSAvailable,
			FPSComparable:                fpsComparable,
			BaselineFrameP95MS:           b.FrameP95MS,
			CandidateFrameP95MS:          c.FrameP95MS,
			FrameTailComparisonAvailable: b.FrameDistributionState == "mergeable_histogram_v2" && c.FrameDistributionState == "mergeable_histogram_v2",
			Severity:                     severity,
			Comparable:                   comparable,
			ComparisonNote:               note,
			BaselinePresent:              hasBaseline,
			CandidatePresent:             hasCandidate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if math.Abs(rows[i].DeltaJankPct) != math.Abs(rows[j].DeltaJankPct) {
			return math.Abs(rows[i].DeltaJankPct) > math.Abs(rows[j].DeltaJankPct)
		}
		return rows[i].Screen < rows[j].Screen
	})
	return rows
}

type ownerCompareRow struct {
	Owner            string
	Kind             string
	BaselineCount    int
	CandidateCount   int
	BaselineMaxMS    uint64
	CandidateMaxMS   uint64
	DeltaMaxMS       int64
	BaselineTotalMS  uint64
	CandidateTotalMS uint64
	Severity         string
	Comparable       bool
	ComparisonNote   string
	BaselinePresent  bool
	CandidatePresent bool
}

func ownerCompareRows(baseline, candidate analyze.Summary) []ownerCompareRow {
	base := map[string]analyze.OwnerStats{}
	cand := map[string]analyze.OwnerStats{}
	names := map[string]struct{}{}
	for _, owner := range baseline.Owners {
		base[owner.Owner] = owner
		names[owner.Owner] = struct{}{}
	}
	for _, owner := range candidate.Owners {
		cand[owner.Owner] = owner
		names[owner.Owner] = struct{}{}
	}
	rows := make([]ownerCompareRow, 0, len(names))
	for name := range names {
		b, hasBaseline := base[name]
		c, hasCandidate := cand[name]
		kind := firstNonEmpty(c.Kind, b.Kind)
		delta := saturatingSignedDelta(c.MaxMS, b.MaxMS)
		comparable := hasBaseline && hasCandidate && b.Count > 0 && c.Count > 0
		severity := "ok"
		note := comparePresenceNote(hasBaseline, hasCandidate, "источник")
		if comparable {
			severity = latencyDeltaSeverity(b.MaxMS, c.MaxMS)
			if min(b.Count, c.Count) < 3 {
				severity = capSeverity(severity, "medium")
				note = "меньше трех событий хотя бы в одном прогоне"
			}
		}
		rows = append(rows, ownerCompareRow{
			Owner:            name,
			Kind:             kind,
			BaselineCount:    b.Count,
			CandidateCount:   c.Count,
			BaselineMaxMS:    b.MaxMS,
			CandidateMaxMS:   c.MaxMS,
			DeltaMaxMS:       delta,
			BaselineTotalMS:  b.TotalMS,
			CandidateTotalMS: c.TotalMS,
			Severity:         severity,
			Comparable:       comparable,
			ComparisonNote:   note,
			BaselinePresent:  hasBaseline,
			CandidatePresent: hasCandidate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if int64Magnitude(rows[i].DeltaMaxMS) != int64Magnitude(rows[j].DeltaMaxMS) {
			return int64Magnitude(rows[i].DeltaMaxMS) > int64Magnitude(rows[j].DeltaMaxMS)
		}
		return rows[i].Owner < rows[j].Owner
	})
	return rows
}

type signalContextCompareRow struct {
	Screen                string
	Operation             string
	Owner                 string
	BaselineProblems      uint64
	CandidateProblems     uint64
	DeltaProblems         int64
	BaselineLogSpam       uint64
	CandidateLogSpam      uint64
	DeltaLogSpam          int64
	BaselineHTTPP95MS     uint64
	CandidateHTTPP95MS    uint64
	DeltaHTTPP95MS        int64
	BaselineStallMaxMS    uint64
	CandidateStallMaxMS   uint64
	DeltaStallMaxMS       int64
	BaselineJankPct       float64
	CandidateJankPct      float64
	DeltaJankPct          float64
	Severity              string
	Comparable            bool
	ComparisonNote        string
	BaselinePresent       bool
	CandidatePresent      bool
	BaselineHTTPPresent   bool
	CandidateHTTPPresent  bool
	BaselineUIPresent     bool
	CandidateUIPresent    bool
	BaselineStallPresent  bool
	CandidateStallPresent bool
	CountsComparable      bool
}

func signalContextCompareRows(baseline, candidate analyze.Summary) []signalContextCompareRow {
	base := map[string]analyze.SignalContextStats{}
	cand := map[string]analyze.SignalContextStats{}
	keys := map[string]struct{}{}
	for _, context := range baseline.SignalContexts {
		key := signalContextStatsKey(context)
		base[key] = context
		keys[key] = struct{}{}
	}
	for _, context := range candidate.SignalContexts {
		key := signalContextStatsKey(context)
		cand[key] = context
		keys[key] = struct{}{}
	}
	rows := make([]signalContextCompareRow, 0, len(keys))
	for key := range keys {
		b, hasBaseline := base[key]
		c, hasCandidate := cand[key]
		countsComparable := hasBaseline && hasCandidate && durationComparableForReport(baseline.DurationMS, candidate.DurationMS)
		httpComparable := hasBaseline && hasCandidate && b.HTTPCount > 0 && c.HTTPCount > 0
		stallComparable := hasBaseline && hasCandidate && b.StallCount > 0 && c.StallCount > 0
		uiComparable := hasBaseline && hasCandidate && b.UIFrames > 0 && c.UIFrames > 0
		problemDelta := int64(0)
		logDelta := int64(0)
		httpDelta := int64(0)
		stallDelta := int64(0)
		jankDelta := float64(0)
		if countsComparable {
			problemDelta = saturatingSignedDelta(c.ProblemCount, b.ProblemCount)
			logDelta = saturatingSignedDelta(c.LogSpam, b.LogSpam)
		}
		if httpComparable {
			httpDelta = saturatingSignedDelta(c.HTTPP95MS, b.HTTPP95MS)
		}
		if stallComparable {
			stallDelta = saturatingSignedDelta(c.StallMaxMS, b.StallMaxMS)
		}
		if uiComparable {
			jankDelta = c.UIJankPct - b.UIJankPct
		}
		comparable := countsComparable || httpComparable || stallComparable || uiComparable
		severity := "ok"
		if comparable {
			severity = signalContextDeltaSeverity(problemDelta, logDelta, httpDelta, stallDelta, jankDelta)
		}
		note := comparePresenceNote(hasBaseline, hasCandidate, "контекст операции")
		if hasBaseline && hasCandidate && !comparable {
			note = "нет метрик с сопоставимым покрытием"
		} else if hasBaseline && hasCandidate && !countsComparable {
			note = "количества не сравниваются из-за разной длительности; статус рассчитан по доступным latency/UI метрикам"
		}
		rows = append(rows, signalContextCompareRow{
			Screen:                firstNonEmpty(c.Screen, b.Screen),
			Operation:             firstNonEmpty(c.Operation, b.Operation),
			Owner:                 firstNonEmpty(c.Owner, b.Owner),
			BaselineProblems:      b.ProblemCount,
			CandidateProblems:     c.ProblemCount,
			DeltaProblems:         problemDelta,
			BaselineLogSpam:       b.LogSpam,
			CandidateLogSpam:      c.LogSpam,
			DeltaLogSpam:          logDelta,
			BaselineHTTPP95MS:     b.HTTPP95MS,
			CandidateHTTPP95MS:    c.HTTPP95MS,
			DeltaHTTPP95MS:        httpDelta,
			BaselineStallMaxMS:    b.StallMaxMS,
			CandidateStallMaxMS:   c.StallMaxMS,
			DeltaStallMaxMS:       stallDelta,
			BaselineJankPct:       b.UIJankPct,
			CandidateJankPct:      c.UIJankPct,
			DeltaJankPct:          jankDelta,
			Severity:              severity,
			Comparable:            comparable,
			ComparisonNote:        note,
			BaselinePresent:       hasBaseline,
			CandidatePresent:      hasCandidate,
			BaselineHTTPPresent:   b.HTTPCount > 0,
			CandidateHTTPPresent:  c.HTTPCount > 0,
			BaselineUIPresent:     b.UIFrames > 0,
			CandidateUIPresent:    c.UIFrames > 0,
			BaselineStallPresent:  b.StallCount > 0,
			CandidateStallPresent: c.StallCount > 0,
			CountsComparable:      countsComparable,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		left := signalContextDeltaSortScore(rows[i])
		right := signalContextDeltaSortScore(rows[j])
		if left != right {
			return left > right
		}
		return signalContextLabel(rows[i].Screen, rows[i].Operation, rows[i].Owner) < signalContextLabel(rows[j].Screen, rows[j].Operation, rows[j].Owner)
	})
	return rows
}

func comparePresenceNote(hasBaseline, hasCandidate bool, entity string) string {
	switch {
	case hasBaseline && hasCandidate:
		return ""
	case hasCandidate:
		return "новый " + entity + " кандидата; дельта не вычисляется"
	default:
		return entity + " есть только в базе; дельта не вычисляется"
	}
}

func capSeverity(value, maximum string) string {
	if severityRank(value) > severityRank(maximum) {
		return maximum
	}
	return value
}

func durationComparableForReport(baselineMS, candidateMS uint64) bool {
	if baselineMS == 0 || candidateMS == 0 {
		return false
	}
	shorter := baselineMS
	longer := candidateMS
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	return float64(longer-shorter)/float64(shorter) <= 0.2
}

func signalContextDeltaSortScore(row signalContextCompareRow) uint64 {
	score := saturatingMulUint64(int64Magnitude(row.DeltaProblems), 10_000)
	score = saturatingAddUint64(score, saturatingMulUint64(int64Magnitude(row.DeltaLogSpam), 10))
	score = saturatingAddUint64(score, int64Magnitude(row.DeltaStallMaxMS))
	return saturatingAddUint64(score, int64Magnitude(row.DeltaHTTPP95MS))
}

func signalContextStatsKey(context analyze.SignalContextStats) string {
	return strings.Join([]string{context.Screen, context.Operation, context.Owner}, "\x00")
}

func signalContextLabel(screen, operation, owner string) string {
	parts := compactReportParts(screen, operation, owner)
	if len(parts) == 0 {
		return "контекст не задан"
	}
	return strings.Join(parts, " / ")
}

func signalContextLabelHint(screen, operation, owner string) template.HTML {
	label := signalContextLabel(screen, operation, owner)
	hint := signalContextHint(screen, operation, owner)
	if hint == "" {
		return inlineHTMLText(label)
	}
	return tooltipHTML(label, hint)
}

func contextValueHint(value string, field string) template.HTML {
	return reportValueHint(value, "нет данных", field)
}

func databaseSourceQueryHint(source, query string) template.HTML {
	body := strings.TrimSpace(query)
	if isUnknownReportValue(body) {
		body = missingDataHint("query")
	}
	return tooltipHTML(reportValue(source, "место вызова не записано"), body)
}

func reportValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if isUnknownReportValue(value) {
		return fallback
	}
	return displayDateText(value)
}

func displayDateText(value string) string {
	return isoDatePattern.ReplaceAllStringFunc(value, func(candidate string) string {
		parsed, err := time.Parse("2006-01-02", candidate)
		if err != nil {
			return candidate
		}
		return parsed.Format("02.01.2006")
	})
}

func reportValueHint(value string, fallback string, field string) template.HTML {
	label := reportValue(value, fallback)
	if !valueNeedsMissingHint(value, field) {
		return inlineHTMLText(label)
	}
	if hint := missingDataHint(field); hint != "" {
		return tooltipHTML(label, hint)
	}
	return inlineHTMLText(label)
}

func valueNeedsMissingHint(value string, field string) bool {
	if isUnknownReportValue(value) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "session", "device", "app", "build", "sdk", "android", "process", "network":
		normalized := datavalue.NormalizeUnknown(value)
		return strings.Contains(normalized, "неизвест") || strings.Contains(normalized, "нет данных")
	default:
		return false
	}
}

func compactReportParts(values ...string) []string {
	parts := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if isUnknownReportValue(value) {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, value)
	}
	return parts
}

func isUnknownReportValue(value string) bool {
	return datavalue.IsUnknown(value)
}

func cohortValue(value string) string {
	value = strings.TrimSpace(value)
	if isUnknownReportValue(value) {
		return "неизвестно"
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "неизвестно"
	}
	for i, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			fields[i] = reportValue(field, "неизвестно")
			continue
		}
		fields[i] = key + "=" + reportValue(raw, "неизвестно")
	}
	return strings.Join(fields, " ")
}

func cohortValueHint(value string) template.HTML {
	label := cohortValue(value)
	if !cohortHasMissingPart(value) {
		return inlineHTMLText(label)
	}
	return tooltipHTML(label, missingDataHint("cohort"))
}

func cohortHasMissingPart(value string) bool {
	if isUnknownReportValue(value) {
		return true
	}
	for _, field := range strings.Fields(value) {
		_, raw, ok := strings.Cut(field, "=")
		if ok && isUnknownReportValue(raw) {
			return true
		}
	}
	return false
}

func signalContextHint(screen, operation, owner string) string {
	missing := make([]string, 0, 3)
	if isUnknownReportValue(screen) {
		missing = append(missing, "экран: "+missingDataHint("screen"))
	}
	if isUnknownReportValue(operation) {
		missing = append(missing, "операция: "+missingDataHint("operation"))
	}
	if isUnknownReportValue(owner) {
		missing = append(missing, "источник: "+missingDataHint("owner"))
	}
	return strings.Join(missing, "\n\n")
}

func localizedGrowthFreshnessReason(value string) string {
	return strings.NewReplacer(
		"growth checkpoint", "контрольная точка роста журнала",
		"committed events", "зафиксированные события",
		"live checkpoint", "текущая контрольная точка",
		"session", "сессия",
		"freshness", "актуальность",
		"lag=", "отставание ",
	).Replace(value)
}

func missingDataHint(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "screen":
		return "Экран определяется по обратным вызовам жизненного цикла Activity. Если он не записался, ActivityTracker не подключился к Application или событие произошло до первого обратного вызова Activity."
	case "operation":
		return "Операция появляется из startOperation/traceOperation, @JankHunterOperation или автоматического измерения взаимодействий. Если её нет, участок не был размечен либо инструментирование не попало в пакет."
	case "owner":
		return "Источник приходит из встроенных символов преобразования байткода, @JankHunterOwner, @JankHunterOperation, withOwner или ownerHint. Если его нет, участок выполнился без атрибуции."
	case "log-source":
		return "Источник логов определяется при инструментировании вызовов Log/Timber. Если его нет, вызов прошёл без ASM-инструментации или сигнатура логгера не поддержана текущим адаптером."
	case "route", "network-route":
		return "Маршрут определяется при инструментировании OkHttp/HTTP. Если он отсутствует при сетевых симптомах, проверьте настройки instrument.okhttp и includePackages, полный охват приложения и поддержку используемой версии OkHttp."
	case "query", "sql":
		return "SQL-шаблон не записан. Откройте указанное рядом место вызова DAO или SQLite и проверьте выполняемый им запрос."
	case "call", "caller", "callee":
		return "Узел графа вызовов определяется инструментированием runtimeCallGraph. Если его нет, вызов не попал в указанные пакеты или связь не удалось сохранить из-за ограничения объёма данных."
	case "stack":
		return "Подсказка стека берется из верхнего пользовательского кадра при фиксации работы. Если ее нет, стек не содержал подходящего кадра."
	case "holder":
		return "Держатель утечки восстанавливается из ownerHint, текущего владельца или имени класса. Если он не определён, watchObject был вызван без ownerHint, имени класса и активного контекста."
	case "device", "session":
		return "Метаданные устройства записываются при начале запуска Jank Hunter. Если они пустые, Jank Hunter не успел запуститься до событий или инициализация прошла без контекста Application."
	case "app", "build":
		return "Версия приложения берётся из PackageInfo при запуске Jank Hunter. Если её нет, начальное событие не записалось или PackageInfo был недоступен в этом процессе."
	case "sdk", "android":
		return "Версия Android и SDK записываются в снимке устройства при запуске Jank Hunter. Если их нет, начальное событие не попало в журнал."
	case "process":
		return "Имя процесса записывается в начале запуска. Если его нет, Jank Hunter не смог получить имя процесса или начальные сведения не попали в журнал."
	case "network":
		return "Тип сети записывается в снимке контекста через ConnectivityManager/NetworkCapabilities. Если его нет, снимок не успел выполниться или система не вернула активную сеть."
	case "cohort":
		return "Когорта собирается из сведений об устройстве, приложении, сборке, процессе и сети. Неопределённая часть означает, что соответствующее начальное событие или снимок контекста не попал в журнал."
	default:
		return ""
	}
}

func signalContextDeltaSeverity(problemDelta, logDelta, httpDelta, stallDelta int64, jankDelta float64) string {
	if problemDelta >= 10 || stallDelta >= 500 || httpDelta >= 500 || jankDelta >= 3 {
		return "high"
	}
	if problemDelta > 0 || logDelta >= 50 || stallDelta >= 100 || httpDelta >= 100 || jankDelta >= 1 {
		return "medium"
	}
	return "ok"
}

func summaryLogSpamTotal(summary analyze.Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total = saturatingAddUint64(total, item.Count)
	}
	return total
}

func summaryProblemWindowTotal(summary analyze.Summary) uint64 {
	var total uint64
	for _, item := range summary.ProblemWindows {
		total = saturatingAddUint64(total, uint64(item.Windows))
	}
	return total
}

func perMinute(value, durationMS uint64) float64 {
	if durationMS == 0 {
		return 0
	}
	return float64(value) * 60_000 / float64(durationMS)
}

func latencyDeltaSeverity(baseline, candidate uint64) string {
	if baseline == 0 && candidate > 0 {
		if candidate >= 1000 {
			return "high"
		}
		return "medium"
	}
	if candidate <= baseline {
		return "ok"
	}
	delta := candidate - baseline
	pct := 0.0
	if baseline > 0 {
		pct = float64(delta) * 100 / float64(baseline)
	}
	if delta >= 500 || pct >= 50 {
		return "high"
	}
	if delta >= 100 || pct >= 15 {
		return "medium"
	}
	return "ok"
}

func screenDeltaSeverity(deltaJankPct, deltaFPS float64) string {
	if deltaJankPct >= 3 || deltaFPS <= -5 {
		return "high"
	}
	if deltaJankPct >= 1 || deltaFPS <= -2 {
		return "medium"
	}
	return "ok"
}

func signedMS(value int64) string {
	if value == 0 {
		return "0 мс"
	}
	return fmt.Sprintf("%+d мс", value)
}

func signedDuration(value int64) string {
	if value == 0 {
		return "0 мс"
	}
	sign := "+"
	if value < 0 {
		sign = "-"
	}
	return sign + humanDuration(int64Magnitude(value))
}

func signedFloat(value float64, unit string) string {
	if value == 0 {
		return "0 " + unit
	}
	return fmt.Sprintf("%+.2f %s", value, unit)
}

const (
	maxSignedInt64 = int64(^uint64(0) >> 1)
	minSignedInt64 = -maxSignedInt64 - 1
)

func saturatingSignedDelta(after, before uint64) int64 {
	if after >= before {
		difference := after - before
		if difference > uint64(maxSignedInt64) {
			return maxSignedInt64
		}
		return int64(difference)
	}
	difference := before - after
	if difference > uint64(maxSignedInt64) {
		return minSignedInt64
	}
	return -int64(difference)
}

func int64Magnitude(value int64) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	return uint64(-(value + 1)) + 1
}

func saturatingAddUint64(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func saturatingMulUint64(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > ^uint64(0)/right {
		return ^uint64(0)
	}
	return left * right
}

func nonNegativeInt(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}
