package analyze

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	maxCausalEntities       = 20_000
	maxCausalEvents         = 100_000
	maxCausalRetainedByte   = 64 << 20
	maxCausalNodeEvents     = 32
	maxCausalScopes         = 1_024
	maxCausalSegmentKeys    = 100_000
	causalNodeRetainedBytes = 1024
	mainBlockingLimitNS     = 100_000_000
	longTaskReviewLimitNS   = 1_000_000_000
	queueWaitReviewNS       = 250_000_000
	scheduleLateReviewNS    = 250_000_000
)

type CausalScope struct {
	RunID, ProcessID, SessionID jhlog.ID128
}

func causalScopeOf(header jhlog.SegmentHeader) CausalScope {
	return CausalScope{header.RunID, header.ProcessInstanceID, header.SessionID}
}

type CausalKey struct {
	Scope CausalScope
	Kind  jhlog.CausalEntityKind
	ID    uint64
}

type CausalEventView struct {
	Phase               uint64 `json:"phase"`
	ObservedNS          uint64 `json:"observed_ns,string"`
	RelativeNS          uint64 `json:"relative_ns,string"`
	Source              string `json:"Source,omitempty"`
	AppCaller           string `json:"AppCaller,omitempty"`
	SourceOrigin        string `json:"SourceOrigin,omitempty"`
	SourceLine          uint64 `json:"SourceLine,omitempty"`
	AppCallerLine       uint64 `json:"AppCallerLine,omitempty"`
	ParentKind          uint64 `json:"ParentKind,omitempty"`
	ParentID            uint64 `json:"ParentID,string,omitempty"`
	SchedulerKind       uint64 `json:"SchedulerKind,omitempty"`
	SchedulerID         uint64 `json:"SchedulerID,string,omitempty"`
	ActorKind           uint64 `json:"ActorKind,omitempty"`
	ActorID             uint64 `json:"ActorID,string,omitempty"`
	ResourceKind        uint64 `json:"ResourceKind,omitempty"`
	OperationKind       uint64 `json:"OperationKind,omitempty"`
	CallResult          uint64 `json:"CallResult,omitempty"`
	TerminalOutcome     uint64 `json:"TerminalOutcome,omitempty"`
	LeaseScopeKind      uint64 `json:"LeaseScopeKind,omitempty"`
	LeaseScopeID        uint64 `json:"LeaseScopeID,string,omitempty"`
	BoundToScope        uint64 `json:"BoundToScope,omitempty"`
	WorkerInstanceID    uint64 `json:"WorkerInstanceID,string,omitempty"`
	RunAttempt          uint64 `json:"RunAttempt,omitempty"`
	Generation          uint64 `json:"Generation,omitempty"`
	CoroutineToken      uint64 `json:"CoroutineToken,string,omitempty"`
	CoroutineGeneration uint64 `json:"CoroutineGeneration,omitempty"`
	ScheduledForNS      uint64 `json:"ScheduledForNS,string,omitempty"`
	QueueEnteredNS      uint64 `json:"QueueEnteredNS,string,omitempty"`
	RequestedPeriodNS   uint64 `json:"RequestedPeriodNS,omitempty"`
	RequestedDelayNS    uint64 `json:"RequestedDelayNS,omitempty"`
	ScheduleCadence     uint64 `json:"ScheduleCadence,omitempty"`
	Occurrence          uint64 `json:"Occurrence,omitempty"`
	TargetTID           uint64 `json:"target_tid,omitempty"`
	TargetJavaID        uint64 `json:"TargetJavaID,string,omitempty"`
	LeaseID             uint64 `json:"LeaseID,string,omitempty"`
	DurationNS          uint64 `json:"duration_ns,string,omitempty"`
	CPUNS               uint64 `json:"cpu_ns,string,omitempty"`
	ReadBytes           uint64 `json:"read_bytes,string,omitempty"`
	WriteBytes          uint64 `json:"write_bytes,string,omitempty"`
	KnownFields         uint64 `json:"known_fields"`
}

type CausalEntityView struct {
	Scope, Process, Source, AppCaller, Responsibility  string
	Anchor, ParentAnchor, SchedulerAnchor, ActorAnchor string
	Kind                                               uint64
	ID                                                 uint64 `json:"ID,string"`
	FirstNS                                            uint64 `json:"FirstNS,string"`
	LastNS                                             uint64 `json:"LastNS,string"`
	ObservedSpanNS                                     uint64 `json:"ObservedSpanNS,string"`
	LifecycleNote                                      string
	SourceLine, AppCallerLine                          uint64
	ParentKind                                         uint64
	ParentID                                           uint64 `json:"ParentID,string,omitempty"`
	SchedulerKind                                      uint64
	SchedulerID                                        uint64 `json:"SchedulerID,string,omitempty"`
	ActorKind                                          uint64
	ActorID                                            uint64 `json:"ActorID,string,omitempty"`
	Events                                             []CausalEventView
	OmittedEvents                                      uint64
	DirectOrigin                                       string
	Proof                                              string
	AppCallerEvidence                                  string
	IsJankHunter                                       bool
	WorkerAttemptLinked                                bool
	WorkerUUID, WorkerOutcome, WorkerCoverage          string
	ParentAmbiguous                                    bool
}

type CausalFinding struct {
	Level, Rule, Title, Consequence, Source, Responsibility, Confidence, Coverage, NextAction string
	Anchor                                                                                    string
	SourceLine, Count, MaxDurationMS                                                          uint64
	Proof                                                                                     []string
}

type CausalLossReason struct {
	Reason string
	Count  uint64
}

