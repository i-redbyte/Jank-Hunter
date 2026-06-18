package analyze

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	codeCategoryNetwork    = "Сеть"
	codeCategoryUI         = "UI"
	codeCategoryMainThread = "Главный поток"
	codeCategoryMemory     = "Память"
	codeCategoryLogs       = "Логи"
	codeCategoryRuntime    = "Выполнение"
	codeCategoryInfluence  = "Граф влияния"
	codeCategoryANR        = "ANR-risk"
	codeCategoryOOM        = "OOM-risk"
	codeCategoryGCPressure = "GC pressure"
	codeCategoryDuplicate  = "duplicate network"
	codeCategoryLifecycle  = "lifecycle leak"
	codeCategoryLogSpam    = "log spam"
	codeCategoryMainIO     = "main-thread IO"
)

type codeProblemAccumulator struct {
	className       string
	method          string
	owner           string
	score           float64
	runtimeEvidence bool
	problems        uint64
	logSpam         uint64
	mainThreadMS    uint64
	networkMS       uint64
	uiJank          uint64
	retained        uint64
	memoryKB        uint64
	runtimeCalls    uint64
	runtimeMS       uint64
	maxMS           uint64
	categories      map[string]struct{}
	problemNames    map[string]struct{}
	signals         map[string]*CodeProblemSignal
	screens         map[string]struct{}
	flows           map[string]struct{}
	steps           map[string]struct{}
	routes          map[string]struct{}
}

func BuildCodeProblemRegistry(summary Summary) []CodeProblemStats {
	builder := codeProblemBuilder{items: map[string]*codeProblemAccumulator{}}
	builder.addOwners(summary.Owners)
	builder.addFlows(summary.Flows)
	builder.addLogSpam(summary.LogSpam)
	builder.addProblemWindows(summary.ProblemWindows)
	builder.addRetained(summary.RetainedClasses, summary.LowMemoryCount)
	builder.addMemoryLeaks(summary.MemoryLeaks)
	builder.addRoutes(summary.Routes)
	builder.addRuntimeCalls(summary.RuntimeCalls)
	builder.addInfluence(summary.Influence)
	return builder.finish()
}

type codeProblemBuilder struct {
	items map[string]*codeProblemAccumulator
}

func (b *codeProblemBuilder) addOwners(owners []OwnerStats) {
	for _, owner := range owners {
		className, method := codeLocationFromOwner(owner.Owner)
		if className == "" {
			continue
		}
		item := b.item(className, method, owner.Owner)
		item.runtimeEvidence = true
		switch owner.Kind {
		case "main_thread_stall":
			item.addCategory(codeCategoryANR)
			item.mainThreadMS += owner.TotalMS
			item.problems += uint64(owner.Count)
			item.maxMS = maxUint64(item.maxMS, owner.MaxMS)
			item.addSignal(CodeProblemSignal{
				Name:     "Пауза главного потока",
				Category: codeCategoryMainThread,
				Severity: severityFromDuration(owner.MaxMS, 2_000, 8_000),
				Score:    scoreDuration(owner.TotalMS, 1_800) + scoreDuration(owner.MaxMS, 500),
				Count:    uint64(owner.Count),
				TotalMS:  owner.TotalMS,
				MaxMS:    owner.MaxMS,
				Detail:   "Работа блокировала главный поток; это повышает риск АНР и пропуска кадров.",
			})
		case "http":
			item.networkMS += owner.TotalMS
			item.maxMS = maxUint64(item.maxMS, owner.MaxMS)
			item.addSignal(CodeProblemSignal{
				Name:     "Сетевая задержка",
				Category: codeCategoryNetwork,
				Severity: severityFromDuration(owner.MaxMS, 700, 1_500),
				Score:    scoreDuration(owner.TotalMS, 2_500) + scoreDuration(owner.MaxMS, 900),
				Count:    uint64(owner.Count),
				TotalMS:  owner.TotalMS,
				MaxMS:    owner.MaxMS,
				Detail:   "Источник связан с медленной сетевой работой.",
			})
		case "retained_object":
			item.addCategory(codeCategoryLifecycle)
			item.addCategory(codeCategoryOOM)
			item.retained += uint64(owner.Count)
			item.addSignal(CodeProblemSignal{
				Name:     "Удержанный объект",
				Category: codeCategoryMemory,
				Severity: severityFromCount(uint64(owner.Count), 2, 8),
				Score:    scoreCount(uint64(owner.Count), 8) + scoreDuration(owner.MaxMS, 60_000),
				Count:    uint64(owner.Count),
				Value:    owner.MaxMS,
				Unit:     "мс возраста",
				Detail:   "Объекты живут дольше ожидаемого и могут усиливать давление памяти.",
			})
		default:
			if owner.Count > 0 || owner.TotalMS > 0 {
				item.runtimeCalls += uint64(owner.Count)
				item.runtimeMS += owner.TotalMS
				item.maxMS = maxUint64(item.maxMS, owner.MaxMS)
				item.addSignal(CodeProblemSignal{
					Name:     ownerKindForCodeProblem(owner.Kind),
					Category: codeCategoryRuntime,
					Severity: severityFromDuration(owner.MaxMS, 500, 2_000),
					Score:    scoreCount(uint64(owner.Count), 120) + scoreDuration(owner.TotalMS, 2_500),
					Count:    uint64(owner.Count),
					TotalMS:  owner.TotalMS,
					MaxMS:    owner.MaxMS,
					Detail:   "Источник часто встречается в событиях выполнения.",
				})
			}
		}
	}
}

