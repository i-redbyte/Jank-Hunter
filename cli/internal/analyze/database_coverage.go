package analyze

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func buildDatabaseCoverage(
	summary Summary,
	diagnostics *InstrumentationDiagnostics,
) DatabaseCoverage {
	coverage := DatabaseCoverage{
		CollectorSessions:      summary.CollectorSessions,
		RuntimeEnabledSessions: summary.DatabaseCoverage.RuntimeEnabledSessions,
	}
	if coverage.RuntimeEnabledSessions > 0 {
		// The streaming collector records the exact count. Direct Summary callers and test fixtures
		// fall through to the bounded Any/All derivation below.
	} else if summary.CollectorFlagsAll&uint64(jhlog.CollectorDatabase) != 0 {
		coverage.RuntimeEnabledSessions = summary.CollectorSessions
	} else if summary.CollectorFlagsAny&uint64(jhlog.CollectorDatabase) != 0 {
		// CollectorFlagsAny/All deliberately keep only bounded set-level state. For a partial set the
		// exact enabled count is unavailable, but at least one enabled session is proven.
		coverage.RuntimeEnabledSessions = 1
	}
	coverage.DiagnosticsAvailable = diagnostics != nil && diagnostics.Available
	if coverage.DiagnosticsAvailable {
		for _, hook := range diagnostics.Hooks {
			if strings.HasPrefix(hook.Intent, "database.") {
				coverage.InstrumentedHooks = saturatingUint64Sum(coverage.InstrumentedHooks, hook.Count)
			}
		}
		for _, decision := range diagnostics.Decisions {
			if decision.Family != "database" && decision.Module != "database" {
				continue
			}
			switch decision.Kind {
			case "disabled":
				coverage.DisabledCandidates = saturatingUint64Sum(coverage.DisabledCandidates, decision.Count)
			case "unsupported":
				coverage.UnsupportedCandidates = saturatingUint64Sum(coverage.UnsupportedCandidates, decision.Count)
			}
		}
	}
	if analysis := summary.DatabaseAnalysis; analysis != nil {
		coverage.ObservedCalls = analysis.Overall.Calls
		coverage.KnownSQLCalls = analysis.KnownSQLCalls
		coverage.DroppedStatementEvents = analysis.DroppedStatementEvents
		coverage.DroppedContextEvents = analysis.DroppedContextEvents
		coverage.DroppedCorrelationEvents = saturatingUint64Sum(
			analysis.DroppedDBIntervals,
			analysis.DroppedTransactionIntervals,
		)
		coverage.DroppedScenarioEvents = analysis.Scenarios.DroppedEvents
		coverage.DroppedScenarioCandidates = analysis.Scenarios.DroppedCandidates
		if transactions := analysis.Transactions; transactions != nil {
			coverage.ObservedTransactionEvents = transactions.Events
			coverage.CompletedTransactions = transactions.Completed
			coverage.IncompleteTransactions = transactions.Incomplete
			coverage.DroppedTransactionDetails = saturatingUint64Sum(
				transactions.DroppedActiveStarts,
				transactions.DroppedTransactionDetails,
			)
		}
	}

	switch {
	case coverage.CollectorSessions == 0:
		coverage.Status = "unavailable"
		coverage.StatusLabel = "нет данных о начале сессии"
		coverage.Explanation = "Журнал не содержит события начала сессии, поэтому нельзя доказать, что сбор данных БД был включён."
		coverage.Action = "Соберите новый .jhlog текущей версией Jank Hunter и проверьте, что начало сессии записано."
	case coverage.RuntimeEnabledSessions == 0:
		coverage.Status = "disabled"
		coverage.StatusLabel = "сборщик выключен"
		coverage.Explanation = "Во всех переданных сессиях сбор данных БД выключен; отсутствие SQL ожидаемо."
		coverage.Action = "Включите instrument.databaseTracing.set(true), пересоберите приложение и повторите целевой сценарий."
	case coverage.RuntimeEnabledSessions < coverage.CollectorSessions:
		coverage.Status = "partial"
		coverage.StatusLabel = "включён не во всех сессиях"
		coverage.Explanation = fmt.Sprintf(
			"Сбор данных БД подтверждён только для части набора (%d из %d сессий); абсолютные значения смешивают разные режимы сбора.",
			coverage.RuntimeEnabledSessions,
			coverage.CollectorSessions,
		)
		coverage.Action = "Сравнивайте только сессии с одинаковым databaseTracing и повторите сценарий на однородном наборе."
	case coverage.DiagnosticsAvailable && coverage.InstrumentedHooks == 0:
		coverage.Status = "no_hooks"
		coverage.StatusLabel = "перехватчики БД не найдены"
		coverage.Explanation = "Сборщик включён, но диагностика преобразования байткода не содержит ни одного перехватчика БД."
		coverage.Action = "Проверьте includePackages/excludePackages, поддерживаемые сигнатуры и флаг databaseTracing в том же варианте сборки."
	case coverage.ObservedCalls == 0 && coverage.ObservedTransactionEvents == 0:
		coverage.Status = "no_observations"
		coverage.StatusLabel = "SQL не наблюдался"
		if coverage.DiagnosticsAvailable {
			coverage.Explanation = "Сборщик включён и перехватчики в байткоде найдены, но SQL-вызовы не наблюдались."
			coverage.Action = "Повторите целевой сценарий с обращением к базе; если событий всё ещё нет, проверьте фактический DB API по diagnostics."
		} else {
			coverage.Explanation = "Сбор данных БД включён, но SQL-вызовы не наблюдались; без диагностики преобразования байткода нельзя отличить неисполненный сценарий от отсутствующих перехватчиков."
			coverage.Action = "Подключите instrumentation-diagnostics.jsonl и повторите сценарий с обращением к базе."
		}
	case coverage.DroppedStatementEvents > 0 || coverage.DroppedContextEvents > 0 ||
		coverage.DroppedCorrelationEvents > 0 || coverage.DroppedTransactionDetails > 0 ||
		coverage.DroppedScenarioEvents > 0 || coverage.DroppedScenarioCandidates > 0 ||
		coverage.IncompleteTransactions > 0 || coverage.KnownSQLCalls < coverage.ObservedCalls ||
		coverage.UnsupportedCandidates > 0:
		coverage.Status = "degraded"
		coverage.StatusLabel = "данные собраны с ограничениями"
		coverage.Explanation = fmt.Sprintf(
			"Наблюдалось %d SQL-вызовов, но часть SQL-шаблонов, мест вызова или связей с транзакциями не записана. Поэтому отдельные карточки могут занижать частоту и длительность проблемы.",
			coverage.ObservedCalls,
		)
		coverage.Action = "Повторите только проблемный сценарий. Если SQL-шаблон или место вызова снова не записаны, проверьте охват пакета приложения и поддержку используемого API базы данных."
	default:
		coverage.Status = "observed"
		coverage.StatusLabel = "данные собраны"
		coverage.Explanation = fmt.Sprintf(
			"Сбор данных БД активен: наблюдалось %d SQL-вызовов и %d событий жизненного цикла транзакций; SQL распознан для каждого вызова.",
			coverage.ObservedCalls,
			coverage.ObservedTransactionEvents,
		)
		coverage.Action = "Используйте карточки SQL-проблем и сравнение одинакового сценария для проверки оптимизаций."
	}
	return coverage
}
