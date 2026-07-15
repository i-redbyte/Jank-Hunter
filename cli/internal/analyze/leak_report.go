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
	TimeOnly            int
	AfterExplicitGC     int
	UnconfirmedHPROF    int
	DegradedQuality     int
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
		switch suspect.EvidenceKind {
		case RetentionEvidenceAfterExplicitGC:
			stats.AfterExplicitGC++
		case RetentionEvidenceUnconfirmedHPROF:
			stats.UnconfirmedHPROF++
		case RetentionEvidenceTimeOnly, "":
			stats.TimeOnly++
		}
		if suspect.DataQuality == "degraded" {
			stats.DegradedQuality++
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
	warnings := leakWarnings(summary.Warnings)
	for _, item := range items {
		for _, warning := range item.Suspect.QualityWarnings {
			warnings = append(warnings, fmt.Sprintf("Качество сигнала %s: %s.", item.Suspect.ClassName, warning))
		}
	}
	warnings = uniqueStrings(warnings)
	report := LeakReport{
		Title:    "Удержания и возможные утечки памяти",
		Mode:     mode,
		Verdict:  leakReportVerdict(stats),
		Stats:    stats,
		Items:    items,
		Warnings: warnings,
	}
	if mode == LeakModeHeap {
		report.ModeTitle = "Подтвержденные пути HPROF"
		report.ModeHint = "Для части объектов HPROF подтвердил путь от распознанного корня GC. Это доказательство удержания в момент дампа, но окончательный диагноз утечки требует проверки ожидаемого жизненного цикла."
	} else {
		report.ModeTitle = "Сигналы достижимости"
		report.ModeHint = "Подтвержденного пути HPROF нет. time_only означает только жизнь после задержки; after_explicit_gc — жизнь после запрошенного GC. Оба уровня являются сигналами для проверки, а не доказательством утечки."
	}
	return report
}

func BuildLeakCompareReport(comparison Comparison) LeakCompareReport {
	baseline := BuildLeakReport(comparison.Baseline)
	candidate := BuildLeakReport(comparison.Candidate)
	deltas := CompareLeakSuspects(comparison.Baseline.MemoryLeaks, comparison.Candidate.MemoryLeaks)
	comparisonConfidence := retentionComparisonConfidence(comparison.Confidence(), baseline, candidate)
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
		Verdict:    leakCompareVerdict(stats, comparisonConfidence),
		Confidence: comparisonConfidence,
		Baseline:   baseline,
		Candidate:  candidate,
		Stats:      stats,
		Deltas:     deltas,
		Warnings: uniqueStrings(append(
			append(append([]string(nil), comparison.Warnings...), baseline.Warnings...),
			candidate.Warnings...,
		)),
	}
}

