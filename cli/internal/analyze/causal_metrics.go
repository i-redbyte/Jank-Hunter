package analyze

import (
	"bytes"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// CausalMetrics are observed lower bounds for app-owned or app-initiated work.
// Unknown coverage and uninstrumented work remain outside these counts.
type CausalMetrics struct {
	ThreadGenerations, ThreadCreated, ThreadStarts, TaskSubmissions, HandlerPosts                        uint64
	CoroutineStarts, CoroutineObservedMachines, WorkerAttempts, RunStarts, Operations, ResourcesAcquired uint64
	ResourcesObservedUse, ResourcesReleased, WithoutObservedEnd, MaxObservedSpanNS                       uint64
	KnownCPUNS, KnownCPUIntervals, UnknownCPURuns, AmbiguousCPUIntervals                                 uint64
	KnownReadBytes, KnownReadOperations, UnknownReadOperations                                           uint64
	KnownWrittenBytes, KnownWriteOperations, UnknownWriteOperations                                      uint64
	LongestDurationNS                                                                                    uint64
}

// CausalCreatorGroup is an attributed code site. CPU here is inclusive; rows
// must never be added together because a nested run can appear in two rows.
type CausalCreatorGroup struct {
	Source, Role, Responsibility string
	Line                         uint64
	CausalMetrics
	Entities, CPUInclusiveNS, CPUInclusiveRuns uint64
}

type causalNodeMetrics struct {
	threadCreated, threadStart, taskSubmitted, handlerPost                 bool
	coroutineStart, coroutineObservedMachine, workerAttempt                bool
	runStarts                                                              uint64
	operations, resourcesAcquired, resourcesObservedUse, resourcesReleased uint64
	knownReadBytes, knownReadOperations, unknownReadOperations             uint64
	knownWrittenBytes, knownWriteOperations, unknownWriteOperations        uint64
	knownCPUInclusiveNS, knownCPURuns, unknownCPURuns                      uint64
	longestDurationNS                                                      uint64
	longRunCount, longestRunNS, longestRunCPUNS, longestRunPhase           uint64
	longestRunCPUKnown                                                     bool
}

type causalCPUInterval struct {
	key                  CausalKey
	tid, start, end, cpu uint64
}

func causalRunStart(kind jhlog.CausalEntityKind, phase uint64) bool {
	switch kind {
	case jhlog.CausalThread:
		return phase == 6
	case jhlog.CausalExecutorTask:
		return phase == 4
	case jhlog.CausalHandlerPost:
		return phase == 3
	case jhlog.CausalDirectExecution:
		return phase == 1
	case jhlog.CausalCoroutine:
		return phase == 2
	default:
		return false
	}
}

func causalRunEnd(kind jhlog.CausalEntityKind, phase uint64) bool {
	switch kind {
	case jhlog.CausalThread:
		return phase == 7
	case jhlog.CausalExecutorTask:
		return phase == 5
	case jhlog.CausalHandlerPost:
		return phase == 4
	case jhlog.CausalDirectExecution:
		return phase == 2
	case jhlog.CausalCoroutine:
		return phase == 3 || phase == 5
	default:
		return false
	}
}

func causalReads(kind uint64) bool {
	switch kind {
	case jhlog.CausalOperationFileRead, jhlog.CausalOperationDatabase,
		jhlog.CausalOperationHTTP, jhlog.CausalOperationContentRead:
		return true
	default:
		return false
	}
}

func causalWrites(kind uint64) bool {
	switch kind {
	case jhlog.CausalOperationFileWrite, jhlog.CausalOperationDatabase,
		jhlog.CausalOperationHTTP, jhlog.CausalOperationContentWrite:
		return true
	default:
		return false
	}
}

func causalSaturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func (s *causalCollectorState) observeCausalMetric(node *causalNode, event *jhlog.CausalEvent) {
	m := &node.metrics
	switch event.EntityKind {
	case jhlog.CausalThread:
		m.threadCreated = m.threadCreated || event.Phase == 1 || event.Phase == 2
		m.threadStart = m.threadStart || event.Phase == 4
	case jhlog.CausalExecutorTask:
		m.taskSubmitted = m.taskSubmitted || event.Phase == 1
	case jhlog.CausalHandlerPost:
		m.handlerPost = m.handlerPost || event.Phase == 2 && event.CallResult == 1
	case jhlog.CausalCoroutine:
		m.coroutineStart = m.coroutineStart || event.Phase == 1
		m.coroutineObservedMachine = m.coroutineObservedMachine || event.Phase == 6
	case jhlog.CausalWorkerAttempt:
		m.workerAttempt = m.workerAttempt || event.Phase == 1
	case jhlog.CausalResource:
		if event.Phase == 2 {
			m.resourcesAcquired++
		} else if event.Phase == 7 {
			m.resourcesObservedUse++
		} else if event.Phase == 4 {
			m.resourcesReleased++
		}
	case jhlog.CausalOperation:
		if event.Phase == 1 {
			m.operations++
			if causalReads(event.OperationKind) {
				if event.Presence&jhlog.CausalHasReadBytes != 0 {
					m.knownReadOperations++
					m.knownReadBytes = causalSaturatingAdd(m.knownReadBytes, event.ReadBytes)
				} else {
					m.unknownReadOperations++
				}
			}
			if causalWrites(event.OperationKind) {
				if event.Presence&jhlog.CausalHasWrittenBytes != 0 {
					m.knownWriteOperations++
					m.knownWrittenBytes = causalSaturatingAdd(m.knownWrittenBytes, event.WrittenBytes)
				} else {
					m.unknownWriteOperations++
				}
			}
		}
	}
	if event.Presence&jhlog.CausalHasDurationNanos != 0 && event.DurationNanos > m.longestDurationNS {
		m.longestDurationNS = event.DurationNanos
	}
	if causalRunStart(event.EntityKind, event.Phase) {
		m.runStarts++
		node.runAmbiguous = node.runAmbiguous || node.runOpen
		node.runOpen = true
		if event.Presence&jhlog.CausalHasTargetTID != 0 {
			node.runStartSeen, node.runStartTID, node.runStartAt = true, event.TargetTID, event.ObservedElapsedNanos
		} else {
			node.runStartSeen, node.runStartTID, node.runStartAt = false, 0, 0
		}
	}
	if !causalRunEnd(event.EntityKind, event.Phase) {
		return
	}
	validCPU := event.Presence&(jhlog.CausalHasCPUNanos|jhlog.CausalHasDurationNanos|jhlog.CausalHasTargetTID) ==
		jhlog.CausalHasCPUNanos|jhlog.CausalHasDurationNanos|jhlog.CausalHasTargetTID &&
		!node.runAmbiguous && node.runStartSeen && node.runStartTID == event.TargetTID && event.ObservedElapsedNanos >= node.runStartAt &&
		event.DurationNanos == event.ObservedElapsedNanos-node.runStartAt
	node.runStartSeen, node.runStartTID, node.runStartAt = false, 0, 0
	node.runOpen, node.runAmbiguous = false, false
	if event.EntityKind == jhlog.CausalCoroutine && event.Phase == 5 &&
		event.Presence&jhlog.CausalHasDurationNanos == 0 {
		return
	}
	if causalTaskExecution(event.EntityKind) && event.Presence&jhlog.CausalHasDurationNanos != 0 &&
		event.DurationNanos >= longTaskReviewLimitNS {
		m.longRunCount = causalSaturatingAdd(m.longRunCount, 1)
		if event.DurationNanos > m.longestRunNS {
			m.longestRunNS, m.longestRunPhase = event.DurationNanos, event.Phase
			m.longestRunCPUNS, m.longestRunCPUKnown = event.CPUNanos, validCPU
		}
	}
	if !validCPU {
		m.unknownCPURuns++
		return
	}
	m.knownCPUInclusiveNS = causalSaturatingAdd(m.knownCPUInclusiveNS, event.CPUNanos)
	m.knownCPURuns++
	if s.retainedBytes+96 > maxCausalRetainedByte {
		s.dropped++
		return
	}
	s.cpuIntervals = append(s.cpuIntervals, causalCPUInterval{key: node.key, tid: event.TargetTID,
		start: event.ObservedElapsedNanos - event.DurationNanos, end: event.ObservedElapsedNanos, cpu: event.CPUNanos})
	s.retainedBytes += 96
}

func causalTaskExecution(kind jhlog.CausalEntityKind) bool {
	return kind == jhlog.CausalExecutorTask || kind == jhlog.CausalDirectExecution || kind == jhlog.CausalCoroutine
}

type causalGroupKey struct {
	source, role, responsibility string
	line                         uint64
}

func causalGroupSite(n *causalNode, responsibility, caller string, line uint64) (string, uint64, string) {
	if n.appAction != "" && n.appAction == caller && n.appActionLine == line {
		if n.key.Kind == jhlog.CausalResource && n.appActionPhase == 7 {
			return caller, line, "место наблюдения use; приобретение неизвестно"
		}
		return caller, line, "инициатор/постановка"
	}
	if n.appCaller != "" && n.appCaller == caller && n.appCallerLine == line {
		return caller, line, "точный caller приложения"
	}
	if responsibility == "app_direct" {
		return caller, line, "код наблюдаемого действия"
	}
	return caller, line, "инициатор по parent ID"
}

func (s *causalCollectorState) finishCausalMetrics(a *CausalAnalysis, keys []CausalKey) {
	groups := make(map[causalGroupKey]*CausalCreatorGroup)
	appKeys := make(map[CausalKey]bool)
	for _, key := range keys {
		n := s.nodes[key]
		if causalIsJH(n) {
			continue
		}
		responsibility, caller, line, _ := s.appAttribution(n)
		if responsibility == "unknown" {
			continue
		}
		appKeys[key] = true
		source, sourceLine, role := causalGroupSite(n, responsibility, caller, line)
		groupKey := causalGroupKey{source, role, responsibility, sourceLine}
		g := groups[groupKey]
		if g == nil {
			g = &CausalCreatorGroup{Source: source, Line: sourceLine, Role: role, Responsibility: responsibility}
			groups[groupKey] = g
		}
		g.Entities++
		m := &n.metrics
		if n.key.Kind == jhlog.CausalThread {
			g.ThreadGenerations++
		}
		if m.threadCreated {
			g.ThreadCreated++
		}
		if m.threadStart {
			g.ThreadStarts++
		}
		if m.taskSubmitted {
			g.TaskSubmissions++
		}
		if m.handlerPost {
			g.HandlerPosts++
		}
		if m.coroutineStart {
			g.CoroutineStarts++
		}
		if m.coroutineObservedMachine && !m.coroutineStart {
			g.CoroutineObservedMachines++
		}
		if m.workerAttempt {
			g.WorkerAttempts++
		}
		g.RunStarts = causalSaturatingAdd(g.RunStarts, m.runStarts)
		if (m.threadStart || m.taskSubmitted || m.handlerPost || m.coroutineStart || m.workerAttempt || m.runStarts != 0) && !n.scopeEnded {
			g.WithoutObservedEnd++
		}
		g.Operations = causalSaturatingAdd(g.Operations, m.operations)
		g.ResourcesAcquired = causalSaturatingAdd(g.ResourcesAcquired, m.resourcesAcquired)
		g.ResourcesObservedUse = causalSaturatingAdd(g.ResourcesObservedUse, m.resourcesObservedUse)
		g.ResourcesReleased = causalSaturatingAdd(g.ResourcesReleased, m.resourcesReleased)
		g.KnownReadBytes = causalSaturatingAdd(g.KnownReadBytes, m.knownReadBytes)
		g.KnownReadOperations = causalSaturatingAdd(g.KnownReadOperations, m.knownReadOperations)
		g.UnknownReadOperations = causalSaturatingAdd(g.UnknownReadOperations, m.unknownReadOperations)
		g.KnownWrittenBytes = causalSaturatingAdd(g.KnownWrittenBytes, m.knownWrittenBytes)
		g.KnownWriteOperations = causalSaturatingAdd(g.KnownWriteOperations, m.knownWriteOperations)
		g.UnknownWriteOperations = causalSaturatingAdd(g.UnknownWriteOperations, m.unknownWriteOperations)
		g.CPUInclusiveNS = causalSaturatingAdd(g.CPUInclusiveNS, m.knownCPUInclusiveNS)
		g.CPUInclusiveRuns = causalSaturatingAdd(g.CPUInclusiveRuns, m.knownCPURuns)
		g.UnknownCPURuns = causalSaturatingAdd(g.UnknownCPURuns, m.unknownCPURuns)
		if m.longestDurationNS > g.LongestDurationNS {
			g.LongestDurationNS = m.longestDurationNS
		}
		if n.last >= n.first && n.last-n.first > g.MaxObservedSpanNS {
			g.MaxObservedSpanNS = n.last - n.first
		}
	}
	for _, g := range groups {
		a.Metrics.ThreadGenerations += g.ThreadGenerations
		a.Metrics.ThreadCreated += g.ThreadCreated
		a.Metrics.ThreadStarts += g.ThreadStarts
		a.Metrics.TaskSubmissions += g.TaskSubmissions
		a.Metrics.HandlerPosts += g.HandlerPosts
		a.Metrics.CoroutineStarts += g.CoroutineStarts
		a.Metrics.CoroutineObservedMachines += g.CoroutineObservedMachines
		a.Metrics.WorkerAttempts += g.WorkerAttempts
		a.Metrics.RunStarts += g.RunStarts
		a.Metrics.Operations += g.Operations
		a.Metrics.ResourcesAcquired += g.ResourcesAcquired
		a.Metrics.ResourcesObservedUse += g.ResourcesObservedUse
		a.Metrics.ResourcesReleased += g.ResourcesReleased
		a.Metrics.WithoutObservedEnd += g.WithoutObservedEnd
		a.Metrics.UnknownCPURuns += g.UnknownCPURuns
		a.Metrics.KnownReadBytes = causalSaturatingAdd(a.Metrics.KnownReadBytes, g.KnownReadBytes)
		a.Metrics.KnownReadOperations = causalSaturatingAdd(a.Metrics.KnownReadOperations, g.KnownReadOperations)
		a.Metrics.UnknownReadOperations += g.UnknownReadOperations
		a.Metrics.KnownWrittenBytes = causalSaturatingAdd(a.Metrics.KnownWrittenBytes, g.KnownWrittenBytes)
		a.Metrics.KnownWriteOperations = causalSaturatingAdd(a.Metrics.KnownWriteOperations, g.KnownWriteOperations)
		a.Metrics.UnknownWriteOperations += g.UnknownWriteOperations
		if g.LongestDurationNS > a.Metrics.LongestDurationNS {
			a.Metrics.LongestDurationNS = g.LongestDurationNS
		}
		if g.MaxObservedSpanNS > a.Metrics.MaxObservedSpanNS {
			a.Metrics.MaxObservedSpanNS = g.MaxObservedSpanNS
		}
		a.CreatorGroups = append(a.CreatorGroups, *g)
	}
	s.causalAggregateCPU(&a.Metrics, appKeys)
	sort.Slice(a.CreatorGroups, func(i, j int) bool {
		left, right := a.CreatorGroups[i], a.CreatorGroups[j]
		if left.CPUInclusiveNS != right.CPUInclusiveNS {
			return left.CPUInclusiveNS > right.CPUInclusiveNS
		}
		leftBytes := causalSaturatingAdd(left.KnownReadBytes, left.KnownWrittenBytes)
		rightBytes := causalSaturatingAdd(right.KnownReadBytes, right.KnownWrittenBytes)
		if leftBytes != rightBytes {
			return leftBytes > rightBytes
		}
		if left.Entities != right.Entities {
			return left.Entities > right.Entities
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		return left.Line < right.Line
	})
	if len(a.CreatorGroups) > 30 {
		a.CreatorGroupsOmitted = uint64(len(a.CreatorGroups) - 30)
		a.CreatorGroups = a.CreatorGroups[:30]
	}
}

func (s *causalCollectorState) causalAggregateCPU(result *CausalMetrics, appKeys map[CausalKey]bool) {
	intervals := make([]causalCPUInterval, 0, len(s.cpuIntervals))
	for _, interval := range s.cpuIntervals {
		if appKeys[interval.key] {
			intervals = append(intervals, interval)
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		a, b := intervals[i], intervals[j]
		if a.key.Scope != b.key.Scope {
			if c := bytes.Compare(a.key.Scope.RunID[:], b.key.Scope.RunID[:]); c != 0 {
				return c < 0
			}
			if c := bytes.Compare(a.key.Scope.ProcessID[:], b.key.Scope.ProcessID[:]); c != 0 {
				return c < 0
			}
			return bytes.Compare(a.key.Scope.SessionID[:], b.key.Scope.SessionID[:]) < 0
		}
		if a.tid != b.tid {
			return a.tid < b.tid
		}
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end > b.end
		}
		return a.cpu > b.cpu
	})
	var current causalCPUInterval
	var componentSize uint64
	var ambiguous bool
	flush := func() {
		if componentSize == 0 {
			return
		}
		if ambiguous {
			result.AmbiguousCPUIntervals += componentSize
		} else {
			result.KnownCPUNS = causalSaturatingAdd(result.KnownCPUNS, current.cpu)
			result.KnownCPUIntervals++
		}
	}
	for _, interval := range intervals {
		if componentSize == 0 || current.key.Scope != interval.key.Scope || current.tid != interval.tid || interval.start >= current.end {
			flush()
			current, componentSize, ambiguous = interval, 1, false
			continue
		}
		componentSize++
		if interval.end > current.end {
			ambiguous = true
			current.end = interval.end
		}
	}
	flush()
}