func (b *codeProblemBuilder) addFlows(flows []FlowStats) {
	for _, flow := range flows {
		className, method := codeLocationFromOwner(flow.Owner)
		if className == "" {
			continue
		}
		item := b.item(className, method, flow.Owner)
		item.runtimeEvidence = true
		item.addContext(flow.Screen, flow.Flow, flow.Step, flow.RouteSample)
		if flow.HTTPCount > 0 || flow.HTTPP95MS > 0 || flow.HTTPFailed > 0 {
			item.networkMS += flow.HTTPP95MS
			item.maxMS = maxUint64(item.maxMS, flow.HTTPP95MS)
			score := scoreDuration(flow.HTTPP95MS, 900) + scoreCount(uint64(flow.HTTPFailed), 3)
			item.addSignal(CodeProblemSignal{
				Name:     "HTTP в флоу",
				Category: codeCategoryNetwork,
				Severity: severityFromNetwork(flow.HTTPP95MS, flow.HTTPFailed),
				Score:    score,
				Count:    uint64(flow.HTTPCount),
				MaxMS:    flow.HTTPP95MS,
				Detail:   fmt.Sprintf("В этом контексте HTTP p95=%d мс, ошибок=%d.", flow.HTTPP95MS, flow.HTTPFailed),
			})
		}
		if flow.UIJank > 0 || flow.UIJankPct > 0 {
			item.uiJank += flow.UIJank
			item.addSignal(CodeProblemSignal{
				Name:     "UI-подтормаживания",
				Category: codeCategoryUI,
				Severity: severityFromPercent(flow.UIJankPct, 3, 8),
				Score:    scoreCount(flow.UIJank, 50) + math.Min(flow.UIJankPct/2, 6),
				Count:    flow.UIJank,
				Value:    flow.UIFrames,
				Unit:     "кадров",
				Detail:   fmt.Sprintf("В флоу медленных кадров %d из %d (%.2f%%).", flow.UIJank, flow.UIFrames, flow.UIJankPct),
			})
		}
		if flow.StallCount > 0 || flow.StallMaxMS > 0 {
			item.addCategory(codeCategoryANR)
			item.mainThreadMS += flow.StallMaxMS
			item.maxMS = maxUint64(item.maxMS, flow.StallMaxMS)
			item.addSignal(CodeProblemSignal{
				Name:     "Пауза главного потока",
				Category: codeCategoryMainThread,
				Severity: severityFromDuration(flow.StallMaxMS, 2_000, 8_000),
				Score:    scoreDuration(flow.StallMaxMS, 500) + scoreCount(uint64(flow.StallCount), 4),
				Count:    uint64(flow.StallCount),
				MaxMS:    flow.StallMaxMS,
				Detail:   fmt.Sprintf("Максимальная пауза в флоу: %d мс.", flow.StallMaxMS),
			})
		}
		if flow.LogSpam > 0 {
			item.addCategory(codeCategoryLogSpam)
			item.logSpam += flow.LogSpam
			item.addSignal(CodeProblemSignal{
				Name:     "Спам логами",
				Category: codeCategoryLogs,
				Severity: severityFromCount(flow.LogSpam, 100, 1_000),
				Score:    scoreCount(flow.LogSpam, 180),
				Count:    flow.LogSpam,
				Detail:   "Частые вызовы логирования в этом контексте могут мешать измерениям и добавлять работу.",
			})
		}
		if flow.ProblemCount > 0 {
			item.problems += flow.ProblemCount
			item.addSignal(CodeProblemSignal{
				Name:     "Проблемные окна",
				Category: codeCategoryRuntime,
				Severity: severityFromCount(flow.ProblemCount, 4, 20),
				Score:    scoreCount(flow.ProblemCount, 10),
				Count:    flow.ProblemCount,
				MaxMS:    flow.ProblemMaxMS,
				Detail:   "Сигналы попали в агрегированные проблемные окна.",
			})
		}
		if flow.MemoryMaxKB > 0 {
			item.addCategory(codeCategoryOOM)
			item.memoryKB = maxUint64(item.memoryKB, flow.MemoryMaxKB)
			item.addSignal(CodeProblemSignal{
				Name:     "Память в флоу",
				Category: codeCategoryMemory,
				Severity: severityFromKB(flow.MemoryMaxKB, 256*1024, 768*1024),
				Score:    scoreDuration(flow.MemoryMaxKB, 256*1024),
				Value:    flow.MemoryMaxKB,
				Unit:     "KB",
				Detail:   "В этом контексте был высокий PSS процесса.",
			})
		}
	}
}

