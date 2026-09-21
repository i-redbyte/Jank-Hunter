package analyze

import (
	"fmt"
	"strings"
)

type lambdaCaptureRisk struct {
	detector         string
	subcategory      string
	title            string
	detail           string
	impact           string
	action           string
	claim            string
	confidence       string
	confidenceReason string
	why              string
	heapConfirmed    bool
	priority         []ProblemPriorityComponent
}

func (b *problemBuilder) detectLambdaCaptures() {
	if b.lambdaCaptures == nil || !b.lambdaCaptures.Available {
		return
	}
	heapEvidence := buildLambdaHeapEvidenceIndex(b.summary.MemoryLeaks)
	for _, capture := range b.lambdaCaptures.Captures {
		for _, risk := range lambdaCaptureRisks(capture, heapEvidence) {
			className, method := codeLocationFromOwner(capture.Owner)
			where := []ProblemLocation{{Owner: capture.Owner, Class: className, Method: method}}
			var limitations []string
			if risk.detector == "memory.lambda_capture" && !risk.heapConfirmed {
				limitations = append(
					limitations,
					"Статический анализ нашёл захваченное значение и долгоживущего получателя, но без данных выполнения или дампа памяти нельзя доказать, что объект пережил владельца жизненного цикла.",
				)
			}
			if capture.Desugared {
				limitations = append(limitations, "D8/R8 сохранил точные поля захвата, но исходное место вызова Kotlin восстановлено приблизительно.")
			}
			b.add(ProblemFinding{
				DetectorID: risk.detector, DetectorVersion: b.cfg.Version,
				Category: categoryForLambdaRisk(risk), Subcategory: risk.subcategory, Status: "observed",
				Confidence: risk.confidence, ConfidenceReasons: []string{risk.confidenceReason},
				Title: risk.title, WhatHappened: risk.detail, Where: where,
				Why:    ProblemWhy{ClaimLevel: risk.claim, Summary: risk.why},
				Impact: []string{risk.impact},
				Evidence: []ProblemEvidence{
					{Name: "Захваченные значения", Observed: lambdaCapturedTypes(capture), Source: "lambda_capture_artifact"},
					{Name: "Долгоживущие получатели", Observed: lambdaSinkSummary(capture.Sinks), Source: "lambda_capture_artifact"},
				},
				PriorityBreakdown: risk.priority,
				Recommendations: []ProblemRecommendation{{
					Action:       risk.action,
					Rationale:    "Если захватывать только нужные значения или сократить время жизни получателя, лямбда не будет удерживать весь объект.",
					Verification: "Пересоберите вариант и убедитесь, что место вызова исчезло из lambda-captures.jsonl или больше не отмечено как риск. Для подтверждённой утечки повторите анализ дампа памяти.",
				}},
				Limitations: limitations,
			})
		}
	}
}

