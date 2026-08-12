package analyze

import (
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	maxAgentTopIntervals   = 16
	maxAgentTimelineItems  = 256
	maxAgentThreads        = 2_048
	maxAgentFingerprints   = 128
	maxAgentMethods        = 4_096
	maxAgentSequenceRanges = 256
	maxAgentSources        = 128
	agentTemporalWindowNS  = 250_000_000
)

type agentSequenceRange struct{ first, last uint64 }
type agentProducerKey struct {
	source     string
	producerID uint64
}

type agentSequenceState struct {
	ranges        []agentSequenceRange
	lostPrecision bool
}

type agentContext struct{ screen, flow, owner, step string }

func (context agentContext) label() string {
	parts := make([]string, 0, 4)
	for _, value := range []string{context.screen, context.flow, context.step, context.owner} {
		if value != "" && value != "unknown" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " / ")
}

func (context agentContext) known() bool {
	for _, value := range [...]string{context.screen, context.flow, context.owner, context.step} {
		if value != "" && value != "unknown" {
			return true
		}
	}
	return false
}

type agentStackState struct {
	count     uint64
	thread    uint64
	context   agentContext
	methodIDs []uint64
}

type agentStackSample struct {
	timeNS, fingerprint, thread, contextToken, sequence uint64
	source                                              string
	context                                             agentContext
}

type agentSymptom struct {
	startNS, endNS         uint64
	durationMS, jankFrames uint64
	kind, source, stack    string
	context                agentContext
}

// agentAggregator implements empty/add/merge/snapshot over bounded state.
// Temporal joins retain only the strongest 256 observations and never join
// different sources. Sequence coverage is mergeable and order independent.
type agentAggregator struct {
	events                                             uint64
	statusCode, statusDetail, configHash, nativeMemory uint64
	preset                                             string
	available                                          bool
	reason                                             string
	capabilities                                       AgentCapabilitySummary
	capabilityVariants                                 map[string]struct{}
	quality                                            AgentQualitySummary
	gc                                                 AgentIntervalSummary
	contention                                         AgentIntervalSummary
	threads                                            AgentThreadSummary
	threadSet                                          map[uint64]struct{}
	activeThreads                                      map[uint64]struct{}
	contexts                                           map[uint64]agentContext
	methods                                            map[uint64]string
	stacks                                             map[uint64]*agentStackState
	stackSamples                                       []agentStackSample
	symptoms                                           []agentSymptom
	sequences                                          map[agentProducerKey]*agentSequenceState
	clockOffsets                                       map[string]int64
	sources                                            map[string]struct{}
	dataGaps                                           []string
	sourceCapacityLost                                 bool
	truncatedStacks                                    uint64
}

func newAgentAggregator() *agentAggregator {
	return &agentAggregator{
		capabilityVariants: map[string]struct{}{}, threadSet: map[uint64]struct{}{},
		activeThreads: map[uint64]struct{}{}, contexts: map[uint64]agentContext{},
		methods: map[uint64]string{}, stacks: map[uint64]*agentStackState{},
		sequences: map[agentProducerKey]*agentSequenceState{}, clockOffsets: map[string]int64{},
		sources: map[string]struct{}{},
	}
}

