package analyze

import "strings"

func codeProblemImpact(categories []string, runtimeEvidence bool) string {
	if runtimeEvidence && len(categories) == 1 && categories[0] == codeCategoryRuntime {
		return codeProblemRuntimeImpact
	}
	additional := ""
	if !runtimeEvidence {
		additional = "пока нет подтверждения выполнением в этом прогоне"
	}
	return joinCodeProblemGuidance(
		categories,
		codeProblemGuidanceImpact,
		additional,
		false,
		"Нужна ручная проверка: сигнал есть, но влияние пока слабое.",
	)
}

func codeProblemRecommendation(categories []string) string {
	if len(categories) == 1 && categories[0] == codeCategoryRuntime {
		return codeProblemRuntimeRecommendation
	}
	return joinCodeProblemGuidance(
		categories,
		codeProblemGuidanceRecommendation,
		"",
		true,
		"Проверьте источник вручную и сопоставьте его с временной шкалой.",
	)
}

type codeProblemGuidanceKind uint8

const (
	codeProblemGuidanceImpact codeProblemGuidanceKind = iota
	codeProblemGuidanceRecommendation
)

func joinCodeProblemGuidance(
	categories []string,
	kind codeProblemGuidanceKind,
	additional string,
	deduplicate bool,
	fallback string,
) string {
	length := 1
	count := 0
	for index, category := range categories {
		message := codeProblemGuidanceMessage(category, kind)
		if message == "" || deduplicate && codeProblemGuidanceSeen(categories[:index], message, kind) {
			continue
		}
		if count > 0 {
			length += 2
		}
		length += len(message)
		count++
	}
	if additional != "" {
		if count > 0 {
			length += 2
		}
		length += len(additional)
		count++
	}
	if count == 0 {
		return fallback
	}
	var builder strings.Builder
	builder.Grow(length)
	written := 0
	for index, category := range categories {
		message := codeProblemGuidanceMessage(category, kind)
		if message == "" || deduplicate && codeProblemGuidanceSeen(categories[:index], message, kind) {
			continue
		}
		if written > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(message)
		written++
	}
	if additional != "" {
		if written > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(additional)
	}
	builder.WriteByte('.')
	return builder.String()
}

func codeProblemGuidanceSeen(categories []string, message string, kind codeProblemGuidanceKind) bool {
	for _, category := range categories {
		if codeProblemGuidanceMessage(category, kind) == message {
			return true
		}
	}
	return false
}

func codeProblemGuidanceMessage(category string, kind codeProblemGuidanceKind) string {
	if kind == codeProblemGuidanceImpact {
		switch category {
		case codeCategoryNetwork:
			return "увеличивает задержки сценария и может создавать сетевые циклы"
		case codeCategoryUI:
			return "ухудшает плавность интерфейса и отклик на действия"
		case codeCategoryMainThread:
			return "блокирует главный поток, повышая риск АНР и пропуска кадров"
		case codeCategoryMemory:
			return "повышает давление памяти, частоту GC и риск удержаний"
		case codeCategoryLogs:
			return "создает шум логами и лишнюю работу в горячем сценарии"
		case codeCategoryRuntime:
			return "утяжеляет цепочку выполнения в измеренном сценарии"
		case codeCategoryInfluence:
			return "попал в граф влияния рядом с симптомами; это подсказка для расследования, а не самостоятельное доказательство бага"
		case codeCategoryANR:
			return "создает риск ANR из-за долгой работы или цепочки на главном потоке"
		case codeCategoryOOM:
			return "повышает риск OOM из-за роста памяти или удержаний"
		case codeCategoryGCPressure:
			return "создает давление GC и может давать периодические паузы"
		case codeCategoryDuplicate:
			return "может дублировать сетевые запросы или повторять один маршрут без дедупликации"
		case codeCategoryLifecycle:
			return "похож на утечку жизненного цикла: объект живет дольше экрана или сценария"
		case codeCategoryLogSpam:
			return "создает спам логами в горячем пути"
		case codeCategoryMainIO:
			return "указывает на риск IO на главном потоке"
		default:
			return ""
		}
	}
	switch category {
	case codeCategoryNetwork:
		return "проверьте дедупликацию запросов, кеширование, таймауты и повторные фоновые циклы"
	case codeCategoryUI:
		return "проверьте отрисовку, привязку данных, сложную компоновку и работу при прокрутке"
	case codeCategoryMainThread:
		return "перенесите тяжёлую работу с главного потока и проверьте цепочку диспетчеризации, обработки нажатий и слушателей"
	case codeCategoryMemory:
		return "проверьте владельцев ссылок, жизненный цикл, кеши и рост PSS рядом с GC"
	case codeCategoryLogs:
		return "уменьшите частоту логирования или вынесите шумные отладочные логи из часто выполняемого пути"
	case codeCategoryRuntime:
		return "проверьте цепочку вызовов и стоимость вызываемого метода"
	case codeCategoryInfluence:
		return "откройте граф влияния и проверьте соседние узлы с подтверждёнными вызовами; приоритет выше, если рядом есть паузы, сеть, память или вызовы при выполнении"
	case codeCategoryANR:
		return "разбейте долгую работу, проверьте StrictMode и трассу выполнения и уберите блокировки с главного потока"
	case codeCategoryOOM:
		return "проверьте рост кучи и PSS, лимиты кэшей, создание изображений и буферов и жизненный цикл владельцев"
	case codeCategoryGCPressure:
		return "уменьшите текучесть аллокаций в горячем пути и проверьте повторные сборки/создание временных объектов"
	case codeCategoryDuplicate:
		return "добавьте дедупликацию запросов в работе, кеширование ответа или задержку повторного запуска сценария"
	case codeCategoryLifecycle:
		return "проверьте очистку слушателей, обратных вызовов и привязок представления, а также отмену корутинных задач и задач исполнителя на границе жизненного цикла"
	case codeCategoryLogSpam:
		return "ограничьте частоту логов, уберите отладочные логи из часто выполняемого пути или агрегируйте события"
	case codeCategoryMainIO:
		return "вынесите дисковый и сетевой ввод-вывод с главного потока и проверьте нарушения StrictMode"
	default:
		return ""
	}
}
