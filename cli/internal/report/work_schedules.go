package report

import (
	"encoding/json"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"html/template"
)

func workSchedulesJSON(s analyze.Summary) (template.JS, error) {
	payload, err := json.Marshal(analyze.BuildWorkSchedules(s))
	return template.JS(payload), err
}