func lambdaCaptureRisks(capture LambdaCapture, heapEvidence lambdaHeapEvidenceIndex) []lambdaCaptureRisk {
	if capture.Suppressed {
		return nil
	}
	var risks []lambdaCaptureRisk
	if capture.WeakDereference == "force_unwrap" && lambdaHasWeakValue(capture) {
		risks = append(risks, lambdaCaptureRisk{
			detector: "stability.lambda_lifetime_mismatch", subcategory: "weak_reference_force_unwrap",
			title:  capture.Implementation + " небезопасно разыменовывает WeakReference",
			detail: "Лямбда не удерживает владельца жизненного цикла сильной ссылкой, но после destroy или GC принудительно считает результат WeakReference.get() ненулевым.",
			impact: "Если объект уже удалён, приложение может аварийно завершиться. Это не утечка памяти.",
			action: "Обработайте null от WeakReference.get() и завершите callback без обращения к уже уничтоженному владельцу.",
			claim:  "linked", confidence: "high",
			confidenceReason: "ASM-анализ связал WeakReference.get() с принудительной проверкой на null внутри лямбды.",
			why:              "После завершения lifecycle WeakReference.get() может вернуть null, поэтому результат нельзя считать ненулевым.",
			priority:         priority(28, 12, 8, 5, 0, "риск аварийного завершения", "принудительное разыменование", "долгоживущий callback", "точное место вызова", "нет подтверждения аварии при выполнении"),
		})
	}
	strongLifecycle := lambdaStrongLifecycleTypes(capture)
	if len(strongLifecycle) == 0 {
		return risks
	}
	sink := strongestLambdaSink(capture.Sinks)
	heapConfirmed := lambdaCaptureHeapConfirmed(capture, heapEvidence)
	if (sink == "safe_lifecycle" || sink == "compose.provider") && !heapConfirmed {
		return risks
	}
	if sink == "" && !heapConfirmed {
		return risks
	}
	priorityParts := priority(26, 10, 7, 5, 0, "возможное удержание экрана", "сильная ссылка", "неизвестная длительность", "точное место вызова", "нет подтверждения при выполнении")
	confidence := "medium"
	claim := "hypothesis"
	confidenceReason := "ASM-анализ нашёл сильную ссылку на объект с lifecycle и передачу лямбды в долгоживущий API."
	why := "Статический анализ байткода нашёл объект лямбды и место, которое может хранить его дольше владельца жизненного цикла."
	title := capture.Implementation + " удерживает объект с lifecycle"
	capturedTypeNames := strings.Join(strongLifecycle, ", ")
	detail := lambdaCaptureDetail(capture, capturedTypeNames, sink)
	action := "Захватывайте только нужные неизменяемые значения или используйте владельца с подходящим lifecycle."
	if sink == "" {
		confidenceReason = "ASM-анализ подтвердил сильное поле с объектом lifecycle в состоянии coroutine."
		why = "Coroutine хранит захваченное значение между точками приостановки."
	}
	switch sink {
	case "flow.global_scope", "coroutine.global_scope":
		priorityParts = priority(40, 20, 15, 5, 5, "объект переживает свой lifecycle", "сильная ссылка", "область всего процесса", "точное место вызова", "GlobalScope")
		detail = fmt.Sprintf("Лямбда сильно захватывает %s и запускается через GlobalScope, поэтому может жить до завершения процесса.", capturedTypeNames)
		action = "Замените GlobalScope на lifecycleScope, viewModelScope или управляемый CoroutineScope, который гарантированно отменяется."
	case "static.field":
		priorityParts = priority(40, 18, 16, 5, 5, "удержание до завершения процесса", "сильная ссылка", "статическое поле", "точное место вызова", "долгоживущий GC root")
	case "compose.derived_state":
		if strings.Contains(capture.Owner, "ViewModel") {
			priorityParts = priority(38, 18, 14, 5, 5, "Activity из ViewModel", "сильная ссылка", "долгоживущее состояние Compose", "точное место вызова", "разное время жизни")
		} else {
			priorityParts = priority(32, 14, 10, 5, 3, "удержание Composition", "сильная ссылка", "derived state", "точное место вызова", "кеш Compose")
		}
	case "compose.remember":
		priorityParts = priority(32, 14, 10, 5, 3, "удержание Composition", "сильная ссылка", "remember", "точное место вызова", "кеш Compose")
	case "handler.queue", "executor.queue", "listener.registration":
		priorityParts = priority(34, 14, 11, 5, 3, "удержание владельца lifecycle", "сильная ссылка", "долгоживущий callback", "точное место вызова", "внешняя очередь или реестр")
	case "flow.callback", "flow.lifecycle_launch", "instance.field", "":
		// Keep the conservative medium-priority baseline.
	}
	if heapConfirmed {
		confidence = "high"
		claim = "linked"
		confidenceReason = "Дамп памяти содержит точный путь через класс лямбды и её синтетическое поле."
		why = "Сильное поле из статического анализа совпало с путём ссылок в дампе памяти до объекта, пережившего lifecycle."
		priorityParts = priority(40, 20, 16, 7, 5, "дамп памяти подтвердил удержание", "сильная ссылка", "объект пережил lifecycle", "место вызова и путь ссылок", "совпали статический анализ и дамп памяти")
		detail += " Дамп памяти подтвердил сильную ссылку через синтетическое поле лямбды."
	}
	risks = append(risks, lambdaCaptureRisk{
		detector: "memory.lambda_capture", subcategory: "strong_lifecycle_capture",
		title: title, detail: detail,
		impact: "Объект с lifecycle и связанные с ним данные могут оставаться в памяти дольше ожидаемого.",
		action: action, claim: claim, confidence: confidence, confidenceReason: confidenceReason,
		why: why, heapConfirmed: heapConfirmed, priority: priorityParts,
	})
	return risks
}