type CausalAnalysis struct {
	Available, Complete                                                                                           bool
	Events, Entities, AppEntities, AppInitiated, LibraryInitiated, UnknownOriginInitiated, JHExcluded, DroppedCLI uint64
	ObservedThreadGenerations, RootRunObservedThreadGenerations                                                   uint64
	TransportLoss, RegistryLoss, StaleEpoch, BindingMiss                                                          uint64
	UnknownEntities                                                                                               uint64
	Metrics                                                                                                       CausalMetrics
	CreatorGroups                                                                                                 []CausalCreatorGroup
	CreatorGroupsOmitted                                                                                          uint64
	TransportLossByReason                                                                                         []CausalLossReason
	Coverage                                                                                                      string
	Findings                                                                                                      []CausalFinding
	Observations                                                                                                  []CausalFinding
	FindingGroups, FindingsOmitted, ObservationsOmitted                                                           uint64
	EntitiesForReview                                                                                             []CausalEntityView
	ReviewOmitted                                                                                                 uint64
	AllEntities                                                                                                   []CausalEntityView `json:"-"`
}

type causalScopeState struct {
	process         string
	pid             uint64
	diagnostics     bool
	detailed        bool
	firstSegment    bool
	allSealed       bool
	segments        int
	quality         map[uint64]uint64
	segmentSeen     map[uint64]bool
	maxSegment      uint64
	segmentGap      bool
	qualitySegments int
	hookFailures    uint64
}

type causalNode struct {
	key                      CausalKey
	process                  string
	first, last              uint64
	directSource             string
	directLine               uint64
	directOrigin             jhlog.CausalSourceOrigin
	appCaller                string
	appCallerLine            uint64
	appAction                string
	appActionLine            uint64
	appActionPhase           uint64
	parent, scheduler, actor CausalKey
	parentConflict           bool
	events                   []jhlog.CausalEvent
	omittedEvents            uint64
	scopeEnded               bool
	terminalObserved         bool
	cancellationObserved     bool
	scopeEndAt               uint64
	hasThreadRun             bool
	metrics                  causalNodeMetrics
	runStartTID              uint64
	runStartAt               uint64
	runStartSeen             bool
	runOpen, runAmbiguous    bool
}

type causalCollectorState struct {
	current       CausalScope
	nodes         map[CausalKey]*causalNode
	scopes        map[CausalScope]*causalScopeState
	events        uint64
	dropped       uint64
	retainedBytes int
	cpuIntervals  []causalCPUInterval
	segmentKeys   int
}

func (s *causalCollectorState) startSegment(header jhlog.SegmentHeader) {
	s.current = causalScopeOf(header)
	if s.scopes == nil {
		s.scopes = make(map[CausalScope]*causalScopeState)
	}
	q := s.scopes[s.current]
	if q == nil {
		if len(s.scopes) >= maxCausalScopes {
			s.dropped++
			return
		}
		q = &causalScopeState{process: header.ProcessName, pid: header.OSPID, allSealed: true, quality: make(map[uint64]uint64), segmentSeen: make(map[uint64]bool)}
		s.scopes[s.current] = q
	}
}

func (s *causalCollectorState) session(flags uint64) {
	q := s.scopes[s.current]
	if q == nil {
		return
	}
	q.diagnostics = q.diagnostics || flags&uint64(jhlog.CollectorConcurrencyDiagnostics) != 0
	q.detailed = q.detailed || flags&uint64(jhlog.CollectorConcurrencyDetailed) != 0
}

func (s *causalCollectorState) segmentResult(result jhlog.StreamResult) {
	key := causalScopeOf(result.Header)
	q := s.scopes[key]
	if q == nil {
		return
	}
	q.segments++
	if q.segmentSeen[result.Header.SegmentIndex] {
		q.segmentGap = true
	} else if len(q.segmentSeen) >= 10_000 || s.segmentKeys >= maxCausalSegmentKeys {
		q.segmentGap = true
		s.dropped++
	} else {
		q.segmentSeen[result.Header.SegmentIndex] = true
		s.segmentKeys++
		if result.Header.SegmentIndex > q.maxSegment {
			q.maxSegment = result.Header.SegmentIndex
		}
	}
	q.firstSegment = q.firstSegment || result.Header.SegmentIndex == 0
	q.allSealed = q.allSealed && result.Sealed && result.Status == jhlog.SegmentStatusClosedClean && result.TailBytes == 0
	if result.LatestQuality != nil {
		q.qualitySegments++
		for id, value := range result.LatestQuality.Counters {
			if value > q.quality[id] {
				q.quality[id] = value
			}
		}
		_, q.hookFailures, _ = runtimeHookFailureDetails(q.quality)
	}
}