func retentionComparisonConfidence(global string, reports ...LeakReport) string {
	rank := confidenceRank(global)
	for _, report := range reports {
		if report.Stats.DegradedQuality > 0 {
			rank = 1
			continue
		}
		if report.Stats.TimeOnly > 0 && report.Stats.HeapConfirmed == 0 && rank > 2 {
			rank = 2
		}
	}
	switch rank {
	case 3:
		return "high"
	case 2:
		return "medium"
	default:
		return "low"
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
	if hasAfter && severity == "high" &&
		(after.EvidenceKind == RetentionEvidenceTimeOnly || after.EvidenceKind == RetentionEvidenceUnconfirmedHPROF) {
		severity = "medium"
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
		DeltaCount:        saturatingSignedUint64Delta(countBefore, countAfter),
		AgeBeforeMS:       ageBefore,
		AgeAfterMS:        ageAfter,
		DeltaAgeMS:        saturatingSignedUint64Delta(ageBefore, ageAfter),
		EstimatedBeforeKB: sizeBefore,
		EstimatedAfterKB:  sizeAfter,
		DeltaEstimatedKB:  saturatingSignedUint64Delta(sizeBefore, sizeAfter),
		MatchConfidence:   leakMatchConfidence(before, hasBefore, after, hasAfter),
	}
	delta.Explanation = leakDeltaExplanation(delta)
	delta.Recommendation = leakDeltaRecommendation(delta)
	delta.PlainText = leakDeltaPlainText(delta)
	return delta
}

func saturatingSignedUint64Delta(before, after uint64) int64 {
	const maxInt64AsUint64 = uint64(1<<63 - 1)
	if after >= before {
		difference := after - before
		if difference > maxInt64AsUint64 {
			return int64(1<<63 - 1)
		}
		return int64(difference)
	}
	difference := before - after
	if difference > maxInt64AsUint64 {
		return -1 << 63
	}
	return -int64(difference)
}

func leakMatchConfidence(before MemoryLeakSuspect, hasBefore bool, after MemoryLeakSuspect, hasAfter bool) string {
	if (hasBefore && before.DataQuality == "degraded") || (hasAfter && after.DataQuality == "degraded") {
		return "низкое: часть retained/HPROF-данных потеряна или усечена"
	}
	if hasBefore && hasAfter && before.EvidenceKind != after.EvidenceKind {
		return "низкое: уровни evidence базы и кандидата различаются (" + before.EvidenceKind + " → " + after.EvidenceKind + ")"
	}
	if !hasBefore || !hasAfter {
		if (hasBefore && before.HeapEvidence) || (hasAfter && after.HeapEvidence) {
			return "среднее: отпечаток цепочки из дампа памяти уникален для одной стороны"
		}
		return "среднее: отпечаток найден только в одной версии"
	}
	if before.ChainFingerprint != "" && before.ChainFingerprint == after.ChainFingerprint {
		if before.HeapEvidence || after.HeapEvidence {
			return "высокое: совпал нормализованный отпечаток цепочки из дампа памяти"
		}
		return "высокое: совпал нормализованный отпечаток цепочки выполнения"
	}
	if before.ClassName == after.ClassName && before.Holder != "" && before.Holder == after.Holder {
		return "среднее: совпали класс и держатель"
	}
	if before.ClassName == after.ClassName {
		return "низкое: совпал только класс, уточните ownerHint или добавьте доказательства из дампа памяти"
	}
	return "низкое: сопоставление построено по резервному отпечатку"
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
			edges = append(edges, LeakGraphEdge{From: targetID, To: id, Label: "удерживает", Kind: "retained"})
		}
	}
	return LeakGraph{
		Mode:        LeakModeHeap,
		Title:       "Подтвержденная цепочка ссылок",
		Subtitle:    "Путь построен из дампа памяти: от корня GC к удержанному объекту, затем выборка доминируемых классов.",
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
		labels = []string{"сигнал удержания из выполнения", "удержанный объект: " + suspect.ClassName}
	}
	nodes := make([]LeakGraphNode, 0, len(labels))
	edges := make([]LeakGraphEdge, 0, len(labels)-1)
	for index, label := range labels {
		id := fmt.Sprintf("runtime-%d", index)
		kind := runtimeLeakNodeKind(label)
		nodes = append(nodes, LeakGraphNode{
			ID:     id,
			Label:  label,
			Detail: runtimeLeakNodeDetail(kind),
			Kind:   kind,
			Depth:  index,
		})
		if index > 0 {
			edges = append(edges, LeakGraphEdge{
				From:  fmt.Sprintf("runtime-%d", index-1),
				To:    id,
				Label: runtimeLeakRelation(nodes[index-1].Kind, kind),
				Kind:  "runtime",
			})
		}
	}
	return LeakGraph{
		Mode:     LeakModeLight,
		Title:    "Контекст обнаружения удержанного объекта",
		Subtitle: "Дамп памяти не передан: схема связывает место наблюдения с lifecycle-сигналом, но не изображает ссылки между объектами и не доказывает утечку.",
		Nodes:    nodes,
		Edges:    edges,
		RootID:   "runtime-0",
		TargetID: fmt.Sprintf("runtime-%d", len(nodes)-1),
	}
}

func runtimeLeakNodeKind(label string) string {
	switch {
	case strings.HasPrefix(label, "экран:"):
		return "screen"
	case strings.HasPrefix(label, "сценарий:"), strings.HasPrefix(label, "флоу:"):
		return "flow"
	case strings.HasPrefix(label, "шаг:"):
		return "step"
	case strings.HasPrefix(label, "держатель:"):
		return "holder"
	case strings.HasPrefix(label, "метод:"):
		return "method"
	case strings.HasPrefix(label, "удержанный объект:"):
		return "target"
	default:
		return "context"
	}
}