func lambdaCaptureDetail(capture LambdaCapture, lifecycleTypes string, sink string) string {
	if sink != "" {
		return fmt.Sprintf("Лямбда сильно захватывает %s и передана в %s.", lifecycleTypes, lambdaSinkLabel(sink))
	}
	if capture.Representation == "coroutine" {
		return fmt.Sprintf("Coroutine хранит сильную ссылку на %s между точками приостановки.", lifecycleTypes)
	}
	return fmt.Sprintf("Объект лямбды хранит сильную ссылку на %s.", lifecycleTypes)
}

func lambdaCaptureHasRisk(capture LambdaCapture) bool {
	if capture.Suppressed {
		return false
	}
	if capture.WeakDereference == "force_unwrap" && lambdaHasWeakValue(capture) {
		return true
	}
	hasStrongLifecycle := false
	for _, value := range capture.Values {
		if value.Strength == "strong" && isLifecycleCaptureType(value.Type) {
			hasStrongLifecycle = true
			break
		}
	}
	if !hasStrongLifecycle {
		return false
	}
	sink := strongestLambdaSink(capture.Sinks)
	if sink == "safe_lifecycle" || sink == "compose.provider" {
		return false
	}
	return sink != ""
}

func categoryForLambdaRisk(risk lambdaCaptureRisk) string {
	if risk.detector == "stability.lambda_lifetime_mismatch" {
		return ProblemCategoryStability
	}
	return ProblemCategoryMemory
}

func lambdaStrongLifecycleTypes(capture LambdaCapture) []string {
	var result []string
	for _, value := range capture.Values {
		if value.Strength == "strong" && isLifecycleCaptureType(value.Type) {
			result = append(result, value.Type)
		}
	}
	return sortUniqueStringsInPlace(result)
}

func isLifecycleCaptureType(value string) bool {
	name := strings.TrimSpace(value)
	for strings.HasSuffix(name, "[]") {
		name = strings.TrimSuffix(name, "[]")
	}
	simple := name
	if separator := strings.LastIndexAny(simple, ".$"); separator >= 0 {
		simple = simple[separator+1:]
	}
	if name == "android.app.Application" || simple == "Application" {
		return false
	}
	return name == "android.content.Context" || name == "android.app.Activity" ||
		strings.HasSuffix(simple, "Activity") || strings.HasSuffix(simple, "Fragment") ||
		name == "android.app.Service" ||
		name == "android.view.View" || strings.HasSuffix(simple, "View") ||
		strings.Contains(simple, "ViewBinding") || strings.HasSuffix(simple, "Binding") ||
		strings.HasSuffix(simple, "Dialog")
}

func lambdaHasWeakValue(capture LambdaCapture) bool {
	for _, value := range capture.Values {
		if value.Strength == "weak" {
			return true
		}
	}
	return false
}

func strongestLambdaSink(sinks []string) string {
	best := ""
	bestRank := 14
	for _, sink := range sinks {
		rank := lambdaSinkRank(sink)
		if rank < bestRank {
			best = sink
			bestRank = rank
		}
	}
	if best == "flow.lifecycle_scope" || best == "flow.repeat_on_lifecycle" {
		return "safe_lifecycle"
	}
	return best
}

