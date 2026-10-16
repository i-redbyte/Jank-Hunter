package analyze

import (
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// ThreadAnalysis retains observations, never interpolated states or inferred CPU activity.
type ThreadAnalysis struct {
	Resources                                     []ThreadResourceObservation
	DroppedResources                              uint64
	CoroutineSegments                             []ThreadCoroutineSegment
	DroppedCoroutineSegments                      uint64
	Annotations                                   []ThreadAnnotation
	DroppedAnnotations                            uint64
	LifecycleObserved                             bool
	Lifecycle                                     []ThreadLifecycleObservation
	DroppedLifecycle                              uint64
	RunObservations, StartReturns, FactoryReturns uint64
	HistoryOmitted                                bool
	Processes                                     []ThreadProcessAnalysis
	DroppedSnapshots                              uint64
}
type ThreadResourceObservation struct {
	Scope, Process, CompletionTID, Kind, Source, Target, Outcome, OperationID string
	StartMS                                                                   *uint64
	EndMS                                                                     uint64
	StartDerived                                                              bool
}
type threadRawResource struct {
	log                                                   uint64
	process, completionTID, kind, source, target, outcome string
	start, end, operationID                               uint64
	startKnown                                            bool
}

func ceilMicrosToMillis(value uint64) uint64 {
	return value/1000 + boolUint64(value%1000 != 0)
}

type ThreadCoroutineSegment struct {
	Scope, Process, Token, Generation                             string
	EntryJavaThreadID, ExitJavaThreadID, EntryTID, ExitTID, Owner string
	StartedElapsedMS                                              uint64
	EndedElapsedMS, ObservedThroughElapsedMS                      *uint64
	State                                                         string
}

func optionalUnsignedID(id uint64) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

type ThreadProcessAnalysis struct {
	LastSnapshotAgeMS, ObservationEndMS           *uint64
	Dynamics                                      *ThreadDynamics
	SamplingIntervalMS                            uint64
	Scope                                         string
	Process                                       string
	StartCount, PeakCount, LastCount              int
	StartAtMS, PeakAtMS, LastAtMS                 uint64
	Appearances, Disappearances, PartialSnapshots int
	Snapshots                                     []ThreadSnapshotPoint
	Histories                                     []ThreadHistory
}
type ThreadSnapshotPoint struct {
	TimeBasis string
	TimeMS    uint64
	Snapshot  *jhlog.ThreadSnapshotEvent
}
type ThreadHistory struct {
	MainThread                          *bool
	KernelStart                         *ThreadKernelStart
	TID, StartTicks                     uint64
	FirstMS, LastMS                     uint64
	PresentAtObservationStart           bool
	DisappearedAfterMS, DisappearedByMS uint64
	Observations                        []ThreadHistoryPoint
}
type ThreadHistoryPoint struct {
	TimeMS                    uint64
	ElapsedMS, TicksPerSecond uint64
	CPU                       *ThreadCPUDelta
	Row                       jhlog.ThreadObservation
}
type threadRawSnapshot struct {
	timeBasis string
	log       uint64
	process   string
	time      uint64
	snapshot  *jhlog.ThreadSnapshotEvent
}
type threadRawCoroutineSegment struct {
	log     uint64
	process string
	segment *jhlog.CoroutineSegmentEvent
}
type threadCaptureBoundary struct {
	elapsed uint64
	unix    *uint64
}
type threadCollectorState struct {
	resourceOn               bool
	resources                []threadRawResource
	resourceBytes            int
	droppedResources         uint64
	coroutineSegments        []threadRawCoroutineSegment
	retainedCoroutineBytes   int
	droppedCoroutineSegments uint64
	origins                  map[uint64]threadOriginMetadata
	captures                 map[string]threadCaptureBoundary
	annotations              []threadRawAnnotation
	droppedAnnotations       uint64
	lifecycle                []threadRawLifecycle
	lifecycleBytes           int
	droppedLifecycle         uint64
	snapshots                []threadRawSnapshot
	identities               map[uint64]string
	retainedRows             int
	retainedBytes            int
	dropped                  uint64
}

func (c *collector) recordThreadResource(event jhlog.Event, kind, source, target, outcome string, durationMS uint64) {
	state := &c.threadAnalysis
	if !state.resourceOn || !event.Producer.HasThread || event.Producer.ThreadID == 0 {
		return
	}
	estimate := 128 + len(source) + len(target) + len(c.currentProcess)
	if len(state.resources) >= 50000 || state.resourceBytes+estimate > 16<<20 {
		state.droppedResources++
		return
	}
	state.resourceBytes += estimate
	if len(state.resources) == cap(state.resources) {
		capacity := max(128, 2*cap(state.resources))
		capacity = min(capacity, 50000)
		grown := make([]threadRawResource, len(state.resources), capacity)
		copy(grown, state.resources)
		state.resources = grown
	}
	state.resources = append(state.resources, threadRawResource{
		log: c.currentLogIndex, process: c.currentProcess,
		completionTID: fmt.Sprintf("%d", event.Producer.ThreadID), kind: kind, source: source,
		target:  target,
		outcome: outcome,
		start:   event.TimeMS - min(event.TimeMS, durationMS), end: event.TimeMS,
		startKnown:  durationMS <= event.TimeMS,
		operationID: c.currentOperationID,
	})
}

func (c *collector) recordCoroutineSegment(event jhlog.Event) {
	segment := event.CoroutineSegment
	if segment == nil {
		return
	}
	context := c.eventContext("", "")
	if !c.matchesFilters("", context, []string{segment.Owner}, segment.Owner, context.Owner) {
		return
	}
	estimate := 128 + len(c.currentProcess) + len(segment.Owner)
	s := &c.threadAnalysis
	if len(s.coroutineSegments) >= 50000 || s.retainedCoroutineBytes+estimate > 16<<20 {
		s.droppedCoroutineSegments++
		return
	}
	s.retainedCoroutineBytes += estimate
	c.markCohort()
	s.coroutineSegments = append(s.coroutineSegments, threadRawCoroutineSegment{
		log: c.currentLogIndex, process: c.currentProcess, segment: segment,
	})
}

func (c *collector) recordThreadSnapshot(event jhlog.Event) {
	// Bound empty snapshots, rows and variable-width Java metadata independently.
	estimate := 128 + len(c.currentProcess)
	for _, row := range event.ThreadSnapshot.Threads {
		estimate += 256 + len(row.Name)
		if row.Java != nil {
			estimate += 64 + len(row.Java.Name) + len(row.Java.State) + len(row.Java.Source)
		}
	}
	if len(c.threadAnalysis.snapshots) >= 100000 || c.threadAnalysis.retainedRows+len(event.ThreadSnapshot.Threads) > 100000 || c.threadAnalysis.retainedBytes+estimate > 32<<20 {
		c.threadAnalysis.dropped++
		return
	}
	c.threadAnalysis.retainedRows += len(event.ThreadSnapshot.Threads)
	c.threadAnalysis.retainedBytes += estimate
	observedTime := event.TimeMS
	timeBasis := "record_time"
	if unix := observationUnixMS(c.workRegistry.anchorElapsedMS, c.workRegistry.anchorUnixMS, event.ThreadSnapshot.ElapsedMS); unix != nil {
		observedTime = *unix
		timeBasis = "observation_clock"
	}
	c.threadAnalysis.snapshots = append(c.threadAnalysis.snapshots, threadRawSnapshot{log: c.currentLogIndex, process: c.currentProcess, time: observedTime, snapshot: event.ThreadSnapshot, timeBasis: timeBasis})
}
func (s *threadCollectorState) finishLog(index uint64, result jhlog.StreamResult) {
	hasSnapshot := len(s.snapshots) > 0 && s.snapshots[len(s.snapshots)-1].log == index
	hasLifecycle := len(s.lifecycle) > 0 && s.lifecycle[len(s.lifecycle)-1].log == index
	hasAnnotations := len(s.annotations) > 0 && s.annotations[len(s.annotations)-1].log == index
	hasCoroutine := len(s.coroutineSegments) > 0 && s.coroutineSegments[len(s.coroutineSegments)-1].log == index
	hasResource := len(s.resources) > 0 && s.resources[len(s.resources)-1].log == index
	if !hasSnapshot && !hasLifecycle && !hasAnnotations && !hasCoroutine && !hasResource {
		return
	}
	if s.identities == nil {
		s.identities = make(map[uint64]string)
	}
	s.recordOrigin(index, result)
	s.identities[index] = hex.EncodeToString([]byte(workerStreamIdentity(result)))
	if result.LatestQuality != nil {
		elapsed := result.LatestQuality.CapturedElapsedUS / 1000
		if elapsed == 0 {
			elapsed = result.LatestQuality.Counters[jhlog.QualityCollectionWindowEndElapsedMS]
		}
		if elapsed > 0 {
			if s.captures == nil {
				s.captures = make(map[string]threadCaptureBoundary)
			}
			scope := s.identities[index]
			if previous, ok := s.captures[scope]; !ok || elapsed > previous.elapsed {
				s.captures[scope] = threadCaptureBoundary{elapsed, observationUnixMS(result.Header.SegmentStartElapsedUS/1000, result.Header.SegmentStartUnixMS, elapsed)}
			}
		}
	}
}
func (s *threadCollectorState) finalize() *ThreadAnalysis {
	if len(s.snapshots) == 0 && s.dropped == 0 && len(s.lifecycle) == 0 && s.droppedLifecycle == 0 &&
		len(s.coroutineSegments) == 0 && s.droppedCoroutineSegments == 0 &&
		len(s.annotations) == 0 && s.droppedAnnotations == 0 &&
		len(s.resources) == 0 && s.droppedResources == 0 {
		return nil
	}
	result := &ThreadAnalysis{DroppedSnapshots: s.dropped, DroppedResources: s.droppedResources,
		Processes:                make([]ThreadProcessAnalysis, 0),
		DroppedCoroutineSegments: s.droppedCoroutineSegments}
	result.Resources = make([]ThreadResourceObservation, 0, len(s.resources))
	for _, raw := range s.resources {
		scope := s.identities[raw.log]
		if scope == "" {
			scope = fmt.Sprintf("log:%d", raw.log)
		}
		var start *uint64
		if raw.startKnown {
			value := raw.start
			start = &value
		}
		result.Resources = append(result.Resources, ThreadResourceObservation{
			Scope: scope, Process: raw.process, CompletionTID: raw.completionTID,
			Kind: raw.kind, Source: raw.source, Target: raw.target, Outcome: raw.outcome,
			StartMS: start, EndMS: raw.end,
			OperationID: optionalUnsignedID(raw.operationID), StartDerived: raw.startKnown,
		})
	}
	for _, raw := range s.coroutineSegments {
		segment := raw.segment
		scope := s.identities[raw.log]
		if scope == "" {
			scope = fmt.Sprintf("log:%d", raw.log)
		}
		state := []string{"suspended", "completed", "failure", "cancelled", "running_observed"}[segment.State]
		var ended, observedThrough *uint64
		until := segment.EndedElapsedMS
		if segment.State == 4 {
			observedThrough = &until
		} else {
			ended = &until
		}
		result.CoroutineSegments = append(result.CoroutineSegments, ThreadCoroutineSegment{
			Scope: scope, Process: raw.process, Token: fmt.Sprintf("%d", segment.Token),
			Generation:        fmt.Sprintf("%d", segment.Generation),
			EntryJavaThreadID: fmt.Sprintf("%d", segment.JavaThreadID),
			ExitJavaThreadID:  optionalUnsignedID(segment.ExitJavaThreadID),
			EntryTID:          optionalUnsignedID(segment.EntryTID),
			ExitTID:           optionalUnsignedID(segment.ExitTID),
			StartedElapsedMS:  segment.StartedElapsedMS, EndedElapsedMS: ended,
			ObservedThroughElapsedMS: observedThrough,
			State:                    state, Owner: segment.Owner,
		})
	}
	sort.SliceStable(result.CoroutineSegments, func(i, j int) bool {
		left, right := result.CoroutineSegments[i], result.CoroutineSegments[j]
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		if left.StartedElapsedMS != right.StartedElapsedMS {
			return left.StartedElapsedMS < right.StartedElapsedMS
		}
		leftUntil, rightUntil := left.EndedElapsedMS, right.EndedElapsedMS
		if leftUntil == nil {
			leftUntil = left.ObservedThroughElapsedMS
		}
		if rightUntil == nil {
			rightUntil = right.ObservedThroughElapsedMS
		}
		return *leftUntil < *rightUntil
	})
	s.finalizeLifecycle(result)
	s.finalizeAnnotations(result)
	groups := make(map[string][]threadRawSnapshot)
	for _, snapshot := range s.snapshots {
		scope := s.identities[snapshot.log]
		if scope == "" {
			scope = fmt.Sprintf("log:%d", snapshot.log)
		}
		groups[scope] = append(groups[scope], snapshot)
	}
	scopes := make([]string, 0, len(groups))
	for scope := range groups {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	for _, scope := range scopes {
		snapshots := groups[scope]
		monotonicClock := true
		for _, raw := range snapshots {
			if raw.timeBasis != "observation_clock" {
				monotonicClock = false
				break
			}
		}
		sort.SliceStable(snapshots, func(i, j int) bool {
			if monotonicClock {
				return snapshots[i].snapshot.ElapsedMS < snapshots[j].snapshot.ElapsedMS
			}
			return snapshots[i].time < snapshots[j].time
		})
		p := ThreadProcessAnalysis{Scope: scope, Process: snapshots[0].process}
		indexes := make(map[[2]uint64]int)
		active := make(map[[2]uint64]int)
		for n, raw := range snapshots {
			snap := raw.snapshot
			point := ThreadSnapshotPoint{TimeMS: raw.time, Snapshot: snap, TimeBasis: raw.timeBasis}
			p.Snapshots = append(p.Snapshots, point)
			if n == 0 {
				p.StartCount = len(snap.Threads)
				p.StartAtMS = raw.time
				p.PeakAtMS = raw.time
			}
			if len(snap.Threads) > p.PeakCount {
				p.PeakCount = len(snap.Threads)
				p.PeakAtMS = raw.time
			}
			p.LastCount = len(snap.Threads)
			p.LastAtMS = raw.time
			if !snap.Complete {
				p.PartialSnapshots++
			}
			seen := make(map[[2]uint64]bool, len(snap.Threads))
			for _, row := range snap.Threads {
				key := [2]uint64{row.TID, row.StartTicks}
				if seen[key] {
					continue
				}
				seen[key] = true
				index, ok := indexes[key]
				if !ok {
					index = len(p.Histories)
					indexes[key] = index
					metadata := s.origins[raw.log]
					var main *bool
					if metadata.pid > 0 {
						value := row.TID == metadata.pid
						main = &value
					}
					observedBy := snap.ElapsedMS
					if snap.ScanDurationMS <= math.MaxUint64-observedBy {
						observedBy += snap.ScanDurationMS
					}
					origin := threadKernelStart(row.StartTicks, snap.TicksPerSecond, observedBy, metadata.encodedCollectionStart, metadata.anchorElapsed, metadata.anchorUnix)
					p.Histories = append(p.Histories, ThreadHistory{TID: row.TID, StartTicks: row.StartTicks, FirstMS: raw.time, PresentAtObservationStart: n == 0, MainThread: main, KernelStart: origin})
					if n > 0 {
						p.Appearances++
					}
				}
				h := &p.Histories[index]
				h.LastMS = raw.time
				// Reappearance invalidates the earlier disappearance inference (e.g. racy enumeration).
				if h.DisappearedByMS != 0 {
					h.DisappearedByMS = 0
					h.DisappearedAfterMS = 0
					p.Disappearances--
				}
				observation := ThreadHistoryPoint{TimeMS: raw.time, ElapsedMS: snap.ElapsedMS, TicksPerSecond: snap.TicksPerSecond, Row: row}
				if len(h.Observations) > 0 {
					observation.CPU = threadCPUDelta(h.Observations[len(h.Observations)-1], observation)
				}
				h.Observations = append(h.Observations, observation)
				active[key] = index
			}
			if snap.Complete {
				for key, index := range active {
					h := &p.Histories[index]
					if !seen[key] && h.DisappearedByMS == 0 {
						h.DisappearedAfterMS = h.LastMS
						h.DisappearedByMS = raw.time
						// Enumeration and reads span the scan, not one atomic instant.
						if raw.timeBasis == "observation_clock" {
							if snap.ScanDurationMS > math.MaxUint64-raw.time {
								h.DisappearedByMS = math.MaxUint64
							} else {
								h.DisappearedByMS += snap.ScanDurationMS
							}
						}
						p.Disappearances++
						delete(active, key)
					}
				}
			}
		}
		if end, ok := s.captures[scope]; ok {
			last := p.Snapshots[len(p.Snapshots)-1].Snapshot.ElapsedMS
			if end.elapsed >= last {
				age := end.elapsed - last
				p.LastSnapshotAgeMS = &age
			}
			p.ObservationEndMS = end.unix
		}
		p.Dynamics = summarizeThreadDynamics(p)
		result.Processes = append(result.Processes, p)
	}
	return result
}
