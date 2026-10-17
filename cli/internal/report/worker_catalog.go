package report

import (
	"encoding/json"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"html/template"
)

func workerCatalogJSON(a *analyze.WorkerCatalogAnalysis) (template.JS, error) {
	data, err := json.Marshal(a)
	return template.JS(data), err // json.Marshal escapes HTML delimiters in class names.
}
