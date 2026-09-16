package analyze

import "strconv"

// FormatRollingPeak preserves numeric lower bounds without presenting them as exact.
// Empty precision is accepted for legacy summaries; their quality is not upgraded.
func FormatRollingPeak(value uint64, status string) string {
	if status == "unknown" {
		return "n/a"
	}
	text := strconv.FormatUint(value, 10)
	if status == "lower_bound_rolling_second" || status == "bounded_approximation" {
		return "≥ " + text
	}
	return text
}

func rollingPeakWarnings(summary Summary) []string {
	var warnings []string
	if db := summary.DatabaseAnalysis; db != nil {
		inexact := db.BurstEstimateStatus == "lower_bound_rolling_second"
		for _, statement := range db.Statements {
			inexact = inexact || statement.BurstEstimateStatus == "lower_bound_rolling_second"
			for _, context := range statement.Contexts {
				inexact = inexact || context.BurstEstimateStatus == "lower_bound_rolling_second"
			}
		}
		if inexact {
			warnings = append(warnings, "Качество сбора: DB: пик за скользящую секунду в отмеченных группах является нижней границей из-за нарушения порядка событий или вытеснения истории группы. Значение ниже порога не исключает всплеск.")
		}
	}
	if io := summary.IOAnalysis; io != nil {
		for _, call := range io.Calls {
			if call.BurstEstimateStatus == "lower_bound_rolling_second" {
				warnings = append(warnings, "Качество сбора: IO: пик за скользящую секунду в отмеченных группах является нижней границей из-за нарушения порядка событий. Общий пик восстановлен по полным интервалам; значение группы ниже порога не исключает всплеск.")
				break
			}
		}
	}
	return warnings
}
