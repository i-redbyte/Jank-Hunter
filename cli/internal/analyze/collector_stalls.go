package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type stallObservation struct {
	event    jhlog.Event
	context  SignalContextStats
	owner    string
	stack    string
	cohort   string
	excluded bool
}

func (c *collector) addStallObservation(dict map[uint64]string, event jhlog.Event) {
	context := c.eventContext("", c.currentAttrOwner)
	c.agent.addStallSymptom(event, context)
	if c.nameMap != nil && !event.Stall.StackRef.IsUnknown() && event.Stall.StackRef.Origin == jhlog.SymbolOriginUnknown {
		c.summary.MappingIdentity.UnknownOriginReferences++
	}
	owner := c.currentAttrOwner
	observation := stallObservation{
		event: event, context: c.eventContext("", owner), owner: owner,
		stack: jhlog.ResolveSymbol(dict, event.Stall.StackRef), cohort: c.cohortKey(),
		excluded: c.isHeapDumpStall(event.TimeMS, owner) || isJankHunterDiagnosticOwner(owner),
	}
	id := event.Stall.IncidentID
	if id == 0 {
		c.addCompletedStall(observation)
		return
	}
	pending, err := c.stallTracker.Observe(event.Stall)
	if err != nil {
		c.stallLifecycleError = err
		return
	}
	if !pending {
		delete(c.pendingStalls, id)
		c.addCompletedStall(observation)
		return
	}
	if c.pendingStalls == nil {
		c.pendingStalls = make(map[uint64]stallObservation)
	}
	// Copy the payload: reader workspaces may be reused after the callback returns.
	value := *event.Stall
	observation.event.Stall = &value
	c.pendingStalls[id] = observation
}

func (c *collector) flushPendingStalls() {
	if len(c.pendingStalls) == 0 {
		return
	}
	ids := make([]uint64, 0, len(c.pendingStalls))
	for id := range c.pendingStalls {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		c.addCompletedStall(c.pendingStalls[id])
		delete(c.pendingStalls, id)
	}
}

func (c *collector) addCompletedStall(observation stallObservation) {
	context, owner, event := observation.context, observation.owner, observation.event
	if observation.excluded || !c.matchesFilters("", context, nil, owner) {
		return
	}
	c.operationAnalysis.recordSignal(event, event.Attribution.OperationID, owner)
	c.databaseCorrelation.addStall(databaseTimelineContext{
		screen: context.Screen, operation: context.Operation, operationID: event.Attribution.OperationID,
	}, event)
	c.cohortSamples[observation.cohort]++
	c.summary.StallCount++
	c.summary.StallStates.add(event.Stall.State)
	c.summary.StallMaxMS = maxUint64(c.summary.StallMaxMS, event.Stall.DurationMS)
	addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, observation.stack, event.Stall.StackRef.Origin)
	contextStats := c.ensureSignalContext(signalContextKeyFromStats(context))
	contextStats.StallCount++
	contextStats.StallStates.add(event.Stall.State)
	contextStats.StallMaxMS = maxUint64(contextStats.StallMaxMS, event.Stall.DurationMS)
	c.addProblemWindow(context, "main_thread_stall", event.Stall.DurationMS, 1, event.Stall.DurationMS)
}

func (counts *StallStateCounts) add(state jhlog.StallState) {
	switch state {
	case jhlog.StallStateOngoing:
		counts.Ongoing++
	case jhlog.StallStateRecovered:
		counts.Recovered++
	case jhlog.StallStateInterrupted:
		counts.Interrupted++
	default:
		counts.Unknown++
	}
}

const stallDurationMetric = "Main-thread stall max"
const incompleteStallDurationNote = "завершение зависаний не подтверждено: наблюдаемая длительность не сопоставима с полным временем"

func stallDurationUnavailable(baseline, candidate Summary) bool {
	for _, counts := range [2]StallStateCounts{baseline.StallStates, candidate.StallStates} {
		if counts.Ongoing > 0 || counts.Interrupted > 0 || counts.Unknown > 0 {
			return true
		}
	}
	return false
}

func stallDurationDelta(baseline, candidate Summary) Delta {
	before, after := uint64(baseline.StallCount), uint64(candidate.StallCount)
	result := delta(stallDurationMetric, baseline.StallMaxMS, candidate.StallMaxMS, "мс", true, minUint64(before, after))
	if stallDurationUnavailable(baseline, candidate) {
		return markDeltaUnavailable(result, before, after, incompleteStallDurationNote)
	}
	return result
}
