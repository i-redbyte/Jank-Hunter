package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

const (
	composeSemanticPrefix = "jankhunter.semantic.v1.compose."
	roomSemanticPrefix    = "jankhunter.semantic.v1.room."
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
	Kind           string
	Thread         string
	Owner          string
	Context        string
	ContextHelp    string
	Count          uint64
	TotalMS        uint64
	MaxMS          uint64
	Status         string
	StatusHelp     string
	Severity       string
	Action         string
	NeedsAttention bool
}

type semanticWorkReport struct {
	Overview     semanticWorkOverview
	Problems     []semanticWorkRow
	Observations []semanticWorkRow
}

type semanticWorkVerdict struct {
	Status         string
	Help           string
	Severity       string
	Action         string
	NeedsAttention bool
}

func semanticWorkOverviewFromItems(items []analyze.SemanticWorkStats, domain string) *semanticWorkOverview {
	if len(items) == 0 {
		return nil
	}
	overview := semanticOverviewBase(domain)
	overview.Boundaries = len(items)
	for _, item := range items {
		overview.Executions += item.Count
		overview.MaxDuration = max(overview.MaxDuration, item.MaxMS)
		if item.MainThread {
			overview.MainThread += item.Count
		}
		if semanticWorkVerdictFor(item).NeedsAttention {
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
	return &overview
}

func semanticWorkRowsForDomain(summary analyze.Summary, domain string) []semanticWorkRow {
	return semanticWorkRows(semanticWorkForDomain(summary, domain))
}

func semanticWorkRows(items []analyze.SemanticWorkStats) []semanticWorkRow {
	rows := make([]semanticWorkRow, 0, len(items))
	for _, item := range items {
		verdict := semanticWorkVerdictFor(item)
		context, contextHelp := reportContextPresentation(item.Screen, item.ContextOperation, "")
		rows = append(rows, semanticWorkRow{
			Kind:    semanticOperationLabel(item),
			Thread:  ioThreadLabel(item.MainThread),
			Owner:   reportValue(item.Owner, "место кода не определено"),
			Context: context, ContextHelp: contextHelp,
			Count: item.Count, TotalMS: item.TotalMS, MaxMS: item.MaxMS,
			Status: verdict.Status, StatusHelp: verdict.Help, Severity: verdict.Severity,
			Action: verdict.Action, NeedsAttention: verdict.NeedsAttention,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].NeedsAttention != rows[j].NeedsAttention {
			return rows[i].NeedsAttention
		}
		leftRank, rightRank := severityRank(rows[i].Severity), severityRank(rows[j].Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if rows[i].MaxMS != rows[j].MaxMS {
			return rows[i].MaxMS > rows[j].MaxMS
		}
		return rows[i].Owner < rows[j].Owner
	})
	return rows
}

func composeWorkReport(summary analyze.Summary) *semanticWorkReport {
	items := semanticWorkForDomain(summary, analyze.SemanticDomainCompose)
	overview := semanticWorkOverviewFromItems(items, analyze.SemanticDomainCompose)
	if overview == nil {
		return nil
	}
	rows := semanticWorkRows(items)
	split := overview.Suspicious
	if split > len(rows) {
		split = len(rows)
	}
	return &semanticWorkReport{
		Overview:     *overview,
		Problems:     rows[:split:split],
		Observations: rows[split:],
	}
}

func hasComposeWork(summary analyze.Summary) bool {
	return hasSemanticWork(summary, composeSemanticPrefix)
}

func hasRoomWork(summary analyze.Summary) bool {
	return hasSemanticWork(summary, roomSemanticPrefix)
}

func hasSemanticWork(summary analyze.Summary, callerPrefix string) bool {
	for _, call := range summary.RuntimeCalls {
		if strings.HasPrefix(call.Caller, callerPrefix) {
			return true
		}
	}
	return false
}

func semanticWorkForDomain(summary analyze.Summary, domain string) []analyze.SemanticWorkStats {
	items := analyze.ActionableSemanticWork(summary)
	result := items[:0]
	for _, item := range items {
		if item.Domain == domain {
			result = append(result, item)
		}
	}
	return result
}

func roomWorkRows(summary analyze.Summary) []semanticWorkRow {
	return semanticWorkRowsForDomain(summary, analyze.SemanticDomainRoom)
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
			Domain: domain, Title: "Интерфейс на Jetpack Compose",
			Meaning:    "Каждая строка показывает, какая функция интерфейса действительно выполнялась, сколько раз и сколько занял худший запуск. Если один запуск занял весь бюджет кадра, пользователь мог увидеть рывок.",
			FirstCheck: "Сначала откройте красные и жёлтые строки ниже. В них уже указаны место в коде, причина подозрения и конкретная проверка.",
			Tooltip:    "Автоматическая инструментация считает выполнения функций с @Composable. Пропущенные Compose-группы не выполняются и не попадают в счётчик; ручной вызов traceComposeWork добавляет фазы измерения, размещения и отрисовки.",
		}
	case analyze.SemanticDomainRoom:
		return semanticWorkOverview{
			Domain: domain, Title: "Вызовы базы данных через Room",
			Meaning:    "Показывает вызовы сгенерированных DAO, их длительность и поток выполнения. DAO на главном потоке может непосредственно сорвать кадр.",
			FirstCheck: "Сначала устраните DAO на главном потоке, затем проверьте индексы, план запроса, размер результата и повторные обращения.",
			Tooltip:    "Автоматически измеряется синхронная граница сгенерированного метода доступа к данным Room. Для полной длительности асинхронного запроса добавьте traceIO вокруг фактической операции.",
		}
	default:
		return semanticWorkOverview{
			Domain: domain, Title: "Фоновые задачи",
			Meaning:    "Показывает длительность, повторы и результат фоновой задачи. Долгие или повторно неуспешные задачи расходуют ресурсы и задерживают синхронизацию.",
			FirstCheck: "Проверьте условия запуска, ограничения, задержку между повторами, идемпотентность и самый дорогой этап выполнения задачи.",
			Tooltip:    "Обычная фоновая задача и её результат измеряются автоматически. Для задачи с приостановками используйте traceSuspendingWorker, чтобы измерение продолжалось после первой точки приостановки и сохранило итог.",
		}
	}
}

func semanticWorkVerdictFor(item analyze.SemanticWorkStats) semanticWorkVerdict {
	switch item.Domain {
	case analyze.SemanticDomainCompose:
		if item.MainThread && item.MaxMS >= 16 {
			_, _, _, action := composeUICauseText(item.Operation)
			return semanticWorkVerdict{
				Status:   "превышен бюджет кадра",
				Help:     "Одно выполнение заняло не меньше стандартного бюджета кадра 16 мс и само могло вызвать видимый рывок. Сравните с целевым временем кадра конкретного экрана.",
				Severity: "high", Action: action, NeedsAttention: true,
			}
		}
		if item.MainThread && item.Count >= 120 && item.TotalMS >= 100 {
			return semanticWorkVerdict{
				Status:         "подозрительно частые выполнения",
				Help:           "Функция много раз выполнялась на главном потоке и накопила заметное время. Это требует проверки причин повторного выполнения.",
				Severity:       "medium",
				Action:         "Проверьте стабильность параметров, область чтения состояния и причины повторного выполнения этой функции.",
				NeedsAttention: true,
			}
		}
	case analyze.SemanticDomainRoom:
		if item.MainThread && item.MaxMS >= 16 {
			return semanticWorkVerdict{
				Status:   "база данных на главном потоке",
				Help:     "DAO выполнялся на главном потоке и мог блокировать построение кадра или обработку ввода.",
				Severity: "high", Action: "Перенесите обращение к базе с главного потока и повторите сценарий.", NeedsAttention: true,
			}
		}
		if (!item.MainThread && item.MaxMS >= 500) || item.Count >= 50 {
			return semanticWorkVerdict{
				Status:   "долгая или частая работа с базой данных",
				Help:     "Проверьте план запроса, индексы, размер результата и повторные обращения.",
				Severity: "medium", Action: "Проверьте план запроса, индексы, размер результата и причины повторных обращений.", NeedsAttention: true,
			}
		}
	case analyze.SemanticDomainWorker:
		if item.Outcome == "failure" || item.Outcome == "retry" || item.Outcome == "cancelled" {
			return semanticWorkVerdict{
				Status: "неуспешное завершение",
				Help:   "Задача завершилась ошибкой, повторным запуском или отменой.", Severity: "high",
				Action: "Проверьте причину завершения, условия запуска и политику повторов.", NeedsAttention: true,
			}
		}
		if item.MaxMS >= 10_000 || (item.Count >= 10 && item.TotalMS >= 30_000) {
			return semanticWorkVerdict{
				Status: "долгая или повторная задача", Help: "Задача долго выполнялась или накопила большую стоимость.",
				Severity: "medium", Action: "Разбейте работу на измеримые этапы и найдите самый дорогой из них.", NeedsAttention: true,
			}
		}
	}
	return semanticWorkVerdict{
		Status:   "явных отклонений нет",
		Help:     "Порог явной проблемы не превышен. Это не гарантия оптимальности, а отсутствие сильного сигнала в записанном прогоне.",
		Severity: "ok", Action: "Дополнительные действия не требуются, пока эта строка не совпадает с пользовательской проблемой.",
	}
}

func semanticOperationLabel(item analyze.SemanticWorkStats) string {
	if item.Domain == analyze.SemanticDomainWorker {
		return "Выполнение · итог: " + semanticOutcomeLabel(item.Outcome)
	}
	switch item.Operation {
	case "composition":
		return "Построение интерфейса"
	case "measure":
		return "Измерение размеров"
	case "layout":
		return "Размещение элементов"
	case "draw":
		return "Отрисовка"
	case "dao":
		return "Вызов DAO"
	default:
		return "Выполнение"
	}
}

func semanticOutcomeLabel(outcome string) string {
	switch outcome {
	case "success":
		return "успешно"
	case "failure":
		return "ошибка"
	case "retry":
		return "повтор"
	case "cancelled":
		return "отмена"
	default:
		return "неизвестен"
	}
}
