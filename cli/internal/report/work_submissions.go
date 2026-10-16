package report

import (
	"encoding/json"
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"html/template"
)

func workSubmissionsJSON(a *analyze.WorkSubmissionAnalysis) (template.JS, error) {
	data, err := json.Marshal(a)
	return template.JS(data), err
}
func workSubmissionSummary(a *analyze.WorkSubmissionAnalysis) string {
	if a == nil {
		return "неизвестно: нет наблюдений вызова"
	}
	return fmt.Sprintf("Захвачено запросов в вызовах: %d; unique: %d; периодических: %d; пропущено запросов адаптером: %d; отброшено наблюдений CLI: %d", a.CapturedCalls, a.UniqueCalls, a.PeriodicCalls, a.OmittedRequests, a.DroppedCalls)
}