func (a *agentAggregator) add(dict map[uint64]string, event jhlog.Event, flow FlowStats) {
	payload := event.Agent
	if payload == nil {
		return
	}
	a.events++
	source := firstNonEmpty(event.Source, "unknown")
	if _, exists := a.sources[source]; exists || len(a.sources) < maxAgentSources {
		a.sources[source] = struct{}{}
	} else {
		a.sourceCapacityLost = true
	}
	a.observeSequence(source, payload.ProducerID, payload.ProducerSequence)
	context := agentContext{screen: flow.Screen, flow: flow.Flow, owner: flow.Owner, step: flow.Step}
	if defined, ok := a.contexts[payload.ContextToken]; ok && !context.known() {
		context = defined
	}
	switch payload.SemanticType {
	case jhlog.AgentStatus:
		a.addStatus(payload)
	case jhlog.AgentCapability:
		a.capabilities = AgentCapabilitySummary{Requested: payload.Payload0, Potential: payload.Payload1, Granted: payload.Payload2, Active: payload.Payload3}
		a.capabilities.Missing = payload.Payload0 &^ payload.Payload3
		key := fmt.Sprintf("%x/%x/%x/%x", payload.Payload0, payload.Payload1, payload.Payload2, payload.Payload3)
		if len(a.capabilityVariants) < maxAgentSources {
			a.capabilityVariants[key] = struct{}{}
		}
	case jhlog.AgentQualitySnapshot:
		a.quality.QueueHighWatermark = maxUint64(a.quality.QueueHighWatermark, payload.Payload0)
		a.quality.QueueFullTotal = maxUint64(a.quality.QueueFullTotal, payload.Payload1)
		a.quality.AdmissionContentionTotal = maxUint64(a.quality.AdmissionContentionTotal, payload.Payload2)
		a.quality.OtherNativeLossTotal = maxUint64(a.quality.OtherNativeLossTotal, payload.Payload3)
	case jhlog.AgentThreadStart:
		a.threads.Starts++
		a.observeThread(payload.ThreadToken)
		if len(a.activeThreads) < maxAgentThreads {
			a.activeThreads[payload.ThreadToken] = struct{}{}
		}
		if uint64(len(a.activeThreads)) > a.threads.MaxConcurrent {
			a.threads.MaxConcurrent = uint64(len(a.activeThreads))
		}
	case jhlog.AgentThreadEnd:
		a.threads.Ends++
		a.observeThread(payload.ThreadToken)
		delete(a.activeThreads, payload.ThreadToken)
	case jhlog.AgentGCInterval:
		a.addInterval(&a.gc, event, payload)
	case jhlog.AgentMonitorContentionInterval:
		a.observeThread(payload.ThreadToken)
		a.addInterval(&a.contention, event, payload)
	case jhlog.AgentThreadStackSample:
		a.addStackSample(event, payload, context)
	case jhlog.AgentStackDefinition:
		a.addStackDefinition(payload)
	case jhlog.AgentClockSync:
		a.quality.ClockSyncCount++
		a.quality.MaxClockUncertaintyNS = maxUint64(a.quality.MaxClockUncertaintyNS, payload.Payload1)
		offset := signedDifference(payload.Payload0, saturatingMultiply(event.TimeUS, 1_000))
		if prior, ok := a.clockOffsets[source]; ok && absInt64(offset) < absInt64(prior) {
			a.clockOffsets[source] = offset
		} else if !ok && len(a.clockOffsets) < maxAgentSources {
			a.clockOffsets[source] = offset
		}
	case jhlog.AgentCorrelationLink:
		if payload.EventFlags&jhlog.AgentFlagContextDefinition != 0 {
			if len(a.contexts) < maxAgentThreads {
				a.contexts[firstNonZero(payload.ContextToken, payload.Payload0)] = context
			}
		}
	case jhlog.AgentMethodDefinition:
		name := jhlog.ResolveSymbol(dict, payload.MethodRef)
		if name != "unknown" && len(a.methods) < maxAgentMethods {
			a.methods[payload.Payload0] = name
		}
	}
}

func (a *agentAggregator) addStatus(payload *jhlog.AgentEvent) {
	a.statusCode, a.statusDetail = payload.Payload0, payload.Payload1
	a.configHash = firstNonZero(payload.Payload2, a.configHash)
	a.nativeMemory = maxUint64(a.nativeMemory, payload.Payload3)
	switch payload.Payload0 {
	case 2:
		a.available, a.reason = true, "attached"
	case 3:
		a.available, a.reason = true, "capability_degraded"
	case 4:
		a.reason = "attach_failed"
	case 5:
		if a.reason == "" {
			a.reason = "stopped"
		}
	case 0x100:
		a.preset = agentPreset(payload.Payload1)
	case 0x1000 + 1:
		a.reason = "api_unsupported"
	case 0x1000 + 2:
		a.reason = "app_not_debuggable"
	default:
		if payload.Payload0 >= 0x1000 {
			a.reason = agentSDKReason(payload.Payload0 - 0x1000)
		}
	}
}

func (a *agentAggregator) observeThread(token uint64) {
	if token == 0 {
		return
	}
	if len(a.threadSet) < maxAgentThreads {
		a.threadSet[token] = struct{}{}
	}
}

func (a *agentAggregator) addInterval(summary *AgentIntervalSummary, event jhlog.Event, payload *jhlog.AgentEvent) {
	interval := AgentInterval{StartNS: payload.Payload0, DurationNS: payload.Payload1, DurationMS: float64(payload.Payload1) / 1e6,
		ThreadToken: payload.ThreadToken, ContextToken: payload.ContextToken, RelatedToken: payload.Payload2,
		Reason: payload.Payload3, Sequence: payload.ProducerSequence, Source: firstNonEmpty(event.Source, "unknown")}
	summary.Count++
	summary.TotalNS = saturatingAdd(summary.TotalNS, interval.DurationNS)
	summary.MaxNS = maxUint64(summary.MaxNS, interval.DurationNS)
	insertTopInterval(&summary.Top, interval)
}

func (a *agentAggregator) addStackSample(event jhlog.Event, payload *jhlog.AgentEvent, context agentContext) {
	a.observeThread(payload.ThreadToken)
	state := a.stacks[payload.Payload0]
	if state == nil && len(a.stacks) < maxAgentFingerprints {
		state = &agentStackState{}
		a.stacks[payload.Payload0] = state
	}
	if state != nil {
		state.count++
		state.thread = payload.ThreadToken
		if context.known() {
			state.context = context
		}
	}
	sample := agentStackSample{timeNS: saturatingMultiply(event.TimeUS, 1_000), fingerprint: payload.Payload0, thread: payload.ThreadToken,
		contextToken: payload.ContextToken, sequence: payload.ProducerSequence, source: firstNonEmpty(event.Source, "unknown"), context: context}
	a.stackSamples = insertRecentStack(a.stackSamples, sample)
	if payload.Payload3 != 0 {
		a.quality.IncompleteWindows++
		a.truncatedStacks++
	}
}