func (b *codeProblemBuilder) addLogSpam(spamRows []LogSpamStats) {
	for _, spam := range spamRows {
		className, method := codeLocationFromOwner(spam.Owner)
		if className == "" {
			continue
		}
		item := b.item(className, method, spam.Owner)
		item.runtimeEvidence = true
		item.addCategory(codeCategoryLogSpam)
		item.logSpam += spam.Count
		item.addContext(spam.Screen, spam.Flow, spam.Step, "")
		item.addSignal(CodeProblemSignal{
			Name:     "Спам логами",
			Category: codeCategoryLogs,
			Severity: severityFromCount(spam.Count, 100, 1_000),
			Score:    scoreCount(spam.Count, 160),
			Count:    spam.Count,
			Detail:   fmt.Sprintf("%s.%s вызван %d раз.", spam.Source, spam.Level, spam.Count),
		})
	}
}

func (b *codeProblemBuilder) addProblemWindows(windows []ProblemWindowStats) {
	for _, window := range windows {
		className, method := codeLocationFromOwner(window.Owner)
		if className == "" {
			continue
		}
		item := b.item(className, method, window.Owner)
		item.runtimeEvidence = true
		item.problems += window.Count
		item.maxMS = maxUint64(item.maxMS, window.MaxMS)
		item.addContext(window.Screen, window.Flow, window.Step, "")
		category := categoryForProblemKind(window.Kind)
		for _, category := range extraCategoriesForProblemKind(window.Kind) {
			item.addCategory(category)
		}
		item.addSignal(CodeProblemSignal{
			Name:     problemKindForCodeProblem(window.Kind),
			Category: category,
			Severity: severityFromProblemWindow(window),
			Score:    scoreCount(window.Count, 8) + scoreDuration(window.MaxMS, 500),
			Count:    window.Count,
			TotalMS:  window.TotalWindowMS,
			MaxMS:    window.MaxMS,
			Detail:   fmt.Sprintf("Окон: %d, событий: %d, максимальное значение: %d мс.", window.Windows, window.Count, window.MaxMS),
		})
	}
}

func (b *codeProblemBuilder) addRetained(retainedRows []NamedValue, lowMemoryCount int) {
	for _, retained := range retainedRows {
		className := normalizeClassName(retained.Name)
		if className == "" {
			continue
		}
		item := b.item(className, "", retained.Name)
		item.runtimeEvidence = true
		item.addCategory(codeCategoryLifecycle)
		item.addCategory(codeCategoryOOM)
		item.retained += retained.Value
		severity := severityFromCount(retained.Value, 2, 8)
		if lowMemoryCount > 0 && severity != "high" {
			severity = "medium"
		}
		item.addSignal(CodeProblemSignal{
			Name:     "Удержанные объекты",
			Category: codeCategoryMemory,
			Severity: severity,
			Score:    scoreCount(retained.Value, 8),
			Count:    retained.Value,
			Detail:   strings.TrimSpace("Класс встречается среди удержанных объектов. " + retained.Extra),
		})
	}
}

func (b *codeProblemBuilder) addMemoryLeaks(leaks []MemoryLeakSuspect) {
	for _, leak := range leaks {
		target := leak.ClassName
		if isLikelyAppClass(leak.Holder) {
			target = leak.Holder
		}
		className, method := codeLocationFromOwner(target)
		if className == "" {
			className = normalizeClassName(target)
		}
		if className == "" {
			continue
		}
		item := b.item(className, method, target)
		item.runtimeEvidence = true
		item.addCategory(codeCategoryLifecycle)
		if leak.EstimatedRetainedKB >= 4*1024 || leak.RetainedObjectCount >= 3 {
			item.addCategory(codeCategoryOOM)
		}
		item.retained += leak.Count
		item.memoryKB += leak.EstimatedRetainedKB
		item.maxMS = maxUint64(item.maxMS, leak.MaxAgeMS)
		item.addContext(leak.Screen, leak.Flow, leak.Step, "")
		detail := fmt.Sprintf("Удержан %s; держатель: %s; качество привязки: %s.", leak.ClassName, leak.Holder, leak.HolderQuality)
		if leak.ObjectKind != "" {
			detail += " Тип: " + leak.ObjectKind + "."
		}
		if leak.HeapEvidence {
			detail += " Heap dump подтвердил путь до GC root"
			if leak.GCRoot != "" {
				detail += " " + leak.GCRoot
			}
			if leak.HolderField != "" {
				detail += "; поле " + leak.HolderField
			}
			detail += "."
		}
		item.addSignal(CodeProblemSignal{
			Name:     "Подозрение утечки памяти",
			Category: codeCategoryMemory,
			Severity: leak.Severity,
			Score:    leak.Score,
			Count:    leak.Count,
			Value:    leak.MaxAgeMS,
			Unit:     "мс возраста",
			Detail:   detail,
		})
	}
}

