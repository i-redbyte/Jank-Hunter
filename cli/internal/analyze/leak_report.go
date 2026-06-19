package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	LeakModeLight = "light"
	LeakModeHeap  = "heap"

	LeakDeltaNew       = "new"
	LeakDeltaWorse     = "worse"
	LeakDeltaSame      = "same"
	LeakDeltaBetter    = "better"
	LeakDeltaResolved  = "resolved"
	LeakDeltaNoLeaks   = "no_leaks"
	LeakDeltaMixed     = "mixed"
	LeakDeltaRegressed = "regressed"
	LeakDeltaImproved  = "improved"
)

type LeakReport struct {
	Title     string
	Mode      string
	ModeTitle string
	ModeHint  string
	Verdict   string
	Stats     LeakReportStats
	Items     []LeakReportItem
	Warnings  []string
}

type LeakReportStats struct {
	TotalSuspects       int
	High                int
	Medium              int
	OK                  int
	HeapConfirmed       int
	RuntimeOnly         int
	UnknownHolder       int
	UserOwned           int
	SystemRetained      int
	TotalRetained       uint64
	EstimatedRetainedKB uint64
	MaxAgeMS            uint64
	UniqueClasses       int
	UniqueHolders       int
}

type LeakReportItem struct {
	Fingerprint string
	Suspect     MemoryLeakSuspect
	Rank        int
	Graph       LeakGraph
	PlainText   string
}

type LeakGraph struct {
	Mode        string
	Title       string
	Subtitle    string
	Nodes       []LeakGraphNode
	Edges       []LeakGraphEdge
	RootID      string
	TargetID    string
	HasHeapPath bool
}

type LeakGraphNode struct {
	ID     string
	Label  string
	Detail string
	Kind   string
	Depth  int
}

type LeakGraphEdge struct {
	From  string
	To    string
	Label string
	Kind  string
}

type LeakCompareReport struct {
	Title      string
	Verdict    string
	Confidence string
	Baseline   LeakReport
	Candidate  LeakReport
	Stats      LeakCompareStats
	Deltas     []LeakDelta
	Warnings   []string
}

type LeakCompareStats struct {
	BaselineTotal        int
	CandidateTotal       int
	New                  int
	Worse                int
	Same                 int
	Better               int
	Resolved             int
	HeapConfirmedBefore  int
	HeapConfirmedAfter   int
	BaselineRetained     uint64
	CandidateRetained    uint64
	BaselineEstimatedKB  uint64
	CandidateEstimatedKB uint64
	ChangeLabel          string
	OverallStatus        string
}

type LeakDelta struct {
	Fingerprint       string
	Status            string
	StatusLabel       string
	Severity          string
	HasBaseline       bool
	HasCandidate      bool
	Baseline          MemoryLeakSuspect
	Candidate         MemoryLeakSuspect
	Graph             LeakGraph
	ScoreBefore       float64
	ScoreAfter        float64
	DeltaScore        float64
	CountBefore       uint64
	CountAfter        uint64
	DeltaCount        int64
	AgeBeforeMS       uint64
	AgeAfterMS        uint64
	DeltaAgeMS        int64
	EstimatedBeforeKB uint64
	EstimatedAfterKB  uint64
	DeltaEstimatedKB  int64
	MatchConfidence   string
	Explanation       string
	Recommendation    string
	PlainText         string
}