func (a *agentAggregator) addStackDefinition(payload *jhlog.AgentEvent) {
	state := a.stacks[payload.Payload0]
	if state == nil && len(a.stacks) < maxAgentFingerprints {
		state = &agentStackState{}
		a.stacks[payload.Payload0] = state
	}
	if state == nil {
		return
	}
	frameIndex := uint32(payload.Payload3)
	if int(frameIndex) >= maxAgentTopIntervals*4 {
		return
	}
	for len(state.methodIDs) <= int(frameIndex) {
		state.methodIDs = append(state.methodIDs, 0)
	}
	state.methodIDs[frameIndex] = payload.Payload1
}

func (a *agentAggregator) addStallSymptom(event jhlog.Event, flow FlowStats, stack string) {
	endNS := saturatingMultiply(event.TimeMS, 1_000_000)
	durationNS := saturatingMultiply(event.Stall.DurationMS, 1_000_000)
	startNS := uint64(0)
	if endNS >= durationNS {
		startNS = endNS - durationNS
	}
	a.addSymptom(agentSymptom{startNS: startNS, endNS: endNS, durationMS: event.Stall.DurationMS,
		kind: "main_thread_stall", source: firstNonEmpty(event.Source, "unknown"), stack: stack,
		context: agentContext{screen: flow.Screen, flow: flow.Flow, owner: flow.Owner, step: flow.Step}})
}

func (a *agentAggregator) addUISymptom(event jhlog.Event, flow FlowStats) {
	if event.UIWindow.JankCount == 0 {
		return
	}
	endNS := saturatingMultiply(event.TimeMS, 1_000_000)
	durationNS := saturatingMultiply(event.UIWindow.WindowMS, 1_000_000)
	startNS := uint64(0)
	if endNS >= durationNS {
		startNS = endNS - durationNS
	}
	a.addSymptom(agentSymptom{startNS: startNS, endNS: endNS, durationMS: event.UIWindow.WindowMS,
		jankFrames: event.UIWindow.JankCount, kind: "ui_jank", source: firstNonEmpty(event.Source, "unknown"),
		context: agentContext{screen: flow.Screen, flow: flow.Flow, owner: flow.Owner, step: flow.Step}})
}

func (a *agentAggregator) addSymptom(symptom agentSymptom) {
	if len(a.symptoms) < maxAgentTimelineItems {
		a.symptoms = append(a.symptoms, symptom)
		return
	}
	worst := 0
	for index := 1; index < len(a.symptoms); index++ {
		if symptomWeight(a.symptoms[index]) < symptomWeight(a.symptoms[worst]) {
			worst = index
		}
	}
	if symptomWeight(symptom) > symptomWeight(a.symptoms[worst]) {
		a.symptoms[worst] = symptom
	}
}

func (a *agentAggregator) observeSequence(source string, producerID, sequence uint64) {
	if sequence == 0 {
		return
	}
	key := agentProducerKey{source: source, producerID: producerID}
	state := a.sequences[key]
	if state == nil {
		if len(a.sequences) >= maxAgentSources {
			a.sourceCapacityLost = true
			return
		}
		state = &agentSequenceState{}
		a.sequences[key] = state
	}
	state.add(sequence)
}

func (state *agentSequenceState) add(sequence uint64) {
	if len(state.ranges) == 0 {
		state.ranges = append(state.ranges, agentSequenceRange{first: sequence, last: sequence})
		return
	}
	last := &state.ranges[len(state.ranges)-1]
	if sequence >= last.first {
		if sequence <= last.last || (last.last != ^uint64(0) && sequence == last.last+1) {
			last.last = maxUint64(last.last, sequence)
			return
		}
		if len(state.ranges) >= maxAgentSequenceRanges {
			state.lostPrecision = true
			return
		}
		state.ranges = append(state.ranges, agentSequenceRange{first: sequence, last: sequence})
		return
	}
	state.addRange(agentSequenceRange{first: sequence, last: sequence})
}

func (state *agentSequenceState) addRange(value agentSequenceRange) {
	state.ranges = append(state.ranges, value)
	sort.Slice(state.ranges, func(i, j int) bool { return state.ranges[i].first < state.ranges[j].first })
	merged := state.ranges[:0]
	for _, current := range state.ranges {
		lastIndex := len(merged) - 1
		if lastIndex < 0 || (merged[lastIndex].last != ^uint64(0) && current.first > merged[lastIndex].last+1) {
			merged = append(merged, current)
			continue
		}
		merged[lastIndex].last = maxUint64(merged[lastIndex].last, current.last)
	}
	state.ranges = merged
	if len(state.ranges) > maxAgentSequenceRanges {
		state.lostPrecision = true
		state.ranges = state.ranges[:maxAgentSequenceRanges]
	}
}

func (state *agentSequenceState) gaps() uint64 {
	var gaps uint64
	if len(state.ranges) > 0 && state.ranges[0].first > 1 {
		gaps = state.ranges[0].first - 1
	}
	for index := 1; index < len(state.ranges); index++ {
		gaps = saturatingAdd(gaps, state.ranges[index].first-state.ranges[index-1].last-1)
	}
	return gaps
}

