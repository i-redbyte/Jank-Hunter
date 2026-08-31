package mathanalysis

import "github.com/i-redbyte/jank-hunter/cli/internal/datavalue"

func analysisOwnerLabel(owner string) string {
	return datavalue.HumanUnknown(owner, "не записано")
}

func analysisOwnerIsKnown(owner string) bool {
	return !datavalue.IsUnknown(owner)
}

func missingOwnerAction() string {
	return "Проверьте инструментирование сетевого клиента и включение пакета с кодом, который запускает запрос."
}