func (s *causalCollectorState) record(event *jhlog.CausalEvent) {
	if event == nil {
		return
	}
	if s.scopes[s.current] == nil {
		s.dropped++
		return
	}
	if s.nodes == nil {
		s.nodes = make(map[CausalKey]*causalNode)
	}
	key := CausalKey{Scope: s.current, Kind: event.EntityKind, ID: event.EntityID}
	node := s.nodes[key]
	if node == nil {
		if len(s.nodes) >= maxCausalEntities || s.retainedBytes+causalNodeRetainedBytes > maxCausalRetainedByte {
			s.dropped++
			return
		}
		q := s.scopes[s.current]
		process := ""
		if q != nil {
			process = q.process
		}
		node = &causalNode{key: key, process: process, first: event.ObservedElapsedNanos, last: event.ObservedElapsedNanos}
		s.nodes[key] = node
		s.retainedBytes += causalNodeRetainedBytes
	}
	if s.events >= maxCausalEvents || s.retainedBytes+400+len(event.SourceMethod)+len(event.AppCallerMethod) > maxCausalRetainedByte {
		s.dropped++
		return
	}
	s.events++
	s.observeCausalMetric(node, event)
	if event.ObservedElapsedNanos < node.first {
		node.first = event.ObservedElapsedNanos
	}
	if event.ObservedElapsedNanos > node.last {
		node.last = event.ObservedElapsedNanos
	}
	if event.Presence&jhlog.CausalHasSource != 0 && (node.directSource == "" || node.directOrigin == jhlog.CausalOriginUnknown && event.SourceOrigin != jhlog.CausalOriginUnknown) {
		node.directSource, node.directLine, node.directOrigin = event.SourceMethod, event.SourceLine, event.SourceOrigin
	}
	if event.Presence&jhlog.CausalHasAppCaller != 0 && node.appCaller == "" {
		node.appCaller, node.appCallerLine = event.AppCallerMethod, event.AppCallerLine
	}
	if event.SourceOrigin == jhlog.CausalOriginProject && !causalInternalJHSource(event.SourceMethod) &&
		node.appAction == "" && causalInitiationPhase(event.EntityKind, event.Phase) {
		node.appAction, node.appActionLine, node.appActionPhase = event.SourceMethod, event.SourceLine, event.Phase
	}
	if event.ParentID != 0 {
		parent := CausalKey{Scope: s.current, Kind: event.ParentKind, ID: event.ParentID}
		if node.parent.ID == 0 {
			node.parent = parent
		} else if node.parent != parent {
			node.parentConflict = true
		}
	}
	if event.SchedulerID != 0 && node.scheduler.ID == 0 {
		node.scheduler = CausalKey{Scope: s.current, Kind: event.SchedulerKind, ID: event.SchedulerID}
	}
	if event.ActorID != 0 && node.actor.ID == 0 {
		node.actor = CausalKey{Scope: s.current, Kind: event.ActorKind, ID: event.ActorID}
	}
	if event.EntityKind == jhlog.CausalThread && event.Phase == 6 {
		node.hasThreadRun = true
	}
	if causalScopeEnded(event.EntityKind, event.Phase) {
		node.scopeEnded = true
		node.scopeEndAt = event.ObservedElapsedNanos
	}
	// Lifecycle facts outlive the bounded list used to render individual stages.
	node.terminalObserved = node.terminalObserved || causalTerminal(event.EntityKind, event.Phase)
	node.cancellationObserved = node.cancellationObserved || event.EntityKind == jhlog.CausalCoroutine && event.Phase == 4
	if len(node.events) < maxCausalNodeEvents {
		node.events = append(node.events, *event)
		s.retainedBytes += 400 + len(event.SourceMethod) + len(event.AppCallerMethod)
	} else {
		node.omittedEvents++
	}
}

func causalTerminal(kind jhlog.CausalEntityKind, phase uint64) bool {
	switch kind {
	case jhlog.CausalThread:
		return phase == 11
	case jhlog.CausalExecutorTask:
		return phase == 5 || phase == 8
	case jhlog.CausalHandlerPost:
		return phase == 4
	case jhlog.CausalCoroutine, jhlog.CausalExecutor:
		return phase == 5
	case jhlog.CausalWorkerAttempt:
		return phase == 5
	case jhlog.CausalSeries:
		return phase == 2
	case jhlog.CausalDirectExecution:
		return phase == 2
	default:
		return false
	}
}

func causalScopeEnded(kind jhlog.CausalEntityKind, phase uint64) bool {
	if kind == jhlog.CausalThread && phase == 7 || kind == jhlog.CausalWorkerAttempt && phase == 3 {
		return true
	}
	return causalTerminal(kind, phase)
}

func causalInitiationPhase(kind jhlog.CausalEntityKind, phase uint64) bool {
	switch kind {
	case jhlog.CausalThread:
		return phase <= 3
	case jhlog.CausalExecutorTask, jhlog.CausalHandlerPost, jhlog.CausalSeries:
		return phase == 1
	case jhlog.CausalCoroutine:
		return phase == 1
	case jhlog.CausalWorkerAttempt:
		return phase == 1
	case jhlog.CausalResource:
		return phase == 1 || phase == 2 || phase == 7
	case jhlog.CausalExecutor:
		return phase == 1 || phase == 2
	case jhlog.CausalDirectExecution:
		return phase == 1
	default:
		return false
	}
}

func causalLoss(q *causalScopeState) (transport, registry, stale, binding uint64) {
	if q == nil {
		return
	}
	for reason := jhlog.QualityLossQueueFull; reason <= jhlog.QualityLossStorageBudget; reason++ {
		transport += q.quality[jhlog.EventQualityCounterID(jhlog.EventCausalEvent, reason)]
	}
	return transport, q.quality[jhlog.QualityCausalRegistryCapacity], q.quality[jhlog.QualityCausalStaleEpoch], q.quality[jhlog.QualityCausalBindingMiss]
}

func (s *causalCollectorState) scopeComplete(key CausalScope) bool {
	q := s.scopes[key]
	if q == nil || !q.diagnostics || !q.firstSegment || !q.allSealed || q.qualitySegments != q.segments || q.segments == 0 ||
		q.segmentGap || uint64(len(q.segmentSeen)) != q.maxSegment+1 {
		return false
	}
	t, r, stale, binding := causalLoss(q)
	return t == 0 && r == 0 && stale == 0 && binding == 0 && q.hookFailures == 0 && s.dropped == 0
}

