package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

// Detailed observations remain in inspect. Comparison snapshots carry their sufficient statistics.
func comparisonThreadSummary(source *ThreadAnalysis) *ThreadAnalysis {
	if source == nil {
		return nil
	}
	result := *source
	result.HistoryOmitted = true
	result.Resources = nil
	result.CoroutineSegments = nil
	result.Lifecycle = nil
	result.Annotations = nil
	result.Processes = append([]ThreadProcessAnalysis(nil), source.Processes...)
	for i := range result.Processes {
		p := &result.Processes[i]
		p.SamplingIntervalMS = ThreadSamplingInterval(*p)
		p.Snapshots = nil
		p.Histories = nil
	}
	return &result
}

// Zero means absent or nonuniform cadence; never infer comparability from an empty history.
func ThreadSamplingInterval(p ThreadProcessAnalysis) uint64 {
	if len(p.Snapshots) == 0 {
		return p.SamplingIntervalMS
	}
	var interval uint64
	for _, point := range p.Snapshots {
		if point.Snapshot == nil || point.Snapshot.IntervalMS == 0 {
			return 0
		}
		if interval != 0 && interval != point.Snapshot.IntervalMS {
			return 0
		}
		interval = point.Snapshot.IntervalMS
	}
	return interval
}

func comparisonWorkerSummary(source *WorkerAnalysis) *WorkerAnalysis {
	if source == nil {
		return nil
	}
	result := *source
	result.Attempts = nil
	result.AttemptsOmitted = true
	return &result
}

func comparisonRegistrySummary(source *WorkRegistryAnalysis) *WorkRegistryAnalysis {
	if source == nil {
		return nil
	}
	result := *source
	result.HistoryOmitted = true
	result.Snapshots = nil
	latest := make(map[string]int)
	for _, snapshot := range source.Snapshots {
		if previous, ok := latest[snapshot.Scope]; ok {
			if snapshot.ObservedElapsedMS >= result.Snapshots[previous].ObservedElapsedMS {
				result.Snapshots[previous] = snapshot
			}
		} else {
			latest[snapshot.Scope] = len(result.Snapshots)
			result.Snapshots = append(result.Snapshots, snapshot)
		}
	}
	for i := range result.Snapshots {
		snapshot := &result.Snapshots[i]
		snapshot.DataMetadataOmitted = true
		snapshot.Jobs = append([]jhlog.WorkRegistryJob(nil), snapshot.Jobs...)
		for j := range snapshot.Jobs {
			snapshot.Jobs[j].Progress = nil
			snapshot.Jobs[j].Output = nil
		}
	}
	return &result
}
