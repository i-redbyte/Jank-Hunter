package analyze

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type WorkerRegistryComparison struct {
	Process                                   string
	BaselineKnown, CandidateKnown, Comparable bool
	BaselineStates, CandidateStates           [6]uint64
	Schedules                                 []WorkerScheduleComparison
	Note                                      string
}
type WorkerScheduleComparison struct {
	Configuration       string
	Baseline, Candidate uint64
}

func compareWorkerRegistries(before, after *WorkRegistryAnalysis, coverage bool) []WorkerRegistryComparison {
	byProcess := func(a *WorkRegistryAnalysis) map[string][]WorkRegistrySnapshot {
		result := make(map[string][]WorkRegistrySnapshot)
		latest := make(map[string]WorkRegistrySnapshot)
		for _, s := range comparisonRegistrySummary(a).safeSnapshots() {
			key := registryProcessInstanceKey(s.Scope)
			if previous, ok := latest[key]; !ok || s.ObservedElapsedMS > previous.ObservedElapsedMS {
				latest[key] = s
			}
		}
		for _, s := range latest {
			result[s.Process] = append(result[s.Process], s)
		}
		return result
	}
	left, right := byProcess(before), byProcess(after)
	names := map[string]bool{}
	for name := range left {
		names[name] = true
	}
	for name := range right {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	var rows []WorkerRegistryComparison
	for _, name := range sorted {
		row := WorkerRegistryComparison{Process: name, Note: "Сопоставление по имени процесса и распределению заявленных расписаний/ограничений; это не соответствие отдельных UUID. Класс и unique policy из WorkInfo неизвестны."}
		valid := func(s []WorkRegistrySnapshot) bool { return len(s) == 1 && s[0].Unavailable == 0 }
		row.BaselineKnown, row.CandidateKnown = valid(left[name]), valid(right[name])
		if row.BaselineKnown {
			row.Note += fmt.Sprintf(" База: сохранено %d из %d строк ответа API.", len(left[name][0].Jobs), left[name][0].TotalObserved)
		}
		if row.CandidateKnown {
			row.Note += fmt.Sprintf(" Проверяемый прогон: сохранено %d из %d строк ответа API.", len(right[name][0].Jobs), right[name][0].TotalObserved)
		}
		fresh := func(s WorkRegistrySnapshot) bool {
			if !s.Complete || s.AgeAtCaptureMS == nil || s.IntervalMS == 0 || *s.AgeAtCaptureMS > s.IntervalMS {
				return false
			}
			for _, job := range s.Jobs {
				if job.OmittedContentTriggers > 0 || job.TruncatedFields > 0 || job.Constraints&workConstraintCustomNetwork != 0 {
					return false
				}
			}
			return true
		}
		row.Comparable = coverage && name != "" && name != "unknown" && row.BaselineKnown && row.CandidateKnown && fresh(left[name][0]) && fresh(right[name][0]) && before.DroppedPackets == 0 && after.DroppedPackets == 0
		if len(left[name]) > 1 || len(right[name]) > 1 {
			row.Note += " Несколько экземпляров или неподтверждённых сессий процесса: соответствие неоднозначно."
		}
		if !row.Comparable {
			row.Note += " Дельта недоступна: неполный, устаревший или несопоставимый охват."
		}
		profiles := make(map[string]*WorkerScheduleComparison)
		add := func(s WorkRegistrySnapshot, baseline bool) {
			for _, job := range s.Jobs {
				if job.State < 6 {
					if baseline {
						row.BaselineStates[job.State]++
					} else {
						row.CandidateStates[job.State]++
					}
				}
				key := workerScheduleKey(job)
				p := profiles[key]
				if p == nil {
					p = &WorkerScheduleComparison{Configuration: workerScheduleDescription(job)}
					profiles[key] = p
				}
				if baseline {
					p.Baseline++
				} else {
					p.Candidate++
				}
			}
		}
		if row.BaselineKnown {
			add(left[name][0], true)
		}
		if row.CandidateKnown {
			add(right[name][0], false)
		}
		keys := make([]string, 0, len(profiles))
		for key := range profiles {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			row.Schedules = append(row.Schedules, *profiles[key])
		}
		rows = append(rows, row)
	}
	return rows
}
func (a *WorkRegistryAnalysis) safeSnapshots() []WorkRegistrySnapshot {
	if a == nil {
		return nil
	}
	return a.Snapshots
}

const workConstraintCustomNetwork = uint64(1 << 5)

func workerScheduleKey(j jhlog.WorkRegistryJob) string {
	triggers := append([]string(nil), j.ContentTriggers...)
	sort.Strings(triggers)
	// Primitive fields cannot fail JSON encoding; length-delimited strings prevent URI-list collisions.
	encoded, _ := json.Marshal(struct {
		Delay        uint64
		Period, Flex *uint64
		Network      string
		Mask         uint64
		Triggers     []string
	}{j.InitialDelayMS, j.PeriodMS, j.FlexMS, j.NetworkType, j.Constraints, triggers})
	return string(encoded)
}
func workerScheduleDescription(j jhlog.WorkRegistryJob) string {
	period := "одноразовое"
	if j.PeriodMS != nil && j.FlexMS != nil {
		period = fmt.Sprintf("период %d / flex %d мс", *j.PeriodMS, *j.FlexMS)
	}
	names := []string{"зарядка", "батарея не разряжена", "место доступно", "простой устройства", "триггер URI", "особый запрос сети (детали неизвестны)"}
	var constraints []string
	for bit, name := range names {
		if j.Constraints&(1<<bit) != 0 {
			constraints = append(constraints, name)
		}
	}
	triggers := append([]string(nil), j.ContentTriggers...)
	sort.Strings(triggers)
	return fmt.Sprintf("%s; задержка %d мс; сеть %s; %s; URI %q", period, j.InitialDelayMS, j.NetworkType, strings.Join(constraints, ", "), triggers)
}

// Sessions from an SDK restart share verified nonzero run/process words; process deaths do not.
func registryProcessInstanceKey(scope string) string {
	raw, err := hex.DecodeString(scope)
	if err != nil || len(raw) != 48 {
		return "session:" + scope
	}
	var run, process jhlog.ID128
	copy(run[:], raw[:16])
	copy(process[:], raw[16:32])
	if run.IsZero() || process.IsZero() {
		return "session:" + scope
	}
	return "instance:" + scope[:64]
}