func (s *causalCollectorState) appAttribution(n *causalNode) (responsibility, appCaller string, appLine uint64, evidence string) {
	if n == nil {
		return "unknown", "", 0, ""
	}
	if n.key.Kind == jhlog.CausalWorkerAttempt && n.appCaller != "" {
		responsibility = causalInitiatedResponsibility(n)
		if n.directOrigin == jhlog.CausalOriginProject && !causalInternalJHSource(n.directSource) {
			responsibility = "app_direct"
		}
		return responsibility, n.appCaller, n.appCallerLine,
			"наблюдавшийся app caller; вызов enqueue не доказывает регистрацию или принятие новой работы"
	}
	if n.directOrigin == jhlog.CausalOriginProject && !causalInternalJHSource(n.directSource) {
		return "app_direct", n.directSource, n.directLine, "непосредственный PROJECT site"
	}
	if n.appCaller != "" {
		return causalInitiatedResponsibility(n), n.appCaller, n.appCallerLine, "явный appCaller"
	}
	if source, line := causalWorkerImplementation(n); source != "" {
		return "app_direct", source, line, "наблюдавшийся PROJECT метод Worker; постановка может быть неизвестна"
	}
	if n.appAction != "" {
		return causalInitiatedResponsibility(n), n.appAction, n.appActionLine, fmt.Sprintf("точный PROJECT site этапа %d", n.appActionPhase)
	}
	seen := map[CausalKey]bool{n.key: true}
	if n.parentConflict {
		return "unknown", "", 0, "противоречивые parent ID"
	}
	parent := n.parent
	for depth := 0; depth < 16 && parent.ID != 0; depth++ {
		if seen[parent] {
			break
		}
		seen[parent] = true
		ancestor := s.nodes[parent]
		if ancestor == nil {
			break
		}
		// An internal telemetry boundary is not evidence that its unclassified
		// child is app work, even if JH itself was called from app code.
		if causalInternalJHSource(ancestor.directSource) {
			break
		}
		if ancestor.directOrigin == jhlog.CausalOriginProject {
			return causalInitiatedResponsibility(n), ancestor.directSource, ancestor.directLine, "точная цепочка parent ID"
		}
		if ancestor.appCaller != "" {
			return causalInitiatedResponsibility(n), ancestor.appCaller, ancestor.appCallerLine, "точная цепочка parent ID → appCaller"
		}
		if ancestor.appAction != "" {
			return causalInitiatedResponsibility(n), ancestor.appAction, ancestor.appActionLine, "точная цепочка parent ID → PROJECT action"
		}
		if ancestor.parentConflict {
			break
		}
		parent = ancestor.parent
	}
	return "unknown", "", 0, ""
}

func causalInitiatedResponsibility(n *causalNode) string {
	if n.directOrigin == jhlog.CausalOriginDependency && !causalInternalJHSource(n.directSource) {
		return "library_initiated_by_app"
	}
	return "app_initiated_origin_unknown"
}