func (b *codeProblemBuilder) addRoutes(routes []RouteStats) {
	for _, route := range routes {
		className, method := codeLocationFromOwner(route.OwnerSample)
		if className == "" {
			continue
		}
		item := b.item(className, method, route.OwnerSample)
		item.runtimeEvidence = true
		item.networkMS += route.P95MS
		item.maxMS = maxUint64(item.maxMS, route.MaxMS)
		item.addContext("", "", "", route.Route)
		if route.Count >= 5 {
			item.addCategory(codeCategoryDuplicate)
		}
		item.addSignal(CodeProblemSignal{
			Name:     "Сетевой маршрут",
			Category: codeCategoryNetwork,
			Severity: severityFromNetwork(route.P95MS, route.Failures),
			Score:    scoreDuration(route.P95MS, 900) + scoreCount(uint64(route.Failures), 3) + scoreCount(uint64(route.Count), 120)*0.25,
			Count:    uint64(route.Count),
			Value:    route.P95MS,
			Unit:     "мс p95",
			MaxMS:    route.MaxMS,
			Detail:   fmt.Sprintf("Маршрут %s: p95=%d мс, ошибок=%d.", route.Route, route.P95MS, route.Failures),
		})
	}
}

func (b *codeProblemBuilder) addRuntimeCalls(calls []RuntimeCallStats) {
	for _, call := range calls {
		b.addRuntimeCallEndpoint(call.Caller, call, 0.45, "Инициатор вызова выполнения")
		b.addRuntimeCallEndpoint(call.Callee, call, 1.0, "Вызванная работа")
	}
}

func (b *codeProblemBuilder) addRuntimeCallEndpoint(owner string, call RuntimeCallStats, weight float64, name string) {
	className, method := codeLocationFromOwner(owner)
	if className == "" {
		return
	}
	item := b.item(className, method, owner)
	item.runtimeEvidence = true
	item.runtimeCalls += call.Count
	item.runtimeMS += call.TotalMS
	item.maxMS = maxUint64(item.maxMS, call.MaxMS)
	item.addContext(call.Screen, call.Flow, call.Step, "")
	if call.MaxMS >= 700 && likelyMainThreadOwner(owner) {
		item.addCategory(codeCategoryANR)
	}
	item.addSignal(CodeProblemSignal{
		Name:     name,
		Category: codeCategoryRuntime,
		Severity: severityFromDuration(call.MaxMS, 500, 2_000),
		Score:    (scoreCount(call.Count, 160) + scoreDuration(call.TotalMS, 2_200) + scoreDuration(call.MaxMS, 500)) * weight,
		Count:    call.Count,
		TotalMS:  call.TotalMS,
		MaxMS:    call.MaxMS,
		Detail:   fmt.Sprintf("Связка %s → %s, вызовов %d.", call.Caller, call.Callee, call.Count),
	})
}

func (b *codeProblemBuilder) addInfluence(influence InfluenceSummary) {
	if !influence.Available {
		return
	}
	for _, node := range influence.TopNodes {
		className := normalizeClassName(node.ClassName)
		if className == "" {
			continue
		}
		item := b.item(className, "", className)
		if node.RuntimeEvidence {
			item.runtimeEvidence = true
		}
		for _, flow := range node.Flows {
			item.addContext("", flow, "", "")
		}
		for _, screen := range node.Screens {
			item.addContext(screen, "", "", "")
		}
		for _, route := range node.Routes {
			item.addContext("", "", "", route)
		}
		item.problems += node.Problems
		item.logSpam += node.LogSpam
		item.mainThreadMS += node.MainThreadMS
		item.networkMS += node.NetworkMS
		item.memoryKB += node.MemoryPressure
		item.uiJank += node.UIJank
		item.retained += node.Retained
		item.addSignal(CodeProblemSignal{
			Name:     "Узел графа влияния",
			Category: codeCategoryInfluence,
			Severity: node.Severity,
			Score:    node.Score * 0.35,
			Detail:   influenceDetail(node),
		})
	}
}

