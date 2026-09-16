package main

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func TestNetworkLoopContextQualityHasDedicatedExportRow(t *testing.T) {
	for _, status := range []string{"unknown", "ambiguous"} {
		model := &mathanalysis.MathReport{NetworkLoops: []mathanalysis.NetworkLoopFinding{{Route: "GET /loop", RouteAttributionStatus: "observed", OwnerAttributionStatus: status}}}
		rows := mathFindingRows(model)
		if len(rows) != 1 || rows[0].Section != "Качество данных" || !strings.Contains(rows[0].Detail, "Контекст сетевых циклов") {
			t.Errorf("%s: missing dedicated export diagnostic: %+v", status, rows)
		}
	}
}
