package analyze

import "sort"

func flowScenarioFromSignalContext(context SignalContextStats) FlowScenarioStats {
	return FlowScenarioStats{
		StallStates:  context.StallStates,
		Screen:       context.Screen,
		Flow:         firstNonEmpty(context.Flow, context.Operation),
		Step:         context.Step,
		Owner:        context.Owner,
		RouteSample:  context.RouteSample,
		HTTPCount:    context.HTTPCount,
		HTTPFailed:   context.HTTPFailed,
		HTTPP95MS:          context.HTTPP95MS,
		HTTPP95Approximate: context.HTTPP95Approximate,
		StallCount:   context.StallCount,
		StallMaxMS:   context.StallMaxMS,
		UIWindows:    context.UIWindows,
		UIFrames:     context.UIFrames,
		UIJank:       context.UIJank,
		UIJankPct:    context.UIJankPct,
		LogSpam:      context.LogSpam,
		ProblemCount: context.ProblemCount,
		ProblemMaxMS: context.ProblemMaxMS,
		MemoryMaxKB:  context.MemoryMaxKB,
	}
}

func buildFlowScenarios(contexts []SignalContextStats) []FlowScenarioStats {
	flows := make([]FlowScenarioStats, 0, len(contexts))
	for _, context := range contexts {
		flows = append(flows, flowScenarioFromSignalContext(context))
	}
	sortFlowScenarios(flows)
	return flows
}

func sortFlowScenarios(flows []FlowScenarioStats) {
	sort.Slice(flows, func(i, j int) bool {
		left := flowScenarioSeverityScore(flows[i])
		right := flowScenarioSeverityScore(flows[j])
		if left != right {
			return left > right
		}
		if flows[i].Flow != flows[j].Flow {
			return flows[i].Flow < flows[j].Flow
		}
		if flows[i].Step != flows[j].Step {
			return flows[i].Step < flows[j].Step
		}
		if flows[i].Screen != flows[j].Screen {
			return flows[i].Screen < flows[j].Screen
		}
		if flows[i].Owner != flows[j].Owner {
			return flows[i].Owner < flows[j].Owner
		}
		return flows[i].RouteSample < flows[j].RouteSample
	})
}

func flowScenarioSeverityScore(flow FlowScenarioStats) uint64 {
	return flow.ProblemCount*10_000 +
		uint64(flow.StallCount)*5_000 +
		flow.UIJank*100 +
		flow.LogSpam*10 +
		uint64(flow.HTTPFailed)*500 +
		flow.HTTPP95MS +
		flow.ProblemMaxMS
}
