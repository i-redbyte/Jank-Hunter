package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type workerReportRow struct {
	analyze.WorkerStats
	Status      string
	Severity    string
	Context     string
	ContextHelp string
	Lifecycle   string
	Outcomes    string
	CPU         string
	Allocation  string
	Memory      string
}

func workerRows(summary analyze.Summary) []workerReportRow {
	if summary.WorkerAnalysis == nil {
		return nil
	}
	rows := make([]workerReportRow, 0, len(summary.WorkerAnalysis.Workers))
	for _, worker := range summary.WorkerAnalysis.Workers {
		status, severity := workerReportStatus(worker)
		context, contextHelp := reportContextPresentation(worker.Screen, worker.Operation, worker.Owner)
		rows = append(rows, workerReportRow{
			WorkerStats: worker,
			Status:      status,
			Severity:    severity,
			Context:     context,
			ContextHelp: contextHelp,
			Lifecycle: fmt.Sprintf(
				"поставлено в очередь %d · запущено %d · завершено %d · неполных цепочек %d",
				worker.Enqueued, worker.Started, worker.Finished,
				worker.MissingEnqueue+worker.MissingStart+worker.MissingFinish,
			),
			Outcomes: fmt.Sprintf(
				"успешно %d · ошибок %d · повторов %d · отмен %d",
				worker.Success, worker.Failures, worker.Retries, worker.Cancelled,
			),
			CPU: workerCPUDescription(worker),
			Allocation: workerSampleDescription(
				worker.AllocationSamples,
				fmt.Sprintf("среднее %d Б/с · максимум %d Б/с", worker.AvgAllocationRateBytesPerSec, worker.MaxAllocationRateBytesPerSec),
			),
			Memory: workerMemoryDescription(worker),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := severityRank(rows[i].Severity), severityRank(rows[j].Severity)
		if left != right {
			return left > right
		}
		if rows[i].RunMaxMS != rows[j].RunMaxMS {
			return rows[i].RunMaxMS > rows[j].RunMaxMS
		}
		return rows[i].Worker < rows[j].Worker
	})
	return rows
}

func workerReportStatus(worker analyze.WorkerStats) (string, string) {
	if worker.Failures+worker.Retries+worker.Cancelled > 0 {
		return "есть неуспешные завершения", "high"
	}
	if worker.MainThreadStarts > 0 {
		return "работа на главном потоке", "high"
	}
	if worker.RunMaxMS >= 10_000 {
		return "долгое выполнение", "medium"
	}
	if worker.MissingStart+worker.MissingFinish > 0 {
		return "жизненный цикл записан не полностью", "medium"
	}
	return "наблюдение", "ok"
}

func workerCPUDescription(worker analyze.WorkerStats) string {
	if worker.CPUSamples == 0 && worker.CoreCPUSamples == 0 {
		return "нет замеров в интервале выполнения"
	}
	parts := make([]string, 0, 2)
	if worker.CPUSamples > 0 {
		parts = append(parts, fmt.Sprintf(
			"весь процессор: среднее %.2f%% · максимум %.2f%% · %d замеров",
			float64(worker.AvgDeviceCPUPercentX100)/100,
			float64(worker.MaxDeviceCPUPercentX100)/100,
			worker.CPUSamples,
		))
	}
	if worker.CoreCPUSamples > 0 {
		parts = append(parts, fmt.Sprintf(
			"одно ядро: среднее %.2f%% · максимум %.2f%% · %d замеров",
			float64(worker.AvgCoreCPUPercentX100)/100,
			float64(worker.MaxCoreCPUPercentX100)/100,
			worker.CoreCPUSamples,
		))
	}
	return strings.Join(parts, "; ")
}

func workerSampleDescription(samples uint64, value string) string {
	if samples == 0 {
		return "нет замеров в интервале выполнения"
	}
	return value + fmt.Sprintf(" · %d замеров", samples)
}

func workerMemoryDescription(worker analyze.WorkerStats) string {
	if worker.MemoryPairs == 0 && worker.MaxPSSDuringKB == 0 {
		return "нет достаточной пары замеров"
	}
	return fmt.Sprintf(
		"изменение PSS в среднем %+d КБ · максимум в окне %s · %d пар",
		worker.AvgPSSDeltaKB, humanDataSizeKB(worker.MaxPSSDuringKB), worker.MemoryPairs,
	)
}