func (b *codeProblemBuilder) finish() []CodeProblemStats {
	out := make([]CodeProblemStats, 0, len(b.items))
	for _, item := range b.items {
		row := item.toStats()
		if row.Score <= 0 && len(row.Signals) == 0 {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].ClassName == out[j].ClassName {
				return out[i].Method < out[j].Method
			}
			return out[i].ClassName < out[j].ClassName
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func (b *codeProblemBuilder) item(className, method, owner string) *codeProblemAccumulator {
	key := className + "\x00" + method
	item := b.items[key]
	if item != nil {
		if item.owner == "" {
			item.owner = owner
		}
		return item
	}
	item = &codeProblemAccumulator{
		className:    className,
		method:       method,
		owner:        owner,
		categories:   map[string]struct{}{},
		problemNames: map[string]struct{}{},
		signals:      map[string]*CodeProblemSignal{},
		screens:      map[string]struct{}{},
		flows:        map[string]struct{}{},
		steps:        map[string]struct{}{},
		routes:       map[string]struct{}{},
	}
	b.items[key] = item
	return item
}

func (a *codeProblemAccumulator) addContext(screen, flow, step, route string) {
	addNonEmpty(a.screens, screen)
	addNonEmpty(a.flows, flow)
	addNonEmpty(a.steps, step)
	addNonEmpty(a.routes, route)
}

func (a *codeProblemAccumulator) addSignal(signal CodeProblemSignal) {
	if signal.Name == "" {
		return
	}
	if signal.Category == "" {
		signal.Category = codeCategoryRuntime
	}
	if signal.Severity == "" {
		signal.Severity = "ok"
	}
	key := signal.Category + "\x00" + signal.Name
	existing := a.signals[key]
	if existing == nil {
		clone := signal
		a.signals[key] = &clone
	} else {
		existing.Score += signal.Score
		existing.Count += signal.Count
		existing.TotalMS += signal.TotalMS
		existing.MaxMS = maxUint64(existing.MaxMS, signal.MaxMS)
		existing.Value = maxUint64(existing.Value, signal.Value)
		if signal.Detail != "" && !strings.Contains(existing.Detail, signal.Detail) {
			if existing.Detail != "" {
				existing.Detail += " "
			}
			existing.Detail += signal.Detail
		}
		existing.Severity = maxSeverity(existing.Severity, signal.Severity)
	}
	a.score += signal.Score
	a.categories[signal.Category] = struct{}{}
	a.problemNames[signal.Name] = struct{}{}
}

func (a *codeProblemAccumulator) addCategory(category string) {
	if category == "" {
		return
	}
	a.categories[category] = struct{}{}
}

func (a *codeProblemAccumulator) toStats() CodeProblemStats {
	signals := make([]CodeProblemSignal, 0, len(a.signals))
	for _, signal := range a.signals {
		signal.Score = math.Round(signal.Score*10) / 10
		signals = append(signals, *signal)
	}
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Score == signals[j].Score {
			return signals[i].Name < signals[j].Name
		}
		return signals[i].Score > signals[j].Score
	})
	score := math.Round(a.score*10) / 10
	categories := sortedSet(a.categories, 0)
	problems := sortedSet(a.problemNames, 0)
	return CodeProblemStats{
		ClassName:       a.className,
		Method:          a.method,
		Owner:           a.owner,
		Score:           score,
		Severity:        codeProblemSeverity(score, signals),
		RuntimeEvidence: a.runtimeEvidence,
		Categories:      categories,
		Problems:        problems,
		Signals:         signals,
		Screens:         sortedSet(a.screens, 6),
		Flows:           sortedSet(a.flows, 6),
		Steps:           sortedSet(a.steps, 6),
		Routes:          sortedSet(a.routes, 6),
		DrillDown:       codeProblemDrillDown(a, signals),
		Impact:          codeProblemImpact(categories, a.runtimeEvidence),
		Recommendation:  codeProblemRecommendation(categories),
		Evidence:        codeProblemEvidence(a),
	}
}

