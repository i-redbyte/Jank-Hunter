package report

import (
	"encoding/json"
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"html/template"
	"sort"
	"strings"
)

func threadAnalysisJSON(a *analyze.ThreadAnalysis) (template.JS, error) {
	b, err := json.Marshal(a)
	return template.JS(b), err // encoding/json escapes HTML, including closing script tags.
}

type threadComparisonRow struct {
	Process, Baseline, Candidate, Explanation string
	Comparable                                bool
}

func threadComparison(c analyze.Comparison) []threadComparisonRow {
	left, right := map[string][]analyze.ThreadProcessAnalysis{}, map[string][]analyze.ThreadProcessAnalysis{}
	if c.Baseline.ThreadAnalysis != nil {
		for _, p := range c.Baseline.ThreadAnalysis.Processes {
			left[p.Process] = append(left[p.Process], p)
		}
	}
	if c.Candidate.ThreadAnalysis != nil {
		for _, p := range c.Candidate.ThreadAnalysis.Processes {
			right[p.Process] = append(right[p.Process], p)
		}
	}
	names := map[string]bool{}
	for k := range left {
		names[k] = true
	}
	for k := range right {
		names[k] = true
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]threadComparisonRow, 0, len(keys))
	describe := func(p []analyze.ThreadProcessAnalysis) string {
		if len(p) == 0 {
			return "нет наблюдений"
		}
		if len(p) != 1 {
			return "несколько сессий: неоднозначное соответствие"
		}
		v := p[0]
		description := fmt.Sprintf("старт %d · пик %d · последний %d · появлений %d · исчезновений %d · неполных снимков %d", v.StartCount, v.PeakCount, v.LastCount, v.Appearances, v.Disappearances, v.PartialSnapshots)
		if d := v.Dynamics; d != nil {
			description += fmt.Sprintf(" · окно %d мс · максимальный прирост между снимками %d", d.WindowMS, d.MaxObservedRise)
			if d.AppearancesPerMinute != nil {
				description += fmt.Sprintf(" · появлений/мин %.2f", *d.AppearancesPerMinute)
			}
			if d.MeanObservedThreadCorePercent != nil {
				description += fmt.Sprintf(" · средняя нагрузка наблюдаемого потока %.2f%% ядра (%d дельт)", *d.MeanObservedThreadCorePercent, d.CPUSamples)
			} else {
				description += " · CPU неизвестен"
			}
			names := make([]string, 0, 20)
			for _, n := range d.LastNames {
				if len(names) == 20 {
					break
				}
				names = append(names, fmt.Sprintf("%s: %d", n.Name, n.Count))
			}
			description += " · имена в последнем снимке (не владельцы пулов): " + strings.Join(names, ", ")
			if len(d.LastNames)+d.OmittedNames > len(names) {
				description += fmt.Sprintf("; ещё групп: %d", len(d.LastNames)+d.OmittedNames-len(names))
			}
		}
		return description
	}
	for _, name := range keys {
		row := threadComparisonRow{Process: name, Baseline: describe(left[name]), Candidate: describe(right[name]), Explanation: "Сопоставление по имени процесса. TID и поколение не сопоставляются между запусками."}
		if len(left[name]) == 1 && len(right[name]) == 1 {
			a, b := left[name][0], right[name][0]
			clockOrdered := a.LastAtMS >= a.StartAtMS && b.LastAtMS >= b.StartAtMS
			row.Comparable = clockOrdered && threadComparisonScope(c) && threadSamplingInterval(a) != 0 && threadSamplingInterval(a) == threadSamplingInterval(b) && a.PartialSnapshots == 0 && b.PartialSnapshots == 0 && c.Baseline.ThreadAnalysis.DroppedSnapshots == 0 && c.Candidate.ThreadAnalysis.DroppedSnapshots == 0
			if row.Comparable {
				row.Explanation += fmt.Sprintf(" Изменение старта %+d, пика %+d, последнего %+d. Окна наблюдения %d → %d мс; окна разной длительности дают разную возможность наблюдать пик и появления. Имена объединены только по точному тексту; принадлежность пулу и идентичность потока между запусками не установлены.", b.StartCount-a.StartCount, b.PeakCount-a.PeakCount, b.LastCount-a.LastCount, a.LastAtMS-a.StartAtMS, b.LastAtMS-b.StartAtMS)
				if a.Dynamics != nil && b.Dynamics != nil && a.Dynamics.AppearancesPerMinute != nil && b.Dynamics.AppearancesPerMinute != nil {
					row.Explanation += fmt.Sprintf(" Нормированное изменение появлений/мин %+.2f по наблюдаемым окнам.", *b.Dynamics.AppearancesPerMinute-*a.Dynamics.AppearancesPerMinute)
				}
				if a.Dynamics != nil && b.Dynamics != nil && a.Dynamics.CPUSamples > 0 && b.Dynamics.CPUSamples > 0 &&
					a.Dynamics.MeanObservedThreadCorePercent != nil && b.Dynamics.MeanObservedThreadCorePercent != nil {
					row.Explanation += fmt.Sprintf(" Измеренное изменение средней нагрузки %+.2f п.п. одного ядра (%d → %d дельт); состав наблюдавшихся потоков может различаться.",
						*b.Dynamics.MeanObservedThreadCorePercent-*a.Dynamics.MeanObservedThreadCorePercent,
						a.Dynamics.CPUSamples, b.Dynamics.CPUSamples)
				}
			} else if !clockOrdered {
				row.Explanation += " Временная ось наблюдений идёт назад; окно и числовая дельта недоступны."
			} else {
				row.Explanation += " Соответствие сценариев, область процессов или полный одинаковый охват не подтверждены: дельта недоступна."
			}
		} else {
			row.Explanation += " Дельта недоступна."
		}
		rows = append(rows, row)
	}
	return rows
}

// A changing or absent collection interval cannot establish comparable coverage.
func threadSamplingInterval(p analyze.ThreadProcessAnalysis) uint64 {
	return analyze.ThreadSamplingInterval(p)
}

func threadComparisonScope(c analyze.Comparison) bool {
	if c.Scope.Comparability != analyze.ScenarioFull {
		return false
	}
	a, b := c.Baseline.CollectionQuality, c.Candidate.CollectionQuality
	return a.Complete && b.Complete && a.ProcessRosterComplete && b.ProcessRosterComplete && a.ProcessScope != "" && a.ProcessScope == b.ProcessScope && a.ProcessScopeFingerprint != "" && a.ProcessScopeFingerprint == b.ProcessScopeFingerprint
}

func threadDynamicsCPU(d *analyze.ThreadDynamics) string {
	if d == nil || d.MeanObservedThreadCorePercent == nil {
		return "CPU-время и средняя нагрузка неизвестны: нет допустимых пар наблюдений."
	}
	return fmt.Sprintf("Измеренное CPU-время: %.1f мс в %.1f поток-мс наблюдений. Средняя нагрузка наблюдаемого потока: %.2f%% одного ядра.", d.CPUTimeMS, d.ObservedThreadTimeMS, *d.MeanObservedThreadCorePercent)
}

func threadLifecycleCounts(a *analyze.ThreadAnalysis) string {
	if a == nil || !a.LifecycleObserved {
		return "неизвестно: точечный охват не подтверждён"
	}
	return fmt.Sprintf("run %d · start %d · factory %d · отброшено CLI %d", a.RunObservations, a.StartReturns, a.FactoryReturns, a.DroppedLifecycle)
}