func (a *agentAggregator) merge(other *agentAggregator) {
	if other == nil {
		return
	}
	// Merge is used by chunked analyzers/tests. Replaying bounded canonical
	// state is deterministic; cumulative fields and sequence ranges are exact.
	a.events = saturatingAdd(a.events, other.events)
	if other.statusCode != 0 {
		a.statusCode, a.statusDetail, a.reason = other.statusCode, other.statusDetail, other.reason
	}
	a.available = a.available || other.available
	a.configHash = firstNonZero(other.configHash, a.configHash)
	a.nativeMemory = maxUint64(a.nativeMemory, other.nativeMemory)
	if other.preset != "" {
		a.preset = other.preset
	}
	if other.capabilities.Requested != 0 {
		a.capabilities = other.capabilities
	}
	for key := range other.capabilityVariants {
		a.capabilityVariants[key] = struct{}{}
	}
	mergeAgentInterval(&a.gc, other.gc)
	mergeAgentInterval(&a.contention, other.contention)
	a.quality.QueueHighWatermark = maxUint64(a.quality.QueueHighWatermark, other.quality.QueueHighWatermark)
	a.quality.QueueFullTotal = maxUint64(a.quality.QueueFullTotal, other.quality.QueueFullTotal)
	a.quality.AdmissionContentionTotal = maxUint64(a.quality.AdmissionContentionTotal, other.quality.AdmissionContentionTotal)
	a.quality.OtherNativeLossTotal = maxUint64(a.quality.OtherNativeLossTotal, other.quality.OtherNativeLossTotal)
	a.quality.ClockSyncCount += other.quality.ClockSyncCount
	a.quality.IncompleteWindows += other.quality.IncompleteWindows
	a.truncatedStacks += other.truncatedStacks
	a.quality.MaxClockUncertaintyNS = maxUint64(a.quality.MaxClockUncertaintyNS, other.quality.MaxClockUncertaintyNS)
	for key, state := range other.sequences {
		for _, interval := range state.ranges {
			a.observeSequenceRange(key, interval)
		}
	}
	for _, sample := range other.stackSamples {
		a.stackSamples = insertRecentStack(a.stackSamples, sample)
	}
	for _, symptom := range other.symptoms {
		a.addSymptom(symptom)
	}
	for token, context := range other.contexts {
		if len(a.contexts) < maxAgentThreads {
			a.contexts[token] = context
		}
	}
	for id, method := range other.methods {
		if len(a.methods) < maxAgentMethods {
			a.methods[id] = method
		}
	}
	for fingerprint, state := range other.stacks {
		a.mergeStack(fingerprint, state)
	}
	for token := range other.threadSet {
		a.observeThread(token)
	}
	a.threads.Starts += other.threads.Starts
	a.threads.Ends += other.threads.Ends
	a.threads.MaxConcurrent = maxUint64(a.threads.MaxConcurrent, other.threads.MaxConcurrent)
	for source, offset := range other.clockOffsets {
		if len(a.clockOffsets) < maxAgentSources {
			a.clockOffsets[source] = offset
		}
	}
	for source := range other.sources {
		if len(a.sources) < maxAgentSources {
			a.sources[source] = struct{}{}
		}
	}
	a.sourceCapacityLost = a.sourceCapacityLost || other.sourceCapacityLost
}

func (a *agentAggregator) observeSequenceRange(key agentProducerKey, interval agentSequenceRange) {
	state := a.sequences[key]
	if state == nil {
		state = &agentSequenceState{}
		a.sequences[key] = state
	}
	state.addRange(interval)
}

func (a *agentAggregator) mergeStack(fingerprint uint64, other *agentStackState) {
	state := a.stacks[fingerprint]
	if state == nil && len(a.stacks) < maxAgentFingerprints {
		copyState := *other
		copyState.methodIDs = append([]uint64(nil), other.methodIDs...)
		a.stacks[fingerprint] = &copyState
		return
	}
	if state == nil {
		return
	}
	state.count += other.count
	if state.thread == 0 {
		state.thread = other.thread
	}
	if !state.context.known() {
		state.context = other.context
	}
	if len(other.methodIDs) > len(state.methodIDs) {
		state.methodIDs = append(state.methodIDs, make([]uint64, len(other.methodIDs)-len(state.methodIDs))...)
	}
	for index, id := range other.methodIDs {
		if state.methodIDs[index] == 0 {
			state.methodIDs[index] = id
		}
	}
}