func codeProblemDrillDown(a *codeProblemAccumulator, signals []CodeProblemSignal) []CodeProblemDrillDown {
	signalNames := make([]string, 0, len(signals))
	for _, signal := range signals {
		signalNames = append(signalNames, signal.Name)
	}
	evidence := codeProblemEvidence(a)
	recommendation := codeProblemRecommendation(sortedSet(a.categories, 0))
	var out []CodeProblemDrillDown
	flows := sortedSet(a.flows, 0)
	if len(flows) == 0 {
		flows = []string{""}
	}
	screens := sortedSet(a.screens, 0)
	if len(screens) == 0 {
		screens = []string{""}
	}
	steps := sortedSet(a.steps, 0)
	if len(steps) == 0 {
		steps = []string{""}
	}
	routes := sortedSet(a.routes, 0)
	if len(routes) == 0 {
		routes = []string{""}
	}
	for _, flow := range flows {
		for _, screen := range firstNStrings(screens, 3) {
			for _, step := range firstNStrings(steps, 3) {
				for _, route := range firstNStrings(routes, 3) {
					out = append(out, CodeProblemDrillDown{
						ClassName:      a.className,
						Method:         a.method,
						Screen:         screen,
						Flow:           flow,
						Step:           step,
						Route:          route,
						Evidence:       evidence,
						Recommendation: recommendation,
						Signals:        append([]string(nil), signalNames...),
					})
					if len(out) >= 12 {
						return out
					}
				}
			}
		}
	}
	return out
}

func codeLocationFromOwner(owner string) (string, string) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(owner, "owner."))
	if trimmed == "" || trimmed == "unknown" {
		return "", ""
	}
	className := classFromOwner(trimmed)
	if className == "" {
		className = normalizeClassName(trimmed)
	}
	if className == "" {
		return "", ""
	}
	method := ""
	withoutHash := trimmed
	if hashIndex := strings.LastIndex(withoutHash, "#"); hashIndex > 0 {
		withoutHash = withoutHash[:hashIndex]
	}
	withoutHash = strings.Trim(strings.ReplaceAll(withoutHash, "/", "."), ".")
	if strings.HasPrefix(withoutHash, className+".") {
		method = strings.TrimPrefix(withoutHash, className+".")
	} else if dot := strings.LastIndex(withoutHash, "."); dot > 0 {
		candidate := withoutHash[dot+1:]
		if candidate != "" && !isUpperASCII(candidate[0]) {
			method = candidate
		}
	}
	method = normalizeMethodName(method)
	return className, method
}

func normalizeMethodName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, ".")
	if value == "" || value == "unknown" {
		return ""
	}
	if strings.Contains(value, " ") {
		return ""
	}
	return value
}

func codeProblemSeverity(score float64, signals []CodeProblemSignal) string {
	severity := "ok"
	for _, signal := range signals {
		severity = maxSeverity(severity, signal.Severity)
	}
	switch {
	case score >= 18:
		return "high"
	case score >= 7:
		if severity == "ok" {
			return "medium"
		}
		return severity
	default:
		return severity
	}
}

func codeProblemImpact(categories []string, runtimeEvidence bool) string {
	parts := make([]string, 0, len(categories)+1)
	for _, category := range categories {
		switch category {
		case codeCategoryNetwork:
			parts = append(parts, "увеличивает задержки сценария и может создавать сетевые циклы")
		case codeCategoryUI:
			parts = append(parts, "ухудшает плавность интерфейса и отклик на действия")
		case codeCategoryMainThread:
			parts = append(parts, "блокирует главный поток, повышая риск АНР и пропуска кадров")
		case codeCategoryMemory:
			parts = append(parts, "повышает давление памяти, частоту GC и риск удержаний")
		case codeCategoryLogs:
			parts = append(parts, "создает шум логами и лишнюю работу в горячем сценарии")
		case codeCategoryRuntime:
			parts = append(parts, "утяжеляет цепочку выполнения в измеренном флоу")
		case codeCategoryInfluence:
			parts = append(parts, "связан с другими узлами графа влияния")
		case codeCategoryANR:
			parts = append(parts, "создает риск ANR из-за долгой работы или цепочки на главном потоке")
		case codeCategoryOOM:
			parts = append(parts, "повышает риск OOM из-за роста памяти или удержаний")
		case codeCategoryGCPressure:
			parts = append(parts, "создает давление GC и может давать периодические паузы")
		case codeCategoryDuplicate:
			parts = append(parts, "может дублировать сетевые запросы или повторять один маршрут без дедупликации")
		case codeCategoryLifecycle:
			parts = append(parts, "похож на lifecycle leak: объект живет дольше экрана или флоу")
		case codeCategoryLogSpam:
			parts = append(parts, "создает log spam в горячем пути")
		case codeCategoryMainIO:
			parts = append(parts, "указывает на риск IO на главном потоке")
		}
	}
	if !runtimeEvidence {
		parts = append(parts, "пока нет подтверждения выполнением в этом прогоне")
	}
	if len(parts) == 0 {
		return "Нужна ручная проверка: сигнал есть, но влияние пока слабое."
	}
	return strings.Join(parts, "; ") + "."
}

