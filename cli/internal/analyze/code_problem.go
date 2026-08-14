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
	codeCategoryANR        = "Риск ANR"
	codeCategoryOOM        = "Риск OOM"
	codeCategoryGCPressure = "Давление GC"
	codeCategoryDuplicate  = "Дублирование сети"
	codeCategoryLifecycle  = "Утечка жизненного цикла"
	codeCategoryLogSpam    = "Спам логами"
	codeCategoryMainIO     = "Ввод-вывод на главном потоке"
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
	builder.addProblemWindows(summary.ProblemWindows)
	builder.addMemoryLeaks(summary.MemoryLeaks)
	builder.addLogSpam(summary.LogSpam)
	builder.addRuntimeCalls(summary.RuntimeCalls)
	return builder.finish()
}

type codeProblemBuilder struct {
	items map[string]*codeProblemAccumulator
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
			Score:    scoreContribution(spam.Count, 160),
			Count:    spam.Count,
			Detail:   fmt.Sprintf("%s.%s вызван %d раз.", spam.Source, spam.Level, spam.Count),
		})
	}
}

func (b *codeProblemBuilder) addProblemWindows(windows []ProblemWindowStats) {
	for _, window := range windows {
		if window.Kind == "retained_object" || window.Kind == "log_spam" {
			// Retained/log-spam events have their own canonical scoring paths.
			continue
		}
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
			Score:    scoreContribution(window.Count, 8) + scoreContribution(window.MaxMS, 500),
			Count:    window.Count,
			TotalMS:  window.TotalWindowMS,
			MaxMS:    window.MaxMS,
			Detail:   fmt.Sprintf("Окон: %d, событий: %d, максимальное значение: %d мс.", window.Windows, window.Count, window.MaxMS),
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
		item.runtimeEvidence = leak.TimeOnlyCount+leak.AfterExplicitGCCount > 0
		item.addCategory(codeCategoryLifecycle)
		if leak.EstimatedRetainedKB >= 4*1024 || leak.RetainedObjectCount >= 3 {
			item.addCategory(codeCategoryOOM)
		}
		item.retained += leak.Count
		item.memoryKB += leak.EstimatedRetainedKB
		item.maxMS = maxUint64(item.maxMS, leak.MaxAgeMS)
		item.addContext(leak.Screen, leak.Flow, leak.Step, "")
		detail := fmt.Sprintf(
			"Наблюдался достижимый %s; уровень: %s; держатель: %s; качество привязки: %s. %s.",
			leak.ClassName,
			leak.EvidenceKind,
			leak.Holder,
			leak.HolderQuality,
			retainedEvidenceMeaning(leak.EvidenceKind),
		)
		if leak.ObjectKind != "" {
			detail += " Тип: " + leak.ObjectKind + "."
		}
		if leak.LeakPattern != "" {
			detail += " Паттерн: " + leak.LeakPattern + "."
		}
		if leak.HeapEvidence {
			detail += " Дамп памяти подтвердил путь до корня GC"
			if leak.GCRoot != "" {
				detail += " " + leak.GCRoot
			}
			if leak.HolderField != "" {
				detail += "; поле " + leak.HolderField
			}
			detail += "."
		}
		signalName := "Сигнал удержания памяти"
		if leak.HeapEvidence {
			signalName = "Подтвержденный путь удержания HPROF"
		}
		item.addSignal(CodeProblemSignal{
			Name:     signalName,
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

func (b *codeProblemBuilder) addRuntimeCalls(calls []RuntimeCallStats) {
	for _, call := range calls {
		b.addRuntimeCallEndpoint(call.Caller, call, 0.45, "Инициатор вызова")
		b.addRuntimeCallEndpoint(call.Callee, call, 1.0, "Выполняемый метод")
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
		Score:    (scoreContribution(call.Count, 160) + scoreContribution(call.TotalMS, 2_200) + scoreContribution(call.MaxMS, 500)) * weight,
		Count:    call.Count,
		TotalMS:  call.TotalMS,
		MaxMS:    call.MaxMS,
		Detail:   fmt.Sprintf("Связка %s → %s, вызовов %d.", call.Caller, call.Callee, call.Count),
	})
}

func (b *codeProblemBuilder) finish() []CodeProblemStats {
	items := make([]*codeProblemAccumulator, 0, len(b.items))
	for _, item := range b.items {
		if roundedCodeProblemScore(item.score) <= 0 && len(item.signals) == 0 {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		leftScore := roundedCodeProblemScore(items[i].score)
		rightScore := roundedCodeProblemScore(items[j].score)
		if leftScore == rightScore {
			if items[i].className == items[j].className {
				return items[i].method < items[j].method
			}
			return items[i].className < items[j].className
		}
		return leftScore > rightScore
	})
	if len(items) > 200 {
		items = items[:200]
	}

	out := make([]CodeProblemStats, 0, len(items))
	for _, item := range items {
		out = append(out, item.toStats())
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
	score := roundedCodeProblemScore(a.score)
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

func roundedCodeProblemScore(score float64) float64 {
	return math.Round(score*10) / 10
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
	hasNonInfluenceSignal := false
	for _, signal := range signals {
		severity = maxSeverity(severity, signal.Severity)
		if signal.Category != codeCategoryInfluence {
			hasNonInfluenceSignal = true
		}
	}
	switch {
	case score >= 15:
		if !hasNonInfluenceSignal {
			return "medium"
		}
		return "high"
	case score >= 5:
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
			parts = append(parts, "утяжеляет цепочку выполнения в измеренном сценарии")
		case codeCategoryInfluence:
			parts = append(parts, "попал в граф влияния рядом с симптомами; это подсказка для расследования, а не самостоятельное доказательство бага")
		case codeCategoryANR:
			parts = append(parts, "создает риск ANR из-за долгой работы или цепочки на главном потоке")
		case codeCategoryOOM:
			parts = append(parts, "повышает риск OOM из-за роста памяти или удержаний")
		case codeCategoryGCPressure:
			parts = append(parts, "создает давление GC и может давать периодические паузы")
		case codeCategoryDuplicate:
			parts = append(parts, "может дублировать сетевые запросы или повторять один маршрут без дедупликации")
		case codeCategoryLifecycle:
			parts = append(parts, "похож на утечку жизненного цикла: объект живет дольше экрана или сценария")
		case codeCategoryLogSpam:
			parts = append(parts, "создает спам логами в горячем пути")
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
			recommendations = append(recommendations, "проверьте отрисовку, привязку данных, тяжелые layout-операции и работу при скролле")
		case codeCategoryMainThread:
			recommendations = append(recommendations, "перенесите тяжелую работу с главного потока и проверьте цепочку dispatch, click и слушателей")
		case codeCategoryMemory:
			recommendations = append(recommendations, "проверьте владельцев ссылок, жизненный цикл, кеши и рост PSS рядом с GC")
		case codeCategoryLogs:
			recommendations = append(recommendations, "уменьшите частоту логирования или вынесите шумные debug-логи из горячего пути")
		case codeCategoryRuntime:
			recommendations = append(recommendations, "проверьте цепочку вызовов и стоимость вызываемого метода")
		case codeCategoryInfluence:
			recommendations = append(recommendations, "откройте граф влияния и проверьте соседние узлы с доказательствами выполнения; приоритет выше, если рядом есть паузы, сеть, память или runtime-вызовы")
		case codeCategoryANR:
			recommendations = append(recommendations, "разбейте долгую работу, проверьте StrictMode/trace и уберите блокировки с главного потока")
		case codeCategoryOOM:
			recommendations = append(recommendations, "проверьте рост heap/PSS, лимиты кешей, bitmap/buffer allocations и жизненный цикл владельцев")
		case codeCategoryGCPressure:
			recommendations = append(recommendations, "уменьшите текучесть аллокаций в горячем пути и проверьте повторные сборки/создание временных объектов")
		case codeCategoryDuplicate:
			recommendations = append(recommendations, "добавьте дедупликацию запросов в работе, кеширование ответа или задержку повторного запуска сценария")
		case codeCategoryLifecycle:
			recommendations = append(recommendations, "проверьте очистку слушателей, обратных вызовов и binding, а также отмену корутинных задач и задач исполнителя на границе жизненного цикла")
		case codeCategoryLogSpam:
			recommendations = append(recommendations, "ограничьте частоту логов, уберите debug-логи из горячего пути или агрегируйте события")
		case codeCategoryMainIO:
			recommendations = append(recommendations, "вынесите дисковый и сетевой ввод-вывод с главного потока и проверьте нарушения StrictMode")
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
		parts = append(parts, fmt.Sprintf("память=%d КБ", a.memoryKB))
	}
	if a.runtimeCalls > 0 {
		parts = append(parts, fmt.Sprintf("вызовов=%d", a.runtimeCalls))
	}
	if a.maxMS > 0 {
		parts = append(parts, fmt.Sprintf("макс=%d мс", a.maxMS))
	}
	if len(parts) == 0 {
		return "Доказательства выполнения ограничены: отчет видит только слабую связь с графом или статический след."
	}
	return "Сводка сигналов: " + strings.Join(parts, ", ") + "."
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