func BuildLeakReport(summary Summary) LeakReport {
	items := make([]LeakReportItem, 0, len(summary.MemoryLeaks))
	stats := LeakReportStats{}
	classSeen := map[string]struct{}{}
	holderSeen := map[string]struct{}{}
	mode := LeakModeLight
	for index, suspect := range summary.MemoryLeaks {
		if suspect.HeapEvidence {
			mode = LeakModeHeap
		}
		stats.TotalSuspects++
		stats.TotalRetained += suspect.Count
		stats.EstimatedRetainedKB += suspect.EstimatedRetainedKB
		if suspect.MaxAgeMS > stats.MaxAgeMS {
			stats.MaxAgeMS = suspect.MaxAgeMS
		}
		switch suspect.Severity {
		case "high":
			stats.High++
		case "medium":
			stats.Medium++
		default:
			stats.OK++
		}
		if suspect.HeapEvidence {
			stats.HeapConfirmed++
		} else {
			stats.RuntimeOnly++
		}
		if suspect.Holder == "" || suspect.Holder == "не определен" || suspect.Holder == "unknown" {
			stats.UnknownHolder++
		}
		if suspect.UserOwned {
			stats.UserOwned++
		}
		if suspect.SystemRetained {
			stats.SystemRetained++
		}
		if suspect.ClassName != "" {
			classSeen[strings.ToLower(suspect.ClassName)] = struct{}{}
		}
		if suspect.Holder != "" {
			holderSeen[strings.ToLower(suspect.Holder)] = struct{}{}
		}
		items = append(items, LeakReportItem{
			Fingerprint: LeakFingerprint(suspect),
			Suspect:     suspect,
			Rank:        index + 1,
			Graph:       BuildLeakGraph(suspect),
			PlainText:   leakPlainText(suspect),
		})
	}
	stats.UniqueClasses = len(classSeen)
	stats.UniqueHolders = len(holderSeen)
	report := LeakReport{
		Title:    "Отчет по утечкам памяти",
		Mode:     mode,
		Verdict:  leakReportVerdict(stats),
		Stats:    stats,
		Items:    items,
		Warnings: leakWarnings(summary.Warnings),
	}
	if mode == LeakModeHeap {
		report.ModeTitle = "Heap mode"
		report.ModeHint = "Есть HPROF/heap evidence: отчет показывает подтвержденные пути от GC root, поля-ссылки, retained size и граф цепочки удержания."
	} else {
		report.ModeTitle = "Light mode"
		report.ModeHint = "Heap dump не передан: отчет показывает retained-сигналы runtime, ownerHint/flow-контекст и вероятную цепочку. Точный GC root и поле ссылки появятся после --heap-dump или --heap-evidence."
	}
	return report
}

func BuildLeakCompareReport(comparison Comparison) LeakCompareReport {
	baseline := BuildLeakReport(comparison.Baseline)
	candidate := BuildLeakReport(comparison.Candidate)
	deltas := CompareLeakSuspects(comparison.Baseline.MemoryLeaks, comparison.Candidate.MemoryLeaks)
	stats := LeakCompareStats{
		BaselineTotal:        len(comparison.Baseline.MemoryLeaks),
		CandidateTotal:       len(comparison.Candidate.MemoryLeaks),
		HeapConfirmedBefore:  baseline.Stats.HeapConfirmed,
		HeapConfirmedAfter:   candidate.Stats.HeapConfirmed,
		BaselineRetained:     comparison.Baseline.Retained,
		CandidateRetained:    comparison.Candidate.Retained,
		BaselineEstimatedKB:  baseline.Stats.EstimatedRetainedKB,
		CandidateEstimatedKB: candidate.Stats.EstimatedRetainedKB,
	}
	for _, delta := range deltas {
		switch delta.Status {
		case LeakDeltaNew:
			stats.New++
		case LeakDeltaWorse:
			stats.Worse++
		case LeakDeltaSame:
			stats.Same++
		case LeakDeltaBetter:
			stats.Better++
		case LeakDeltaResolved:
			stats.Resolved++
		}
	}
	stats.ChangeLabel = leakCompareChangeLabel(stats)
	stats.OverallStatus = leakCompareOverallStatus(stats)
	return LeakCompareReport{
		Title:      "Сравнение утечек памяти",
		Verdict:    leakCompareVerdict(stats, comparison.Confidence()),
		Confidence: comparison.Confidence(),
		Baseline:   baseline,
		Candidate:  candidate,
		Stats:      stats,
		Deltas:     deltas,
		Warnings:   comparison.Warnings,
	}
}

func CompareLeakSuspects(baseline, candidate []MemoryLeakSuspect) []LeakDelta {
	baselineByKey := map[string]MemoryLeakSuspect{}
	candidateByKey := map[string]MemoryLeakSuspect{}
	keys := map[string]struct{}{}
	for _, row := range baseline {
		key := LeakFingerprint(row)
		baselineByKey[key] = row
		keys[key] = struct{}{}
	}
	for _, row := range candidate {
		key := LeakFingerprint(row)
		candidateByKey[key] = row
		keys[key] = struct{}{}
	}
	out := make([]LeakDelta, 0, len(keys))
	for key := range keys {
		before, hasBefore := baselineByKey[key]
		after, hasAfter := candidateByKey[key]
		out = append(out, buildLeakDelta(key, before, hasBefore, after, hasAfter))
	}
	sort.Slice(out, func(i, j int) bool {
		leftRank := leakDeltaStatusRank(out[i].Status)
		rightRank := leakDeltaStatusRank(out[j].Status)
		if leftRank == rightRank {
			if out[i].Severity == out[j].Severity {
				if out[i].DeltaScore == out[j].DeltaScore {
					return out[i].ScoreAfter > out[j].ScoreAfter
				}
				return out[i].DeltaScore > out[j].DeltaScore
			}
			return leakSeverityRank(out[i].Severity) > leakSeverityRank(out[j].Severity)
		}
		return leftRank > rightRank
	})
	return out
}

