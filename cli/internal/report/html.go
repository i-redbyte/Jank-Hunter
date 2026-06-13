package report

import (
	"fmt"
	"html/template"
	"os"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func WriteInspect(path string, summary analyze.Summary) error {
	return execute(path, inspectTemplate, map[string]any{
		"GeneratedAt": time.Now().Format(time.RFC3339),
		"Summary":     summary,
	})
}

func WriteCompare(path string, comparison analyze.Comparison) error {
	return execute(path, compareTemplate, map[string]any{
		"GeneratedAt": time.Now().Format(time.RFC3339),
		"Comparison":  comparison,
	})
}

func execute(path, source string, data any) error {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"pctWidth": func(value float64) string {
			if value < 0 {
				value = 0
			}
			if value > 100 {
				value = 100
			}
			return fmt.Sprintf("width:%.2f%%", value)
		},
		"msWidth": func(value uint64) string {
			width := float64(value) * 100 / 2000
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return fmt.Sprintf("width:%.2f%%", width)
		},
		"severityClass": func(value string) string {
			switch value {
			case "high":
				return "sev-high"
			case "medium":
				return "sev-medium"
			default:
				return "sev-ok"
			}
		},
	}).Parse(source)
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return tmpl.Execute(file, data)
}