func causalInternalJHSource(source string) bool {
	// The sample app also uses io.jankhunter.*, while internal adapters can be
	// compiled as PROJECT inputs. Use only reserved implementation namespaces.
	for _, prefix := range [...]string{
		"io.jankhunter.annotations.", "io.jankhunter.buildlogic.", "io.jankhunter.gradle.",
		"io.jankhunter.okhttp3.", "io.jankhunter.runtime.", "io.jankhunter.sql.",
		"io.jankhunter.workmanager.",
	} {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

func causalWorkerImplementation(n *causalNode) (string, uint64) {
	if n == nil || n.key.Kind != jhlog.CausalWorkerAttempt {
		return "", 0
	}
	for _, event := range n.events {
		if event.Phase == 6 && event.SourceOrigin == jhlog.CausalOriginProject &&
			event.SourceMethod != "" && !causalInternalJHSource(event.SourceMethod) {
			return event.SourceMethod, event.SourceLine
		}
	}
	return "", 0
}

func causalIsJH(n *causalNode) bool {
	if n == nil || !causalInternalJHSource(n.directSource) {
		return false
	}
	if n.key.Kind == jhlog.CausalWorkerAttempt && n.appCaller != "" && !causalInternalJHSource(n.appCaller) {
		return false
	}
	implementation, _ := causalWorkerImplementation(n)
	return implementation == ""
}

func causalScopeLabel(key CausalScope) string {
	return hex.EncodeToString(key.RunID[:]) + "/" + hex.EncodeToString(key.ProcessID[:]) + "/" + hex.EncodeToString(key.SessionID[:])
}

func causalProof(key CausalKey, phase uint64) string {
	return fmt.Sprintf("%s · kind %d · ID %d · этап %d", causalScopeLabel(key.Scope), key.Kind, key.ID, phase)
}

func causalAnchor(key CausalKey) string {
	return fmt.Sprintf("causal-%x-%x-%x-%d-%d", key.Scope.RunID, key.Scope.ProcessID, key.Scope.SessionID, key.Kind, key.ID)
}

func (s *causalCollectorState) finalize() *CausalAnalysis {
	if len(s.nodes) == 0 && len(s.scopes) == 0 {
		return nil
	}
	if len(s.nodes) == 0 {
		diagnosticsEnabled := false
		for _, q := range s.scopes {
			diagnosticsEnabled = diagnosticsEnabled || q.diagnostics
		}
		if !diagnosticsEnabled {
			return nil
		}
	}
	a := &CausalAnalysis{Available: len(s.nodes) > 0, Events: s.events, Entities: uint64(len(s.nodes)), DroppedCLI: s.dropped, Complete: s.dropped == 0}
	for _, node := range s.nodes {
		if node.parentConflict {
			a.Complete = false
			a.BindingMiss++
		}
	}
	var hookFailures uint64
	for key, q := range s.scopes {
		hookFailures = causalSaturatingAdd(hookFailures, q.hookFailures)
		if !q.diagnostics || !s.scopeComplete(key) {
			a.Complete = false
		}
		t, r, stale, binding := causalLoss(q)
		a.TransportLoss += t
		a.RegistryLoss += r
		a.StaleEpoch += stale
		a.BindingMiss += binding
	}
	for reason := jhlog.QualityLossQueueFull; reason <= jhlog.QualityLossStorageBudget; reason++ {
		var count uint64
		for _, q := range s.scopes {
			count += q.quality[jhlog.EventQualityCounterID(jhlog.EventCausalEvent, reason)]
		}
		if count != 0 {
			a.TransportLossByReason = append(a.TransportLossByReason, CausalLossReason{Reason: causalLossReasonLabel(reason), Count: count})
		}
	}
	if !a.Available {
		a.Coverage = "Причинные события недоступны: журнал старого формата, сбор выключен или события не записаны; отсутствие проблем не доказано."
		return a
	}
	if a.Complete {
		a.Coverage = "Причинные события записаны без известных потерь в закрытых сессиях; полнота инструментации ограничена поддержанными API."
	} else {
		a.Coverage = "Причинные события частичны: есть незакрытое окно, выключенный сбор, ошибки сбора или потери. Отсутствие этапа и release не доказывает отсутствие действия."
		if hookFailures != 0 {
			a.Coverage += fmt.Sprintf(" Внутренних ошибок хуков с возможной потерей данных: %d.", hookFailures)
		}
	}
	keys := make([]CausalKey, 0, len(s.nodes))
	for key := range s.nodes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Scope != keys[j].Scope {
			return causalScopeLabel(keys[i].Scope) < causalScopeLabel(keys[j].Scope)
		}
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		return keys[i].ID < keys[j].ID
	})
	candidates := make([]CausalKey, 0, len(keys))
	anchorKeys := make(map[string]CausalKey, len(keys))
	for _, key := range keys {
		n := s.nodes[key]
		if causalIsJH(n) {
			a.JHExcluded++
			continue
		}
		responsibility, appCaller, appLine, attributionProof := s.appAttribution(n)
		switch responsibility {
		case "app_direct":
			a.AppEntities++
		case "library_initiated_by_app":
			a.AppInitiated++
			a.LibraryInitiated++
		case "app_initiated_origin_unknown":
			a.AppInitiated++
			a.UnknownOriginInitiated++
		}
		if n.key.Kind == jhlog.CausalThread {
			a.ObservedThreadGenerations++
			if n.hasThreadRun {
				a.RootRunObservedThreadGenerations++
			}
		}
		if responsibility == "unknown" {
			a.UnknownEntities++
			continue
		}
		candidates = append(candidates, key)
		anchorKeys[causalAnchor(key)] = key
		s.evaluateNode(a, n, responsibility, appCaller, appLine, attributionProof)
	}
	s.finishCausalMetrics(a, keys)
	sort.SliceStable(a.Findings, func(i, j int) bool {
		if a.Findings[i].MaxDurationMS != a.Findings[j].MaxDurationMS {
			return a.Findings[i].MaxDurationMS > a.Findings[j].MaxDurationMS
		}
		return a.Findings[i].Source < a.Findings[j].Source
	})
	a.Findings = coalesceCausalFindings(a.Findings)
	a.Observations = coalesceCausalFindings(a.Observations)
	a.FindingGroups = uint64(len(a.Findings))
	sort.SliceStable(a.Observations, func(i, j int) bool {
		return a.Observations[i].MaxDurationMS > a.Observations[j].MaxDurationMS
	})
	if len(a.Findings) > 20 {
		a.FindingsOmitted = uint64(len(a.Findings) - 20)
		a.Findings = a.Findings[:20]
	}
	if len(a.Observations) > 20 {
		a.ObservationsOmitted = uint64(len(a.Observations) - 20)
		a.Observations = a.Observations[:20]
	}
	selected := make(map[CausalKey]bool, 100)
	ordered := make([]CausalKey, 0, 100)
	var addChain func(CausalKey, int)
	addChain = func(key CausalKey, depth int) {
		if len(ordered) >= 100 || selected[key] || depth > 8 {
			return
		}
		node := s.nodes[key]
		if node == nil || causalIsJH(node) {
			return
		}
		selected[key] = true
		ordered = append(ordered, key)
		addChain(node.parent, depth+1)
		addChain(node.scheduler, depth+1)
		addChain(node.actor, depth+1)
	}
	priority := append(append([]CausalFinding(nil), a.Findings...), a.Observations...)
	for _, finding := range priority {
		key := anchorKeys[finding.Anchor]
		if key.ID != 0 && !selected[key] && len(ordered) < 100 {
			selected[key] = true
			ordered = append(ordered, key)
		}
	}
	for _, finding := range priority {
		key := anchorKeys[finding.Anchor]
		if node := s.nodes[key]; node != nil {
			addChain(node.parent, 1)
			addChain(node.scheduler, 1)
			addChain(node.actor, 1)
		}
	}
	for _, key := range candidates {
		if len(ordered) >= 100 {
			break
		}
		addChain(key, 0)
	}
	for _, key := range ordered {
		responsibility, appCaller, appLine, evidence := s.appAttribution(s.nodes[key])
		a.EntitiesForReview = append(a.EntitiesForReview, s.entityView(s.nodes[key], responsibility, appCaller, appLine, evidence, selected))
	}
	for _, key := range candidates {
		if !selected[key] {
			a.ReviewOmitted++
		}
	}
	allKeys := append([]CausalKey(nil), ordered...)
	allSelected := make(map[CausalKey]bool, len(keys))
	for _, key := range keys {
		allSelected[key] = true
		if !selected[key] {
			allKeys = append(allKeys, key)
		}
	}
	for _, key := range allKeys {
		node := s.nodes[key]
		responsibility, appCaller, appLine, evidence := s.appAttribution(node)
		view := s.entityView(node, responsibility, appCaller, appLine, evidence, allSelected)
		view.IsJankHunter = causalIsJH(node)
		a.AllEntities = append(a.AllEntities, view)
	}
	return a
}