func lambdaSinkRank(sink string) int {
	switch sink {
	case "flow.global_scope":
		return 0
	case "coroutine.global_scope":
		return 1
	case "static.field":
		return 2
	case "compose.derived_state":
		return 3
	case "compose.remember":
		return 4
	case "handler.queue":
		return 5
	case "executor.queue":
		return 6
	case "listener.registration":
		return 7
	case "flow.callback":
		return 8
	case "flow.lifecycle_launch":
		return 9
	case "instance.field":
		return 10
	case "compose.provider":
		return 11
	case "flow.lifecycle_scope":
		return 12
	case "flow.repeat_on_lifecycle":
		return 13
	default:
		return 14
	}
}

func lambdaSinkSummary(sinks []string) string {
	if len(sinks) == 0 {
		return "не определён"
	}
	var builder strings.Builder
	for index, sink := range sinks {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(lambdaSinkLabel(sink))
	}
	return builder.String()
}

func lambdaSinkLabel(sink string) string {
	switch sink {
	case "flow.global_scope", "coroutine.global_scope":
		return "GlobalScope"
	case "compose.remember":
		return "кеш Compose remember"
	case "compose.derived_state":
		return "Compose derivedStateOf"
	case "handler.queue":
		return "очередь Handler"
	case "executor.queue":
		return "очередь Executor"
	case "listener.registration":
		return "реестр listener"
	case "static.field":
		return "статическое поле"
	case "instance.field":
		return "поле объекта"
	case "flow.callback":
		return "callbackFlow"
	case "flow.lifecycle_launch":
		return "lifecycle-метод launchWhen*"
	case "compose.provider":
		return "CompositionLocalProvider"
	case "safe_lifecycle":
		return "CoroutineScope владельца lifecycle"
	default:
		return sink
	}
}

func lambdaCapturedTypes(capture LambdaCapture) string {
	var builder strings.Builder
	for index, value := range capture.Values {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(value.Type)
		builder.WriteString(" (")
		builder.WriteString(lambdaCaptureStrengthLabel(value.Strength))
		builder.WriteByte(')')
	}
	return builder.String()
}

func lambdaCaptureStrengthLabel(value string) string {
	if value == "strong" {
		return "сильная ссылка"
	}
	if value == "weak" {
		return "слабая ссылка"
	}
	return value
}

type lambdaHeapEvidenceKey struct {
	implementation string
	field          string
}

type lambdaHeapEvidenceIndex map[lambdaHeapEvidenceKey]struct{}

func buildLambdaHeapEvidenceIndex(memoryLeaks []MemoryLeakSuspect) lambdaHeapEvidenceIndex {
	var index lambdaHeapEvidenceIndex
	for _, leak := range memoryLeaks {
		if !leak.HeapEvidence {
			continue
		}
		index = addLambdaHeapPath(index, leak.ReferencePath)
		for _, path := range leak.AlternativePaths {
			index = addLambdaHeapPath(index, path)
		}
	}
	return index
}

func addLambdaHeapPath(index lambdaHeapEvidenceIndex, path []HeapPathElement) lambdaHeapEvidenceIndex {
	for pathIndex := 1; pathIndex < len(path); pathIndex++ {
		implementation := normalizeClassName(strings.TrimPrefix(path[pathIndex-1].ClassName, "GC root: "))
		field := path[pathIndex].FieldName
		if implementation == "" || field == "" {
			continue
		}
		if index == nil {
			index = make(lambdaHeapEvidenceIndex)
		}
		index[lambdaHeapEvidenceKey{implementation: implementation, field: field}] = struct{}{}
	}
	return index
}

func lambdaCaptureHeapConfirmed(capture LambdaCapture, heapEvidence lambdaHeapEvidenceIndex) bool {
	if capture.Representation == "invokedynamic" {
		return false
	}
	implementation := normalizeClassName(capture.Implementation)
	if implementation == "" || len(heapEvidence) == 0 {
		return false
	}
	for _, value := range capture.Values {
		if value.Strength != "strong" || !isLifecycleCaptureType(value.Type) {
			continue
		}
		_, field, found := strings.Cut(value.Role, ":")
		if !found || field == "" {
			continue
		}
		key := lambdaHeapEvidenceKey{implementation: implementation, field: field}
		if _, confirmed := heapEvidence[key]; confirmed {
			return true
		}
	}
	return false
}
