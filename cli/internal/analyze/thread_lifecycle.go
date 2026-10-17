package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"sort"
	"strconv"
)

type ThreadLifecycleObservation struct {
	OperationID                             string
	Scope, Process, TimeBasis, RecordingTID string
	TimeMS                                  uint64
	ContextCaptured                         bool
	Screen, Operation, Owner                string
	Observation                             *jhlog.ThreadLifecycleEvent
}
type threadRawLifecycle struct {
	log   uint64
	value ThreadLifecycleObservation
}

func (c *collector) recordThreadLifecycle(event jhlog.Event) {
	point := event.ThreadLifecycle
	context := c.eventContext("", "")
	if !event.Attribution.Present {
		context = SignalContextStats{}
	}
	estimate := 256 + len(point.Name) + len(point.Source) + len(point.Factory) + len(context.Screen) + len(context.Operation) + len(context.Owner) + len(c.currentProcess)
	if len(c.threadAnalysis.lifecycle) >= 100000 || c.threadAnalysis.lifecycleBytes+estimate > 16<<20 {
		c.threadAnalysis.droppedLifecycle++
		return
	}
	c.threadAnalysis.lifecycleBytes += estimate
	value := ThreadLifecycleObservation{Process: c.currentProcess, TimeMS: event.TimeMS, TimeBasis: "record_time", Observation: point,
		RecordingTID: workerProducerThread(event.Producer), ContextCaptured: event.Attribution.Present, Screen: context.Screen, Operation: context.Operation, Owner: context.Owner}
	if event.Attribution.Present && event.Attribution.OperationID != 0 {
		value.OperationID = strconv.FormatUint(event.Attribution.OperationID, 10)
	}
	if unix := observationUnixMS(c.workRegistry.anchorElapsedMS, c.workRegistry.anchorUnixMS, point.ObservedElapsedNanos/1000000); unix != nil {
		value.TimeMS = *unix
		value.TimeBasis = "observation_clock"
	}
	c.threadAnalysis.lifecycle = append(c.threadAnalysis.lifecycle, threadRawLifecycle{log: c.currentLogIndex, value: value})
}

func (s *threadCollectorState) finalizeLifecycle(result *ThreadAnalysis) {
	result.DroppedLifecycle = s.droppedLifecycle
	result.LifecycleObserved = len(s.lifecycle) > 0 || s.droppedLifecycle > 0
	for _, raw := range s.lifecycle {
		value := raw.value
		value.Scope = s.identities[raw.log]
		if value.Scope == "" {
			value.Scope = fmt.Sprintf("log:%d", raw.log)
		}
		result.Lifecycle = append(result.Lifecycle, value)
		switch value.Observation.Stage {
		case jhlog.ThreadRunObserved:
			result.RunObservations++
		case jhlog.ThreadStartReturned:
			result.StartReturns++
		case jhlog.ThreadFactoryReturned:
			result.FactoryReturns++
		}
	}
	sort.SliceStable(result.Lifecycle, func(i, j int) bool { return result.Lifecycle[i].TimeMS < result.Lifecycle[j].TimeMS })
}