func (a *agentAggregator) snapshot() AgentSummary {
	result := AgentSummary{Available: a.available, Availability: "данных агента нет", Reason: a.reason, StatusCode: a.statusCode,
		StatusDetail: a.statusDetail, EffectivePreset: firstNonEmpty(a.preset, "unknown"), NativeMemoryBytes: a.nativeMemory,
		EventCount: a.events, Capabilities: a.capabilities, Quality: a.quality, GC: a.gc, Contention: a.contention, Threads: a.threads}
	if a.events > 0 {
		result.Availability = "недоступен"
		if a.available {
			result.Availability = "доступен"
		}
	}
	if a.configHash != 0 {
		result.ConfigHash = fmt.Sprintf("0x%016x", a.configHash)
	}
	result.Capabilities.RequestedList = capabilityNames(result.Capabilities.Requested)
	result.Capabilities.ActiveList = capabilityNames(result.Capabilities.Active)
	result.GC.TotalMS = float64(result.GC.TotalNS) / 1e6
	result.GC.MaxMS = float64(result.GC.MaxNS) / 1e6
	result.Contention.TotalMS = float64(result.Contention.TotalNS) / 1e6
	result.Contention.MaxMS = float64(result.Contention.MaxNS) / 1e6
	result.Threads.UniqueObserved = len(a.threadSet)
	if result.Threads.Starts >= result.Threads.Ends {
		result.Threads.EstimatedActive = result.Threads.Starts - result.Threads.Ends
	}
	result.Stacks = a.stackSnapshot()
	result.Stacks.TruncatedSamples = a.truncatedStacks
	for _, state := range a.sequences {
		result.Quality.SequenceGaps = saturatingAdd(result.Quality.SequenceGaps, state.gaps())
		if state.lostPrecision {
			result.Quality.IncompleteWindows++
		}
	}
	result.Quality.CompatibleClockCalibration = a.compatibleClocks()
	result.DataGaps = a.buildDataGaps(result)
	result.Findings = a.buildFindings(result)
	result.Limitations = []string{
		"Совпадение GC/contention со stall измеряет временное перекрытие, но само по себе не доказывает причинность.",
		"Triggered stack — ограниченная выборка конкретного момента; отсутствие метода в sample не доказывает, что он не выполнялся.",
		"Идентификаторы thread/method/stack действуют только внутри process instance и не сравниваются как стабильные символы между запусками.",
	}
	return result
}

func (a *agentAggregator) stackSnapshot() AgentStackSummary {
	result := AgentStackSummary{}
	for fingerprint, state := range a.stacks {
		result.Samples += state.count
		result.Definitions += uint64(len(state.methodIDs))
		methods := make([]string, 0, len(state.methodIDs))
		missing := false
		for _, id := range state.methodIDs {
			if name := a.methods[id]; name != "" {
				methods = append(methods, name)
			} else if id != 0 {
				missing = true
			}
		}
		if missing {
			result.MissingDefinitions++
		}
		if state.count > 0 {
			result.Hotspots = append(result.Hotspots, AgentStackHotspot{Fingerprint: fmt.Sprintf("0x%016x", fingerprint), Samples: state.count, ThreadToken: state.thread, Context: state.context.label(), Methods: methods})
		}
	}
	sort.Slice(result.Hotspots, func(i, j int) bool {
		if result.Hotspots[i].Samples == result.Hotspots[j].Samples {
			return result.Hotspots[i].Fingerprint < result.Hotspots[j].Fingerprint
		}
		return result.Hotspots[i].Samples > result.Hotspots[j].Samples
	})
	if len(result.Hotspots) > 16 {
		result.Hotspots = result.Hotspots[:16]
	}
	for _, sample := range a.stackSamples {
		if state := a.stacks[sample.fingerprint]; state == nil || len(state.methodIDs) == 0 {
			result.MissingDefinitions++
		}
	}
	return result
}

func (a *agentAggregator) buildDataGaps(result AgentSummary) []string {
	gaps := append([]string(nil), a.dataGaps...)
	if result.Quality.SequenceGaps > 0 {
		gaps = append(gaps, fmt.Sprintf("В native producer sequence отсутствует %d событий.", result.Quality.SequenceGaps))
	}
	if result.Quality.QueueFullTotal > 0 {
		gaps = append(gaps, fmt.Sprintf("Native queue была заполнена %d раз.", result.Quality.QueueFullTotal))
	}
	if result.Quality.AdmissionContentionTotal > 0 {
		gaps = append(gaps, fmt.Sprintf("Native admission отклонён из-за contention %d раз.", result.Quality.AdmissionContentionTotal))
	}
	if result.Quality.OtherNativeLossTotal > 0 {
		gaps = append(gaps, fmt.Sprintf("Другие native losses: %d.", result.Quality.OtherNativeLossTotal))
	}
	if result.Stacks.MissingDefinitions > 0 {
		gaps = append(gaps, fmt.Sprintf("Для %d stack observations отсутствует полное определение.", result.Stacks.MissingDefinitions))
	}
	if len(a.capabilityVariants) > 1 {
		gaps = append(gaps, "В объединённых streams различается capability matrix; причинные метрики не полностью сопоставимы.")
	}
	if a.sourceCapacityLost {
		gaps = append(gaps, "Bounded agent source/sequence index достиг лимита; часть coverage metadata агрегирована неточно.")
	}
	if !result.Quality.CompatibleClockCalibration && result.EventCount > 0 {
		gaps = append(gaps, "Нет совместимой clock calibration для всех streams; cross-stream temporal joins запрещены.")
	}
	return gaps
}

