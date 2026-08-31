package report

import "strings"

const (
	unmarkedContextLabel = "Контекст не указан"
	unmarkedContextHelp  = "Jank Hunter записал выполнение, но в момент события не было имени экрана, пользовательской операции или владельца участка кода. Это не потеря самого события: длительность и класс известны, однако связать их с конкретным пользовательским сценарием нельзя. Добавьте разметку экрана, операции или владельца и повторите сценарий."
)

// reportContextPresentation builds the shared screen/operation/owner label without temporary
// slices. Empty and synthetic unknown values are intentionally omitted from user-facing text.
func reportContextPresentation(screen, operation, owner string) (label, help string) {
	var value strings.Builder
	value.Grow(len(screen) + len(operation) + len(owner) + 32)
	appendReportContextPart(&value, "экран", screen)
	appendReportContextPart(&value, "операция", operation)
	appendReportContextPart(&value, "источник", owner)
	if value.Len() == 0 {
		return unmarkedContextLabel, unmarkedContextHelp
	}
	return value.String(), ""
}

func appendReportContextPart(target *strings.Builder, label, value string) {
	if isUnknownReportValue(value) {
		return
	}
	if target.Len() > 0 {
		target.WriteString(" · ")
	}
	target.WriteString(label)
	target.WriteByte(' ')
	target.WriteString(value)
}
