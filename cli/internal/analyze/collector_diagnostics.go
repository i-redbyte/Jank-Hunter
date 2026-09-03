package analyze

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

func (c *collector) instrumentationQualityWarnings() []string {
	var warnings []string
	diagnostics := c.diagnostics
	if diagnostics == nil || !diagnostics.Available {
		// Build-time diagnostics are optional developer evidence. End users are expected to have
		// only a self-contained .jhlog, so their absence is not a collection-quality defect.
		return warnings
	} else {
		if diagnostics.ClassCount == 0 {
			warnings = append(warnings, "Качество сбора: ASM-диагностика пустая, значит instrument matcher не увидел классы или артефакт не был собран.")
		}
		if diagnostics.ClassCount > 0 && diagnostics.HookCount == 0 && diagnostics.AnnotatedMethodCount == 0 {
			warnings = append(warnings, "Качество сбора: ASM прошел по классам, но не нашел hooks или аннотации; проверьте include/exclude, версии библиотек и включенные bridge-флаги.")
		}
		if unsupported := unsupportedDecisionCount(diagnostics); unsupported > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: ASM встретил неподдержанные сигнатуры hooks: %d; часть телеметрии могла не попасть в лог.", unsupported))
		}
	}
	return warnings
}

func (c *collector) attributionQualityWarnings(summary Summary) []string {
	var warnings []string
	if totalProblemWindows(summary) > 0 && unknownProblemOwnerRate(summary.ProblemWindows) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство проблемных окон не имеют понятного owner; добавьте ownerHint/withOwner или проверьте охват ASM-инструментации.")
	}
	if len(summary.SignalContexts) > 0 && unknownSignalContextRate(summary.SignalContexts) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство сигналов не имеют экрана, операции или источника; проверьте автоматическое отслеживание экранов, @JankHunterOperation/traceOperation/withOwner и охват инструментирования.")
	}
	if summary.EventCount > 0 && datavalue.IsUnknown(c.currentDevice) {
		warnings = append(warnings, "Качество сбора: модель устройства не записана в session-событие; проверьте JankHunter init и device snapshot при старте runtime.")
	}
	if summary.EventCount > 0 && datavalue.IsUnknown(c.currentAppVersion) && datavalue.IsUnknown(c.currentBuild) {
		warnings = append(warnings, "Качество сбора: версия приложения не записана в session-событие; проверьте PackageInfo/versionName/versionCode на старте runtime.")
	}
	if summary.EventCount > 0 && len(summary.Processes) == 1 && datavalue.IsUnknown(summary.Processes[0].Name) {
		warnings = append(warnings, "Качество сбора: процесс неизвестен; проверьте session-события и mainProcessOnly/allowedProcesses.")
	}
	return warnings
}

func unsupportedDecisionCount(diagnostics *InstrumentationDiagnostics) uint64 {
	var total uint64
	if diagnostics == nil {
		return 0
	}
	for _, decision := range diagnostics.Decisions {
		if decision.Kind == "unsupported" || decision.Reason == "unsupported_signature" {
			total += decision.Count
		}
	}
	return total
}

func unknownProblemOwnerRate(problems []ProblemWindowStats) float64 {
	var total uint64
	var unknown uint64
	for _, problem := range problems {
		if problem.Count == 0 {
			continue
		}
		total += problem.Count
		if datavalue.IsUnknown(problem.Owner) {
			unknown += problem.Count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(unknown) / float64(total)
}

func unknownSignalContextRate(contexts []SignalContextStats) float64 {
	var total uint64
	var unknown uint64
	for _, context := range contexts {
		count := uint64(context.HTTPCount) + uint64(context.StallCount) + context.LogSpam + context.ProblemCount + uint64(context.UIWindows)
		if count == 0 {
			count = 1
		}
		total += count
		if datavalue.IsUnknown(context.Screen) &&
			datavalue.IsUnknown(context.Operation) &&
			datavalue.IsUnknown(context.Owner) {
			unknown += count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(unknown) / float64(total)
}

func (c *collector) filterWarnings(summary Summary) []string {
	if !filterActive(c.filter) {
		return nil
	}
	var globalSignals []string
	if summary.ContextCount > 0 {
		globalSignals = append(globalSignals, "контекст устройства")
	}
	if len(summary.Counters) > 0 || len(summary.Gauges) > 0 {
		globalSignals = append(globalSignals, "custom metrics")
	}
	if len(globalSignals) == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf(
			"Фильтр применен к событиям с маршрутом, экраном, источником или классом; %s не несут полного контекста выполнения и показаны глобально.",
			strings.Join(globalSignals, " и "),
		),
	}
}

func (c *collector) runEnvironment(summary Summary) RunEnvironment {
	device := unknownIfEmpty(c.currentDevice)
	manufacturer := unknownIfEmpty(c.currentMaker)
	brand := unknownIfEmpty(c.currentBrand)
	hardware := unknownIfEmpty(c.currentHardware)
	board := unknownIfEmpty(c.currentBoard)
	product := unknownIfEmpty(c.currentProduct)
	abi := unknownIfEmpty(c.currentPrimaryABI)
	abis := unknownIfEmpty(c.currentABIs)
	network := unknownIfEmpty(c.currentNetwork)
	app := unknownIfEmpty(c.currentAppVersion)
	build := unknownIfEmpty(c.currentBuild)
	process := unknownIfEmpty(c.currentProcess)

	return RunEnvironment{
		Title:    datavalue.HumanUnknown(device, "неизвестное устройство"),
		Subtitle: fmt.Sprintf("%s · %s · процесс %s", osValue(c.currentAndroid, c.currentSDK), appBuildValue(app, build), datavalue.HumanUnknown(process, "неизвестен")),
		Items: []InfoItem{
			{Label: "Батарея", Value: batteryValue(summary.BatteryLastPct), Detail: batteryDetail(summary)},
			{Label: "Сеть", Value: datavalue.HumanUnknown(network, "неизвестно"), Detail: networkDetail(summary)},
			{Label: "Свободная RAM", Value: formatDataSize(summary.AvailMemoryLastKB), Detail: memoryDetail(summary)},
			{Label: "Свободное хранилище", Value: formatDataSize(summary.FreeStorageKB), Detail: storageDetail(summary)},
			{Label: "Android", Value: osValue(c.currentAndroid, c.currentSDK), Detail: androidDetail(c.currentSDK, c.currentPatch)},
			{Label: "Рут-доступ", Value: rootValue(summary.DeviceRootKnown, summary.DeviceRooted), Detail: rootDetail(summary.DeviceRootKnown, summary.DeviceRooted)},
			{Label: "CPU ABI", Value: datavalue.HumanUnknown(abi, "неизвестно"), Detail: fmt.Sprintf("поддерживаются %s", datavalue.HumanUnknown(abis, "неизвестно"))},
			{Label: "Железо", Value: datavalue.HumanUnknown(hardware, "неизвестно"), Detail: fmt.Sprintf("плата %s · продукт %s", datavalue.HumanUnknown(board, "неизвестна"), datavalue.HumanUnknown(product, "неизвестен"))},
			{Label: "Бренд", Value: datavalue.HumanUnknown(manufacturer, "неизвестно"), Detail: fmt.Sprintf("бренд %s", datavalue.HumanUnknown(brand, "неизвестен"))},
		},
	}
}
