package report

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type collectorCapability struct {
	Label       string
	Status      string
	StatusLabel string
	Description string
	Observation string
}

type collectorCapabilityDefinition struct {
	flag        jhlog.CollectorFlag
	label       string
	description string
	observation func(analyze.Summary) string
}

var collectorCapabilityDefinitions = [...]collectorCapabilityDefinition{
	{
		flag:        jhlog.CollectorFPS,
		label:       "Частота кадров",
		description: "Резервный источник кадров и подтормаживаний UI.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"В журнале суммарно %s UI.",
				russianCount(summary.UIFrames, "кадр", "кадра", "кадров"),
			)
		},
	},
	{
		flag:        jhlog.CollectorJankStats,
		label:       "JankStats",
		description: "Основной Android-источник длительности кадров и UI jank.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"В журнале суммарно %s UI; источники кадров могут быть смешаны.",
				russianCount(summary.UIFrames, "кадр", "кадра", "кадров"),
			)
		},
	},
	{
		flag:  jhlog.CollectorProcessExit,
		label: "История завершений процессов",
		description: "Читает ApplicationExitInfo и помогает находить ANR, аварии и завершения " +
			"из-за памяти.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"Записано %s. Ноль нормален, если Android не вернул завершений прошлых процессов.",
				russianCount(len(summary.ProcessExits), "завершение", "завершения", "завершений"),
			)
		},
	},
	{
		flag:  jhlog.CollectorIOTracing,
		label: "Типизированные I/O операции",
		description: "Принимает операции, переданные приложением через recordIO/traceIO; " +
			"автоматически все обращения к диску и БД не перехватывает.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"Записано %s.",
				russianCount(len(summary.IOOperations), "операция", "операции", "операций"),
			)
		},
	},
	{
		flag:        jhlog.CollectorSystemSampler,
		label:       "Системный контекст",
		description: "Собирает память, CPU, сеть, накопитель, батарею и нагрев устройства.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"Записано %s контекста и %s памяти.",
				russianCount(summary.ContextCount, "снимок", "снимка", "снимков"),
				russianCount(summary.MemoryCount, "снимок", "снимка", "снимков"),
			)
		},
	},
	{
		flag:  jhlog.CollectorMainThreadStalls,
		label: "Зависания главного потока",
		description: "Фиксирует длительные паузы главного потока для поиска ANR и тяжёлой работы " +
			"в UI.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"Записано %s. Ноль означает, что порог зависания не был превышен.",
				russianCount(summary.StallCount, "зависание", "зависания", "зависаний"),
			)
		},
	},
	{
		flag:  jhlog.CollectorRetainedObjects,
		label: "Удерживаемые объекты",
		description: "Наблюдает объекты после завершения жизненного цикла и формирует признаки " +
			"возможных утечек памяти.",
		observation: func(summary analyze.Summary) string {
			return fmt.Sprintf(
				"Записано %s.",
				russianCount(summary.Retained, "удержание", "удержания", "удержаний"),
			)
		},
	},
	{
		flag:        jhlog.CollectorCompose,
		label:       "Jetpack Compose",
		description: "Измеряет фактические выполнения функций с @Composable и явно отмеченные фазы отрисовки.",
		observation: func(summary analyze.Summary) string {
			return semanticCollectorObservation(summary, "jankhunter.semantic.v1.compose.")
		},
	},
	{
		flag:  jhlog.CollectorRoom,
		label: "Room и база данных",
		description: "Измеряет границы сгенерированных Room DAO; точную асинхронную операцию можно " +
			"дополнить traceIO.",
		observation: func(summary analyze.Summary) string {
			return semanticCollectorObservation(summary, "jankhunter.semantic.v1.room.")
		},
	},
	{
		flag:  jhlog.CollectorWorker,
		label: "Фоновые Worker-задачи",
		description: "Измеряет синхронный Worker автоматически и полную CoroutineWorker-работу через " +
			"traceSuspendingWorker.",
		observation: func(summary analyze.Summary) string {
			return semanticCollectorObservation(summary, "jankhunter.semantic.v1.worker.")
		},
	},
}

func semanticCollectorObservation(summary analyze.Summary, prefix string) string {
	var calls uint64
	var boundaries int
	for _, call := range summary.RuntimeCalls {
		if strings.HasPrefix(call.Caller, prefix) {
			calls += call.Count
			boundaries++
		}
	}
	return fmt.Sprintf(
		"Записано %s в %s.",
		russianCount(calls, "выполнение", "выполнения", "выполнений"),
		russianCount(boundaries, "границе", "границах", "границах"),
	)
}

func collectorCapabilities(summary analyze.Summary) []collectorCapability {
	if summary.CollectorSessions == 0 {
		return nil
	}
	result := make([]collectorCapability, 0, len(collectorCapabilityDefinitions))
	for _, definition := range collectorCapabilityDefinitions {
		status, statusLabel := collectorCapabilityStatus(summary, definition.flag)
		observation := definition.observation(summary)
		if status == "disabled" {
			observation = "В этом прогоне данные этого типа не собирались."
		}
		result = append(result, collectorCapability{
			Label:       definition.label,
			Status:      status,
			StatusLabel: statusLabel,
			Description: definition.description,
			Observation: observation,
		})
	}
	return result
}

func collectorCapabilityStatus(summary analyze.Summary, flag jhlog.CollectorFlag) (string, string) {
	mask := uint64(flag)
	switch {
	case summary.CollectorFlagsAll&mask != 0:
		return "enabled", "включён"
	case summary.CollectorFlagsAny&mask != 0:
		return "partial", "включён не во всех сессиях"
	default:
		return "disabled", "выключен"
	}
}