func runtimeLeakNodeDetail(kind string) string {
	switch kind {
	case "screen":
		return "экран во время наблюдения"
	case "flow":
		return "пользовательский сценарий"
	case "step":
		return "шаг пользовательского сценария"
	case "holder":
		return "вероятный владелец ссылки"
	case "method":
		return "место наблюдения"
	case "target":
		return "lifecycle-сигнал удержания"
	default:
		return "сопутствующий сигнал выполнения"
	}
}

func runtimeLeakRelation(fromKind, toKind string) string {
	switch {
	case fromKind == "screen" && toKind == "flow":
		return "сценарий на экране"
	case (fromKind == "flow" || fromKind == "screen" || fromKind == "context") && toKind == "step":
		return "шаг сценария"
	case (fromKind == "step" || fromKind == "flow" || fromKind == "screen" || fromKind == "context") && toKind == "holder":
		return "атрибутировано вероятному владельцу"
	case fromKind == "holder" && toKind == "method":
		return "место наблюдения"
	case (fromKind == "holder" || fromKind == "method") && toKind == "target":
		return "объект оставался жив после lifecycle"
	default:
		return "наблюдался в этом контексте"
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
		return "Сигналов неожиданной достижимости объектов нет."
	case stats.High > 0:
		return fmt.Sprintf("Найдено %d сигналов удержания, из них %d высокого риска. Сначала проверьте строки с подтвержденным HPROF-путем; runtime-сигналы без пути не являются доказательством утечки.", stats.TotalSuspects, stats.High)
	case stats.Medium > 0:
		return fmt.Sprintf("Найдено %d сигналов удержания. Проверьте повторяемость и уровень evidence; для точной цепочки нужен HPROF-путь от корня GC.", stats.TotalSuspects)
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
		return "сигналов удержания нет ни в базе, ни в кандидате"
	case stats.CandidateTotal > stats.BaselineTotal:
		return "в новой версии больше сигналов удержания"
	case stats.CandidateTotal < stats.BaselineTotal:
		return "в новой версии меньше сигналов удержания"
	default:
		return "количество сигналов удержания не изменилось"
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
		return "новый сигнал"
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
		return fmt.Sprintf("В базовом прогоне такой отпечаток не встречался, а в проверяемом появился сигнал для %s. Уровень evidence: %s; без HPROF-пути это не доказательство утечки.", delta.Candidate.ClassName, delta.Candidate.EvidenceKind)
	case LeakDeltaWorse:
		return fmt.Sprintf("Подозрение уже было в базовом прогоне, но стало сильнее: оценка %+0.1f, количество %+d, возраст %+d мс, размер %+d КБ.", delta.DeltaScore, delta.DeltaCount, delta.DeltaAgeMS, delta.DeltaEstimatedKB)
	case LeakDeltaBetter:
		return fmt.Sprintf("Сигнал стал слабее: оценка %+0.1f, количество %+d, возраст %+d мс, размер %+d КБ.", delta.DeltaScore, delta.DeltaCount, delta.DeltaAgeMS, delta.DeltaEstimatedKB)
	case LeakDeltaResolved:
		return fmt.Sprintf("В проверяемом прогоне этот отпечаток не найден. Если сценарий, evidence и когорты совпадают, сигнал удержания %s, вероятно, устранен.", delta.Baseline.ClassName)
	default:
		return "Сигнал остался примерно на том же уровне: проверьте, что сценарии базового и проверяемого прогонов действительно одинаковые."
	}
}

func leakDeltaRecommendation(delta LeakDelta) string {
	if delta.HasCandidate {
		return delta.Candidate.Recommendation
	}
	return "Сохраните этот отпечаток в списке отслеживания регрессий: если он вернется в проверяемом прогоне, проверяйте тот же держатель, сценарий и очистку жизненного цикла."
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
		suspect.EvidenceKind,
		suspect.EvidenceLabel,
		suspect.EvidenceConfidence,
		suspect.DataQuality,
		strings.Join(suspect.QualityWarnings, " "),
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