func BuildLeakGraph(suspect MemoryLeakSuspect) LeakGraph {
	if len(suspect.ReferencePath) > 0 {
		return heapLeakGraph(suspect)
	}
	return runtimeLeakGraph(suspect)
}

func LeakFingerprint(suspect MemoryLeakSuspect) string {
	if suspect.ChainFingerprint != "" {
		return "chain\x00" + suspect.ChainFingerprint
	}
	if len(suspect.ReferencePath) > 0 {
		return "path\x00" + heapChainFingerprint(
			suspect.ClassName,
			suspect.Holder,
			suspect.HolderField,
			suspect.GCRootCategory,
			suspect.ReferencePath,
		)
	}
	parts := []string{
		strings.ToLower(strings.TrimSpace(suspect.ClassName)),
		strings.ToLower(strings.TrimSpace(suspect.Holder)),
		strings.ToLower(strings.TrimSpace(suspect.Screen)),
		strings.ToLower(strings.TrimSpace(suspect.Flow)),
		strings.ToLower(strings.TrimSpace(suspect.Step)),
	}
	return strings.Join(parts, "\x00")
}

func (comparison Comparison) Confidence() string {
	if len(comparison.Deltas) > 0 && comparison.Deltas[0].Confidence != "" {
		return comparison.Deltas[0].Confidence
	}
	return confidence(comparison.Baseline, comparison.Candidate)
}

func buildLeakDelta(key string, before MemoryLeakSuspect, hasBefore bool, after MemoryLeakSuspect, hasAfter bool) LeakDelta {
	status := LeakDeltaSame
	severity := "ok"
	scoreAfter := float64(0)
	scoreBefore := float64(0)
	countAfter := uint64(0)
	countBefore := uint64(0)
	ageAfter := uint64(0)
	ageBefore := uint64(0)
	sizeAfter := uint64(0)
	sizeBefore := uint64(0)
	var graph LeakGraph
	switch {
	case hasBefore && !hasAfter:
		status = LeakDeltaResolved
		severity = "ok"
		scoreBefore = before.Score
		countBefore = before.Count
		ageBefore = before.MaxAgeMS
		sizeBefore = before.EstimatedRetainedKB
		graph = BuildLeakGraph(before)
	case !hasBefore && hasAfter:
		status = LeakDeltaNew
		severity = maxLeakSeverity(after.Severity, "medium")
		scoreAfter = after.Score
		countAfter = after.Count
		ageAfter = after.MaxAgeMS
		sizeAfter = after.EstimatedRetainedKB
		graph = BuildLeakGraph(after)
	default:
		scoreBefore = before.Score
		scoreAfter = after.Score
		countBefore = before.Count
		countAfter = after.Count
		ageBefore = before.MaxAgeMS
		ageAfter = after.MaxAgeMS
		sizeBefore = before.EstimatedRetainedKB
		sizeAfter = after.EstimatedRetainedKB
		deltaScore := scoreAfter - scoreBefore
		switch {
		case deltaScore >= 8 || countAfter >= countBefore+5 || ageAfter >= ageBefore+30_000 || sizeAfter >= sizeBefore+8*1024:
			status = LeakDeltaWorse
			severity = "high"
		case deltaScore >= 3 || countAfter > countBefore || ageAfter > ageBefore || sizeAfter > sizeBefore:
			status = LeakDeltaWorse
			severity = maxLeakSeverity(after.Severity, "medium")
		case deltaScore <= -3 || countAfter < countBefore || ageAfter < ageBefore || sizeAfter < sizeBefore:
			status = LeakDeltaBetter
			severity = "ok"
		default:
			status = LeakDeltaSame
			severity = after.Severity
		}
		graph = BuildLeakGraph(after)
	}
	delta := LeakDelta{
		Fingerprint:       key,
		Status:            status,
		StatusLabel:       leakDeltaStatusLabel(status),
		Severity:          severity,
		HasBaseline:       hasBefore,
		HasCandidate:      hasAfter,
		Baseline:          before,
		Candidate:         after,
		Graph:             graph,
		ScoreBefore:       scoreBefore,
		ScoreAfter:        scoreAfter,
		DeltaScore:        math.Round((scoreAfter-scoreBefore)*10) / 10,
		CountBefore:       countBefore,
		CountAfter:        countAfter,
		DeltaCount:        int64(countAfter) - int64(countBefore),
		AgeBeforeMS:       ageBefore,
		AgeAfterMS:        ageAfter,
		DeltaAgeMS:        int64(ageAfter) - int64(ageBefore),
		EstimatedBeforeKB: sizeBefore,
		EstimatedAfterKB:  sizeAfter,
		DeltaEstimatedKB:  int64(sizeAfter) - int64(sizeBefore),
		MatchConfidence:   leakMatchConfidence(before, hasBefore, after, hasAfter),
	}
	delta.Explanation = leakDeltaExplanation(delta)
	delta.Recommendation = leakDeltaRecommendation(delta)
	delta.PlainText = leakDeltaPlainText(delta)
	return delta
}

