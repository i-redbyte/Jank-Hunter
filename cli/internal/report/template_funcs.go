package report

import (
	"fmt"
	"html/template"
	"math"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func reportTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"reportCSS": func(includeMath bool) template.CSS {
			return template.CSS(reportStylesheet(includeMath))
		},
		"reportJS": func() template.JS {
			return template.JS(reportJS)
		},
		"problemSearchJS": func() template.JS {
			return template.JS(problemSearchJS)
		},
		"logGrowthCSS": func() template.CSS {
			return template.CSS(logGrowthCSS)
		},
		"logGrowthJS": func() template.JS {
			return template.JS(logGrowthJS)
		},
		"pctWidth": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("width:%.2f%%", clampPct(value)))
		},
		"msWidth": func(value uint64) template.CSS {
			width := float64(value) * 100 / 2000
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"deltaWidth": func(value float64) template.CSS {
			width := math.Abs(value)
			if width > 100 {
				width = 100
			}
			if width < 1 && value != 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"scoreWidth": func(value float64) template.CSS {
			width := value * 12.5
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"ringStyle": func(value float64) template.CSS {
			value = clampPct(value)
			capStyle := "round"
			if value < 1 {
				capStyle = "butt"
			}
			return template.CSS(fmt.Sprintf("--value:%.2f;--cap:%s", value, capStyle))
		},
		"rate": func(part int, total int) float64 {
			if total <= 0 {
				return 0
			}
			return float64(part) * 100 / float64(total)
		},
		"sub": func(left, right int) int {
			return max(0, left-right)
		},
		"fpsScore": func(value float64) float64 {
			return clampPct(value * 100 / 60)
		},
		"severityClass": severityCSSClass,
		"statusLabel": func(value string) string {
			switch value {
			case "high":
				return "критично"
			case "medium":
				return "предупреждение"
			case "ok":
				return "готово"
			case "pending":
				return "ожидает данных"
			default:
				return "каркас"
			}
		},
		"sparkline": func(series mathanalysis.Series) template.HTML {
			return sparklineSVG(series)
		},
		"seriesMax": func(series mathanalysis.Series) float64 {
			return seriesMax(series)
		},
		"seriesLast": func(series mathanalysis.Series) float64 {
			return seriesLast(series)
		},
		"bucketRange": func(bucket mathanalysis.TimelineBucket) string {
			return fmt.Sprintf("%.1f-%.1fs", float64(bucket.StartMS)/1000, float64(bucket.EndMS)/1000)
		},
		"humanDuration":                   humanDuration,
		"humanMicros":                     humanMicroseconds,
		"unixMillisTime":                  unixMillisTime,
		"formatDurationNs":                formatDurationNs,
		"dataSize":                        humanDataSizeKB,
		"ruCount":                         russianCount,
		"tip":                             tooltipHTML,
		"metricHelp":                      metricHelp,
		"memoryHelp":                      memoryMetricHelp,
		"integralHelp":                    integralHelp,
		"scoreHelp":                       scoreHelp,
		"scoreGuide":                      scoreGuideHTML,
		"integralCriteria":                integralCriteria,
		"ownerKind":                       ownerKindLabel,
		"frameSourceLabel":                frameSourceLabel,
		"httpPhaseLabel":                  httpPhaseLabel,
		"webSocketFailureLabel":           webSocketFailureLabel,
		"problemKind":                     problemKindLabel,
		"codeProblemLocation":             codeProblemLocation,
		"codeProblemDrillPath":            codeProblemDrillPath,
		"codeProblemMetric":               codeProblemMetric,
		"codeProblemCategoryOptions":      codeProblemCategoryOptions,
		"codeProblemCategories":           codeProblemCategoryStats,
		"codeProblemSeverities":           codeProblemSeverityStats,
		"codeProblemEvidenceKey":          codeProblemEvidenceKey,
		"codeProblemEvidenceArchive":      codeProblemEvidenceArchive,
		"codeProblemCompareArchive":       codeProblemCompareArchive,
		"limitRows":                       limitRows,
		"rowLimitNote":                    rowLimitNote,
		"leakObjectKindOptions":           leakObjectKindOptions,
		"leakObjectKindLabel":             leakObjectKindLabel,
		"leakGraphSVG":                    leakGraphSVG,
		"leakModeLabel":                   leakModeLabel,
		"leakDeltaStatusClass":            leakDeltaStatusClass,
		"codeProblemCompareRows":          codeProblemCompareRows,
		"memoryLeakCompareRows":           memoryLeakCompareRows,
		"deltaGroups":                     compareDeltaGroups,
		"deltaLabel":                      compareDeltaLabel,
		"deltaHelp":                       compareDeltaHelp,
		"deltaValue":                      compareDeltaValue,
		"deltaChange":                     compareDeltaChange,
		"deltaInterval":                   compareDeltaInterval,
		"problemDeltas":                   problemDeltas,
		"databaseProblemRows":             databaseProblemRows,
		"databaseObservationRows":         databaseObservationRows,
		"databaseTransactionRows":         databaseTransactionRows,
		"databaseScenarioRows":            databaseScenarioRows,
		"databaseScenarioScopeLabel":      databaseScenarioScopeLabel,
		"databasePlanKindLabel":           databasePlanKindLabel,
		"databaseProblemTransactionRows":  databaseProblemTransactionRows,
		"databaseObservedTransactionRows": databaseObservedTransactionRows,
		"databaseTaxonomyLabel":           databaseTaxonomyLabel,
		"severityLabel":                   severityLabel,
		"codeSeverityLabel":               codeSeverityLabel,
		"confidenceLabel": func(value string) string {
			return confidenceLabel(value)
		},
		"problemCategoryLabel":          problemCategoryLabel,
		"problemCoverageStatusLabel":    problemCoverageStatusLabel,
		"evidenceQualityStatusLabel":    evidenceQualityStatusLabel,
		"problemStatusLabel":            problemStatusLabel,
		"problemClaimLabel":             problemClaimLabel,
		"problemLocationText":           problemLocationText,
		"problemEvidenceLabel":          problemEvidenceLabel,
		"problemEvidenceHelp":           problemEvidenceHelp,
		"problemEvidenceDisplay":        problemEvidenceDisplay,
		"problemEvidenceUnit":           problemEvidenceUnit,
		"problemEvidenceThreshold":      problemEvidenceThreshold,
		"problemPriorityComponentLabel": problemPriorityComponentLabel,
		"problemPriorityBandLabel":      problemPriorityBandLabel,
		"diTermLabel":                   dependencyInjectionTermLabel,
		"problemPrimaryRecommendation": func(value analyze.ProblemFinding) *analyze.ProblemRecommendation {
			if len(value.Recommendations) == 0 {
				return nil
			}
			return &value.Recommendations[0]
		},
		"problemDeltaFinding": func(value analyze.ProblemDelta) analyze.ProblemFinding {
			if value.Candidate != nil {
				return *value.Candidate
			}
			if value.Baseline != nil {
				return *value.Baseline
			}
			return analyze.ProblemFinding{}
		},
		"problemDiagnosisData":              problemDiagnosisData,
		"problemDeltaDiagnosisData":         problemDeltaDiagnosisData,
		"problemOrientedCollectionWarnings": problemOrientedCollectionWarnings,
		"priorityWidth": func(value int) template.CSS {
			return template.CSS(fmt.Sprintf("width:%d%%", min(100, max(0, value))))
		},
		"diagnosticCompletenessLevelLabel": diagnosticCompletenessLevelLabel,
		"processScopeLabel": func(value string) string {
			return processScopeLabel(value)
		},
		"influenceRoleLabel":         influenceRoleLabel,
		"influenceGraphData":         influenceGraphData,
		"influenceEvidenceLabel":     influenceEvidenceLabel,
		"routeCompareRows":           routeCompareRows,
		"screenCompareRows":          screenCompareRows,
		"ownerCompareRows":           ownerCompareRows,
		"signalContextCompareRows":   signalContextCompareRows,
		"operationKindLabel":         operationKindLabel,
		"operationOutcomeLabel":      operationOutcomeLabel,
		"operationStatusLabel":       operationStatusLabel,
		"databaseCompareStatusLabel": databaseCompareStatusLabel,
		"uiScreenInsights":           uiScreenInsights,
		"uiProblemCount":             uiProblemCount,
		"hasComposeWork":             hasComposeWork,
		"hasRoomWork":                hasRoomWork,
		"composeWorkReport":          composeWorkReport,
		"roomWorkRows":               roomWorkRows,
		"databaseStatementRows":      databaseStatementRows,
		"databaseCoverage":           databaseCoverage,
		"workerRows":                 workerRows,
		"criticalIORows":             criticalIORows,
		"asyncKindLabel":             asyncKindLabel,
		"hundredths":                 hundredths,
		"humanBytesPerSecond":        humanBytesPerSecond,
		"ordinaryRuntimeCalls":       ordinaryRuntimeCalls,
		"operationContextInsights":   operationContextInsights,
		"customMetricInsights":       customMetricInsights,
		"primaryCategoryCoverage":    primaryCategoryCoverage,
		"findingCategoryCoverage":    findingCategoryCoverage,
		"problemCards": func(summary analyze.Summary) []analyze.ProblemFinding {
			if len(summary.ProblemIncidents) > 0 {
				return summary.ProblemIncidents
			}
			return summary.Problems
		},
		"hiddenCoverageSummary":  hiddenCoverageSummary,
		"collectionWindowNotice": collectionWindowNotice,
		"collectorCapabilities":  collectorCapabilities,
		"summaryLogSpam":         summaryLogSpamTotal,
		"summaryProblemWindows":  summaryProblemWindowTotal,
		"perMinute":              perMinute,
		"signedMS":               signedMS,
		"signedDuration":         signedDuration,
		"signedFloat":            signedFloat,
		"networkBucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroNetworkBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"uiBucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroUIBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"memoryBucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroMemoryBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"robustGroups":                 robustStatGroups,
		"robustDeltaGroups":            robustDeltaGroups,
		"causalEdges":                  uniqueCausalEdges,
		"causalPaths":                  uniqueCausalPaths,
		"causalGraphSVG":               causalGraphSVG,
		"influenceStatus":              influenceStatusLabel,
		"influenceSeverity":            influenceSeverityLabel,
		"topInfluenceNodes":            topInfluenceNodes,
		"mathHeuristic":                inspectMathHeuristic,
		"compareMathHeuristic":         compareMathHeuristic,
		"significantMathFindings":      significantMathFindings,
		"significantReportFindings":    significantReportFindings,
		"significantMarkovStates":      significantMarkovStates,
		"hiddenMarkovStates":           hiddenMarkovStates,
		"significantMarkovTransitions": significantMarkovTransitions,
		"hiddenMarkovTransitions":      hiddenMarkovTransitions,
		"significantMarkovDeltas":      significantMarkovDeltas,
		"hiddenMarkovDeltas":           hiddenMarkovDeltas,
		"join": func(values []string, separator string) string {
			return strings.Join(values, separator)
		},
		"seconds": func(ms uint64) float64 {
			return float64(ms) / 1000
		},
		"jankPct": func(jankyFrames uint64, frames uint64) float64 {
			if frames == 0 {
				return 0
			}
			return float64(jankyFrames) * 100 / float64(frames)
		},
		"motifText": func(tokens []string) string {
			return mathanalysis.NetworkLoopMotifText(tokens)
		},
		"pathText": func(path mathanalysis.GraphPath) string {
			if len(path.Nodes) == 0 {
				return ""
			}
			return strings.Join(path.Nodes, " -> ")
		},
		"markovState": func(state string) string {
			return mathanalysis.MarkovStateLabel(state)
		},
		"markovConfidence": func(confidence string) string {
			return mathanalysis.MarkovConfidenceLabel(confidence)
		},
		"causalKind": func(kind string) string {
			return mathanalysis.CausalKindLabel(kind)
		},
		"percent01": func(value float64) float64 {
			return value * 100
		},
		"fallback": func(value string, fallback string) string {
			if isUnknownReportValue(value) {
				return fallback
			}
			return value
		},
		"contextHint":             contextValueHint,
		"databaseSourceQueryHint": databaseSourceQueryHint,
		"growthFreshnessReason":   localizedGrowthFreshnessReason,
		"reportValue":             reportValue,
		"reportHint":              reportValueHint,
		"cohortHint":              cohortValueHint,
		"signalContextHint":       signalContextLabelHint,
		"bodyClass":               bodyClass,
	}
}

func httpPhaseLabel(value string) string {
	switch value {
	case "queue":
		return "Очередь"
	case "dns":
		return "Поиск адреса (DNS)"
	case "connect":
		return "Соединение"
	case "tls":
		return "Защищённое соединение (TLS)"
	case "request":
		return "Отправка"
	case "ttfb":
		return "Ожидание первого байта (TTFB)"
	case "response":
		return "Получение"
	default:
		return value
	}
}

func webSocketFailureLabel(value string) string {
	switch value {
	case "timeout":
		return "тайм-аут"
	case "connection":
		return "соединение"
	case "tls":
		return "TLS"
	case "protocol":
		return "протокол"
	case "io":
		return "ввод-вывод"
	case "other":
		return "другая"
	default:
		return value
	}
}

func operationKindLabel(value string) string {
	switch value {
	case "user":
		return "действие пользователя"
	case "screen":
		return "открытие экрана"
	case "background":
		return "фоновая работа"
	case "system":
		return "системная работа"
	case "stage":
		return "этап"
	default:
		return reportValue(value, "тип не указан")
	}
}

func operationOutcomeLabel(value string) string {
	switch value {
	case "success":
		return "успешно"
	case "failure":
		return "ошибка"
	case "cancelled":
		return "отменено"
	case "timeout":
		return "превышено время ожидания"
	default:
		return reportValue(value, "результат не указан")
	}
}

func operationStatusLabel(value string) string {
	switch value {
	case "regressed":
		return "ухудшение"
	case "improved":
		return "улучшение"
	case "stable":
		return "без заметного изменения"
	case "new":
		return "есть только в кандидате"
	case "removed":
		return "есть только в базе"
	case "insufficient_data":
		return "недостаточно данных"
	default:
		return reportValue(value, "статус не определён")
	}
}

func databaseCompareStatusLabel(value string) string {
	switch value {
	case "compared":
		return "сопоставлено"
	case "new":
		return "только в кандидате"
	case "removed":
		return "только в базе"
	case "insufficient_data":
		return "недостаточно данных"
	case "not_comparable":
		return "наборы несопоставимы"
	default:
		return reportValue(value, "статус не определён")
	}
}
