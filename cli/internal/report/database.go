package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type databaseStatementRow struct {
	Query                  string
	Source                 string
	Framework              string
	Operation              string
	Screen                 string
	Context                string
	ContextCount           int
	Overall                analyze.DatabaseExecutionStats
	Main                   analyze.DatabaseExecutionStats
	Background             analyze.DatabaseExecutionStats
	Telemetry              analyze.DatabaseTelemetryStats
	MainCorrelation        analyze.DatabaseCorrelationStats
	BackgroundCorrelation  analyze.DatabaseCorrelationStats
	PeakCallsPerSecond     uint64
	PeakWindowStartMS      uint64
	RapidRepeats           uint64
	EstimatedCalls         uint64
	FrequencyEstimateError uint64
	Status                 string
	Severity               string
	Problem                bool
	Why                    string
	Action                 string
	Contexts               []analyze.DatabaseStatementContextStats
}

type databaseTransactionRow struct {
	Stats       analyze.DatabaseTransactionStats
	Thread      string
	Mode        string
	Outcome     string
	FailureKind string
	Status      string
	Severity    string
	Problem     bool
	Why         string
	Action      string
}

type databaseScenarioRow struct {
	Stats    analyze.DatabaseScenarioStats
	Title    string
	Severity string
	Why      string
	Action   string
}

func databaseScenarioRows(summary analyze.Summary) []databaseScenarioRow {
	if summary.DatabaseAnalysis == nil {
		return nil
	}
	rows := make([]databaseScenarioRow, 0, len(summary.DatabaseAnalysis.Scenarios.Candidates))
	for _, scenario := range summary.DatabaseAnalysis.Scenarios.Candidates {
		title := "возможный N+1 или повторный запрос"
		if scenario.Kind == "batch_candidate" {
			title = "возможность пакетной обработки"
		}
		severity := "medium"
		if scenario.MainThreadCalls > 0 || scenario.Failures > 0 {
			severity = "high"
		}
		costQualifier := "наблюдаемая стоимость"
		if scenario.CostLowerBound {
			costQualifier = "нижняя граница стоимости"
		}
		rows = append(rows, databaseScenarioRow{
			Stats: scenario, Title: title, Severity: severity,
			Why: fmt.Sprintf(
				"Один нормализованный SQL-шаблон вызван до %d раз внутри одной границы сценария (%s); %s — %s. Серия повторов подтверждена, но N+1 или одинаковые параметры не доказаны.",
				scenario.MaxCallsPerScope, databaseScenarioScopeLabel(scenario.ScopeKind), costQualifier,
				humanMicroseconds(scenario.TotalDurationUS),
			),
			Action: "Проверьте пакетную обработку, объединённый запрос и кэширование как гипотезы; оставьте изменение только если в том же сценарии снизились число вызовов на одну границу и общая длительность БД при неизменном результате.",
		})
	}
	return rows
}

func databaseScenarioScopeLabel(value string) string {
	switch value {
	case "transaction":
		return "транзакция"
	case "operation":
		return "операция приложения"
	default:
		return "граница сценария"
	}
}

func databasePlanKindLabel(value string) string {
	switch value {
	case "scan":
		return "SCAN"
	case "temp_btree":
		return "TEMP B-TREE"
	case "automatic_index":
		return "AUTOMATIC INDEX"
	default:
		return value
	}
}

