package analyze

import (
	"fmt"
	"sort"
)

// CPU exposure is summed thread-observation time, not wall time or process CPU.
// Name groups are labels only: a shared name does not establish pool ownership.
type ThreadDynamics struct {
	WindowMS                                                 uint64
	AppearancesPerMinute                                     *float64
	CPUSamples                                               int
	CPUTimeMS, ObservedThreadTimeMS, PeakObservedCorePercent float64
	MeanObservedThreadCorePercent                            *float64
	MaxObservedRise, MaxObservedFall                         int
	LastNames                                                []ThreadNameCount
	OmittedNames                                             int
	Signals                                                  []ThreadSignal
	OmittedSignals                                           int
}
type ThreadNameCount struct {
	Name  string
	Count int
}
type ThreadSignal struct {
	Code, Evidence, Limitation string
	TID, StartTicks            uint64
	FromMS, ToMS               uint64
}

func summarizeThreadDynamics(p ThreadProcessAnalysis) *ThreadDynamics {
	d := &ThreadDynamics{}
	emit := func(s ThreadSignal) {
		if len(d.Signals) < 50 {
			d.Signals = append(d.Signals, s)
		} else {
			d.OmittedSignals++
		}
	}
	summarizeThreadCounts(p, d, emit)
	for _, h := range p.Histories {
		var high *ThreadHistoryPoint
		if signal := repeatedThreadOSWait(h); signal != nil {
			emit(*signal)
		}
		for i := range h.Observations {
			o := &h.Observations[i]
			if o.CPU == nil {
				continue
			}
			d.CPUSamples++
			d.CPUTimeMS += o.CPU.CPUTimeMS
			d.ObservedThreadTimeMS += float64(o.CPU.ElapsedMS)
			if o.CPU.CorePercent > d.PeakObservedCorePercent {
				d.PeakObservedCorePercent = o.CPU.CorePercent
			}
			if o.CPU.ElapsedMS >= 1000 && o.CPU.CorePercent >= 80 && (high == nil || o.CPU.CorePercent > high.CPU.CorePercent) {
				high = o
			}
		}
		if high != nil {
			emit(ThreadSignal{Code: "measured_cpu", TID: h.TID, StartTicks: h.StartTicks, ToMS: high.TimeMS,
				Evidence:   fmt.Sprintf("Измерено %.1f%% одного ядра в среднем за %d мс; CPU %.1f мс", high.CPU.CorePercent, high.CPU.ElapsedMS, high.CPU.CPUTimeMS),
				Limitation: "Порог 80% за интервал не менее 1 с - повод проверить нагрузку, не ошибка. Снимки неатомарны; краткие пики внутри интервала неизвестны."})
		}
	}
	if d.CPUSamples > 0 && d.ObservedThreadTimeMS > 0 {
		v := 100 * d.CPUTimeMS / d.ObservedThreadTimeMS
		d.MeanObservedThreadCorePercent = &v
	}
	return d
}

func summarizeThreadCounts(p ThreadProcessAnalysis, d *ThreadDynamics, emit func(ThreadSignal)) {
	snapshots := p.Snapshots
	if len(snapshots) == 0 {
		return
	}
	monotonic := snapshots[0].Snapshot != nil
	increasing := 0
	growthEmitted := false
	burstEmitted := false
	for i := 1; i < len(snapshots); i++ {
		a, b := snapshots[i-1], snapshots[i]
		if a.Snapshot == nil || b.Snapshot == nil || b.Snapshot.ElapsedMS <= a.Snapshot.ElapsedMS {
			monotonic = false
			increasing = 0
			continue
		}
		rise := len(b.Snapshot.Threads) - len(a.Snapshot.Threads)
		if rise > d.MaxObservedRise {
			d.MaxObservedRise = rise
		}
		if -rise > d.MaxObservedFall {
			d.MaxObservedFall = -rise
		}
		elapsed := b.Snapshot.ElapsedMS - a.Snapshot.ElapsedMS
		cadence := a.Snapshot.IntervalMS
		// Allow at most two nominal periods for scheduler jitter; longer gaps break the evidence.
		contiguous := cadence > 0 && cadence == b.Snapshot.IntervalMS && (elapsed <= cadence || elapsed-cadence <= cadence) && a.Snapshot.Complete && b.Snapshot.Complete
		if !contiguous || rise <= 0 {
			increasing = 0
			continue
		}
		if rise >= 8 && elapsed <= 10000 && !burstEmitted {
			emit(ThreadSignal{Code: "observed_burst", FromMS: a.TimeMS, ToMS: b.TimeMS,
				Evidence:   fmt.Sprintf("На %d больше прочитанных потоков за %d мс между полными снимками", rise, elapsed),
				Limitation: "Порог 8 потоков за не более 10 с - повод проверить массовый запуск. Это изменение состава снимков, не точное число вызовов Thread.start; намеренный запуск пула допустим."})
			burstEmitted = true
		}
		increasing++
		if increasing >= 3 && !growthEmitted && len(b.Snapshot.Threads)-len(snapshots[i-3].Snapshot.Threads) >= 5 {
			emit(ThreadSignal{Code: "observed_growth", FromMS: snapshots[i-3].TimeMS, ToMS: b.TimeMS,
				Evidence:   fmt.Sprintf("Число прочитанных потоков растёт в 4 последовательных полных снимках: %d → %d", len(snapshots[i-3].Snapshot.Threads), len(b.Snapshot.Threads)),
				Limitation: "Рост минимум на 5 потоков - повод проверить сценарий, не доказательство утечки. Между снимками активность неизвестна; намеренное расширение пула допустимо."})
			growthEmitted = true
		}
	}
	first, last := snapshots[0].Snapshot, snapshots[len(snapshots)-1].Snapshot
	if monotonic && first != nil && last != nil && last.ElapsedMS > first.ElapsedMS {
		d.WindowMS = last.ElapsedMS - first.ElapsedMS
		rate := float64(p.Appearances) * 60000 / float64(d.WindowMS)
		d.AppearancesPerMinute = &rate
	}
	if last == nil {
		return
	}
	counts := make(map[string]int)
	for _, r := range last.Threads {
		counts[r.Name]++
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(d.LastNames) < 200 {
			d.LastNames = append(d.LastNames, ThreadNameCount{Name: name, Count: counts[name]})
		} else {
			d.OmittedNames++
		}
	}
}

func repeatedThreadOSWait(h ThreadHistory) *ThreadSignal {
	count := 0
	var first uint64
	var previous uint64
	for _, o := range h.Observations {
		if o.Row.OSState != "D" || (count > 0 && o.ElapsedMS <= previous) {
			count = 0
			continue
		}
		if count == 0 {
			first = o.TimeMS
		}
		previous = o.ElapsedMS
		count++
		if count == 3 {
			return &ThreadSignal{Code: "repeated_os_wait", TID: h.TID, StartTicks: h.StartTicks, FromMS: first, ToMS: o.TimeMS,
				Evidence:   "В трёх последовательных наблюдениях поколения потока отмечено состояние ОС D: непрерываемое ожидание.",
				Limitation: "Состояние между наблюдениями неизвестно; это не Java BLOCKED и не доказательство deadlock. Нужно проверить I/O и стек в соответствующем окне."}
		}
	}
	return nil
}
