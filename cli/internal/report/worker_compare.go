package report

import (
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func workerDiagnosticNumber(v *float64) string {
	if v == nil {
		return "нет данных"
	}
	return fmt.Sprintf("%.2f", *v)
}
func workerCatalogCount(v *int) string {
	if v == nil {
		return "нет данных"
	}
	return fmt.Sprint(*v)
}
func workerRegistryComparisonJSON(v []analyze.WorkerRegistryComparison) (template.JS, error) {
	data, err := json.Marshal(v)
	return template.JS(data), err // encoding/json escapes names and declared URI strings.
}