func codeProblemRecommendation(categories []string) string {
	recommendations := []string{}
	for _, category := range categories {
		switch category {
		case codeCategoryNetwork:
			recommendations = append(recommendations, "проверьте дедупликацию запросов, кеширование, таймауты и повторные фоновые циклы")
		case codeCategoryUI:
			recommendations = append(recommendations, "проверьте render/bind, тяжелые layout-операции и работу при скролле")
		case codeCategoryMainThread:
			recommendations = append(recommendations, "перенесите тяжелую работу с главного потока и проверьте dispatch/click/listener цепочку")
		case codeCategoryMemory:
			recommendations = append(recommendations, "проверьте владельцев ссылок, lifecycle, кеши и рост PSS рядом с GC")
		case codeCategoryLogs:
			recommendations = append(recommendations, "уменьшите частоту логирования или вынесите шумные debug-логи из горячего пути")
		case codeCategoryRuntime:
			recommendations = append(recommendations, "проверьте цепочку вызовов и стоимость вызываемого метода")
		case codeCategoryInfluence:
			recommendations = append(recommendations, "откройте граф влияния и проверьте соседние узлы с runtime-доказательствами")
		case codeCategoryANR:
			recommendations = append(recommendations, "разбейте долгую работу, проверьте StrictMode/trace и уберите блокировки с главного потока")
		case codeCategoryOOM:
			recommendations = append(recommendations, "проверьте рост heap/PSS, лимиты кешей, bitmap/buffer allocations и жизненный цикл владельцев")
		case codeCategoryGCPressure:
			recommendations = append(recommendations, "уменьшите churn аллокаций в горячем пути и проверьте повторные коллекции/создание временных объектов")
		case codeCategoryDuplicate:
			recommendations = append(recommendations, "добавьте дедупликацию in-flight запросов, кеширование ответа или debounce повторного запуска флоу")
		case codeCategoryLifecycle:
			recommendations = append(recommendations, "проверьте очистку listeners/callbacks/binding и отмену coroutine/executor задач на lifecycle boundary")
		case codeCategoryLogSpam:
			recommendations = append(recommendations, "ограничьте частоту логов, уберите debug-логи из горячего пути или агрегируйте события")
		case codeCategoryMainIO:
			recommendations = append(recommendations, "вынесите disk/network IO с главного потока и проверьте StrictMode violations")
		}
	}
	if len(recommendations) == 0 {
		return "Проверьте источник вручную и сопоставьте его с таймлайном."
	}
	return strings.Join(uniqueStrings(recommendations), "; ") + "."
}

func codeProblemEvidence(a *codeProblemAccumulator) string {
	parts := []string{}
	if a.problems > 0 {
		parts = append(parts, fmt.Sprintf("проблем=%d", a.problems))
	}
	if a.mainThreadMS > 0 {
		parts = append(parts, fmt.Sprintf("главный поток=%d мс", a.mainThreadMS))
	}
	if a.networkMS > 0 {
		parts = append(parts, fmt.Sprintf("сеть=%d мс", a.networkMS))
	}
	if a.uiJank > 0 {
		parts = append(parts, fmt.Sprintf("медленных кадров=%d", a.uiJank))
	}
	if a.logSpam > 0 {
		parts = append(parts, fmt.Sprintf("логов=%d", a.logSpam))
	}
	if a.retained > 0 {
		parts = append(parts, fmt.Sprintf("удержано=%d", a.retained))
	}
	if a.memoryKB > 0 {
		parts = append(parts, fmt.Sprintf("память=%d KB", a.memoryKB))
	}
	if a.runtimeCalls > 0 {
		parts = append(parts, fmt.Sprintf("вызовов=%d", a.runtimeCalls))
	}
	if a.maxMS > 0 {
		parts = append(parts, fmt.Sprintf("макс=%d мс", a.maxMS))
	}
	if len(parts) == 0 {
		return "runtime-доказательства ограничены."
	}
	return strings.Join(parts, ", ")
}

