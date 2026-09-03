package analyze

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	canonicalLogSpamWindowMS         = 5_000
	canonicalLogSpamCount            = 50
	heapDumpStallAttributionWindowMS = 2_000
	diagnosticCompletenessModel      = "diagnostic-completeness-v1:active-components-normalized;transport=40,runtime_graph=20,process_roster=20,integrity=20"
)

type qualityCounterWarning struct {
	name  string
	label string
}

var runtimeQualityCounterWarnings = []qualityCounterWarning{
	{"jankhunter.events_dropped.count", "очередь записи не приняла события"},
	{"jankhunter.writer_io_error.count", "при записи возникли ошибки"},
	{"jankhunter.writer_event_lost_on_io.count", "после ошибки записи события не сохранились"},
	{"jankhunter.metric_aggregation.dropped.count", "агрегатор метрик отбросил ключи из-за лимита кардинальности"},
	{"jankhunter.log_spam.dropped_keys.count", "агрегатор спама логами отбросил ключи из-за лимита кардинальности"},
	{"jankhunter.runtime_call_graph.dropped.count", "граф вызовов во время выполнения не сохранил связи из-за лимита или рассинхронизации стека"},
	{"jankhunter.handler_wrapper.dropped_entries.count", "реестр Handler-оберток отбросил записи из-за лимита"},
	{"jankhunter.handler_wrapper.dropped_wrappers.count", "реестр обёрток Handler не сохранил обёртку из-за лимита"},
	{"jankhunter.activity_tracker.unavailable.count", "наблюдатель жизненного цикла Activity не подключился, поэтому экран мог остаться неизвестным"},
}

func InspectFilesWithOptions(title string, paths []string, options Options) (Summary, error) {
	collector := newCollector(title, len(paths), options)
	inputs, err := orderedSessionInputs(paths)
	if err != nil {
		return Summary{}, err
	}
	for index, input := range inputs {
		continuation := index > 0 && sameSession(inputs[index-1].header, input.header)
		completesSession := index+1 == len(inputs) || !sameSession(input.header, inputs[index+1].header)
		collector.startSegment(input.header, continuation)
		if !continuation {
			collector.operationAnalysis.startLog(input.header)
		}
		lastDictSize := 0
		result, err := jhlog.StreamFileWithResult(input.path, func(event jhlog.Event, dict map[uint64]string) error {
			if len(dict) > lastDictSize {
				collector.summary.Dictionary += len(dict) - lastDictSize
				lastDictSize = len(dict)
			}
			collector.add(dict, event)
			return nil
		})
		if err != nil {
			return Summary{}, err
		}
		if err := validateArtifactNamespace(
			options.ArtifactSymbolNamespace,
			result.Header,
			result.Source,
			options.ArtifactDirectory,
		); err != nil {
			return Summary{}, err
		}
		collector.addSegmentStreamResult(result, completesSession)
		if completesSession {
			collector.finishLog()
		}
	}
	if err := collector.validateStableSymbols(); err != nil {
		return Summary{}, err
	}
	if err := collector.validateSegmentIdentityConsistency(); err != nil {
		return Summary{}, err
	}
	return collector.finish(), nil
}

type sessionInput struct {
	path   string
	header jhlog.SegmentHeader
}

// orderedSessionInputs keeps independent sessions in the caller's first-seen order while
// restoring the only valid order inside a rotated session. filepath.Glob is lexical, where
// "0-1.jhlog" precedes "0.jhlog", so consuming paths directly loses the session metadata that
// segment zero establishes for all successors.
func orderedSessionInputs(paths []string) ([]sessionInput, error) {
	groups := make([][]sessionInput, 0, len(paths))
	groupBySession := make(map[jhlog.ID128]int, len(paths))
	for _, path := range paths {
		header, err := jhlog.ReadSessionHeader(path)
		if err != nil {
			return nil, err
		}
		input := sessionInput{path: path, header: header}
		if header.SessionID.IsZero() {
			groups = append(groups, []sessionInput{input})
			continue
		}
		groupIndex, exists := groupBySession[header.SessionID]
		if !exists {
			groupIndex = len(groups)
			groupBySession[header.SessionID] = groupIndex
			groups = append(groups, nil)
		}
		groups[groupIndex] = append(groups[groupIndex], input)
	}

	ordered := make([]sessionInput, 0, len(paths))
	for _, group := range groups {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].header.SegmentIndex < group[j].header.SegmentIndex
		})
		ordered = append(ordered, group...)
	}
	return ordered, nil
}

func sameSession(left, right jhlog.SegmentHeader) bool {
	return !left.SessionID.IsZero() && left.SessionID == right.SessionID
}

