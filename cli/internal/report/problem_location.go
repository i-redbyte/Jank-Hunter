package report

import (
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func problemFindingLocationText(finding analyze.ProblemFinding) string {
	if finding.DetectorID != "stability.main_thread_stall" {
		return problemLocationText(finding.Where)
	}
	hasFrameworkObservation := false
	for _, location := range finding.Where {
		if problemFrameworkFrame(location) != "" {
			hasFrameworkObservation = true
			break
		}
	}
	if !hasFrameworkObservation {
		return problemLocationText(finding.Where)
	}
	locations := make([]string, 0, len(finding.Where))
	for _, location := range finding.Where {
		if text := problemFrameworkObservationText(location); text != "" {
			locations = append(locations, text)
			continue
		}
		locations = append(locations, problemLocationText([]analyze.ProblemLocation{location}))
	}
	locations = uniqueProblemStrings(locations)
	if len(locations) == 0 {
		return "точное место не определено"
	}
	return strings.Join(locations, "; ")
}

func problemFrameworkObservationText(location analyze.ProblemLocation) string {
	frame := problemFrameworkFrame(location)
	if frame == "" {
		return ""
	}
	parts := make([]string, 0, 5)
	for _, item := range []struct {
		label string
		value string
	}{
		{label: "экран", value: location.Screen},
		{label: "операция", value: location.Operation},
		{label: "маршрут", value: location.Route},
	} {
		if value := strings.TrimSpace(item.value); !isUnknownReportValue(value) {
			parts = append(parts, item.label+" "+value)
		}
	}
	owner := strings.TrimSpace(location.Owner)
	if owner != frame && !isUnknownReportValue(owner) && !analyze.IsFrameworkSymbol(owner) && strings.Contains(owner, ".") {
		parts = append(parts, "код приложения "+owner)
	}
	parts = append(parts, "библиотечный код "+frame)
	if process := strings.TrimSpace(location.Process); !isUnknownReportValue(process) {
		parts = append(parts, "процесс "+process)
	}
	return strings.Join(parts, " · ")
}

func problemFrameworkFrame(location analyze.ProblemLocation) string {
	frame := strings.TrimSpace(location.Method)
	if analyze.IsFrameworkSymbol(frame) {
		return frame
	}
	frame = strings.TrimSpace(location.Owner)
	if analyze.IsFrameworkSymbol(frame) {
		return frame
	}
	return ""
}