func (a *agentAggregator) compatibleClocks() bool {
	if a.events == 0 {
		return false
	}
	if len(a.clockOffsets) == 0 || len(a.clockOffsets) != len(a.sources) {
		return false
	}
	var min, max int64
	first := true
	for _, offset := range a.clockOffsets {
		if first || offset < min {
			min = offset
		}
		if first || offset > max {
			max = offset
		}
		first = false
	}
	threshold := saturatingAdd(5_000_000, saturatingAdd(a.quality.MaxClockUncertaintyNS, a.quality.MaxClockUncertaintyNS))
	return int64Distance(min, max) <= threshold
}

func (a *agentAggregator) buildFindings(summary AgentSummary) []AgentFinding {
	findings := make([]AgentFinding, 0, 8)
	symptoms := append([]agentSymptom(nil), a.symptoms...)
	sort.Slice(symptoms, func(i, j int) bool { return symptomWeight(symptoms[i]) > symptomWeight(symptoms[j]) })
	for index, symptom := range symptoms {
		if len(findings) >= 8 {
			break
		}
		gc, hasGC := bestOverlap(symptom, a.gc.Top)
		contention, hasContention := bestOverlap(symptom, a.contention.Top)
		stack, hasStack := a.closestStack(symptom)
		if !hasGC && !hasContention && !hasStack {
			continue
		}
		finding := AgentFinding{ID: fmt.Sprintf("artti-%d", index+1), Symptom: symptomDescription(symptom), Screen: symptom.context.screen,
			Flow: symptom.context.flow, Owner: symptom.context.owner, ConfidenceScore: 45,
			MissingOrCounterEvidence: []string{"Temporal overlap и sampled stack не доказывают, что наблюдаемый метод вызвал GC/contention или был единственной причиной симптома."},
			AlternativeExplanations:  []string{"Другая работа main thread, I/O, scheduler delay или GPU/render bottleneck могла совпасть с окном.", "GC мог быть следствием allocation pressure, а не первичной причиной задержки."},
			Actions:                  []string{"Повторить сценарий с Perfetto/CPU profiler и тем же screen/flow/owner.", "Сравнить с прогоном без подозреваемой работы и проверить устойчивость временной цепочки."},
			TimelineReference:        fmt.Sprintf("%s@%d..%d ns", symptom.source, symptom.startNS, symptom.endNS)}
		if hasGC {
			overlap := intervalOverlapNS(symptom, gc)
			finding.EvidenceChain = append(finding.EvidenceChain, AgentEvidenceStep{Level: "DIRECT", Statement: "GC interval пересёк окно симптома", Measurement: fmt.Sprintf("GC %.3f ms; overlap %.3f ms", gc.DurationMS, float64(overlap)/1e6), EventIDs: []uint64{gc.Sequence}})
			finding.ExactMeasurements = append(finding.ExactMeasurements, fmt.Sprintf("GC total в прогоне %.3f ms, max %.3f ms", summary.GC.TotalMS, summary.GC.MaxMS))
			finding.PositiveEvidence = append(finding.PositiveEvidence, fmt.Sprintf("Измерено %.3f ms перекрытия GC со stall/jank window.", float64(overlap)/1e6))
			finding.ConfidenceScore += 20
		}
		if hasContention {
			overlap := intervalOverlapNS(symptom, contention)
			finding.ThreadToken = contention.ThreadToken
			finding.EvidenceChain = append(finding.EvidenceChain, AgentEvidenceStep{Level: "DIRECT", Statement: "Monitor contention пересёк окно симптома", Measurement: fmt.Sprintf("contention %.3f ms; overlap %.3f ms", contention.DurationMS, float64(overlap)/1e6), EventIDs: []uint64{contention.Sequence}})
			finding.PositiveEvidence = append(finding.PositiveEvidence, "Зафиксирован законченный contention interval в том же temporal window.")
			finding.ConfidenceScore += 20
		}
		if hasStack {
			methods := a.stackMethods(stack.fingerprint)
			method := firstInterestingMethod(methods)
			finding.ThreadToken = firstNonZero(finding.ThreadToken, stack.thread)
			finding.Method = method
			statement := "Triggered stack sample зафиксирован рядом с симптомом"
			if method != "" {
				statement += "; hotspot " + method
			}
			finding.EvidenceChain = append([]AgentEvidenceStep{{Level: "DIRECT", Statement: symptomDescription(symptom), Measurement: fmt.Sprintf("window %d..%d ns", symptom.startNS, symptom.endNS)}, {Level: "DIRECT", Statement: statement, Measurement: fmt.Sprintf("fingerprint=0x%016x", stack.fingerprint), EventIDs: []uint64{stack.sequence}}}, finding.EvidenceChain...)
			finding.PositiveEvidence = append(finding.PositiveEvidence, "Stack sample и симптом принадлежат одному source и bounded temporal window.")
			finding.ConfidenceScore += 20
			if containsImageDecode(methods) {
				finding.SuspectedCause = "Декодирование/подготовка изображения на наблюдаемом потоке с allocation pressure и GC"
				finding.Actions = append([]string{"Перенести decode/resize с main thread, использовать подготовленные thumbnails и повторить сценарий."}, finding.Actions...)
			}
		}
		if finding.SuspectedCause == "" {
			if hasContention {
				finding.SuspectedCause = "Блокировка/ожидание monitor в потоке, пересекшее окно задержки"
			} else if hasGC {
				finding.SuspectedCause = "GC pause/allocation pressure, совпавшие с окном задержки"
			} else {
				finding.SuspectedCause = "Работа из triggered stack sample рядом с задержкой"
			}
		}
		if hasStack && (hasGC || hasContention) {
			finding.EvidenceLevel = "STRONG_ASSOCIATION"
		} else {
			finding.EvidenceLevel = "TEMPORAL_CORRELATION"
		}
		if !hasStack {
			finding.MissingOrCounterEvidence = append(finding.MissingOrCounterEvidence, "Нет triggered stack sample в bounded окне.")
		}
		if !hasGC {
			finding.MissingOrCounterEvidence = append(finding.MissingOrCounterEvidence, "GC overlap не зафиксирован.")
		}
		penalty := qualityPenalty(summary)
		finding.ConfidenceScore -= penalty
		if penalty > 0 {
			finding.DataQualityPenalties = append(finding.DataQualityPenalties, strings.Join(summary.DataGaps, " "))
		}
		finding.ConfidenceScore = clamp(finding.ConfidenceScore, 5, 95)
		finding.Confidence = confidenceLabel(finding.ConfidenceScore)
		findings = append(findings, finding)
	}
	if len(findings) == 0 && summary.Contention.Count > 0 {
		top := summary.Contention.Top[0]
		findings = append(findings, AgentFinding{ID: "artti-contention-1", EvidenceLevel: "DIRECT", Symptom: "Длительный monitor contention", SuspectedCause: "Конкуренция за monitor", ThreadToken: top.ThreadToken, ConfidenceScore: clamp(70-qualityPenalty(summary), 5, 95), Confidence: confidenceLabel(clamp(70-qualityPenalty(summary), 5, 95)), ExactMeasurements: []string{fmt.Sprintf("max %.3f ms; total %.3f ms", summary.Contention.MaxMS, summary.Contention.TotalMS)}, PositiveEvidence: []string{"Зафиксирован законченный JVMTI contention interval."}, MissingOrCounterEvidence: []string{"Нет связанного main-thread stall/UI window."}, AlternativeExplanations: []string{"Contention мог происходить в background thread без влияния на UI."}, Actions: []string{"Проверить владельца monitor и убрать длительную работу из synchronized section."}, TimelineReference: fmt.Sprintf("%s sequence=%d", top.Source, top.Sequence)})
	}
	return findings
}

