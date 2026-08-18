package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type semanticWorkOverview struct {
	Domain      string
	Title       string
	Status      string
	Severity    string
	Summary     string
	Meaning     string
	FirstCheck  string
	Tooltip     string
	Executions  uint64
	Boundaries  int
	Suspicious  int
	MainThread  uint64
	MaxDuration uint64
}

type semanticWorkRow struct {
	Domain     string
	Kind       string
	Thread     string
	Owner      string
	Context    string
	Count      uint64
	TotalMS    uint64
	MaxMS      uint64
	Status     string
	StatusHelp string
}

func semanticWorkOverviews(summary analyze.Summary) []semanticWorkOverview {
	groups := map[string][]analyze.SemanticWorkStats{}
	for _, item := range analyze.ActionableSemanticWork(summary) {
		groups[item.Domain] = append(groups[item.Domain], item)
	}
	order := []string{analyze.SemanticDomainCompose, analyze.SemanticDomainRoom, analyze.SemanticDomainWorker}
	result := make([]semanticWorkOverview, 0, len(groups))
	for _, domain := range order {
		items := groups[domain]
		if len(items) == 0 {
			continue
		}
		overview := semanticOverviewBase(domain)
		overview.Boundaries = len(items)
		for _, item := range items {
			overview.Executions += item.Count
			overview.MaxDuration = max(overview.MaxDuration, item.MaxMS)
			if item.MainThread {
				overview.MainThread += item.Count
			}
			if semanticWorkSuspicious(item) {
				overview.Suspicious++
			}
		}
		overview.Status, overview.Severity = "измерено, явных отклонений нет", "ok"
		if overview.Suspicious > 0 {
			overview.Status, overview.Severity = "есть кандидаты для проверки", "medium"
		}
		overview.Summary = fmt.Sprintf(
			"%s в %s; самое долгое — %d мс.",
			russianCount(overview.Executions, "выполнение", "выполнения", "выполнений"),
			russianCount(overview.Boundaries, "границе кода", "границах кода", "границах кода"),
			overview.MaxDuration,
		)
		if overview.MainThread > 0 {
			overview.Summary += " На главном потоке — " + russianCount(overview.MainThread, "выполнение", "выполнения", "выполнений") + "."
		}
		if overview.Suspicious > 0 {
			overview.Summary += " Требуют внимания: " + russianCount(overview.Suspicious, "граница", "границы", "границ") + "."
		}
		result = append(result, overview)
	}
	return result
}