func causalLossReasonLabel(reason jhlog.QualityLossReason) string {
	switch reason {
	case jhlog.QualityLossQueueFull:
		return "очередь заполнена"
	case jhlog.QualityLossNotAccepting:
		return "запись не принимает события"
	case jhlog.QualityLossIOLost:
		return "ошибка записи"
	case jhlog.QualityLossOversized:
		return "слишком большая запись"
	case jhlog.QualityLossSizeLimit:
		return "лимит размера"
	case jhlog.QualityLossAdmissionContention:
		return "конкуренция при приёме"
	case jhlog.QualityLossStorageBudget:
		return "лимит хранилища"
	default:
		return "другая причина"
	}
}

func (s *causalCollectorState) entityView(n *causalNode, responsibility, appCaller string, appLine uint64, evidence string, selected map[CausalKey]bool) CausalEntityView {
	v := CausalEntityView{Scope: causalScopeLabel(n.key.Scope), Process: n.process, Kind: uint64(n.key.Kind), ID: n.key.ID,
		Anchor:  causalAnchor(n.key),
		FirstNS: n.first, LastNS: n.last, Source: n.directSource, SourceLine: n.directLine,
		AppCaller: appCaller, AppCallerLine: appLine, AppCallerEvidence: evidence, Responsibility: responsibility,
		OmittedEvents: n.omittedEvents, ParentAmbiguous: n.parentConflict,
		Proof: causalProof(n.key, 0)}
	if n.last >= n.first {
		v.ObservedSpanNS = n.last - n.first
	}
	v.LifecycleNote = causalLifecycleNote(n)
	if _, ok := s.nodes[n.parent]; ok && n.parent.ID != 0 && !n.parentConflict {
		v.ParentKind, v.ParentID = uint64(n.parent.Kind), n.parent.ID
		if selected[n.parent] {
			v.ParentAnchor = causalAnchor(n.parent)
		}
	}
	if _, ok := s.nodes[n.scheduler]; ok && n.scheduler.ID != 0 {
		v.SchedulerKind, v.SchedulerID = uint64(n.scheduler.Kind), n.scheduler.ID
		if selected[n.scheduler] {
			v.SchedulerAnchor = causalAnchor(n.scheduler)
		}
	}
	if _, ok := s.nodes[n.actor]; ok && n.actor.ID != 0 {
		v.ActorKind, v.ActorID = uint64(n.actor.Kind), n.actor.ID
		if selected[n.actor] {
			v.ActorAnchor = causalAnchor(n.actor)
		}
	}
	v.DirectOrigin = causalOriginLabel(n.directOrigin)
	for _, event := range n.events {
		v.Events = append(v.Events, CausalEventView{Phase: event.Phase, ObservedNS: event.ObservedElapsedNanos,
			RelativeNS: event.ObservedElapsedNanos - n.first,
			Source:     event.SourceMethod, SourceLine: event.SourceLine, SourceOrigin: causalOriginLabel(event.SourceOrigin),
			AppCaller: event.AppCallerMethod, AppCallerLine: event.AppCallerLine,
			ParentKind: uint64(event.ParentKind), ParentID: event.ParentID,
			SchedulerKind: uint64(event.SchedulerKind), SchedulerID: event.SchedulerID,
			ActorKind: uint64(event.ActorKind), ActorID: event.ActorID,
			ResourceKind: event.ResourceKind, OperationKind: event.OperationKind,
			TargetJavaID: event.TargetJavaID, LeaseID: event.LeaseID,
			CallResult: event.CallResult, TerminalOutcome: event.TerminalOutcome,
			LeaseScopeKind: uint64(event.LeaseScopeKind), LeaseScopeID: event.LeaseScopeID, BoundToScope: event.BoundToScope,
			WorkerInstanceID: event.WorkerInstanceID, RunAttempt: event.RunAttempt, Generation: event.Generation,
			CoroutineToken: event.CoroutineToken, CoroutineGeneration: event.CoroutineGeneration,
			ScheduledForNS: event.ScheduledForNanos, QueueEnteredNS: event.QueueEnteredNanos,
			RequestedPeriodNS: event.RequestedPeriodNanos, RequestedDelayNS: event.RequestedDelayNanos,
			ScheduleCadence: event.ScheduleCadence, Occurrence: event.Occurrence,
			TargetTID: event.TargetTID, DurationNS: event.DurationNanos, CPUNS: event.CPUNanos,
			ReadBytes: event.ReadBytes, WriteBytes: event.WrittenBytes, KnownFields: event.Presence})
	}
	return v
}

// This summary uses facts observed across all events, not only the first retained
// stages. A terminal fact must not disappear when a long-lived coroutine fills
// its detail budget, and cancellation alone must never imply termination.
func causalLifecycleNote(n *causalNode) string {
	switch {
	case n.terminalObserved && n.key.Kind == jhlog.CausalThread:
		return "завершение потока наблюдалось; точный системный момент неизвестен"
	case n.terminalObserved:
		return "завершение работы наблюдалось"
	case n.key.Kind == jhlog.CausalThread && n.scopeEnded:
		return "корневой run завершился; смерть Java/OS потока ещё не подтверждена"
	case n.key.Kind == jhlog.CausalWorkerAttempt && n.scopeEnded:
		return "попытка вернула retry; итог WorkManager ещё неизвестен"
	case n.key.Kind == jhlog.CausalResource && n.metrics.resourcesReleased != 0:
		return "release наблюдался"
	case n.key.Kind == jhlog.CausalResource && n.metrics.resourcesObservedUse != 0:
		return "ресурс передан в use; первоначальное приобретение и открытость неизвестны"
	case n.cancellationObserved:
		return "вызов отмены наблюдался; без подтверждённого callback терминальный исход и остановка выполнения неизвестны"
	case n.key.Kind == jhlog.CausalCoroutine && n.metrics.coroutineObservedMachine && !n.metrics.coroutineStart:
		return "впервые наблюдалось выполнение кода корутины; место и время создания неизвестны"
	default:
		return "последнее наблюдение, завершение не подтверждено"
	}
}