func (a *agentAggregator) closestStack(symptom agentSymptom) (agentStackSample, bool) {
	var best agentStackSample
	distance := ^uint64(0)
	found := false
	for _, sample := range a.stackSamples {
		if sample.source != symptom.source {
			continue
		}
		d := distanceToWindow(sample.timeNS, symptom.startNS, symptom.endNS)
		if d <= agentTemporalWindowNS && d < distance {
			best = sample
			distance = d
			found = true
		}
	}
	return best, found
}

func (a *agentAggregator) stackMethods(fingerprint uint64) []string {
	state := a.stacks[fingerprint]
	if state == nil {
		return nil
	}
	out := make([]string, 0, len(state.methodIDs))
	for _, id := range state.methodIDs {
		if method := a.methods[id]; method != "" {
			out = append(out, method)
		}
	}
	return out
}

func insertTopInterval(items *[]AgentInterval, value AgentInterval) {
	values := *items
	index := 0
	for index < len(values) && (values[index].DurationNS > value.DurationNS ||
		(values[index].DurationNS == value.DurationNS && values[index].Sequence <= value.Sequence)) {
		index++
	}
	if len(values) == maxAgentTopIntervals && index == len(values) {
		return
	}
	if len(values) < maxAgentTopIntervals {
		values = append(values, AgentInterval{})
	}
	copy(values[index+1:], values[index:len(values)-1])
	values[index] = value
	*items = values
}
func insertRecentStack(items []agentStackSample, value agentStackSample) []agentStackSample {
	if len(items) < maxAgentTimelineItems {
		return append(items, value)
	}
	oldest := 0
	for i := 1; i < len(items); i++ {
		if items[i].timeNS < items[oldest].timeNS {
			oldest = i
		}
	}
	if value.timeNS > items[oldest].timeNS {
		items[oldest] = value
	}
	return items
}
func mergeAgentInterval(target *AgentIntervalSummary, other AgentIntervalSummary) {
	target.Count += other.Count
	target.TotalNS = saturatingAdd(target.TotalNS, other.TotalNS)
	target.MaxNS = maxUint64(target.MaxNS, other.MaxNS)
	for _, item := range other.Top {
		insertTopInterval(&target.Top, item)
	}
}
func bestOverlap(symptom agentSymptom, intervals []AgentInterval) (AgentInterval, bool) {
	var best AgentInterval
	var overlap uint64
	for _, item := range intervals {
		if item.Source != symptom.source {
			continue
		}
		current := intervalOverlapNS(symptom, item)
		if current > overlap {
			best = item
			overlap = current
		}
	}
	return best, overlap > 0
}
func intervalOverlapNS(symptom agentSymptom, interval AgentInterval) uint64 {
	end := saturatingAdd(interval.StartNS, interval.DurationNS)
	start := maxUint64(symptom.startNS, interval.StartNS)
	stop := minUint64(symptom.endNS, end)
	if stop <= start {
		return 0
	}
	return stop - start
}
func distanceToWindow(value, start, end uint64) uint64 {
	if value < start {
		return start - value
	}
	if value > end {
		return value - end
	}
	return 0
}
func symptomWeight(value agentSymptom) uint64 {
	return saturatingAdd(value.durationMS, value.jankFrames*16)
}
func symptomDescription(value agentSymptom) string {
	if value.kind == "main_thread_stall" {
		return fmt.Sprintf("Main-thread stall %d ms в %s", value.durationMS, value.context.label())
	}
	return fmt.Sprintf("UI window: %d jank frames в %s", value.jankFrames, value.context.label())
}
func qualityPenalty(summary AgentSummary) int {
	penalty := 0
	if summary.Quality.SequenceGaps > 0 {
		penalty += 15
	}
	if summary.Quality.QueueFullTotal+summary.Quality.AdmissionContentionTotal+summary.Quality.OtherNativeLossTotal > 0 {
		penalty += 15
	}
	if summary.Stacks.MissingDefinitions > 0 {
		penalty += 10
	}
	if !summary.Quality.CompatibleClockCalibration {
		penalty += 10
	}
	if summary.Capabilities.Missing != 0 {
		penalty += 10
	}
	if penalty > 45 {
		return 45
	}
	return penalty
}
func confidenceLabel(score int) string {
	if score >= 80 {
		return "HIGH"
	}
	if score >= 55 {
		return "MEDIUM"
	}
	return "LOW"
}
func capabilityNames(bits uint64) []string {
	names := []struct {
		bit  uint64
		name string
	}{{1, "GC events"}, {2, "thread lifecycle"}, {4, "monitor contention"}, {8, "stack trace"}, {16, "thread metadata"}, {32, "Linux TID"}}
	out := []string{}
	for _, item := range names {
		if bits&item.bit != 0 {
			out = append(out, item.name)
		}
	}
	return out
}
func agentPreset(value uint64) string {
	switch value {
	case 0:
		return "OFF"
	case 1:
		return "LIGHT"
	case 2:
		return "CAUSAL"
	case 3:
		return "DEEP"
	case 4:
		return "CUSTOM"
	}
	return fmt.Sprintf("profile-%d", value)
}
func agentSDKReason(value uint64) string {
	reasons := []string{"attach_requested", "api_unsupported", "app_not_debuggable", "config_invalid", "library_or_abi_missing", "native_initialize_failed", "attach_succeeded", "precondition_failed", "config_runtime_failed", "native_bridge_failed", "native_handshake_failed", "attach_failed", "activation_failed", "metadata_refresh_failed", "drain_terminated", "capability_degraded", "thread_metadata_degraded", "drain_failed", "decode_failed", "correlation_drop", "context_table_drop", "stack_budget_drop", "stack_capture_failed", "canonical_batch_drop", "event_sink_drop", "method_resolution_drop", "final_drain_failed", "final_drain_truncated", "shutdown_timeout", "native_shutdown_incomplete", "stopped"}
	if value < uint64(len(reasons)) {
		return reasons[value]
	}
	return fmt.Sprintf("sdk_reason_%d", value)
}
func containsImageDecode(methods []string) bool {
	for _, method := range methods {
		lower := strings.ToLower(method)
		if strings.Contains(lower, "bitmapfactory") || strings.Contains(lower, "decode") && strings.Contains(lower, "image") {
			return true
		}
	}
	return false
}
func firstInterestingMethod(methods []string) string {
	for _, method := range methods {
		if containsImageDecode([]string{method}) {
			return method
		}
	}
	if len(methods) > 0 {
		return methods[0]
	}
	return ""
}
func firstNonZero(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
func saturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}
func absInt64(value int64) int64 {
	if value == -1<<63 {
		return 1<<63 - 1
	}
	if value < 0 {
		return -value
	}
	return value
}
func int64Distance(left, right int64) uint64 {
	if left > right {
		left, right = right, left
	}
	if left < 0 && right >= 0 {
		return saturatingAdd(uint64(-(left+1))+1, uint64(right))
	}
	return uint64(right - left)
}
func signedDifference(left, right uint64) int64 {
	const maxSigned = uint64(1<<63 - 1)
	if left >= right {
		delta := left - right
		if delta > maxSigned {
			return int64(maxSigned)
		}
		return int64(delta)
	}
	delta := right - left
	if delta > maxSigned {
		return -int64(maxSigned)
	}
	return -int64(delta)
}
func saturatingMultiply(value, multiplier uint64) uint64 {
	if multiplier != 0 && value > ^uint64(0)/multiplier {
		return ^uint64(0)
	}
	return value * multiplier
}
func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
