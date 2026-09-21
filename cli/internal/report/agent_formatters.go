package report

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func agentPresetLabel(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "OFF":
		return "выключен"
	case "LIGHT":
		return "базовый сбор"
	case "CAUSAL":
		return "поиск причин"
	case "DEEP":
		return "углублённый сбор"
	case "CUSTOM":
		return "пользовательский набор"
	default:
		return "неизвестный режим"
	}
}

func agentReasonLabel(value string) string {
	switch strings.TrimSpace(value) {
	case "attached":
		return "агент подключён"
	case "capability_degraded":
		return "часть возможностей недоступна"
	case "attach_failed":
		return "не удалось подключить агент"
	case "api_unsupported":
		return "версия Android не поддерживается"
	case "app_not_debuggable":
		return "приложение не разрешает отладочное подключение"
	case "stopped":
		return "агент остановлен"
	case "":
		return "причина не указана"
	default:
		return "служебная причина: " + value
	}
}

func agentEvidenceLevelLabel(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DIRECT":
		return "прямое измерение"
	case "STRONG_ASSOCIATION":
		return "сильная временная связь"
	case "TEMPORAL_CORRELATION":
		return "временное совпадение"
	default:
		return "предварительное наблюдение"
	}
}

func agentConfidenceLabel(value string) string {
	return confidenceLabel(strings.ToLower(strings.TrimSpace(value)))
}

func agentBooleanLabel(value bool) string {
	if value {
		return "да"
	}
	return "нет"
}

func agentGCConclusion(summary analyze.AgentIntervalSummary) string {
	switch {
	case summary.Count == 0:
		return "Агент не зафиксировал пауз сборки мусора."
	case summary.MaxMS >= 16.7:
		return fmt.Sprintf("Самая длинная пауза %.3f мс могла сорвать кадр на экране 60 Гц. Проверяйте её только вместе с совпавшим проблемным окном.", summary.MaxMS)
	case summary.MaxMS >= 8.3:
		return fmt.Sprintf("Самая длинная пауза %.3f мс заметна для экранов с высокой частотой обновления, но сама по себе не объясняет задержку.", summary.MaxMS)
	default:
		return fmt.Sprintf("Максимальная пауза %.3f мс мала и без временного совпадения с задержкой не является практической проблемой.", summary.MaxMS)
	}
}

func agentContentionConclusion(summary analyze.AgentIntervalSummary) string {
	switch {
	case summary.Count == 0:
		return "Длительное ожидание блокировок не зафиксировано."
	case summary.MaxMS >= 100:
		return fmt.Sprintf("Ожидание %.3f мс достаточно велико, чтобы быть заметной причиной задержки. Найдите совпавший поток и метод ниже.", summary.MaxMS)
	case summary.MaxMS >= 16.7:
		return fmt.Sprintf("Ожидание %.3f мс могло сорвать кадр, если происходило на главном потоке.", summary.MaxMS)
	default:
		return fmt.Sprintf("Максимальное ожидание %.3f мс невелико; приоритет появляется только при повторении в проблемном сценарии.", summary.MaxMS)
	}
}