func ownerKindForCodeProblem(kind string) string {
	switch kind {
	case "wrapped_runnable":
		return "Долгая Runnable-задача"
	case "wrapped_callable":
		return "Долгая Callable-задача"
	case "wrapped_coroutine":
		return "Долгая корутинная задача"
	case "wrapped_executor":
		return "Долгая executor-задача"
	case "wrapped_click":
		return "Долгий click-handler"
	case "main_thread_dispatch":
		return "Медленный dispatch главного потока"
	default:
		if kind == "" {
			return "Источник выполнения"
		}
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func problemKindForCodeProblem(kind string) string {
	switch kind {
	case "http_slow_or_failed":
		return "Медленный или ошибочный HTTP"
	case "main_thread_stall":
		return "Пауза главного потока"
	case "ui_jank":
		return "Подтормаживания UI"
	case "wrapped_runnable":
		return "Долгая Runnable-задача"
	case "wrapped_callable":
		return "Долгая Callable-задача"
	case "wrapped_coroutine":
		return "Долгая корутинная задача"
	case "wrapped_executor":
		return "Долгая executor-задача"
	case "wrapped_click":
		return "Долгий click-handler"
	case "retained_object":
		return "Удержанный объект"
	case "main_thread_dispatch":
		return "Медленный dispatch главного потока"
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return "IO на главном потоке"
	case "log_spam":
		return "Спам логами"
	case "gc_pressure", "gc_count", "gc_time":
		return "Давление GC"
	default:
		if kind == "" {
			return "Проблемное окно"
		}
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func categoryForProblemKind(kind string) string {
	switch kind {
	case "http_slow_or_failed":
		return codeCategoryNetwork
	case "main_thread_stall", "main_thread_dispatch", "wrapped_click":
		return codeCategoryMainThread
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return codeCategoryMainIO
	case "ui_jank":
		return codeCategoryUI
	case "retained_object":
		return codeCategoryMemory
	case "log_spam":
		return codeCategoryLogs
	case "wrapped_runnable", "wrapped_callable", "wrapped_coroutine", "wrapped_executor":
		return codeCategoryRuntime
	default:
		return codeCategoryRuntime
	}
}

func extraCategoriesForProblemKind(kind string) []string {
	switch kind {
	case "http_slow_or_failed":
		return []string{codeCategoryDuplicate}
	case "main_thread_stall", "main_thread_dispatch", "wrapped_click":
		return []string{codeCategoryANR}
	case "main_thread_io", "main_thread_disk_io", "disk_io_main_thread":
		return []string{codeCategoryANR, codeCategoryMainThread}
	case "retained_object":
		return []string{codeCategoryLifecycle, codeCategoryOOM}
	case "log_spam":
		return []string{codeCategoryLogSpam}
	case "gc_pressure", "gc_count", "gc_time":
		return []string{codeCategoryGCPressure, codeCategoryOOM}
	default:
		return nil
	}
}

func severityFromProblemWindow(window ProblemWindowStats) string {
	if window.Kind == "main_thread_stall" || window.Kind == "main_thread_dispatch" || window.Kind == "main_thread_io" || window.Kind == "main_thread_disk_io" || window.Kind == "disk_io_main_thread" {
		return severityFromDuration(window.MaxMS, 2_000, 8_000)
	}
	if window.Kind == "ui_jank" {
		return severityFromCount(window.Count, 20, 100)
	}
	if window.Kind == "http_slow_or_failed" {
		return severityFromDuration(window.MaxMS, 700, 1_500)
	}
	return severityFromCount(window.Count, 4, 20)
}

func likelyMainThreadOwner(owner string) bool {
	lower := strings.ToLower(owner)
	return strings.Contains(lower, "main") ||
		strings.Contains(lower, "ui") ||
		strings.Contains(lower, "click") ||
		strings.Contains(lower, "render") ||
		strings.Contains(lower, "bind")
}

func severityFromNetwork(p95 uint64, failures int) string {
	if p95 >= 1_500 || failures >= 3 {
		return "high"
	}
	if p95 >= 700 || failures > 0 {
		return "medium"
	}
	return "ok"
}

func severityFromDuration(value, medium, high uint64) string {
	switch {
	case value >= high && high > 0:
		return "high"
	case value >= medium && medium > 0:
		return "medium"
	default:
		return "ok"
	}
}

func severityFromCount(value, medium, high uint64) string {
	switch {
	case value >= high && high > 0:
		return "high"
	case value >= medium && medium > 0:
		return "medium"
	default:
		return "ok"
	}
}

func severityFromPercent(value, medium, high float64) string {
	switch {
	case value >= high:
		return "high"
	case value >= medium:
		return "medium"
	default:
		return "ok"
	}
}

func severityFromKB(value, medium, high uint64) string {
	return severityFromCount(value, medium, high)
}

func maxSeverity(a, b string) string {
	return severityRankMax(a, b)
}

func severityRankMax(a, b string) string {
	if codeProblemSeverityRank(b) > codeProblemSeverityRank(a) {
		return b
	}
	return a
}

func codeProblemSeverityRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func influenceDetail(node InfluenceNode) string {
	parts := []string{}
	if len(node.Reasons) > 0 {
		parts = append(parts, "причины: "+strings.Join(node.Reasons, ", "))
	}
	if len(node.Flows) > 0 {
		parts = append(parts, "флоу: "+strings.Join(node.Flows, ", "))
	}
	if node.RuntimeEvidence {
		parts = append(parts, "есть runtime-доказательства")
	} else {
		parts = append(parts, "только статический след")
	}
	return strings.Join(parts, "; ")
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