func leakMatchConfidence(before MemoryLeakSuspect, hasBefore bool, after MemoryLeakSuspect, hasAfter bool) string {
	if !hasBefore || !hasAfter {
		if (hasBefore && before.HeapEvidence) || (hasAfter && after.HeapEvidence) {
			return "medium: fingerprint heap-цепочки уникален для одной стороны"
		}
		return "medium: fingerprint найден только в одной версии"
	}
	if before.ChainFingerprint != "" && before.ChainFingerprint == after.ChainFingerprint {
		if before.HeapEvidence || after.HeapEvidence {
			return "high: совпал нормализованный heap-chain fingerprint"
		}
		return "high: совпал нормализованный runtime-chain fingerprint"
	}
	if before.ClassName == after.ClassName && before.Holder != "" && before.Holder == after.Holder {
		return "medium: совпали class и holder"
	}
	if before.ClassName == after.ClassName {
		return "low: совпал только class, уточните ownerHint или heap evidence"
	}
	return "low: сопоставление построено по fallback fingerprint"
}

func heapLeakGraph(suspect MemoryLeakSuspect) LeakGraph {
	nodes := make([]LeakGraphNode, 0, len(suspect.ReferencePath)+len(suspect.RetainedClassSample))
	edges := make([]LeakGraphEdge, 0, len(suspect.ReferencePath)+len(suspect.RetainedClassSample))
	seen := map[string]struct{}{}
	var prevID string
	rootID := ""
	targetID := ""
	for index, step := range suspect.ReferencePath {
		id := step.ObjectID
		if id == "" {
			id = fmt.Sprintf("path-%d", index)
		}
		kind := leakGraphKind(step.Kind, step.ClassName, suspect.ClassName)
		label := firstNonEmpty(step.ClassName, "object")
		detail := step.FieldName
		if detail == "" {
			detail = step.Kind
		}
		if kind == "root" && suspect.GCRootCategory != "" {
			detail = suspect.GCRootCategory
		}
		if _, ok := seen[id]; !ok {
			nodes = append(nodes, LeakGraphNode{ID: id, Label: label, Detail: detail, Kind: kind, Depth: index})
			seen[id] = struct{}{}
		}
		if index == 0 {
			rootID = id
		}
		if strings.TrimPrefix(step.ClassName, "GC root: ") == suspect.ClassName || step.ClassName == suspect.ClassName {
			targetID = id
		}
		if prevID != "" {
			edges = append(edges, LeakGraphEdge{
				From:  prevID,
				To:    id,
				Label: firstNonEmpty(step.FieldName, step.Kind, "ref"),
				Kind:  step.Kind,
			})
		}
		prevID = id
	}
	if targetID == "" && len(nodes) > 0 {
		targetID = nodes[len(nodes)-1].ID
	}
	for index, retained := range suspect.RetainedClassSample {
		id := fmt.Sprintf("retained-%d", index)
		nodes = append(nodes, LeakGraphNode{
			ID:     id,
			Label:  retained,
			Detail: "объект удерживается доминатором",
			Kind:   "retained",
			Depth:  len(suspect.ReferencePath) + 1,
		})
		if targetID != "" {
			edges = append(edges, LeakGraphEdge{From: targetID, To: id, Label: "retains", Kind: "retained"})
		}
	}
	return LeakGraph{
		Mode:        LeakModeHeap,
		Title:       "Подтвержденная цепочка ссылок",
		Subtitle:    "Путь построен из heap dump: от GC root к удержанному объекту, затем sample доминируемых классов.",
		Nodes:       nodes,
		Edges:       edges,
		RootID:      rootID,
		TargetID:    targetID,
		HasHeapPath: true,
	}
}

