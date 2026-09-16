package mathanalysis

import (
	"math/bits"
	"sort"
	"strings"
)

func (c *networkLoopCollector) findings() []NetworkLoopFinding {
	workspace := c.account.scratch("network candidate workspace")
	defer workspace.close()
	if !workspace.reserveItems(len(c.signals), 256) {
		return nil
	}
	out := make([]NetworkLoopFinding, 0, len(c.signals))
	var points []float64
	keys := make([]string, 0, len(c.signals))
	for key := range c.signals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	countStart, countEnd := 0, c.bucketSize
	if c.timeline != nil {
		countStart, countEnd = longestHTTPCountRun(c.timeline)
	}
	for _, key := range keys {
		signal := c.signals[key]
		// Every burst needs a positive bucket. Reject impossible candidates before allocating a
		// full timeline; all eligible candidates share one scratch array, never retained by output.
		if signal.sparse.positiveBuckets < minNetworkLoopBursts || c.bucketSize < minNetworkLoopPoints {
			continue
		}
		usesHTTPCounts := c.timeline != nil && !strings.HasPrefix(key, "metric:")
		if usesHTTPCounts && countEnd-countStart < minNetworkLoopPoints {
			continue
		}
		// Budget the candidate's scans and sorting as well as its later FFT. Otherwise
		// thousands of rejected candidates could still scan and sort the full timeline.
		work := uint64(c.bucketSize) * (8*uint64(bits.Len(uint(c.bucketSize))) + 8)
		if !c.budget.chargeSpectralWork(work) {
			return nil
		}
		if points == nil {
			// Scratch points, burst indexes, sorted/deviation/positive vectors and spectral input.
			if !workspace.reserveItems(c.bucketSize, 80) {
				return nil
			}
			points = make([]float64, c.bucketSize)
		} else {
			clear(points)
		}
		signal.sparse.writeDense(points)
		candidate := *signal
		candidate.points = points
		if usesHTTPCounts {
			lo, hi := countStart, countEnd
			candidate.points = points[lo:hi]
			candidate.bucketOffset = lo
		}
		outputBytes := uint64(2048) + 16*uint64(len(signal.name)) + 32*uint64(signal.maxTokenBytes)
		if !c.results.reserve(outputBytes) {
			return nil
		}
		if finding, ok := analyzeNetworkLoopSignalWithBudget(&candidate, c.bucketMS, c.budget); ok {
			out = append(out, finding)
		} else {
			c.results.release(outputBytes)
		}
		if c.budget != nil && c.budget.err() != nil {
			return nil
		}
	}
	return out
}
