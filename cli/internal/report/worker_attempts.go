package report

import (
	"encoding/json"
	"html/template"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func workerAttemptsJSON(a *analyze.WorkerAnalysis) (template.JS, error) {
	data, err := json.Marshal(struct {
		Attempts                                                         []analyze.WorkerAttempt
		DroppedAttempts, DroppedLifecycleEvents, DroppedIdentityBindings uint64
	}{a.Attempts, a.DroppedAttempts, a.DroppedLifecycleEvents, a.DroppedIdentityBindings})
	return template.JS(data), err // encoding/json escapes user-controlled class and context text.
}