func runtimeLeakGraph(suspect MemoryLeakSuspect) LeakGraph {
	labels := suspect.DominatorPath
	if len(labels) == 0 {
		labels = []string{"runtime retained signal", "удержанный объект: " + suspect.ClassName}
	}
	nodes := make([]LeakGraphNode, 0, len(labels))
	edges := make([]LeakGraphEdge, 0, len(labels)-1)
	for index, label := range labels {
		id := fmt.Sprintf("runtime-%d", index)
		kind := "context"
		switch {
		case strings.HasPrefix(label, "экран:"):
			kind = "screen"
		case strings.HasPrefix(label, "флоу:"):
			kind = "flow"
		case strings.HasPrefix(label, "шаг:"):
			kind = "step"
		case strings.HasPrefix(label, "держатель:") || strings.HasPrefix(label, "метод:"):
			kind = "holder"
		case strings.HasPrefix(label, "удержанный объект:"):
			kind = "target"
		}
		nodes = append(nodes, LeakGraphNode{ID: id, Label: label, Detail: "runtime signal", Kind: kind, Depth: index})
		if index > 0 {
			edges = append(edges, LeakGraphEdge{From: fmt.Sprintf("runtime-%d", index-1), To: id, Label: "context", Kind: "runtime"})
		}
	}
	return LeakGraph{
		Mode:     LeakModeLight,
		Title:    "Вероятная цепочка удержания",
		Subtitle: "Heap dump не передан: граф показывает runtime-контекст и ownerHint, а не точные ссылки объектов.",
		Nodes:    nodes,
		Edges:    edges,
		RootID:   "runtime-0",
		TargetID: fmt.Sprintf("runtime-%d", len(nodes)-1),
	}
}

func leakGraphKind(kind, className, targetClass string) string {
	switch {
	case kind == "gc_root" || strings.HasPrefix(className, "GC root: "):
		return "root"
	case className == targetClass:
		return "target"
	case isLikelyAppClass(strings.TrimPrefix(className, "GC root: ")):
		return "app"
	case isLikelySystemClass(className):
		return "system"
	default:
		return "library"
	}
}

func leakReportVerdict(stats LeakReportStats) string {
	switch {
	case stats.TotalSuspects == 0:
		return "Подозрений на утечки памяти нет."
	case stats.High > 0:
		return fmt.Sprintf("Найдено %d подозрений, из них %d высокого риска. Начните с строк, где есть Activity/Fragment/Context, heap evidence или большой возраст удержания.", stats.TotalSuspects, stats.High)
	case stats.Medium > 0:
		return fmt.Sprintf("Найдено %d подозрений, явных критичных утечек нет. Проверьте повторяемые удержания и добавьте heap dump для точной цепочки.", stats.TotalSuspects)
	default:
		return fmt.Sprintf("Найдено %d слабых сигналов удержания. Это стоит мониторить, но без роста возраста/количества риск низкий.", stats.TotalSuspects)
	}
}

func leakWarnings(warnings []string) []string {
	out := make([]string, 0, len(warnings))
	seen := map[string]struct{}{}
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		lower := strings.ToLower(warning)
		if !strings.Contains(lower, "heap") && !strings.Contains(lower, "hprof") && !strings.Contains(lower, "retained") && !strings.Contains(lower, "памят") {
			continue
		}
		if _, ok := seen[warning]; ok {
			continue
		}
		seen[warning] = struct{}{}
		out = append(out, warning)
	}
	return out
}

func leakCompareChangeLabel(stats LeakCompareStats) string {
	switch {
	case stats.BaselineTotal == 0 && stats.CandidateTotal == 0:
		return "утечек не обнаружено ни в базе, ни в кандидате"
	case stats.CandidateTotal > stats.BaselineTotal:
		return "в новой версии обнаружено больше утечек"
	case stats.CandidateTotal < stats.BaselineTotal:
		return "в новой версии обнаружено меньше утечек"
	default:
		return "количество утечек такое же"
	}
}

