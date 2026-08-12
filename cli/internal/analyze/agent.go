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
	timeNS, fingerprint, thread, sequence uint64
	source                                string
}

type agentSymptom struct {
	startNS, endNS         uint64
	durationMS, jankFrames uint64
	kind, source           string
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
			a.methods[payload.Payload0] = formatJVMMethodSymbol(name)
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
		sequence: payload.ProducerSequence, source: firstNonEmpty(event.Source, "unknown")}
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

func (a *agentAggregator) addStallSymptom(event jhlog.Event, flow FlowStats) {
	endNS := saturatingMultiply(event.TimeMS, 1_000_000)
	durationNS := saturatingMultiply(event.Stall.DurationMS, 1_000_000)
	startNS := uint64(0)
	if endNS >= durationNS {
		startNS = endNS - durationNS
	}
	a.addSymptom(agentSymptom{startNS: startNS, endNS: endNS, durationMS: event.Stall.DurationMS,
		kind: "main_thread_stall", source: firstNonEmpty(event.Source, "unknown"),
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
		"Совпадение сборки мусора или ожидания блокировки с задержкой измеряет временное перекрытие, но само по себе не доказывает причинность.",
		"Снимок стека показывает только один момент; отсутствие метода в снимке не доказывает, что он не выполнялся.",
		"Идентификаторы потоков, методов и стеков действуют только внутри одного запуска процесса и не подходят для прямого сравнения между запусками.",
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
	var gaps []string
	if result.Quality.SequenceGaps > 0 {
		gaps = append(gaps, fmt.Sprintf("В последовательности событий агента отсутствует %d событий.", result.Quality.SequenceGaps))
	}
	if result.Quality.QueueFullTotal > 0 {
		gaps = append(gaps, fmt.Sprintf("Очередь агента была заполнена %d раз.", result.Quality.QueueFullTotal))
	}
	if result.Quality.AdmissionContentionTotal > 0 {
		gaps = append(gaps, fmt.Sprintf("Запись события была отклонена из-за конкуренции %d раз.", result.Quality.AdmissionContentionTotal))
	}
	if result.Quality.OtherNativeLossTotal > 0 {
		gaps = append(gaps, fmt.Sprintf("Других потерь внутри агента: %d.", result.Quality.OtherNativeLossTotal))
	}
	if result.Stacks.MissingDefinitions > 0 {
		gaps = append(gaps, fmt.Sprintf("Для %d снимков стека отсутствует полное описание.", result.Stacks.MissingDefinitions))
	}
	if len(a.capabilityVariants) > 1 {
		gaps = append(gaps, "В объединённых потоках данных различается набор возможностей агента; причинные показатели не полностью сопоставимы.")
	}
	if a.sourceCapacityLost {
		gaps = append(gaps, "Индекс источников и последовательностей агента достиг лимита; часть сведений о полноте собрана приближённо.")
	}
	if !result.Quality.CompatibleClockCalibration && result.EventCount > 0 {
		gaps = append(gaps, "Не удалось согласовать часы всех потоков данных; временное сопоставление между ними отключено.")
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
func capabilityNames(bits uint64) []string {
	names := []struct {
		bit  uint64
		name string
	}{{1, "сборка мусора"}, {2, "жизненный цикл потоков"}, {4, "ожидание блокировок"}, {8, "снимки стека"}, {16, "сведения о потоках"}, {32, "системные идентификаторы потоков"}}
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