func databaseTransactionRows(summary analyze.Summary) []databaseTransactionRow {
	if summary.DatabaseAnalysis == nil || summary.DatabaseAnalysis.Transactions == nil {
		return nil
	}
	cfg := analyze.DefaultProblemDetectorConfig()
	rows := make([]databaseTransactionRow, 0, len(summary.DatabaseAnalysis.Transactions.Transactions))
	for _, transaction := range summary.DatabaseAnalysis.Transactions.Transactions {
		status, severity := databaseTransactionStatus(transaction, cfg)
		thread := "фон"
		if transaction.MainThread {
			thread = "главный поток"
		}
		rows = append(rows, databaseTransactionRow{
			Stats: transaction, Thread: thread,
			Mode:        databaseTaxonomyLabel(transaction.Mode),
			Outcome:     databaseTaxonomyLabel(transaction.Outcome),
			FailureKind: databaseTaxonomyLabel(transaction.FailureKind),
			Status:      status, Severity: severity, Problem: severity != "ok",
			Why:    databaseTransactionWhy(transaction, status),
			Action: databaseTransactionAction(transaction, cfg),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftRank, rightRank := severityRank(rows[i].Severity), severityRank(rows[j].Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if rows[i].Stats.DurationUS != rows[j].Stats.DurationUS {
			return rows[i].Stats.DurationUS > rows[j].Stats.DurationUS
		}
		if rows[i].Stats.StatementCount != rows[j].Stats.StatementCount {
			return rows[i].Stats.StatementCount > rows[j].Stats.StatementCount
		}
		if rows[i].Stats.Source != rows[j].Stats.Source {
			return rows[i].Stats.Source < rows[j].Stats.Source
		}
		return rows[i].Stats.TransactionID < rows[j].Stats.TransactionID
	})
	return rows
}

func databaseProblemTransactionRows(rows []databaseTransactionRow) []databaseTransactionRow {
	result := make([]databaseTransactionRow, 0, len(rows))
	for _, row := range rows {
		if row.Problem {
			result = append(result, row)
		}
	}
	return result
}

func databaseObservedTransactionRows(rows []databaseTransactionRow) []databaseTransactionRow {
	result := make([]databaseTransactionRow, 0, len(rows))
	for _, row := range rows {
		if !row.Problem {
			result = append(result, row)
		}
	}
	return result
}

func databaseTransactionStatus(
	transaction analyze.DatabaseTransactionStats,
	cfg analyze.ProblemDetectorConfig,
) (string, string) {
	assessment := analyze.AssessDatabaseTransaction(transaction, cfg)
	signals := 0
	for _, signal := range [...]bool{
		assessment.MainThreadSlow, assessment.BackgroundSlow, assessment.Failure,
		assessment.Rollback, assessment.ManyStatements, assessment.Incomplete, assessment.Nested,
	} {
		if signal {
			signals++
		}
	}
	switch {
	case assessment.Failure && (assessment.MainThreadSlow || assessment.ManyStatements):
		return "ошибка с дополнительным влиянием", "high"
	case assessment.MainThreadSlow:
		return "долгая транзакция на главном потоке", "high"
	case assessment.Failure:
		return "транзакция завершилась ошибкой", "high"
	case assessment.Incomplete:
		return "нет события завершения", "medium"
	case assessment.Rollback && signals > 1:
		return "откат с дополнительными признаками", "medium"
	case assessment.Rollback:
		return "явный откат", "medium"
	case assessment.ManyStatements:
		return "много SQL-вызовов", "medium"
	case assessment.BackgroundSlow:
		return "долгая фоновая транзакция", "medium"
	case assessment.Nested:
		return "вложенная транзакция", "low"
	default:
		return "явных отклонений нет", "ok"
	}
}

func databaseTransactionWhy(transaction analyze.DatabaseTransactionStats, status string) string {
	if !transaction.Complete {
		return status + ": начало зафиксировано, но событие завершения отсутствует; результат и длительность не восстанавливаются из конца журнала."
	}
	return fmt.Sprintf(
		"%s: %s, %s, %d SQL-вызовов (чтение %d / запись %d). Общую длительность нельзя разделить на ожидание и выполнение без измерений отдельных фаз.",
		status,
		databaseTaxonomyLabel(transaction.Outcome),
		humanMicroseconds(transaction.DurationUS),
		transaction.StatementCount,
		transaction.ReadCount,
		transaction.WriteCount,
	)
}

func databaseTransactionAction(
	transaction analyze.DatabaseTransactionStats,
	cfg analyze.ProblemDetectorConfig,
) string {
	assessment := analyze.AssessDatabaseTransaction(transaction, cfg)
	switch {
	case assessment.Incomplete:
		return "Проверьте все пути выхода; подтвердите повторным прогоном, что у каждой начатой транзакции есть событие завершения."
	case assessment.MainThreadSlow:
		return "Перенесите транзакцию с главного потока и сократите её границу; проверьте число таких вызовов и длительность той же операции."
	case assessment.Failure || assessment.Rollback:
		return "Проверьте результат завершения и правила повторов или отката в указанном месте кода; повторите те же входные условия до стабильного успеха."
	case assessment.ManyStatements:
		return "Проверьте пакетную запись, вставку с обновлением или объединённый запрос как гипотезы; результат подтверждается снижением числа SQL-вызовов и длительности при том же поведении."
	case assessment.BackgroundSlow:
		return "Сократите транзакционную границу и добавьте доверенный адаптер для измерения фаз, прежде чем выбирать причину задержки."
	case assessment.Nested:
		return "Проверьте необходимость родительской и дочерней транзакций и семантику вложенности конкретного адаптера."
	default:
		return "Используйте строку как контрольное значение и сравнивайте тот же сценарий по длительности, результату и числу SQL-вызовов."
	}
}

func databaseTaxonomyLabel(value string) string {
	switch value {
	case "busy_locked":
		return "БД занята или заблокирована"
	case "disk_full":
		return "диск заполнен"
	case "read_only":
		return "только чтение"
	case "affected_rows":
		return "изменённые строки"
	case "success":
		return "успешно"
	case "failure":
		return "ошибка"
	case "rollback":
		return "откат"
	case "incomplete":
		return "не завершена"
	case "exclusive":
		return "исключительная"
	case "immediate":
		return "немедленная"
	case "deferred":
		return "отложенная"
	case "2_10":
		return "2–10"
	case "11_100":
		return "11–100"
	case "101_plus":
		return ">100"
	case "none", "unknown", "":
		return "не определено"
	default:
		return value
	}
}

func databaseStatementRows(summary analyze.Summary) []databaseStatementRow {
	if summary.DatabaseAnalysis == nil {
		return nil
	}
	cfg := analyze.DefaultProblemDetectorConfig()
	rows := make([]databaseStatementRow, 0, len(summary.DatabaseAnalysis.Statements))
	for _, statement := range summary.DatabaseAnalysis.Statements {
		status, severity := databaseStatementStatus(statement, cfg)
		var primary analyze.DatabaseStatementContextStats
		if len(statement.Contexts) > 0 {
			primary = statement.Contexts[0]
		}
		rows = append(rows, databaseStatementRow{
			Query:        reportValue(statement.Query, "SQL-текст не определён"),
			Source:       reportValue(primary.Source, "место вызова не определено"),
			Framework:    reportValue(primary.Framework, "не определено"),
			Operation:    reportValue(statement.Operation, "не определена"),
			Screen:       reportValue(primary.Screen, "без экрана"),
			Context:      reportValue(databaseContextLabel(primary), "без контекста"),
			ContextCount: len(statement.Contexts),
			Overall:      statement.Overall, Main: statement.Main, Background: statement.Background,
			Telemetry:       statement.Telemetry,
			MainCorrelation: statement.MainCorrelation, BackgroundCorrelation: statement.BackgroundCorrelation,
			PeakCallsPerSecond: statement.PeakCallsPerSecond,
			PeakWindowStartMS:  statement.PeakWindowStartMS, RapidRepeats: statement.RapidRepeats,
			EstimatedCalls: statement.EstimatedCalls, FrequencyEstimateError: statement.FrequencyEstimateError,
			Status: status, Severity: severity, Problem: severity != "ok",
			Why: databaseStatementWhy(statement, status), Action: databaseStatementAction(statement, cfg),
			Contexts: statement.Contexts,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftRank, rightRank := severityRank(rows[i].Severity), severityRank(rows[j].Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if rows[i].Overall.P95DurationUS != rows[j].Overall.P95DurationUS {
			return rows[i].Overall.P95DurationUS > rows[j].Overall.P95DurationUS
		}
		if rows[i].Overall.Calls != rows[j].Overall.Calls {
			return rows[i].Overall.Calls > rows[j].Overall.Calls
		}
		if rows[i].Query != rows[j].Query {
			return rows[i].Query < rows[j].Query
		}
		return rows[i].Source < rows[j].Source
	})
	return rows
}

func databaseProblemRows(rows []databaseStatementRow) []databaseStatementRow {
	result := make([]databaseStatementRow, 0, len(rows))
	for _, row := range rows {
		if row.Problem {
			result = append(result, row)
		}
	}
	return result
}

func databaseObservationRows(rows []databaseStatementRow) []databaseStatementRow {
	result := make([]databaseStatementRow, 0, len(rows))
	for _, row := range rows {
		if !row.Problem {
			result = append(result, row)
		}
	}
	return result
}

func databaseStatementWhy(statement analyze.DatabaseStatementStats, status string) string {
	if statement.MainCorrelation.UIWindowOverlaps > 0 {
		return fmt.Sprintf(
			"%s; точное сопоставление интервалов подтвердило %d пересечений SQL на главном потоке с проблемным окном UI.",
			status,
			statement.MainCorrelation.UIWindowOverlaps,
		)
	}
	return fmt.Sprintf(
		"%s: на главном потоке %d вызовов (верхние 5%% — %s), в фоне %d (верхние 5%% — %s), ошибок %d, быстрых повторов %d.",
		status,
		statement.Main.Calls,
		humanMicroseconds(statement.Main.P95DurationUS),
		statement.Background.Calls,
		humanMicroseconds(statement.Background.P95DurationUS),
		statement.Overall.Failures,
		statement.RapidRepeats,
	)
}

func databaseStatementAction(
	statement analyze.DatabaseStatementStats,
	cfg analyze.ProblemDetectorConfig,
) string {
	assessment := analyze.AssessDatabaseStatement(statement, cfg)
	switch {
	case assessment.MainThreadSlow:
		return "Уберите выполнение с главного потока; затем повторите ту же операцию и сравните верхние 5%, максимум, пересечения с UI и длительность операции."
	case assessment.Failures:
		return "Проверьте место вызова и результат выполнения; воспроизведите операцию и подтвердите отсутствие ошибок."
	case assessment.Storm || assessment.Repeated:
		return "Проверьте N+1/дубликаты внутри указанной операции и объедините чтения или записи; подтвердите снижением вызовов на операцию и быстрых повторов."
	case assessment.BackgroundSlow:
		if databaseOperationIsRead(statement.Operation) {
			return "Проверьте план запроса, объём выборки и индексы как гипотезы; подтвердите улучшение снижением верхних 5% и максимума фоновых длительностей в том же сценарии."
		}
		return "Проверьте размер пакета, границы транзакции и конкуренцию записей; подтвердите улучшение снижением верхних 5% и максимума фоновых длительностей и числа вызовов на транзакцию в том же сценарии."
	default:
		return "Сохраняйте как контрольную строку; после изменения сравните тот же сценарий по частоте, задержке и общей длительности БД."
	}
}

func databaseOperationIsRead(operation string) bool {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "query", "read", "select", "чтение":
		return true
	default:
		return false
	}
}

func databaseContextLabel(context analyze.DatabaseStatementContextStats) string {
	if context.ContextOperation != "" && context.ContextOperation != "unknown" {
		return context.ContextOperation
	}
	return context.ContextOwner
}

func databaseStatementStatus(statement analyze.DatabaseStatementStats, cfg analyze.ProblemDetectorConfig) (string, string) {
	assessment := analyze.AssessDatabaseStatement(statement, cfg)
	signalCount := 0
	for _, signal := range [...]bool{
		assessment.MainThreadSlow,
		assessment.BackgroundSlow,
		assessment.Storm,
		assessment.Repeated,
		assessment.Failures,
	} {
		if signal {
			signalCount++
		}
	}
	switch {
	case assessment.MainThreadSlow && statement.MainCorrelation.UIWindowOverlaps > 0 &&
		statement.MainCorrelation.UIOverlapMaxDurationUS >= cfg.DatabaseMainThreadMS*1_000:
		return "SQL на главном потоке пересёк проблемное окно UI", "high"
	case assessment.MainThreadSlow && signalCount > 1:
		return "блокирует UI и имеет дополнительные признаки", "high"
	case assessment.MainThreadSlow:
		return "занимает бюджет кадра на главном потоке", "high"
	case signalCount > 1:
		return "несколько признаков проблемы", "medium"
	case assessment.BackgroundSlow:
		return "медленный фоновый вызов", "medium"
	case assessment.Storm:
		return "всплеск частоты", "medium"
	case assessment.Repeated:
		return "быстрые повторы", "medium"
	case assessment.Failures:
		return "ошибки выполнения", "medium"
	default:
		return "явных отклонений нет", "ok"
	}
}

func databaseCoverage(known, total uint64) string {
	if total == 0 {
		return "нет данных"
	}
	return fmt.Sprintf("%.1f%%", float64(known)*100/float64(total))
}
