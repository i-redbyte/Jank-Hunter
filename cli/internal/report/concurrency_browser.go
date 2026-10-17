package report

import (
	"encoding/json"
	"html/template"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func concurrencyEntitiesJSON(a *analyze.CausalAnalysis) (template.JS, error) {
	if a == nil || len(a.AllEntities) == 0 {
		return template.JS("[]"), nil
	}
	data, err := json.Marshal(a.AllEntities)
	if err != nil {
		return "", err
	}
	return template.JS(data), nil // json.Marshal escapes HTML delimiters inside source names.
}
