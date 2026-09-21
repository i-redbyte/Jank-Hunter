package report

import (
	"fmt"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type criticalIOReportRow struct {
	analyze.IOStats
	OperationLabel string
	Thread         string
	Status         string
	Severity       string
	Context        string
	ContextHelp    string
	ByteCoverage   string
	Throughput     string
}

func criticalIORows(summary analyze.Summary) []criticalIOReportRow {
	if summary.IOAnalysis == nil {
		return nil
	}
	rows := make([]criticalIOReportRow, 0, len(summary.IOAnalysis.Calls))
	for _, call := range summary.IOAnalysis.Calls {
		status, severity := criticalIOReportStatus(analyze.ClassifyIO(call).Kind)
		context, contextHelp := reportContextPresentation(call.Screen, call.ContextOperation, call.Owner)
		rows = append(rows, criticalIOReportRow{
			IOStats: call, OperationLabel: reportIOOperationLabel(call.Operation),
			Thread: ioThreadLabel(call.MainThread),
			Status: status, Severity: severity, Context: context, ContextHelp: contextHelp,
			ByteCoverage: ioByteCoverage(call.KnownByteOperations, call.Count),
			Throughput:   humanBytesPerSecond(call.BytesPerSecond),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := severityRank(rows[i].Severity), severityRank(rows[j].Severity)
		if left != right {
			return left > right
		}
		if rows[i].MaxDurationUS != rows[j].MaxDurationUS {
			return rows[i].MaxDurationUS > rows[j].MaxDurationUS
		}
		return rows[i].Source < rows[j].Source
	})
	return rows
}

func totalTypedIOOperations(summary analyze.Summary) uint64 {
	if summary.IOAnalysis != nil {
		return summary.IOAnalysis.Operations
	}
	return 0
}

func criticalIOReportStatus(kind analyze.IOClassificationKind) (string, string) {
	switch kind {
	case analyze.IOClassificationMainThreadSync:
		return "синхронизация на главном потоке", "high"
	case analyze.IOClassificationMainThreadLarge:
		return "большой объём на главном потоке", "high"
	case analyze.IOClassificationMainThreadSlow:
		return "медленная операция на главном потоке", "high"
	case analyze.IOClassificationRepeatedFailures:
		return "повторяющиеся ошибки", "high"
	case analyze.IOClassificationBackgroundLarge:
		return "большой объём за операцию", "medium"
	case analyze.IOClassificationBackgroundSlow:
		return "долгая фоновая операция", "medium"
	case analyze.IOClassificationSmallOperationStorm:
		return "частые мелкие операции", "medium"
	case analyze.IOClassificationMainThreadObservation:
		return "файловая операция на главном потоке", "medium"
	default:
		return "наблюдение", "ok"
	}
}

func ioThreadLabel(mainThread bool) string {
	if mainThread {
		return "главный"
	}
	return "фоновый"
}

func ioByteCoverage(known, total uint64) string {
	if total == 0 {
		return "нет операций"
	}
	return fmt.Sprintf("%d из %d · %.1f%%", known, total, float64(known)*100/float64(total))
}

func humanBytesPerSecond(value uint64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case value >= gib:
		return fmt.Sprintf("%.1f ГБ/с", float64(value)/gib)
	case value >= mib:
		return fmt.Sprintf("%.1f МБ/с", float64(value)/mib)
	case value >= kib:
		return fmt.Sprintf("%.1f КБ/с", float64(value)/kib)
	default:
		return fmt.Sprintf("%d Б/с", value)
	}
}