func leakCompareOverallStatus(stats LeakCompareStats) string {
	switch {
	case stats.BaselineTotal == 0 && stats.CandidateTotal == 0:
		return LeakDeltaNoLeaks
	case stats.New > 0 || stats.Worse > 0:
		return LeakDeltaRegressed
	case stats.Resolved > 0 || stats.Better > 0:
		return LeakDeltaImproved
	case stats.CandidateTotal == stats.BaselineTotal:
		return LeakDeltaSame
	default:
		return LeakDeltaMixed
	}
}

func leakCompareVerdict(stats LeakCompareStats, confidence string) string {
	base := fmt.Sprintf("%s: база %d, кандидат %d.", stats.ChangeLabel, stats.BaselineTotal, stats.CandidateTotal)
	if stats.New > 0 || stats.Worse > 0 {
		base += fmt.Sprintf(" Регрессии: новых %d, усилившихся %d.", stats.New, stats.Worse)
	}
	if stats.Resolved > 0 || stats.Better > 0 {
		base += fmt.Sprintf(" Улучшения: исчезло %d, стало легче %d.", stats.Resolved, stats.Better)
	}
	if confidence != "" {
		base += " Доверие сравнения: " + confidence + "."
	}
	return base
}

func leakDeltaStatusLabel(status string) string {
	switch status {
	case LeakDeltaNew:
		return "новая утечка"
	case LeakDeltaWorse:
		return "стало хуже"
	case LeakDeltaBetter:
		return "стало легче"
	case LeakDeltaResolved:
		return "исчезла"
	default:
		return "без сильного изменения"
	}
}

func leakDeltaStatusRank(status string) int {
	switch status {
	case LeakDeltaNew:
		return 5
	case LeakDeltaWorse:
		return 4
	case LeakDeltaBetter:
		return 3
	case LeakDeltaResolved:
		return 2
	default:
		return 1
	}
}

func leakDeltaExplanation(delta LeakDelta) string {
	switch delta.Status {
	case LeakDeltaNew:
		return fmt.Sprintf("В baseline такой fingerprint не встречался, а в candidate удержан %s. Это вероятная новая lifecycle-регрессия.", delta.Candidate.ClassName)
	case LeakDeltaWorse:
		return fmt.Sprintf("Подозрение уже было в baseline, но стало сильнее: score %+0.1f, count %+d, возраст %+d мс, размер %+d KB.", delta.DeltaScore, delta.DeltaCount, delta.DeltaAgeMS, delta.DeltaEstimatedKB)
	case LeakDeltaBetter:
		return fmt.Sprintf("Сигнал стал слабее: score %+0.1f, count %+d, возраст %+d мс, размер %+d KB.", delta.DeltaScore, delta.DeltaCount, delta.DeltaAgeMS, delta.DeltaEstimatedKB)
	case LeakDeltaResolved:
		return fmt.Sprintf("В candidate этот fingerprint не найден. Если сценарий и когорты совпадают, утечка %s, вероятно, исправлена.", delta.Baseline.ClassName)
	default:
		return "Сигнал остался примерно на том же уровне: проверьте, что сценарии baseline/candidate действительно одинаковые."
	}
}

func leakDeltaRecommendation(delta LeakDelta) string {
	if delta.HasCandidate {
		return delta.Candidate.Recommendation
	}
	return "Сохраните этот fingerprint в regressions watchlist: если он вернется в candidate, проверяйте тот же holder/flow и lifecycle cleanup."
}

func leakPlainText(suspect MemoryLeakSuspect) string {
	return strings.Join([]string{
		suspect.ClassName,
		suspect.Holder,
		suspect.Screen,
		suspect.Flow,
		suspect.Step,
		suspect.GCRoot,
		suspect.GCRootCategory,
		suspect.HolderField,
		suspect.ChainFingerprint,
		suspect.Impact,
		suspect.Recommendation,
		suspect.Evidence,
		strings.Join(suspect.AlternativePathSummaries, " "),
		strings.Join(suspect.InvestigationSteps, " "),
		strings.Join(suspect.FixExamples, " "),
		strings.Join(suspect.VerificationSteps, " "),
	}, " ")
}

func leakDeltaPlainText(delta LeakDelta) string {
	return strings.Join([]string{
		delta.StatusLabel,
		leakPlainText(delta.Baseline),
		leakPlainText(delta.Candidate),
		delta.Explanation,
		delta.Recommendation,
		delta.MatchConfidence,
	}, " ")
}

func leakSeverityRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	case "ok":
		return 1
	default:
		return 0
	}
}

func maxLeakSeverity(left, right string) string {
	if leakSeverityRank(left) >= leakSeverityRank(right) {
		return left
	}
	return right
}