func causalOriginLabel(value jhlog.CausalSourceOrigin) string {
	switch value {
	case jhlog.CausalOriginProject:
		return "PROJECT"
	case jhlog.CausalOriginDependency:
		return "DEPENDENCY"
	default:
		return "UNKNOWN"
	}
}

func coalesceCausalFindings(input []CausalFinding) []CausalFinding {
	index := make(map[string]int, len(input))
	out := make([]CausalFinding, 0, len(input))
	for _, item := range input {
		key := fmt.Sprintf("%s|%s|%d|%s", item.Rule, item.Source, item.SourceLine, item.Responsibility)
		if at, ok := index[key]; ok {
			out[at].Count += item.Count
			if item.MaxDurationMS > out[at].MaxDurationMS {
				out[at].MaxDurationMS = item.MaxDurationMS
				out[at].Consequence = item.Consequence
			}
			for _, proof := range item.Proof {
				if len(out[at].Proof) < 5 {
					out[at].Proof = append(out[at].Proof, proof)
				}
			}
			continue
		}
		index[key] = len(out)
		out = append(out, item)
	}
	return out
}

func (s *causalCollectorState) evaluateNode(a *CausalAnalysis, n *causalNode, responsibility, appCaller string, appLine uint64, attributionProof string) {
	source, line := n.directSource, n.directLine
	if appCaller != "" {
		source, line = appCaller, appLine
	}
	if source == "" {
		source = "метод неизвестен"
	}
	var queueEntered, scheduledFor uint64
	var hasQueueEntered, hasScheduledFor bool
	for _, e := range n.events {
		if e.Presence&jhlog.CausalHasQueueEntered != 0 {
			queueEntered, hasQueueEntered = e.QueueEnteredNanos, true
		}
		if e.Presence&jhlog.CausalHasScheduledFor != 0 {
			scheduledFor, hasScheduledFor = e.ScheduledForNanos, true
		}
		if e.EntityKind == jhlog.CausalExecutorTask && e.Phase == 4 {
			if hasQueueEntered && e.ObservedElapsedNanos >= queueEntered && e.ObservedElapsedNanos-queueEntered >= queueWaitReviewNS {
				waitMS := (e.ObservedElapsedNanos - queueEntered) / 1_000_000
				a.Observations = append(a.Observations, CausalFinding{Level: "review", Rule: "queue_wait", Title: "Задача ждала в очереди",
					Anchor: causalAnchor(n.key), Consequence: fmt.Sprintf("От измеренной постановки до старта прошло %d мс (порог 250 мс); влияние на пользовательское действие не установлено.", waitMS),
					Source: source, SourceLine: line, Responsibility: responsibility, Confidence: "очередь измерена для этой задачи; ущерб не подтверждён; " + attributionProof,
					Coverage: a.Coverage, NextAction: "Проверьте загрузку планировщика и ожидание вызывающего кода.", Count: 1, MaxDurationMS: waitMS,
					Proof: []string{causalProof(n.key, e.Phase)}})
			}
			if hasScheduledFor && e.ObservedElapsedNanos >= scheduledFor && e.ObservedElapsedNanos-scheduledFor >= scheduleLateReviewNS {
				lateMS := (e.ObservedElapsedNanos - scheduledFor) / 1_000_000
				a.Observations = append(a.Observations, CausalFinding{Level: "review", Rule: "schedule_late", Title: "Задача запустилась позже заявленного времени",
					Anchor: causalAnchor(n.key), Consequence: fmt.Sprintf("Старт позже заявленного deadline на %d мс (порог 250 мс); причиной может быть нагрузка, планировщик или ограничения платформы.", lateMS),
					Source: source, SourceLine: line, Responsibility: responsibility, Confidence: "deadline и старт записаны; ущерб не подтверждён; " + attributionProof,
					Coverage: a.Coverage, NextAction: "Сравните запрошенный cadence, фактическую очередь и ограничения Android.", Count: 1, MaxDurationMS: lateMS,
					Proof: []string{causalProof(n.key, e.Phase)}})
			}
		}
		if e.EntityKind == jhlog.CausalOperation && e.Presence&jhlog.CausalHasDurationNanos != 0 &&
			e.DurationNanos >= mainBlockingLimitNS && e.Presence&jhlog.CausalHasTargetTID != 0 &&
			s.scopes[n.key.Scope] != nil && e.TargetTID == s.scopes[n.key.Scope].pid && causalBlockingOperation(e.OperationKind) {
			a.Findings = append(a.Findings, CausalFinding{Level: "problem", Rule: "main_thread_blocking_operation", Title: "Измеренная блокирующая операция в главном потоке",
				Anchor:      causalAnchor(n.key),
				Consequence: fmt.Sprintf("Главный поток занят %d мс (порог 100 мс); влияние на кадры отдельно не измерено.", e.DurationNanos/1_000_000),
				Source:      source, SourceLine: line, Responsibility: responsibility, Confidence: "высокая для длительности и TID; исход UI неизвестен; " + attributionProof,
				Coverage: a.Coverage, NextAction: "Проверьте вызов и перенесите блокирующую работу или сократите её критический участок.",
				Count: 1, MaxDurationMS: e.DurationNanos / 1_000_000, Proof: []string{causalProof(n.key, e.Phase)}})
		}
	}
	if n.metrics.longRunCount > 0 {
		cpuEvidence := "CPU неизвестен; elapsed-время не различает вычисления, ожидание и вытеснение."
		if n.metrics.longestRunCPUKnown {
			cpuEvidence = fmt.Sprintf("CPU самого долгого исполнительного сегмента: %.3f мс; остаток elapsed-времени не доказывает блокировку.", float64(n.metrics.longestRunCPUNS)/1_000_000)
		}
		a.Observations = append(a.Observations, CausalFinding{Level: "review", Rule: "long_task", Title: "Долгая задача для проверки",
			Anchor:      causalAnchor(n.key),
			Consequence: fmt.Sprintf("Самый долгий исполнительный сегмент занял %d мс. %s Влияние на пользовательское действие не установлено.", n.metrics.longestRunNS/1_000_000, cpuEvidence),
			Source:      source, SourceLine: line, Responsibility: responsibility, Confidence: "длительность учитывает все принятые события до ограничения подробностей; ущерб не подтверждён; " + attributionProof,
			Coverage: a.Coverage, NextAction: "Сопоставьте исполнительный сегмент с ожиданием вызывающего кода и измерениями очереди.",
			Count: n.metrics.longRunCount, MaxDurationMS: n.metrics.longestRunNS / 1_000_000, Proof: []string{causalProof(n.key, n.metrics.longestRunPhase)}})
	}

	if n.key.Kind != jhlog.CausalResource || n.omittedEvents != 0 || !s.scopeComplete(n.key.Scope) {
		return
	}
	resourceEvents := append([]jhlog.CausalEvent(nil), n.events...)
	sort.SliceStable(resourceEvents, func(i, j int) bool {
		return resourceEvents[i].ObservedElapsedNanos < resourceEvents[j].ObservedElapsedNanos
	})
	var leaseObserved, bound bool
	var bindingAt, lateAction, latePhase uint64
	var owner *causalNode
	var bindingEvent *jhlog.CausalEvent
	bind := func(event *jhlog.CausalEvent) {
		bindingAt, bindingEvent = event.ObservedElapsedNanos, event
		bound = event.Presence&jhlog.CausalHasLeaseScope != 0 && event.BoundToScope == 1 && event.LeaseScopeID != 0
		owner = nil
		if bound {
			owner = s.nodes[CausalKey{Scope: n.key.Scope, Kind: event.LeaseScopeKind, ID: event.LeaseScopeID}]
		}
	}
	for index := range resourceEvents {
		event := &resourceEvents[index]
		if (event.Phase == 2 || event.Phase == 7) && !leaseObserved {
			leaseObserved = true
			bind(event)
			continue
		}
		if !leaseObserved || event.Phase != 4 && event.Phase != 6 {
			continue
		}
		if bound && owner != nil && owner.scopeEnded && owner.scopeEndAt >= bindingAt &&
			event.ObservedElapsedNanos > owner.scopeEndAt {
			lateAction, latePhase = event.ObservedElapsedNanos, event.Phase
			break
		}
		if event.Phase == 4 {
			return
		}
		bind(event)
	}
	if !bound || owner == nil || !owner.scopeEnded || owner.scopeEndAt < bindingAt {
		return
	}
	consequence := "Явно привязанный lease оставался открытым к концу scope; release или передача позже не наблюдались в полном окне сбора."
	if bindingEvent != nil && bindingEvent.Phase == 7 {
		consequence = "Переданный в use ресурс не имеет наблюдаемого close к концу явно связанного scope; первоначальное приобретение и открытость неизвестны."
	}
	if latePhase == 4 {
		consequence = fmt.Sprintf("Lease освобождён через %d мс после конца явно связанного scope.", (lateAction-owner.scopeEndAt)/1_000_000)
		if bindingEvent != nil && bindingEvent.Phase == 7 {
			consequence += " Первоначальное приобретение и открытость до use неизвестны."
		}
	} else if latePhase == 6 {
		consequence = fmt.Sprintf("Lease передан через %d мс после конца исходного scope.", (lateAction-owner.scopeEndAt)/1_000_000)
	}
	if bindingEvent != nil {
		if bindingEvent.SourceOrigin == jhlog.CausalOriginProject && bindingEvent.SourceMethod != "" {
			source, line = bindingEvent.SourceMethod, bindingEvent.SourceLine
		} else if bindingEvent.AppCallerMethod != "" {
			source, line = bindingEvent.AppCallerMethod, bindingEvent.AppCallerLine
		}
	}
	title := "Ресурс не освобождён к концу явно связанной работы"
	if bindingEvent != nil && bindingEvent.Phase == 7 {
		title = "Вызов close для переданного в use ресурса не наблюдался к концу работы"
	}
	a.Findings = append(a.Findings, CausalFinding{Level: "problem", Rule: "bound_resource_not_released", Title: title,
		Anchor:      causalAnchor(n.key),
		Consequence: consequence,
		Source:      source, SourceLine: line, Responsibility: responsibility, Confidence: "высокая для tracked lease при полном охвате; " + attributionProof,
		Coverage: a.Coverage, NextAction: "Закройте lease до выхода из scope либо явно передайте его новому scope.",
		Count: 1, Proof: []string{causalProof(n.key, bindingEvent.Phase), causalProof(owner.key, 0)}})
}

func causalBlockingOperation(kind uint64) bool {
	switch kind {
	case jhlog.CausalOperationFileRead, jhlog.CausalOperationFileWrite, jhlog.CausalOperationFileSync,
		jhlog.CausalOperationDatabase, jhlog.CausalOperationLockWait, jhlog.CausalOperationMonitorWait,
		jhlog.CausalOperationContentRead, jhlog.CausalOperationContentWrite:
		return true
	default:
		return false
	}
}