// ReadArtifactMetadataNamespace validates the compact build identity used to match optional
// diagnostics and class-graph artifacts to their self-contained logs.
func ReadArtifactMetadataNamespace(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxArtifactMetadataBytes {
		return nil, fmt.Errorf("%s: artifact metadata size must be between 1 and %d bytes", path, maxArtifactMetadataBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var metadata artifactMetadataRecord
	if err := decodeArtifactMetadataRecord(data, &metadata); err != nil {
		return nil, fmt.Errorf("%s: parse artifact metadata: %w", path, err)
	}
	if err := validateArtifactFormat(path, "artifact metadata", metadata.Format, ArtifactMetadataFormat); err != nil {
		return nil, err
	}
	if metadata.Kind != "artifact-metadata" {
		return nil, fmt.Errorf("%s: artifact metadata kind must be %q", path, "artifact-metadata")
	}
	namespace, err := decodeSymbolNamespace(metadata.SymbolNamespace)
	if err != nil {
		return nil, fmt.Errorf("%s: parse artifact metadata: %w", path, err)
	}
	return namespace, nil
}

type artifactMetadataRecord struct {
	Format                   int             `json:"format"`
	Kind                     string          `json:"kind"`
	Variant                  string          `json:"variant"`
	IDAlgorithm              string          `json:"idAlgorithm"`
	IDEncoding               string          `json:"idEncoding"`
	SymbolNamespace          string          `json:"symbolNamespace"`
	IncludeWholeApplication  bool            `json:"includeWholeApplication"`
	NetworkWholeApplication  bool            `json:"networkWholeApplication"`
	DatabaseWholeApplication bool            `json:"databaseWholeApplication"`
	Hooks                    map[string]bool `json:"hooks"`
	AndroidNamespace         string          `json:"androidNamespace"`
	IncludePackages          []string        `json:"includePackages"`
	ExcludePackages          []string        `json:"excludePackages"`
}

func decodeArtifactMetadataRecord(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	return nil
}

func decodeSymbolNamespace(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("metadata record has no symbolNamespace")
	}
	if len(value) != symbolNamespaceBytes*2 {
		return nil, fmt.Errorf("symbolNamespace must contain exactly %d lowercase hexadecimal bytes", symbolNamespaceBytes)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, fmt.Errorf("symbolNamespace must contain lowercase hexadecimal bytes")
	}
	return decoded, nil
}

const (
	symbolNamespaceBytes     = 16
	maxArtifactMetadataBytes = 64 * 1024
)

const httpPhaseCount = 7

type networkCallKey struct {
	route     string
	service   string
	initiator string
	screen    string
	operation string
	owner     string
}

type httpInterval struct {
	logIndex uint64
	startMS  uint64
	endMS    uint64
}

type httpAggregate struct {
	durations          uint64SampleSet
	durationTotal      uint64
	phases             [httpPhaseCount]uint64SampleSet
	phaseTotals        [httpPhaseCount]uint64
	count              int
	failures           int
	transportFailures  int
	http4xx            int
	http5xx            int
	canceled           int
	cacheHits          int
	reusedConnections  int
	knownRequestBytes  int
	knownResponseBytes int
	attempts           uint64
	dnsAttempts        uint64
	connectAttempts    uint64
	tlsAttempts        uint64
	retries            uint64
	redirects          uint64
	connectFailures    uint64
	tlsFailures        uint64
	bytesRx            uint64
	bytesTx            uint64
	ownerSample        string
	serviceSample      string
	initiatorSample    string
	intervals          []httpInterval
	burst              routeBurstAccumulator
}

type webSocketKey struct {
	route     string
	screen    string
	operation string
	owner     string
}

type webSocketAggregate struct {
	opened         uint64
	closed         uint64
	failures       uint64
	reconnects     uint64
	connect        uint64SampleSet
	lifetime       uint64SampleSet
	textMessages   uint64
	binaryMessages uint64
	receivedBytes  uint64
	failureKinds   [7]uint64
	closeCodes     map[uint16]uint64
}

type databaseStatementKey struct {
	query             string
	operation         string
	fallbackSource    string
	fallbackFramework string
}

type databaseContextKey struct {
	statement         databaseStatementKey
	source            string
	framework         string
	screen            string
	contextOwner      string
	contextOperation  string
	operationID       uint64
	process           string
	processInstanceID jhlog.ID128
	sessionID         jhlog.ID128
}

type databaseAggregate struct {
	overall      databaseExecutionAggregate
	main         databaseExecutionAggregate
	background   databaseExecutionAggregate
	knownSQL     uint64
	rapidRepeats uint64
	lastLogIndex uint64
	lastTimeMS   uint64
	hasLast      bool
	burst        routeBurstAccumulator
}

type databaseExecutionAggregate struct {
	durations     operationDurationSummary
	calls         uint64
	failures      uint64
	totalDuration uint64
}

func (s *databaseExecutionAggregate) add(event *jhlog.DatabaseEvent) {
	s.calls++
	s.durations.add(event.DurationUS)
	s.totalDuration = saturatingUint64Sum(s.totalDuration, event.DurationUS)
	if event.Outcome == jhlog.DatabaseOutcomeFailure {
		s.failures++
	}
}

func (s *databaseAggregate) add(event *jhlog.DatabaseEvent, flags, logIndex, timeMS uint64) {
	s.overall.add(event)
	if flags&uint64(jhlog.FlagThreadMain) != 0 {
		s.main.add(event)
	} else {
		s.background.add(event)
	}
	if !event.QueryRef.IsUnknown() {
		s.knownSQL++
	}
	if s.hasLast && s.lastLogIndex == logIndex && timeMS >= s.lastTimeMS && timeMS-s.lastTimeMS <= 100 {
		s.rapidRepeats++
	}
	s.lastLogIndex = logIndex
	s.lastTimeMS = timeMS
	s.hasLast = true
	s.burst.add(logIndex, timeMS)
}

func (s *webSocketAggregate) add(event *jhlog.WebSocketEvent) {
	switch event.Stage {
	case jhlog.WebSocketStageOpened:
		s.opened++
		s.connect.add(event.DurationMS)
		if event.ReconnectOrdinal > 0 {
			s.reconnects++
		}
	case jhlog.WebSocketStageClosed:
		s.closed++
		s.lifetime.add(event.DurationMS)
		if event.CloseCode != 0 {
			if s.closeCodes == nil {
				s.closeCodes = make(map[uint16]uint64)
			}
			s.closeCodes[event.CloseCode]++
		}
	case jhlog.WebSocketStageFailed:
		s.failures++
		s.lifetime.add(event.DurationMS)
		if int(event.FailureKind) < len(s.failureKinds) {
			s.failureKinds[event.FailureKind]++
		}
	}
	if event.Stage != jhlog.WebSocketStageOpened {
		s.textMessages = saturatingUint64Sum(s.textMessages, event.TextMessages)
		s.binaryMessages = saturatingUint64Sum(s.binaryMessages, event.BinaryMessages)
		s.receivedBytes = saturatingUint64Sum(s.receivedBytes, event.ReceivedBytes)
	}
}

func (s *webSocketAggregate) activeAtEnd() uint64 {
	terminal := saturatingUint64Sum(s.closed, s.failures)
	if terminal >= s.opened {
		return 0
	}
	return s.opened - terminal
}

func (s *httpAggregate) add(event *jhlog.HTTPEvent, flags uint64, logIndex, endMS uint64, retainInterval bool) {
	s.count++
	s.durations.add(event.DurationMS)
	s.durationTotal = saturatingUint64Sum(s.durationTotal, event.DurationMS)
	phaseValues := [...]uint64{
		event.QueueMS,
		event.DNSMS,
		event.ConnectMS,
		event.TLSMS,
		event.RequestMS,
		event.TTFBMS,
		event.ResponseMS,
	}
	for index, value := range phaseValues {
		if value == 0 {
			continue
		}
		s.phases[index].add(value)
		s.phaseTotals[index] = saturatingUint64Sum(s.phaseTotals[index], value)
	}
	status := httpStatusClass(event)
	transportFailure := flags&uint64(jhlog.FlagHTTPFailed) != 0
	if transportFailure {
		s.transportFailures++
	}
	if status == jhlog.Status4xx {
		s.http4xx++
	}
	if status == jhlog.Status5xx {
		s.http5xx++
	}
	if transportFailure || status == jhlog.Status5xx {
		s.failures++
	}
	if flags&uint64(jhlog.FlagHTTPCancelled) != 0 ||
		event.FailurePhase == jhlog.HTTPFailurePhaseCancelled ||
		event.FailureKind == jhlog.HTTPFailureKindCancelled {
		s.canceled++
	}
	if flags&uint64(jhlog.FlagHTTPCacheHit) != 0 {
		s.cacheHits++
	}
	if flags&uint64(jhlog.FlagHTTPReusedConnection) != 0 {
		s.reusedConnections++
	}
	if flags&uint64(jhlog.FlagHTTPRequestBytesKnown) != 0 {
		s.knownRequestBytes++
	}
	if flags&uint64(jhlog.FlagHTTPResponseBytesKnown) != 0 {
		s.knownResponseBytes++
	}
	s.attempts = saturatingUint64Sum(s.attempts, uint64(event.Attempts))
	s.dnsAttempts = saturatingUint64Sum(s.dnsAttempts, uint64(event.DNSAttempts))
	s.connectAttempts = saturatingUint64Sum(s.connectAttempts, uint64(event.ConnectAttempts))
	s.tlsAttempts = saturatingUint64Sum(s.tlsAttempts, uint64(event.TLSAttempts))
	s.redirects = saturatingUint64Sum(s.redirects, uint64(event.Redirects))
	s.connectFailures = saturatingUint64Sum(s.connectFailures, uint64(event.ConnectFailures))
	s.tlsFailures = saturatingUint64Sum(s.tlsFailures, uint64(event.TLSFailures))
	minimumAttempts := uint64(event.Redirects) + 1
	if uint64(event.Attempts) > minimumAttempts {
		s.retries = saturatingUint64Sum(s.retries, uint64(event.Attempts)-minimumAttempts)
	}
	s.bytesRx = saturatingUint64Sum(s.bytesRx, event.RxBytes)
	s.bytesTx = saturatingUint64Sum(s.bytesTx, event.TxBytes)
	if retainInterval {
		startMS := uint64(0)
		if endMS > event.DurationMS {
			startMS = endMS - event.DurationMS
		}
		s.intervals = append(s.intervals, httpInterval{logIndex: logIndex, startMS: startMS, endMS: endMS})
	}
}

func httpStatusClass(event *jhlog.HTTPEvent) jhlog.StatusClass {
	if event.StatusCode != 0 {
		return jhlog.StatusClassForHTTPCode(event.StatusCode)
	}
	return event.Status
}

func httpEventFailed(event *jhlog.HTTPEvent, flags uint64) bool {
	return flags&uint64(jhlog.FlagHTTPFailed) != 0 || httpStatusClass(event) == jhlog.Status5xx
}

type collector struct {
	collectorOutputState
	collectorInputState
	collectorTimelineState
	collectorQualityState
	collectorDomainState
	collectorSignalState
	collectorSessionState
}

type collectorOutputState struct {
	summary Summary
}

type collectorInputState struct {
	filter              Filter
	nameMap             *NameMapping
	classGraph          *ClassGraph
	diagnostics         *InstrumentationDiagnostics
	dependencyInjection *DependencyInjectionCatalog
	heap                *HeapEvidence
	databaseEvidence    *DatabaseEvidence
	artifactDirectory   string
	artifactAuto        bool
	artifactNamespace   []byte
}

type collectorTimelineState struct {
	seenEvent           bool
	firstTime           uint64
	lastTime            uint64
	logSeen             bool
	logFirst            uint64
	logLast             uint64
	logsWithEvents      int
	totalLogDurationMS  uint64
	logTrafficSeen      bool
	logTrafficFirstRx   uint64
	logTrafficFirstTx   uint64
	logTrafficLastRx    uint64
	logTrafficLastTx    uint64
	totalTrafficRxBytes uint64
	totalTrafficTxBytes uint64
	lastHeapDumpMS      uint64
}

type collectorQualityState struct {
	dictionaryOverflow int
	qualitySnapshots   map[string]segmentQualityState
	streamResults      []jhlog.StreamResult
	chainIssues        []string
}

type collectorDomainState struct {
	networkTotals        httpAggregate
	networkRoutes        map[string]*httpAggregate
	networkCalls         map[networkCallKey]*httpAggregate
	networkStatusCodes   map[uint16]uint64
	networkFailurePhases [9]uint64
	networkFailureKinds  [9]uint64
	networkProtocols     [5]uint64
	webSocketTotals      webSocketAggregate
	webSocketConnections map[webSocketKey]*webSocketAggregate
	databaseTotals       databaseAggregate
	databaseTelemetry    databaseTelemetryAggregate
	databaseStatements   databaseStatementStore
	databaseTransactions databaseTransactionAccumulator
	databaseScenarios    databaseScenarioAccumulator
	databaseCorrelation  databaseCorrelationAccumulator
	workerCollectorState
	runtimeAnalysis   runtimeAnalysisAccumulator
	operationAnalysis operationAnalysisAccumulator
	androidAnalysis   *androidComponentAnalysisAccumulator
}

type collectorSignalState struct {
	screenStats                map[string]*ScreenStats
	processExitStats           map[processExitKey]*ProcessExitStats
	ioStats                    map[ioStatsKey]*ioAggregate
	ioAnalysis                 ioAnalysisAccumulator
	ownerStats                 map[ownerStatKey]*OwnerStats
	signalContextStats         map[signalContextKey]*SignalContextStats
	signalContextHTTPDurations map[signalContextKey]*uint64SampleSet
	logSpamStats               map[logSpamKey]*LogSpamStats
	problemStats               map[problemWindowKey]*ProblemWindowStats
	runtimeCallStats           map[runtimeCallKey]*RuntimeCallStats
	counterValues              map[string]uint64
	gaugeValues                map[string]*gaugeStats
	appVersions                map[string]uint64
	builds                     map[string]uint64
	devices                    map[string]uint64
	sdks                       map[string]uint64
	cohortSamples              map[string]uint64
	networkSamples             map[string]uint64
	processSamples             map[string]uint64
	retainedClasses            map[string]*retainedClassStats
	retainedAgeBuckets         map[string]uint64
	memoryLeakStats            map[memoryLeakKey]*memoryLeakStats
}

type collectorSessionState struct {
	currentAppVersion  string
	currentBuild       string
	currentDevice      string
	currentSDK         string
	currentProcess     string
	currentProcessID   jhlog.ID128
	currentSessionID   jhlog.ID128
	currentNetwork     string
	currentAndroid     string
	currentPatch       string
	currentPrimaryABI  string
	currentABIs        string
	currentMaker       string
	currentBrand       string
	currentHardware    string
	currentBoard       string
	currentProduct     string
	currentRootKnown   bool
	currentRooted      bool
	currentLogIndex    uint64
	currentAttrScreen  string
	currentAttrOwner   string
	currentOperationID uint64
	currentCohortKey   string
	currentCohortDirty bool
	stableSymbols      stableSymbolResolver
}

type stableSymbolResolver struct {
	embedded   map[uint64]string
	unresolved map[string]struct{}
}

func newCollector(title string, logCount int, options Options) *collector {
	return &collector{
		collectorOutputState: collectorOutputState{
			summary: Summary{Title: title, LogCount: logCount},
		},
		collectorInputState: collectorInputState{
			filter:              normalizeFilter(options.Filter),
			nameMap:             options.ObfuscationMap,
			classGraph:          DeobfuscateClassGraph(options.ClassGraph, options.ObfuscationMap),
			diagnostics:         options.InstrumentationDiagnostics,
			dependencyInjection: options.DependencyInjectionCatalog,
			heap:                DeobfuscateHeapEvidence(options.HeapEvidence, options.ObfuscationMap),
			databaseEvidence:    options.DatabaseEvidence,
			artifactDirectory:   options.ArtifactDirectory,
			artifactAuto:        options.ArtifactsAutoDiscovered,
			artifactNamespace:   append([]byte(nil), options.ArtifactSymbolNamespace...),
		},
		collectorQualityState: collectorQualityState{
			qualitySnapshots: map[string]segmentQualityState{},
		},
		collectorDomainState: collectorDomainState{
			networkRoutes:        map[string]*httpAggregate{},
			networkCalls:         map[networkCallKey]*httpAggregate{},
			networkStatusCodes:   map[uint16]uint64{},
			webSocketConnections: map[webSocketKey]*webSocketAggregate{},
			databaseStatements:   newDatabaseStatementStore(databaseStatementGroupLimit),
			databaseScenarios:    newDatabaseScenarioAccumulator(databaseScenarioGroupLimit),
			operationAnalysis:    newOperationAnalysisAccumulator(),
			androidAnalysis:      newAndroidComponentAnalysisAccumulator(options.AndroidComponentCatalog),
		},
		collectorSignalState: collectorSignalState{
			screenStats:                map[string]*ScreenStats{},
			processExitStats:           map[processExitKey]*ProcessExitStats{},
			ioStats:                    map[ioStatsKey]*ioAggregate{},
			ownerStats:                 map[ownerStatKey]*OwnerStats{},
			signalContextStats:         map[signalContextKey]*SignalContextStats{},
			signalContextHTTPDurations: map[signalContextKey]*uint64SampleSet{},
			logSpamStats:               map[logSpamKey]*LogSpamStats{},
			problemStats:               map[problemWindowKey]*ProblemWindowStats{},
			runtimeCallStats:           map[runtimeCallKey]*RuntimeCallStats{},
			counterValues:              map[string]uint64{},
			gaugeValues:                map[string]*gaugeStats{},
			appVersions:                map[string]uint64{},
			builds:                     map[string]uint64{},
			devices:                    map[string]uint64{},
			sdks:                       map[string]uint64{},
			cohortSamples:              map[string]uint64{},
			networkSamples:             map[string]uint64{},
			processSamples:             map[string]uint64{},
			retainedClasses:            map[string]*retainedClassStats{},
			retainedAgeBuckets:         map[string]uint64{},
			memoryLeakStats:            map[memoryLeakKey]*memoryLeakStats{},
		},
		collectorSessionState: collectorSessionState{
			currentAppVersion:  "unknown",
			currentBuild:       "unknown",
			currentDevice:      "unknown",
			currentSDK:         "unknown",
			currentProcess:     "unknown",
			currentNetwork:     "unknown",
			currentAndroid:     "unknown",
			currentPatch:       "unknown",
			currentPrimaryABI:  "unknown",
			currentABIs:        "unknown",
			currentMaker:       "unknown",
			currentBrand:       "unknown",
			currentHardware:    "unknown",
			currentBoard:       "unknown",
			currentProduct:     "unknown",
			currentAttrScreen:  "unknown",
			currentAttrOwner:   "unknown",
			currentCohortDirty: true,
			stableSymbols: stableSymbolResolver{
				embedded:   map[uint64]string{},
				unresolved: map[string]struct{}{},
			},
		},
	}
}

func (c *collector) startLog(header jhlog.SegmentHeader) {
	c.startSegment(header, false)
}

func (c *collector) startSegment(header jhlog.SegmentHeader, continuation bool) {
	if !continuation {
		c.currentLogIndex++
		c.resetSessionContext()
		c.databaseCorrelation.startLog(header, c.currentLogIndex)
		c.workerCollectorState.startLog()
		c.androidAnalysis.startLog(header)
		c.logSeen = false
		c.logFirst = 0
		c.logLast = 0
		c.logTrafficSeen = false
		c.logTrafficFirstRx = 0
		c.logTrafficFirstTx = 0
		c.logTrafficLastRx = 0
		c.logTrafficLastTx = 0
	}
	c.currentProcess = firstNonEmpty(header.ProcessName, "unknown")
	c.currentProcessID = header.ProcessInstanceID
	c.currentSessionID = header.SessionID
	c.currentCohortDirty = true
	clear(c.stableSymbols.embedded)
	c.resetAttribution()
}

func (c *collector) resetSessionContext() {
	c.currentAppVersion = "unknown"
	c.currentBuild = "unknown"
	c.currentDevice = "unknown"
	c.currentSDK = "unknown"
	c.currentNetwork = "unknown"
	c.currentAndroid = "unknown"
	c.currentPatch = "unknown"
	c.currentPrimaryABI = "unknown"
	c.currentABIs = "unknown"
	c.currentMaker = "unknown"
	c.currentBrand = "unknown"
	c.currentHardware = "unknown"
	c.currentBoard = "unknown"
	c.currentProduct = "unknown"
	c.currentRootKnown = false
	c.currentRooted = false
	c.currentCohortKey = ""
	c.currentCohortDirty = true
	c.lastHeapDumpMS = 0
}

func (c *collector) finishLog() {
	if !c.logSeen {
		return
	}
	c.logsWithEvents++
	if c.logLast >= c.logFirst {
		c.totalLogDurationMS += c.logLast - c.logFirst
	}
	if c.logTrafficSeen {
		c.totalTrafficRxBytes += counterDelta(c.logTrafficFirstRx, c.logTrafficLastRx)
		c.totalTrafficTxBytes += counterDelta(c.logTrafficFirstTx, c.logTrafficLastTx)
	}
}

func (c *collector) recordTraffic(rxBytes, txBytes uint64) {
	if !c.logTrafficSeen {
		c.logTrafficSeen = true
		c.logTrafficFirstRx = rxBytes
		c.logTrafficFirstTx = txBytes
	}
	c.logTrafficLastRx = rxBytes
	c.logTrafficLastTx = txBytes
}

func (c *collector) resetAttribution() {
	c.currentAttrScreen = "unknown"
	c.currentAttrOwner = "unknown"
	c.currentOperationID = 0
}

type segmentQualityState struct {
	segmentIndex uint64
	snapshot     jhlog.QualitySnapshot
}

func (c *collector) addSegmentStreamResult(result jhlog.StreamResult, completesSession bool) {
	if completesSession {
		c.workerCollectorState.finishLog(c.currentLogIndex, result)
	}
	segment := CollectionSegment{
		Source:                           result.Source,
		Status:                           string(result.Status),
		Sealed:                           result.Sealed,
		TailBytes:                        result.TailBytes,
		TotalRecords:                     result.TotalRecords,
		DataRecords:                      result.DataRecords,
		DictionaryRecords:                result.DictionaryRecords,
		ControlRecords:                   result.ControlRecords,
		RuntimeGraphLogicalCalls:         result.RuntimeGraphLogicalCalls,
		RunID:                            fmt.Sprintf("%x", result.Header.RunID[:]),
		ProcessInstanceID:                fmt.Sprintf("%x", result.Header.ProcessInstanceID[:]),
		SessionID:                        fmt.Sprintf("%x", result.Header.SessionID[:]),
		SegmentIndex:                     result.Header.SegmentIndex,
		ProcessName:                      result.Header.ProcessName,
		ProcessScope:                     result.Header.ProcessScope.String(),
		AllowedProcessCount:              result.Header.AllowedProcessCount,
		ProcessScopeFingerprint:          hex.EncodeToString(result.Header.ProcessScopeFingerprint),
		ExpectedProcessCount:             result.Header.ExpectedProcessCount,
		ExpectedProcessFingerprint:       hex.EncodeToString(result.Header.ExpectedProcessFingerprint),
		ProcessRosterDeclarationComplete: result.Header.ProcessRosterDeclarationComplete,
	}
	c.summary.TotalRecordCount += result.TotalRecords
	c.summary.DataRecordCount += result.DataRecords
	c.summary.DictionaryRecords += result.DictionaryRecords
	c.summary.ControlRecords += result.ControlRecords
	c.streamResults = append(c.streamResults, result)
	if result.SegmentEnd != nil {
		segment.EndReason = result.SegmentEnd.Reason.String()
		segment.EndReasonCode = uint64(result.SegmentEnd.Reason)
		if warning := segmentEndWarning(result.Source, result.SegmentEnd.Reason); warning != "" {
			c.summary.Warnings = append(c.summary.Warnings, warning)
		}
	}
	if result.LatestQuality != nil {
		segment.QualitySequence = result.LatestQuality.Sequence
		ids := make([]uint64, 0, len(result.LatestQuality.Counters))
		for id := range result.LatestQuality.Counters {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			segment.QualityCounters = append(segment.QualityCounters, NamedValue{
				Name:  jhlog.QualityCounterName(id),
				Value: result.LatestQuality.Counters[id],
				Extra: fmt.Sprintf("id=%d", id),
			})
		}
		key := qualityIdentityKey(result)
		current, exists := c.qualitySnapshots[key]
		candidate := result.LatestQuality
		if !exists || candidate.Sequence > current.snapshot.Sequence ||
			(candidate.Sequence == current.snapshot.Sequence && candidate.CapturedElapsedUS > current.snapshot.CapturedElapsedUS) ||
			(candidate.Sequence == current.snapshot.Sequence && candidate.CapturedElapsedUS == current.snapshot.CapturedElapsedUS && result.Header.SegmentIndex >= current.segmentIndex) {
			c.qualitySnapshots[key] = segmentQualityState{
				segmentIndex: result.Header.SegmentIndex,
				snapshot:     *candidate,
			}
		}
	}
	c.summary.CollectionSegments = append(c.summary.CollectionSegments, segment)
}

func (c *collector) validateSegmentIdentityConsistency() error {
	issues, err := validateSegmentChains(c.streamResults)
	if err != nil {
		return err
	}
	c.chainIssues = issues
	return nil
}

type segmentChain struct {
	header   jhlog.SegmentHeader
	segments []jhlog.StreamResult
	indices  map[uint64]string
}

func validateSegmentChains(results []jhlog.StreamResult) ([]string, error) {
	chains := map[jhlog.ID128]*segmentChain{}
	var issues []string
	for _, result := range results {
		header := result.Header
		missing := make([]string, 0, 3)
		if header.RunID.IsZero() {
			missing = append(missing, "run_id")
		}
		if header.ProcessInstanceID.IsZero() {
			missing = append(missing, "process_instance_id")
		}
		if header.SessionID.IsZero() {
			missing = append(missing, "session_id")
		}
		if len(missing) > 0 {
			issues = append(issues, fmt.Sprintf("сегмент %s не содержит %s", result.Source, strings.Join(missing, ", ")))
		}
		if strings.TrimSpace(header.ProcessName) == "" {
			issues = append(issues, fmt.Sprintf("сегмент %s не содержит process_name", result.Source))
		}
		if header.SegmentIndex == 0 && len(header.PreviousSegmentDigest) != 0 {
			issues = append(issues, fmt.Sprintf("первый сегмент %s содержит недопустимую predecessor-ссылку", result.Source))
		}
		if header.SegmentIndex > 0 && len(header.PreviousSegmentDigest) != 32 {
			issues = append(issues, fmt.Sprintf("сегмент %s не содержит полный SHA-256 digest предшественника", result.Source))
		}
		if header.SessionID.IsZero() {
			continue
		}
		chain := chains[header.SessionID]
		if chain == nil {
			chain = &segmentChain{header: header, indices: map[uint64]string{}}
			chains[header.SessionID] = chain
		} else if err := validateChainIdentity(chain.header, header, result.Source); err != nil {
			return nil, err
		}
		if previous, exists := chain.indices[header.SegmentIndex]; exists {
			return nil, fmt.Errorf(
				"session %x contains duplicate segment index %d in %q and %q",
				header.SessionID,
				header.SegmentIndex,
				previous,
				result.Source,
			)
		}
		chain.indices[header.SegmentIndex] = result.Source
		chain.segments = append(chain.segments, result)
	}

	for sessionID, chain := range chains {
		sort.Slice(chain.segments, func(i, j int) bool {
			return chain.segments[i].Header.SegmentIndex < chain.segments[j].Header.SegmentIndex
		})
		if len(chain.segments) == 0 {
			continue
		}
		first := chain.segments[0]
		if first.Header.SegmentIndex != 0 {
			issues = append(issues, fmt.Sprintf(
				"session %x начинается с segment_index=%d; предыдущие сегменты не переданы",
				sessionID,
				first.Header.SegmentIndex,
			))
		}
		for index := 1; index < len(chain.segments); index++ {
			previous := chain.segments[index-1]
			current := chain.segments[index]
			if current.Header.SegmentIndex != previous.Header.SegmentIndex+1 {
				issues = append(issues, fmt.Sprintf(
					"session %x имеет разрыв segment chain: %d → %d",
					sessionID,
					previous.Header.SegmentIndex,
					current.Header.SegmentIndex,
				))
			}
			if current.Header.SegmentIndex == previous.Header.SegmentIndex+1 {
				if len(previous.SegmentDigest) != 32 || len(current.Header.PreviousSegmentDigest) != 32 {
					issues = append(issues, fmt.Sprintf(
						"session %x не может проверить SHA-256 handoff segment %d → %d",
						sessionID,
						previous.Header.SegmentIndex,
						current.Header.SegmentIndex,
					))
				} else if !bytes.Equal(current.Header.PreviousSegmentDigest, previous.SegmentDigest) {
					return nil, fmt.Errorf(
						"session %x segment %d predecessor digest does not match sealed segment %d",
						sessionID,
						current.Header.SegmentIndex,
						previous.Header.SegmentIndex,
					)
				}
			}
			if current.Header.SegmentStartElapsedUS < previous.Header.SegmentStartElapsedUS {
				issues = append(issues, fmt.Sprintf(
					"session %x имеет немонотонное elapsed-время segment %d → %d",
					sessionID,
					previous.Header.SegmentIndex,
					current.Header.SegmentIndex,
				))
			}
			if current.Header.SegmentStartUnixMS > 0 && previous.Header.SegmentStartUnixMS > 0 &&
				current.Header.SegmentStartUnixMS < previous.Header.SegmentStartUnixMS {
				issues = append(issues, fmt.Sprintf(
					"session %x имеет немонотонное wall-время segment %d → %d",
					sessionID,
					previous.Header.SegmentIndex,
					current.Header.SegmentIndex,
				))
			}
			if !previous.Sealed {
				issues = append(issues, fmt.Sprintf(
					"session %x продолжилась после незапечатанного segment %d",
					sessionID,
					previous.Header.SegmentIndex,
				))
			}
			if previous.SegmentEnd == nil || previous.SegmentEnd.Reason != jhlog.SegmentEndRotation {
				reason := "без segment_end"
				if previous.SegmentEnd != nil {
					reason = previous.SegmentEnd.Reason.String()
				}
				issues = append(issues, fmt.Sprintf(
					"session %x продолжилась после segment %d с причиной %s вместо rotation",
					sessionID,
					previous.Header.SegmentIndex,
					reason,
				))
			}
		}
		last := chain.segments[len(chain.segments)-1]
		if last.SegmentEnd != nil && last.SegmentEnd.Reason == jhlog.SegmentEndRotation {
			issues = append(issues, fmt.Sprintf(
				"session %x обрывается после rotation segment %d; ожидаемый следующий сегмент не передан",
				sessionID,
				last.Header.SegmentIndex,
			))
		}
	}
	return uniqueStrings(issues), nil
}

func validateChainIdentity(expected, actual jhlog.SegmentHeader, source string) error {
	session := fmt.Sprintf("%x", expected.SessionID)
	switch {
	case expected.RunID != actual.RunID:
		return fmt.Errorf("session %s changes run_id in %q", session, source)
	case expected.ProcessInstanceID != actual.ProcessInstanceID:
		return fmt.Errorf("session %s changes process_instance_id in %q", session, source)
	case expected.OSPID != actual.OSPID:
		return fmt.Errorf("session %s changes os_pid in %q", session, source)
	case expected.CollectorStartElapsedUS != actual.CollectorStartElapsedUS:
		return fmt.Errorf("session %s changes collector_start_elapsed_us in %q", session, source)
	case expected.IdentitySource != actual.IdentitySource:
		return fmt.Errorf("session %s changes identity_source in %q", session, source)
	case expected.RequiredFeatures != actual.RequiredFeatures:
		return fmt.Errorf("session %s changes required_features in %q", session, source)
	case expected.ProcessScope != actual.ProcessScope:
		return fmt.Errorf("session %s changes process_scope in %q", session, source)
	case expected.AllowedProcessCount != actual.AllowedProcessCount:
		return fmt.Errorf("session %s changes allowed_process_count in %q", session, source)
	case !bytes.Equal(expected.ProcessScopeFingerprint, actual.ProcessScopeFingerprint):
		return fmt.Errorf("session %s changes process_scope_fingerprint in %q", session, source)
	case expected.ExpectedProcessCount != actual.ExpectedProcessCount:
		return fmt.Errorf("session %s changes expected_process_count in %q", session, source)
	case !bytes.Equal(expected.ExpectedProcessFingerprint, actual.ExpectedProcessFingerprint):
		return fmt.Errorf("session %s changes expected_process_fingerprint in %q", session, source)
	case expected.ProcessRosterDeclarationComplete != actual.ProcessRosterDeclarationComplete:
		return fmt.Errorf("session %s changes process_roster_declaration_complete in %q", session, source)
	case expected.ProcessName != actual.ProcessName:
		return fmt.Errorf("session %s changes process_name in %q", session, source)
	case !bytes.Equal(expected.SymbolNamespace, actual.SymbolNamespace):
		return fmt.Errorf("session %s changes symbol_namespace in %q", session, source)
	default:
		return nil
	}
}

func segmentEndWarning(source string, reason jhlog.SegmentEndReason) string {
	switch reason {
	case jhlog.SegmentEndNormal, jhlog.SegmentEndShutdown, jhlog.SegmentEndRotation:
		return ""
	case jhlog.SegmentEndSizeLimit:
		return "Качество сбора: " + sizeLimitCollectionReason(source) + "."
	case jhlog.SegmentEndIOError:
		return fmt.Sprintf("Качество сбора: сегмент %q завершён из-за ошибки ввода-вывода; часть событий могла не попасть в .jhlog.", source)
	default:
		return fmt.Sprintf("Качество сбора: сегмент %q завершён с неизвестной причиной %d; данные прочитаны, но CLI не может подтвердить штатность завершения.", source, uint64(reason))
	}
}

func sizeLimitCollectionReason(source string) string {
	return fmt.Sprintf("session-файл %s достиг лимита размера; сбор завершён раньше запрошенного, поэтому события после лимита отсутствуют", source)
}

func qualityIdentityKey(result jhlog.StreamResult) string {
	identity := append([]byte(nil), result.Header.RunID[:]...)
	identity = append(identity, result.Header.ProcessInstanceID[:]...)
	identity = append(identity, result.Header.SessionID[:]...)
	allZero := true
	for _, value := range identity {
		if value != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return result.Source
	}
	return string(identity)
}

func (c *collector) applyAttribution(dict map[uint64]string, context jhlog.AttributionContext) {
	c.resetAttribution()
	if !context.Present {
		return
	}
	c.currentAttrScreen = attrValue(jhlog.ResolveSymbol(dict, context.Screen))
	c.currentAttrOwner = attrValue(c.resolveOwnerRef(dict, context.Owner))
	c.currentOperationID = context.OperationID
}