func semanticWorkRows(summary analyze.Summary) []semanticWorkRow {
	items := analyze.ActionableSemanticWork(summary)
	rows := make([]semanticWorkRow, 0, len(items))
	for _, item := range items {
		status, help := semanticWorkStatus(item)
		rows = append(rows, semanticWorkRow{
			Domain: semanticDomainLabel(item.Domain), Kind: semanticOperationLabel(item),
			Thread:  map[bool]string{true: "главный", false: "фоновый"}[item.MainThread],
			Owner:   reportValue(item.Owner, "место кода не определено"),
			Context: semanticContext(item), Count: item.Count, TotalMS: item.TotalMS, MaxMS: item.MaxMS,
			Status: status, StatusHelp: help,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftAttention, rightAttention := rows[i].Status != "наблюдение", rows[j].Status != "наблюдение"
		if leftAttention != rightAttention {
			return leftAttention
		}
		if rows[i].MaxMS != rows[j].MaxMS {
			return rows[i].MaxMS > rows[j].MaxMS
		}
		return rows[i].Owner < rows[j].Owner
	})
	return rows
}

func ordinaryRuntimeCalls(summary analyze.Summary) []analyze.RuntimeCallStats {
	result := make([]analyze.RuntimeCallStats, 0, len(summary.RuntimeCalls))
	for _, call := range summary.RuntimeCalls {
		if !analyze.IsSemanticRuntimeCall(call.Caller) {
			result = append(result, call)
		}
	}
	return result
}

func semanticOverviewBase(domain string) semanticWorkOverview {
	switch domain {
	case analyze.SemanticDomainCompose:
		return semanticWorkOverview{
			Domain: domain, Title: "Jetpack Compose",
			Meaning:    "Показывает фактические выполнения функций с @Composable и явно размеченные фазы измерения, размещения и отрисовки. Само повторное выполнение ещё не означает дефект.",
			FirstCheck: "Для долгих выполнений откройте указанную функцию; для частых — проверьте стабильность параметров и области чтения состояния.",
			Tooltip:    "Автоматическая инструментация считает выполнения функций с @Composable. Пропущенные Compose-группы не выполняются и не попадают в счётчик; ручной вызов traceComposeWork добавляет фазы измерения, размещения и отрисовки.",
		}
	case analyze.SemanticDomainRoom:
		return semanticWorkOverview{
			Domain: domain, Title: "Room и база данных",
			Meaning:    "Показывает вызовы сгенерированных DAO, их длительность и поток выполнения. DAO на главном потоке может непосредственно сорвать кадр.",
			FirstCheck: "Сначала устраните DAO на главном потоке, затем проверьте индексы, план запроса, размер результата и повторные обращения.",
			Tooltip:    "Автоматически измеряется синхронная граница сгенерированного Room DAO. Для полной длительности асинхронного запроса добавьте traceIO вокруг фактической операции.",
		}
	default:
		return semanticWorkOverview{
			Domain: domain, Title: "Worker и фоновые задачи",
			Meaning:    "Показывает длительность, повторы и результат фоновой задачи. Долгие или повторно неуспешные задачи расходуют ресурсы и задерживают синхронизацию.",
			FirstCheck: "Проверьте условия запуска, ограничения, задержку между повторами, идемпотентность и самый дорогой этап doWork.",
			Tooltip:    "Синхронный Worker и его результат измеряются автоматически. Для CoroutineWorker используйте traceSuspendingWorker, чтобы измерение продолжалось после первой точки приостановки и сохранило итог.",
		}
	}
}

func semanticWorkSuspicious(item analyze.SemanticWorkStats) bool {
	switch item.Domain {
	case analyze.SemanticDomainCompose:
		return item.MainThread && (item.MaxMS >= 16 || (item.Count >= 120 && item.TotalMS >= 100))
	case analyze.SemanticDomainRoom:
		return (item.MainThread && item.MaxMS >= 16) ||
			(!item.MainThread && item.MaxMS >= 500) ||
			item.Count >= 50
	case analyze.SemanticDomainWorker:
		return item.Outcome == "failure" ||
			item.Outcome == "retry" ||
			item.Outcome == "cancelled" ||
			item.MaxMS >= 10_000 ||
			(item.Count >= 10 && item.TotalMS >= 30_000)
	default:
		return false
	}
}

func semanticWorkStatus(item analyze.SemanticWorkStats) (string, string) {
	if !semanticWorkSuspicious(item) {
		return "наблюдение", "Порог явной проблемы не превышен. Это не гарантия оптимальности, а отсутствие сильного сигнала в записанном прогоне."
	}
	switch item.Domain {
	case analyze.SemanticDomainCompose:
		if item.MaxMS >= 16 {
			return "превышен бюджет кадра", "Одно выполнение заняло не меньше стандартного бюджета кадра 16 мс. Сравните с фактической частотой дисплея и UI-карточкой экрана."
		}
		return "подозрительно часто", "Большое число выполнений и заметное суммарное время требуют проверки стабильности параметров и областей чтения состояния."
	case analyze.SemanticDomainRoom:
		if item.MainThread {
			return "БД на главном потоке", "DAO выполнялся на главном потоке и мог блокировать построение кадра или ввод."
		}
		return "долгая или частая работа с БД", "Проверьте план запроса, индексы, размер результата и повторные обращения."
	default:
		if item.Outcome == "failure" || item.Outcome == "retry" || item.Outcome == "cancelled" {
			return "неуспешный итог", "Задача завершилась ошибкой, повтором или отменой; проверьте причины и политику повторного запуска."
		}
		return "долгая или повторная задача", "Проверьте ограничения запуска и разбейте работу на измеримые этапы."
	}
}

func semanticDomainLabel(domain string) string {
	return map[string]string{analyze.SemanticDomainCompose: "Compose", analyze.SemanticDomainRoom: "Room", analyze.SemanticDomainWorker: "Worker"}[domain]
}

func semanticOperationLabel(item analyze.SemanticWorkStats) string {
	if item.Domain == analyze.SemanticDomainWorker {
		return "Выполнение · итог: " + map[string]string{"success": "успех", "failure": "ошибка", "retry": "повтор", "cancelled": "отмена", "unknown": "неизвестен"}[item.Outcome]
	}
	return map[string]string{"composition": "Композиция", "measure": "Измерение", "layout": "Размещение", "draw": "Отрисовка", "dao": "Вызов DAO"}[item.Operation]
}

func semanticContext(item analyze.SemanticWorkStats) string {
	parts := make([]string, 0, 3)
	for _, value := range []struct{ label, value string }{{"экран", item.Screen}, {"сценарий", item.Flow}, {"шаг", item.Step}} {
		if !isUnknownReportValue(value.value) {
			parts = append(parts, value.label+" "+value.value)
		}
	}
	if len(parts) == 0 {
		return "контекст не размечен"
	}
	return strings.Join(parts, " · ")
}
