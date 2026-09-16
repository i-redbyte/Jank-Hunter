package report

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestStallPresentationMakesUnconfirmedRecoveryExplicit(t *testing.T) {
	for _, test := range []struct {
		states analyze.StallStateCounts
		want   string
	}{
		{analyze.StallStateCounts{Ongoing: 1}, "восстановление не зафиксировано"},
		{analyze.StallStateCounts{Interrupted: 1}, "наблюдение прекращено"},
		{analyze.StallStateCounts{Unknown: 1}, "статус завершения не указан"},
	} {
		context := analyze.SignalContextStats{StallCount: 1, StallMaxMS: 200, StallStates: test.states}
		text := strings.ToLower(strings.Join(operationContextSignalSummary(context), " "))
		if !strings.Contains(text, test.want) {
			t.Errorf("stall state omitted from presentation: %q, want %q", text, test.want)
		}
	}
}
