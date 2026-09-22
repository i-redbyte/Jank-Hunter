package analyze

import "fmt"

func enrichAgentSummary(summary *AgentSummary) {
	summary.PlatformLimits = agentPlatformLimits()
	summary.ConnectionGuide = buildAgentConnectionGuide(*summary)
	if summary.Capabilities.Missing != 0 && len(summary.Capabilities.RequestedList) > 0 {
		summary.CapabilityGaps = capabilityNames(summary.Capabilities.Missing)
	}
}

func agentPlatformLimits() []string {
	return []string{
		"ART TI работает только в debuggable-сборках и на API 28+; release APK без android:debuggable не выполняет attach.",
		"Набор JVMTI capabilities зависит от версии ART на устройстве; отсутствующие возможности не компенсируются desktop-JVMTI.",
		"Снимок стека фиксирует один момент времени и не доказывает причину задержки.",
		"Native-библиотека агента поставляется для arm64-v8a и x86_64; другие ABI работают без JVM TI evidence.",
	}
}

func buildAgentConnectionGuide(summary AgentSummary) []string {
	if summary.EventCount == 0 {
		return []string{
			"В журнале нет событий ART TI agent: проверьте artTi.mode (для BALANCED/FULL по умолчанию включается LIGHT, если mode не задан).",
			"Убедитесь, что установлена debuggable-сборка, API ≥ 28, ABI arm64-v8a или x86_64, и процесс — основной (если не включена отдельная политика).",
			"Смотрите Logcat с тегом JankHunter и раздел Agent в inspect: api_unsupported, app_not_debuggable, library_or_abi_missing, attach_failed.",
		}
	}
	if !summary.Available {
		return []string{
			fmt.Sprintf("Агент записал %d событий, но не перешёл в рабочее состояние (причина: %s).", summary.EventCount, summary.Reason),
			"Проверьте capability matrix: запрошенные возможности могут быть недоступны на этом устройстве (degraded mode).",
		}
	}
	if summary.Capabilities.Missing != 0 {
		return []string{
			"Часть запрошенных JVMTI capabilities недоступна на устройстве; collectors без capability не работают.",
			"Сравните Requested и Active в отчёте и повторите сценарий на другом API/образе эмулятора при необходимости.",
		}
	}
	return nil
}
