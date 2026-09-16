package analyze

import (
	"encoding/hex"
	"fmt"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// AcquisitionEvidence describes replication units, not a probability of IID
// sampling. Shared run/process identities connect observations: rotation,
// multiple application processes and SDK restarts cannot create independent trials.
type AcquisitionEvidence struct {
	IndependentGroups int
	IdentityComplete  bool
	// Distinct identities below count segments with both identities verified.
	DistinctRunIDs           int
	DistinctProcessInstances int
	UnknownIdentitySegments  int
}

func AcquisitionEvidenceFor(summary Summary) AcquisitionEvidence {
	if summary.Acquisition != nil {
		return *summary.Acquisition
	}
	return buildAcquisitionEvidence(summary.CollectionSegments)
}

type acquisitionGroups struct {
	evidence        AcquisitionEvidence
	runs, processes map[jhlog.ID128]int
	parents, sizes  []int
	components      int
}

func newAcquisitionGroups(capacity int) *acquisitionGroups {
	return &acquisitionGroups{evidence: AcquisitionEvidence{IdentityComplete: capacity > 0}, runs: make(map[jhlog.ID128]int, capacity), processes: make(map[jhlog.ID128]int, capacity), parents: make([]int, 0, capacity), sizes: make([]int, 0, capacity)}
}

func (g *acquisitionGroups) find(node int) int {
	for g.parents[node] != node {
		g.parents[node] = g.parents[g.parents[node]]
		node = g.parents[node]
	}
	return node
}

func (g *acquisitionGroups) add(run, process jhlog.ID128, valid bool) {
	if !valid || run.IsZero() || process.IsZero() {
		g.evidence.UnknownIdentitySegments++
		g.evidence.IdentityComplete = false
		return
	}
	node, exists := g.runs[run]
	if !exists {
		node = len(g.parents)
		g.runs[run] = node
		g.parents = append(g.parents, node)
		g.sizes = append(g.sizes, 1)
		g.components++
	}
	if previous, exists := g.processes[process]; exists {
		left, right := g.find(node), g.find(previous)
		if left != right {
			if g.sizes[left] < g.sizes[right] {
				left, right = right, left
			}
			g.parents[right] = left
			g.sizes[left] += g.sizes[right]
			g.components--
		}
	} else {
		g.processes[process] = node
	}
}

func (g *acquisitionGroups) result() AcquisitionEvidence {
	evidence := g.evidence
	evidence.DistinctRunIDs = len(g.runs)
	evidence.DistinctProcessInstances = len(g.processes)
	// Missing identities may connect otherwise separate components. Their count
	// cannot be presented as confirmed independent replication, even as a lower bound.
	if evidence.IdentityComplete {
		evidence.IndependentGroups = g.components
	}
	return evidence
}

func buildAcquisitionEvidence(segments []CollectionSegment) AcquisitionEvidence {
	groups := newAcquisitionGroups(len(segments))
	for _, segment := range segments {
		run, runOK := acquisitionID(segment.RunID)
		process, processOK := acquisitionID(segment.ProcessInstanceID)
		groups.add(run, process, runOK && processOK)
	}
	return groups.result()
}

func acquisitionID(value string) (jhlog.ID128, bool) {
	var id jhlog.ID128
	if len(value) != 32 {
		return id, false
	}
	_, err := hex.Decode(id[:], []byte(value))
	return id, err == nil && !id.IsZero()
}

func (c *collector) prepareAcquisitionGroups(inputs []SessionInput) {
	c.acquisition = newAcquisitionGroups(len(inputs))
	for _, input := range inputs {
		c.acquisition.add(input.Header.RunID, input.Header.ProcessInstanceID, true)
	}
}

type environmentObservation struct {
	group int
	kind  uint8
	value string
}

func (c *collector) addEnvironmentObservation(kind uint8, value string, counts map[string]uint64) {
	if c.environmentSeen == nil {
		c.environmentSeen = make(map[environmentObservation]struct{})
	}
	key := environmentObservation{group: c.currentAcquisition, kind: kind, value: value}
	if _, exists := c.environmentSeen[key]; exists {
		return
	}
	c.environmentSeen[key] = struct{}{}
	counts[value]++
}

// Session records repeat at rotation boundaries. They are metadata, not extra
// observations for the replication heuristic. EventCount remains the wire count.
func comparisonObservationCount(summary Summary) int {
	return max(0, summary.EventCount-summary.CollectorSessions)
}

func acquisitionGateDetail(label string, summary Summary) string {
	evidence := AcquisitionEvidenceFor(summary)
	groups := "unknown"
	if evidence.IdentityComplete {
		groups = fmt.Sprint(evidence.IndependentGroups)
	}
	return fmt.Sprintf("%s independent acquisition groups=%s, non-session events=%d", label, groups, comparisonObservationCount(summary))
}
