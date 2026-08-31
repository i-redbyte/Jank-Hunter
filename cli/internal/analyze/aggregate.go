package analyze

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
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
	for _, path := range paths {
		header, err := jhlog.ReadSessionHeader(path)
		if err != nil {
			return Summary{}, err
		}
		collector.startLog(header)
		collector.operationAnalysis.startLog(header)
		lastDictSize := 0
		result, err := jhlog.StreamFileWithResult(path, func(event jhlog.Event, dict map[uint64]string) error {
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
		collector.addStreamResult(result)
		collector.finishLog()
	}
	if err := collector.validateStableSymbols(); err != nil {
		return Summary{}, err
	}
	if err := collector.validateSegmentIdentityConsistency(); err != nil {
		return Summary{}, err
	}
	return collector.finish(), nil
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
	summary             Summary
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
	dictionaryOverflow  int
	qualitySnapshots    map[string]segmentQualityState
	streamResults       []jhlog.StreamResult
	chainIssues         []string
	lastHeapDumpMS      uint64

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

	screenStats                map[string]*ScreenStats
	processExitStats           map[string]*ProcessExitStats
	ioStats                    map[string]*ioAggregate
	ioAnalysis                 ioAnalysisAccumulator
	ownerStats                 map[ownerStatKey]*OwnerStats
	signalContextStats         map[string]*SignalContextStats
	signalContextHTTPDurations map[string]*uint64SampleSet
	logSpamStats               map[string]*LogSpamStats
	problemStats               map[string]*ProblemWindowStats
	runtimeCallStats           map[string]*RuntimeCallStats
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
	memoryLeakStats            map[string]*memoryLeakStats

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
		summary:                    Summary{Title: title, LogCount: logCount},
		filter:                     normalizeFilter(options.Filter),
		nameMap:                    options.ObfuscationMap,
		classGraph:                 DeobfuscateClassGraph(options.ClassGraph, options.ObfuscationMap),
		diagnostics:                options.InstrumentationDiagnostics,
		dependencyInjection:        options.DependencyInjectionCatalog,
		heap:                       DeobfuscateHeapEvidence(options.HeapEvidence, options.ObfuscationMap),
		databaseEvidence:           options.DatabaseEvidence,
		artifactDirectory:          options.ArtifactDirectory,
		artifactAuto:               options.ArtifactsAutoDiscovered,
		artifactNamespace:          append([]byte(nil), options.ArtifactSymbolNamespace...),
		networkRoutes:              map[string]*httpAggregate{},
		networkCalls:               map[networkCallKey]*httpAggregate{},
		networkStatusCodes:         map[uint16]uint64{},
		webSocketConnections:       map[webSocketKey]*webSocketAggregate{},
		databaseStatements:         newDatabaseStatementStore(databaseStatementGroupLimit),
		databaseScenarios:          newDatabaseScenarioAccumulator(databaseScenarioGroupLimit),
		screenStats:                map[string]*ScreenStats{},
		processExitStats:           map[string]*ProcessExitStats{},
		ioStats:                    map[string]*ioAggregate{},
		ownerStats:                 map[ownerStatKey]*OwnerStats{},
		signalContextStats:         map[string]*SignalContextStats{},
		signalContextHTTPDurations: map[string]*uint64SampleSet{},
		logSpamStats:               map[string]*LogSpamStats{},
		problemStats:               map[string]*ProblemWindowStats{},
		runtimeCallStats:           map[string]*RuntimeCallStats{},
		counterValues:              map[string]uint64{},
		qualitySnapshots:           map[string]segmentQualityState{},
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
		memoryLeakStats:            map[string]*memoryLeakStats{},
		operationAnalysis:          newOperationAnalysisAccumulator(),
		androidAnalysis:            newAndroidComponentAnalysisAccumulator(options.AndroidComponentCatalog),
		currentAppVersion:          "unknown",
		currentBuild:               "unknown",
		currentDevice:              "unknown",
		currentSDK:                 "unknown",
		currentProcess:             "unknown",
		currentNetwork:             "unknown",
		currentAndroid:             "unknown",
		currentPatch:               "unknown",
		currentPrimaryABI:          "unknown",
		currentABIs:                "unknown",
		currentMaker:               "unknown",
		currentBrand:               "unknown",
		currentHardware:            "unknown",
		currentBoard:               "unknown",
		currentProduct:             "unknown",
		currentAttrScreen:          "unknown",
		currentAttrOwner:           "unknown",
		currentCohortDirty:         true,
		stableSymbols: stableSymbolResolver{
			embedded:   map[uint64]string{},
			unresolved: map[string]struct{}{},
		},
	}
}

func (c *collector) startLog(header jhlog.SegmentHeader) {
	c.currentLogIndex++
	c.currentProcess = firstNonEmpty(header.ProcessName, "unknown")
	c.currentProcessID = header.ProcessInstanceID
	c.currentSessionID = header.SessionID
	c.currentCohortDirty = true
	c.databaseCorrelation.startLog(header, c.currentLogIndex)
	c.workerCollectorState.startLog()
	c.androidAnalysis.startLog(header)
	clear(c.stableSymbols.embedded)
	c.resetAttribution()
	c.logSeen = false
	c.logFirst = 0
	c.logLast = 0
	c.logTrafficSeen = false
	c.logTrafficFirstRx = 0
	c.logTrafficFirstTx = 0
	c.logTrafficLastRx = 0
	c.logTrafficLastTx = 0
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

func (c *collector) addStreamResult(result jhlog.StreamResult) {
	c.workerCollectorState.finishLog(c.currentLogIndex, result)
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

type retainedClassStats struct {
	count    uint64
	maxAgeMs uint64
}

type uint64SampleSet struct {
	values             []uint64
	denseCounts        []uint64
	denseOffset        uint64
	outlierCounts      map[uint64]uint64
	orderedFrequencies []uint64Frequency
	seen               int
	min                uint64
	max                uint64
	sorted             bool
	nextPromotionCheck int
}

func (s *uint64SampleSet) add(value uint64) {
	if s.seen == 0 {
		s.min = value
		s.nextPromotionCheck = uint64SampleSetPromotionThreshold
	}
	s.seen++
	if value < s.min {
		s.min = value
	}
	if value > s.max {
		s.max = value
	}
	if len(s.denseCounts) > 0 {
		if value >= s.denseOffset && value-s.denseOffset < uint64(len(s.denseCounts)) {
			s.denseCounts[value-s.denseOffset]++
		} else {
			if s.outlierCounts == nil {
				s.outlierCounts = make(map[uint64]uint64)
			}
			s.outlierCounts[value]++
		}
		s.sorted = false
		return
	}
	s.values = append(s.values, value)
	s.sorted = false
	if s.seen >= s.nextPromotionCheck {
		s.promoteDenseIfBeneficial()
		if s.nextPromotionCheck <= s.seen {
			if s.seen > int(^uint(0)>>1)/2 {
				s.nextPromotionCheck = int(^uint(0) >> 1)
			} else {
				s.nextPromotionCheck = s.seen * 2
			}
		}
	}
}

func (s *uint64SampleSet) percentile(p float64) uint64 {
	if s.seen == 0 {
		return 0
	}
	target := int(math.Ceil(float64(s.seen) * p))
	if target < 1 {
		target = 1
	}
	if target > s.seen {
		target = s.seen
	}
	if len(s.denseCounts) == 0 {
		if !s.sorted {
			sort.Slice(s.values, func(i, j int) bool { return s.values[i] < s.values[j] })
			s.sorted = true
		}
		return s.values[target-1]
	}
	s.prepareOrderedFrequencies()
	seen := uint64(0)
	for _, frequency := range s.orderedFrequencies {
		seen += frequency.count
		if seen >= uint64(target) {
			return frequency.value
		}
	}
	return s.max
}

func (s *uint64SampleSet) promoteDenseIfBeneficial() {
	if len(s.values) == 0 || s.max < s.min || s.max-s.min >= uint64SampleSetMaxDenseBins {
		return
	}
	span := int(s.max-s.min) + 1
	if span > s.seen/uint64SampleSetMinimumCompression {
		return
	}
	counts := make([]uint64, span)
	for _, value := range s.values {
		counts[value-s.min]++
	}
	s.denseCounts = counts
	s.denseOffset = s.min
	s.values = nil
	s.sorted = false
}

func (s *uint64SampleSet) prepareOrderedFrequencies() {
	if s.sorted {
		return
	}
	unique := len(s.outlierCounts)
	for _, count := range s.denseCounts {
		if count > 0 {
			unique++
		}
	}
	frequencies := make([]uint64Frequency, 0, unique)
	for index, count := range s.denseCounts {
		if count > 0 {
			frequencies = append(frequencies, uint64Frequency{
				value: s.denseOffset + uint64(index),
				count: count,
			})
		}
	}
	for value, count := range s.outlierCounts {
		frequencies = append(frequencies, uint64Frequency{value: value, count: count})
	}
	sort.Slice(frequencies, func(i, j int) bool { return frequencies[i].value < frequencies[j].value })
	s.orderedFrequencies = frequencies
	s.sorted = true
}

type uint64Frequency struct {
	value uint64
	count uint64
}

const (
	uint64SampleSetPromotionThreshold = 4_096
	uint64SampleSetMaxDenseBins       = 65_536
	uint64SampleSetMinimumCompression = 2
)

type gaugeStats struct {
	count uint64
	total uint64
	max   uint64
	last  uint64
	mode  jhlog.MetricMode
}

type routeBurstBucket struct {
	logIndex uint64
	second   uint64
	count    uint64
}

type routeBurstAccumulator struct {
	buckets           []routeBurstBucket
	peak              uint64
	peakWindowStartMS uint64
	approximate       bool
}

func (s *routeBurstAccumulator) add(logIndex, timeMS uint64) {
	second := timeMS / 1_000
	for index := range s.buckets {
		bucket := &s.buckets[index]
		if bucket.logIndex != logIndex || bucket.second != second {
			continue
		}
		bucket.count++
		s.updatePeak(*bucket)
		return
	}
	bucket := routeBurstBucket{logIndex: logIndex, second: second, count: 1}
	if len(s.buckets) < routeBurstRetainedSeconds {
		s.buckets = append(s.buckets, bucket)
	} else {
		oldest := 0
		for index := 1; index < len(s.buckets); index++ {
			if routeBurstBucketBefore(s.buckets[index], s.buckets[oldest]) {
				oldest = index
			}
		}
		s.buckets[oldest] = bucket
		s.approximate = true
	}
	s.updatePeak(bucket)
}

func (s *routeBurstAccumulator) updatePeak(bucket routeBurstBucket) {
	if bucket.count > s.peak {
		s.peak = bucket.count
		s.peakWindowStartMS = bucket.second * 1_000
	}
}

func routeBurstBucketBefore(left, right routeBurstBucket) bool {
	if left.logIndex != right.logIndex {
		return left.logIndex < right.logIndex
	}
	return left.second < right.second
}

const routeBurstRetainedSeconds = 8

var httpPhaseNames = [...]string{"queue", "dns", "connect", "tls", "request", "ttfb", "response"}

func phaseAverage(stats *httpAggregate, index int) uint64 {
	seen := stats.phases[index].seen
	if seen == 0 {
		return 0
	}
	return stats.phaseTotals[index] / uint64(seen)
}

func httpPhaseStats(stats *httpAggregate) []HTTPPhaseStats {
	result := make([]HTTPPhaseStats, len(httpPhaseNames))
	for index, name := range httpPhaseNames {
		set := &stats.phases[index]
		result[index] = HTTPPhaseStats{
			Name:        name,
			SampleCount: set.seen,
			AvgMS:       phaseAverage(stats, index),
			P50MS:       set.percentile(0.50),
			P95MS:       set.percentile(0.95),
			MaxMS:       set.max,
		}
	}
	return result
}

func networkCallStats(key networkCallKey, stats *httpAggregate) NetworkCallStats {
	return NetworkCallStats{
		Route: key.route, Service: key.service, Initiator: key.initiator,
		Screen: key.screen, Operation: key.operation, Owner: key.owner,
		Count: stats.count, Failures: stats.failures,
		TransportFailures: stats.transportFailures, HTTP4xx: stats.http4xx, HTTP5xx: stats.http5xx,
		Canceled: stats.canceled, CacheHits: stats.cacheHits, ReusedConnections: stats.reusedConnections,
		KnownRequestBytes: stats.knownRequestBytes, KnownResponseBytes: stats.knownResponseBytes,
		Attempts: stats.attempts, DNSAttempts: stats.dnsAttempts,
		ConnectAttempts: stats.connectAttempts, TLSAttempts: stats.tlsAttempts,
		Retries: stats.retries, Redirects: stats.redirects,
		ConnectFailures: stats.connectFailures, TLSFailures: stats.tlsFailures,
		P50MS: stats.durations.percentile(0.50), P95MS: stats.durations.percentile(0.95),
		MaxMS: stats.durations.max, BytesRx: stats.bytesRx, BytesTx: stats.bytesTx,
		TotalDurationMS: stats.durationTotal,
		Phases:          httpPhaseStats(stats),
	}
}

func (c *collector) finalizeNetworkAnalysis(result NetworkAnalysis) NetworkAnalysis {
	stats := &c.networkTotals
	result.MaxConcurrency, result.PeakConcurrencyAtMS = maxHTTPConcurrency(stats.intervals)
	result.TransportFailures = stats.transportFailures
	result.HTTP4xx = stats.http4xx
	result.HTTP5xx = stats.http5xx
	result.Canceled = stats.canceled
	result.CacheHits = stats.cacheHits
	result.ReusedConnections = stats.reusedConnections
	result.KnownRequestBytes = stats.knownRequestBytes
	result.KnownResponseBytes = stats.knownResponseBytes
	result.Attempts = stats.attempts
	result.DNSAttempts = stats.dnsAttempts
	result.ConnectAttempts = stats.connectAttempts
	result.TLSAttempts = stats.tlsAttempts
	result.Retries = stats.retries
	result.Redirects = stats.redirects
	result.ConnectFailures = stats.connectFailures
	result.TLSFailures = stats.tlsFailures
	result.BytesRx = stats.bytesRx
	result.BytesTx = stats.bytesTx
	result.TotalDurationMS = stats.durationTotal
	result.Phases = httpPhaseStats(stats)
	for code, count := range c.networkStatusCodes {
		result.StatusCodes = append(result.StatusCodes, NamedValue{Name: fmt.Sprint(code), Value: count})
	}
	sort.Slice(result.StatusCodes, func(i, j int) bool { return result.StatusCodes[i].Name < result.StatusCodes[j].Name })
	for phase, count := range c.networkFailurePhases {
		if count > 0 {
			result.FailurePhases = append(result.FailurePhases, NamedValue{Name: httpFailurePhaseName(jhlog.HTTPFailurePhase(phase)), Value: count})
		}
	}
	for kind, count := range c.networkFailureKinds {
		if count > 0 {
			result.FailureKinds = append(result.FailureKinds, NamedValue{Name: httpFailureKindName(jhlog.HTTPFailureKind(kind)), Value: count})
		}
	}
	for protocol, count := range c.networkProtocols {
		if count > 0 {
			result.Protocols = append(result.Protocols, NamedValue{Name: httpProtocolName(jhlog.HTTPProtocol(protocol)), Value: count})
		}
	}
	return result
}

func (c *collector) finalizeWebSocketAnalysis() *WebSocketAnalysis {
	totals := &c.webSocketTotals
	result := &WebSocketAnalysis{
		Opened: totals.opened, Closed: totals.closed, Failures: totals.failures,
		ActiveAtEnd: totals.activeAtEnd(), Reconnects: totals.reconnects,
		ConnectP50MS: totals.connect.percentile(0.50), ConnectP95MS: totals.connect.percentile(0.95),
		ConnectMaxMS: totals.connect.max, LifetimeP50MS: totals.lifetime.percentile(0.50),
		LifetimeP95MS: totals.lifetime.percentile(0.95), LifetimeMaxMS: totals.lifetime.max,
		TextMessages: totals.textMessages, BinaryMessages: totals.binaryMessages, ReceivedBytes: totals.receivedBytes,
	}
	for kind, count := range totals.failureKinds {
		if count > 0 {
			result.FailureKinds = append(result.FailureKinds, NamedValue{
				Name: webSocketFailureKindName(jhlog.WebSocketFailureKind(kind)), Value: count,
			})
		}
	}
	for code, count := range totals.closeCodes {
		result.CloseCodes = append(result.CloseCodes, NamedValue{Name: fmt.Sprint(code), Value: count})
	}
	sortNamed(result.FailureKinds)
	sortNamed(result.CloseCodes)
	for key, stats := range c.webSocketConnections {
		result.Connections = append(result.Connections, WebSocketConnectionStats{
			Route: key.route, Screen: key.screen, Operation: key.operation, Owner: key.owner,
			Opened: stats.opened, Closed: stats.closed, Failures: stats.failures,
			ActiveAtEnd: stats.activeAtEnd(), Reconnects: stats.reconnects,
			ConnectP50MS: stats.connect.percentile(0.50), ConnectP95MS: stats.connect.percentile(0.95),
			ConnectMaxMS: stats.connect.max, LifetimeP50MS: stats.lifetime.percentile(0.50),
			LifetimeP95MS: stats.lifetime.percentile(0.95), LifetimeMaxMS: stats.lifetime.max,
			TextMessages: stats.textMessages, BinaryMessages: stats.binaryMessages, ReceivedBytes: stats.receivedBytes,
		})
	}
	sort.Slice(result.Connections, func(i, j int) bool {
		left, right := result.Connections[i], result.Connections[j]
		if left.Failures != right.Failures {
			return left.Failures > right.Failures
		}
		if left.Opened != right.Opened {
			return left.Opened > right.Opened
		}
		if left.Route != right.Route {
			return left.Route < right.Route
		}
		return left.Owner < right.Owner
	})
	return result
}

func webSocketFailureKindName(kind jhlog.WebSocketFailureKind) string {
	switch kind {
	case jhlog.WebSocketFailureTimeout:
		return "timeout"
	case jhlog.WebSocketFailureConnection:
		return "connection"
	case jhlog.WebSocketFailureTLS:
		return "tls"
	case jhlog.WebSocketFailureProtocol:
		return "protocol"
	case jhlog.WebSocketFailureIO:
		return "io"
	case jhlog.WebSocketFailureOther:
		return "other"
	default:
		return "unknown"
	}
}

func (c *collector) finalizeDatabaseAnalysis() *DatabaseAnalysis {
	totals := &c.databaseTotals
	store := &c.databaseStatements
	correlations := c.databaseCorrelation.finalize()
	result := &DatabaseAnalysis{
		KnownSQLCalls: totals.knownSQL, Overall: databaseExecutionStats(&totals.overall),
		Main: databaseExecutionStats(&totals.main), Background: databaseExecutionStats(&totals.background),
		Telemetry:       databaseTelemetryStats(&c.databaseTelemetry),
		Transactions:    c.databaseTransactions.finalizeWithCorrelations(correlations.transactions),
		Scenarios:       c.databaseScenarios.finalize(),
		MainCorrelation: correlations.total.Main, BackgroundCorrelation: correlations.total.Background,
		PeakCallsPerSecond: totals.burst.peak,
		PeakWindowStartMS:  totals.burst.peakWindowStartMS, RapidRepeats: totals.rapidRepeats,
		DroppedStatementEvents:      store.droppedStatementEvents,
		DroppedContextEvents:        store.droppedContextEvents,
		EvictedStatementGroups:      store.evictedStatements,
		EvictedContextGroups:        store.evictedContexts,
		FrequencyEstimateError:      store.statementFrequency.estimatedError(),
		DroppedDBIntervals:          c.databaseCorrelation.database.dropped,
		EvictedDBIntervals:          c.databaseCorrelation.database.evicted,
		DroppedTransactionIntervals: c.databaseCorrelation.transactions.dropped,
		EvictedTransactionIntervals: c.databaseCorrelation.transactions.evicted,
		DroppedUIWindows:            c.databaseCorrelation.ui.dropped,
		EvictedUIWindows:            c.databaseCorrelation.ui.evicted,
		DroppedStallIntervals:       c.databaseCorrelation.stalls.dropped,
		EvictedStallIntervals:       c.databaseCorrelation.stalls.evicted,
		DroppedRelatedIntervals: saturatingUint64Sum(
			saturatingUint64Sum(c.databaseCorrelation.http.dropped, c.databaseCorrelation.workers.dropped),
			saturatingUint64Sum(c.databaseCorrelation.fileIO.dropped, c.databaseCorrelation.gc.dropped),
		),
		EvictedRelatedIntervals: saturatingUint64Sum(
			saturatingUint64Sum(c.databaseCorrelation.http.evicted, c.databaseCorrelation.workers.evicted),
			saturatingUint64Sum(c.databaseCorrelation.fileIO.evicted, c.databaseCorrelation.gc.evicted),
		),
	}
	for key, entry := range store.entries {
		stats := &entry.stats
		statementFingerprint := entry.statementFingerprint
		if statementFingerprint == 0 {
			statementFingerprint = databaseStatementFingerprint(key.query)
		}
		statement := DatabaseStatementStats{
			Query: key.query, Operation: key.operation,
			OperationCode:        databaseEvidenceOperationCode(key.operation),
			StatementFingerprint: statementFingerprint,
			Overall:              databaseExecutionStats(&stats.overall), Main: databaseExecutionStats(&stats.main),
			Background:         databaseExecutionStats(&stats.background),
			Telemetry:          databaseTelemetryStats(&entry.telemetry),
			PeakCallsPerSecond: stats.burst.peak, PeakWindowStartMS: stats.burst.peakWindowStartMS,
			RapidRepeats: stats.rapidRepeats, EstimatedCalls: entry.estimatedCalls,
			FrequencyEstimateError: entry.frequencyEstimateError,
		}
		statement.Contexts = make([]DatabaseStatementContextStats, 0, len(entry.contexts))
		for index := range entry.contexts {
			contextEntry := &entry.contexts[index]
			contextKey := contextEntry.key
			contextStats := &contextEntry.stats
			correlation := correlations.contexts[contextKey]
			context := DatabaseStatementContextStats{
				Source: contextKey.source, Framework: contextKey.framework, Screen: contextKey.screen,
				ContextOwner: contextKey.contextOwner, ContextOperation: contextKey.contextOperation,
				OperationID: contextKey.operationID, Process: contextKey.process,
				ProcessInstanceID: databaseIdentity(contextKey.processInstanceID),
				SessionID:         databaseIdentity(contextKey.sessionID),
				Overall:           databaseExecutionStats(&contextStats.overall), Main: databaseExecutionStats(&contextStats.main),
				Background:      databaseExecutionStats(&contextStats.background),
				MainCorrelation: correlation.Main, BackgroundCorrelation: correlation.Background,
				PeakCallsPerSecond: contextStats.burst.peak, PeakWindowStartMS: contextStats.burst.peakWindowStartMS,
				RapidRepeats: contextStats.rapidRepeats, EstimatedCalls: contextEntry.estimatedCalls,
				FrequencyEstimateError: contextEntry.frequencyEstimateError,
			}
			statement.Contexts = append(statement.Contexts, context)
			mergeDatabaseCorrelation(&statement.MainCorrelation, correlation.Main)
			mergeDatabaseCorrelation(&statement.BackgroundCorrelation, correlation.Background)
		}
		sortDatabaseContexts(statement.Contexts)
		result.Statements = append(result.Statements, statement)
	}
	sort.Slice(result.Statements, func(i, j int) bool {
		left, right := result.Statements[i], result.Statements[j]
		if left.Main.Calls != right.Main.Calls {
			return left.Main.Calls > right.Main.Calls
		}
		if left.Overall.P95DurationUS != right.Overall.P95DurationUS {
			return left.Overall.P95DurationUS > right.Overall.P95DurationUS
		}
		if left.Overall.Calls != right.Overall.Calls {
			return left.Overall.Calls > right.Overall.Calls
		}
		if left.Query != right.Query {
			return left.Query < right.Query
		}
		return left.Operation < right.Operation
	})
	c.databaseTotals = databaseAggregate{}
	c.databaseTelemetry = databaseTelemetryAggregate{}
	c.databaseStatements = databaseStatementStore{}
	c.databaseTransactions = databaseTransactionAccumulator{}
	c.databaseScenarios = databaseScenarioAccumulator{}
	c.databaseCorrelation = databaseCorrelationAccumulator{}
	applyDatabaseEvidence(result, c.databaseEvidence)
	return result
}

func sortDatabaseContexts(contexts []DatabaseStatementContextStats) {
	sort.Slice(contexts, func(i, j int) bool {
		left, right := contexts[i], contexts[j]
		if left.Main.Calls != right.Main.Calls {
			return left.Main.Calls > right.Main.Calls
		}
		if left.Overall.P95DurationUS != right.Overall.P95DurationUS {
			return left.Overall.P95DurationUS > right.Overall.P95DurationUS
		}
		if left.Overall.Calls != right.Overall.Calls {
			return left.Overall.Calls > right.Overall.Calls
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.ContextOperation != right.ContextOperation {
			return left.ContextOperation < right.ContextOperation
		}
		if left.ProcessInstanceID != right.ProcessInstanceID {
			return left.ProcessInstanceID < right.ProcessInstanceID
		}
		if left.SessionID != right.SessionID {
			return left.SessionID < right.SessionID
		}
		return left.OperationID < right.OperationID
	})
}

func databaseIdentity(id jhlog.ID128) string {
	if id.IsZero() {
		return "unknown"
	}
	return fmt.Sprintf("%x", id[:])
}

func databaseExecutionStats(stats *databaseExecutionAggregate) DatabaseExecutionStats {
	return DatabaseExecutionStats{
		Calls: stats.calls, Failures: stats.failures,
		P50DurationUS: stats.durations.percentile(0.50),
		P95DurationUS: stats.durations.percentile(0.95),
		MaxDurationUS: stats.durations.max, TotalDurationUS: stats.totalDuration,
		QuantilesApproximated: stats.durations.approximated(),
	}
}

func canonicalDatabaseStatementKey(query, source, framework, operation string) databaseStatementKey {
	key := databaseStatementKey{query: query, operation: operation}
	if datavalue.IsUnknown(query) {
		key.fallbackSource = source
		key.fallbackFramework = framework
	}
	return key
}

func databaseFrameworkName(value jhlog.DatabaseFramework) string {
	switch value {
	case jhlog.DatabaseFrameworkSQLite:
		return "SQLite"
	case jhlog.DatabaseFrameworkSupportSQLite:
		return "SupportSQLite"
	case jhlog.DatabaseFrameworkRoom:
		return "Room"
	case jhlog.DatabaseFrameworkCustom:
		return "Custom adapter"
	default:
		return "unknown"
	}
}

func databaseOperationName(value jhlog.DatabaseOperation) string {
	switch value {
	case jhlog.DatabaseOperationQuery:
		return "чтение"
	case jhlog.DatabaseOperationInsert:
		return "вставка"
	case jhlog.DatabaseOperationUpdate:
		return "обновление"
	case jhlog.DatabaseOperationDelete:
		return "удаление"
	case jhlog.DatabaseOperationExecute:
		return "выполнение"
	case jhlog.DatabaseOperationStatement:
		return "подготовка"
	default:
		return "неизвестно"
	}
}

func maxHTTPConcurrency(intervals []httpInterval) (uint64, uint64) {
	if len(intervals) == 0 {
		return 0, 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].logIndex != intervals[j].logIndex {
			return intervals[i].logIndex < intervals[j].logIndex
		}
		if intervals[i].startMS != intervals[j].startMS {
			return intervals[i].startMS < intervals[j].startMS
		}
		return intervals[i].endMS < intervals[j].endMS
	})
	ends := make([]uint64, 0, min(len(intervals), 64))
	currentLog := intervals[0].logIndex
	var peak uint64
	var peakAtMS uint64
	for _, interval := range intervals {
		if interval.logIndex != currentLog {
			ends = ends[:0]
			currentLog = interval.logIndex
		}
		for len(ends) > 0 && ends[0] <= interval.startMS {
			ends = popUint64MinHeap(ends)
		}
		ends = pushUint64MinHeap(ends, interval.endMS)
		if uint64(len(ends)) > peak {
			peak = uint64(len(ends))
			peakAtMS = interval.startMS
		}
	}
	return peak, peakAtMS
}

func pushUint64MinHeap(values []uint64, value uint64) []uint64 {
	values = append(values, value)
	index := len(values) - 1
	for index > 0 {
		parent := (index - 1) / 2
		if values[parent] <= value {
			break
		}
		values[index] = values[parent]
		index = parent
	}
	values[index] = value
	return values
}

func popUint64MinHeap(values []uint64) []uint64 {
	last := values[len(values)-1]
	values = values[:len(values)-1]
	if len(values) == 0 {
		return values
	}
	index := 0
	for {
		left := index*2 + 1
		if left >= len(values) {
			break
		}
		right := left + 1
		child := left
		if right < len(values) && values[right] < values[left] {
			child = right
		}
		if values[child] >= last {
			break
		}
		values[index] = values[child]
		index = child
	}
	values[index] = last
	return values
}

func httpFailurePhaseName(phase jhlog.HTTPFailurePhase) string {
	return [...]string{"unknown", "call", "queue", "dns", "connect", "tls", "request", "response", "cancelled"}[phase]
}

func httpFailureKindName(kind jhlog.HTTPFailureKind) string {
	return [...]string{"unknown", "dns", "timeout", "connection", "tls", "protocol", "cancelled", "io", "other"}[kind]
}

func httpProtocolName(protocol jhlog.HTTPProtocol) string {
	return [...]string{"unknown", "http/1.0", "http/1.1", "http/2", "http/3"}[protocol]
}

func mergeFrameWindow(stats *ScreenStats, window *jhlog.UIWindowEvent) {
	if len(stats.FrameDurationBuckets) == 0 {
		stats.FrameDurationBuckets = make([]uint64, jhlog.UIFrameHistogramBucketCount)
		stats.FrameSource = uiFrameSourceName(window.Source)
		stats.FrameDeadlineUS = window.FrameDeadlineUS
		stats.FrameDeadlineStatus = "consistent"
	} else {
		if stats.FrameSource != uiFrameSourceName(window.Source) {
			stats.FrameSource = "mixed"
		}
		if stats.FrameDeadlineUS != window.FrameDeadlineUS {
			stats.FrameDeadlineUS = 0
			stats.FrameDeadlineStatus = "mixed"
		}
	}
	for index, count := range window.FrameDurationBuckets {
		stats.FrameDurationBuckets[index] = saturatingUint64Sum(stats.FrameDurationBuckets[index], count)
	}
	stats.FrameDistributionState = "mergeable_histogram_v2"
}

func uiFrameSourceName(source jhlog.UIFrameSource) string {
	switch source {
	case jhlog.UIFrameSourceJankStats:
		return "jankstats"
	case jhlog.UIFrameSourceChoreographer:
		return "choreographer"
	default:
		return "unknown"
	}
}

func ioOperationName(operation jhlog.IOOperationKind) string {
	switch operation {
	case jhlog.IOOperationFileRead:
		return "file_read"
	case jhlog.IOOperationFileWrite:
		return "file_write"
	case jhlog.IOOperationFileSync:
		return "file_sync"
	case jhlog.IOOperationContentRead:
		return "content_read"
	case jhlog.IOOperationContentWrite:
		return "content_write"
	default:
		return "unknown"
	}
}

func (s *gaugeStats) add(value, count, sum, max uint64, mode jhlog.MetricMode) {
	if count == 0 {
		count = 1
	}
	if sum == 0 {
		sum = value
	}
	if max == 0 {
		max = value
	}
	if mode != jhlog.MetricModeUnknown {
		s.mode = mode
	}
	if s.mode == jhlog.MetricModeUnknown {
		s.mode = jhlog.MetricModeAverage
	}
	s.count++
	s.count += count - 1
	s.last = value
	switch s.mode {
	case jhlog.MetricModeLast, jhlog.MetricModeState:
		s.total = value
		s.max = max
	case jhlog.MetricModeBooleanRate:
		s.total += sum
		if max > s.max {
			s.max = max
		}
	default:
		s.total += sum
		if max > s.max {
			s.max = max
		}
	}
}

func (s *gaugeStats) value() uint64 {
	if s.count == 0 {
		return 0
	}
	switch s.mode {
	case jhlog.MetricModeLast, jhlog.MetricModeState:
		return s.last
	case jhlog.MetricModeBooleanRate:
		return (s.total * 100) / s.count
	}
	return s.total / s.count
}

func (s *gaugeStats) extra() string {
	switch s.mode {
	case jhlog.MetricModeLast:
		return fmt.Sprintf("последнее=%d наблюдений=%d", s.last, s.count)
	case jhlog.MetricModeState:
		return fmt.Sprintf("состояние=%d наблюдений=%d", s.last, s.count)
	case jhlog.MetricModeBooleanRate:
		return fmt.Sprintf("доля включённого состояния=%d включено=%d наблюдений=%d", s.value(), s.total, s.count)
	default:
		return fmt.Sprintf("среднее=%d максимум=%d наблюдений=%d", s.value(), s.max, s.count)
	}
}

func metricModeForGauge(name string) jhlog.MetricMode {
	metric := strings.ToLower(strings.TrimSpace(name))
	switch metric {
	case "battery.status",
		"battery.plugged",
		"battery.health",
		"device.thermal.status",
		"memory.trim.last_level":
		return jhlog.MetricModeState
	case "battery.charging",
		"device.power_save_mode",
		"device.interactive",
		"device.idle_mode",
		"network.request.connection_released":
		return jhlog.MetricModeBooleanRate
	}
	if strings.HasSuffix(metric, ".last_id") ||
		strings.Contains(metric, ".last.") ||
		strings.HasSuffix(metric, ".last_level") ||
		strings.HasSuffix(metric, ".core_count") ||
		strings.HasSuffix(metric, ".max_kb") {
		return jhlog.MetricModeLast
	}
	return jhlog.MetricModeAverage
}

type memoryLeakStats struct {
	className            string
	holder               string
	screen               string
	operation            string
	count                uint64
	maxAgeMs             uint64
	timeOnlyCount        uint64
	afterExplicitGCCount uint64
}

type retentionDataQuality struct {
	runtimeLoss            uint64
	runtimeMayBeIncomplete bool
	dictionaryDegraded     bool
	heapDegraded           bool
	runtimeNotes           []string
	dictionaryNotes        []string
	heapNotes              []string
}

func normalizeFilter(filter Filter) Filter {
	return Filter{
		RouteContains:  strings.ToLower(filter.RouteContains),
		ScreenContains: strings.ToLower(filter.ScreenContains),
		OwnerContains:  strings.ToLower(filter.OwnerContains),
		ClassContains:  strings.ToLower(filter.ClassContains),
	}
}

func filterActive(filter Filter) bool {
	return filter.RouteContains != "" ||
		filter.ScreenContains != "" ||
		filter.OwnerContains != "" ||
		filter.ClassContains != ""
}

func containsFilter(value string, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), needle)
}

func containsAnyFilter(needle string, values ...string) bool {
	if needle == "" {
		return true
	}
	for _, value := range values {
		if containsFilter(value, needle) {
			return true
		}
	}
	return false
}

func (c *collector) eventContext(screenOverride, ownerOverride string) SignalContextStats {
	return SignalContextStats{
		Screen:    attrValue(firstKnown(screenOverride, c.currentAttrScreen)),
		Operation: attrValue(c.operationAnalysis.activeName(c.currentOperationID)),
		Owner:     attrValue(firstKnown(ownerOverride, c.currentAttrOwner)),
	}
}

func (c *collector) matchesFilters(route string, context SignalContextStats, classCandidates []string, ownerCandidates ...string) bool {
	if !containsFilter(route, c.filter.RouteContains) {
		return false
	}
	if !containsFilter(context.Screen, c.filter.ScreenContains) {
		return false
	}
	if c.filter.ClassContains != "" && !containsAnyFilter(c.filter.ClassContains, classCandidates...) {
		return false
	}
	if c.filter.OwnerContains != "" {
		candidates := append([]string{context.Owner}, ownerCandidates...)
		if !containsAnyFilter(c.filter.OwnerContains, candidates...) {
			return false
		}
	}
	return true
}

func (c *collector) add(dict map[uint64]string, event jhlog.Event) {
	if event.Dictionary != nil {
		if event.Dictionary.Kind == jhlog.DictStableSymbol && event.Dictionary.Value != "" {
			if _, exists := c.stableSymbols.embedded[event.Dictionary.ID]; !exists {
				c.stableSymbols.embedded[event.Dictionary.ID] = event.Dictionary.Value
			}
		}
		if event.Dictionary.Value == "__jh_dictionary_overflow__" {
			c.dictionaryOverflow++
		}
		return
	}
	if !event.Type.IsSemanticData() {
		return
	}
	c.applyAttribution(dict, event.Attribution)
	c.summary.EventCount++
	if !c.seenEvent {
		c.seenEvent = true
		c.firstTime = event.TimeMS
		c.lastTime = event.TimeMS
	} else {
		if event.TimeMS < c.firstTime {
			c.firstTime = event.TimeMS
		}
		if event.TimeMS > c.lastTime {
			c.lastTime = event.TimeMS
		}
	}
	if !c.logSeen {
		c.logSeen = true
		c.logFirst = event.TimeMS
		c.logLast = event.TimeMS
	} else {
		if event.TimeMS < c.logFirst {
			c.logFirst = event.TimeMS
		}
		if event.TimeMS > c.logLast {
			c.logLast = event.TimeMS
		}
	}
	if event.Operation != nil {
		c.operationAnalysis.recordLifecycle(dict, event, c.currentAttrScreen, c.filter)
	} else if event.Database == nil && event.DatabaseTransaction == nil {
		c.operationAnalysis.recordSignal(event, c.currentOperationID, c.currentAttrOwner)
	}
	switch {
	case event.Session != nil:
		c.summary.CollectorSessions++
		c.summary.CollectorFlagsAny |= event.Session.CollectorFlags
		if event.Session.CollectorFlags&uint64(jhlog.CollectorDatabase) != 0 {
			c.summary.DatabaseCoverage.RuntimeEnabledSessions++
		}
		if event.Session.CollectorFlags&uint64(jhlog.CollectorWorker) != 0 {
			c.enableWorkerCorrelation()
		}
		if c.summary.CollectorSessions == 1 {
			c.summary.CollectorFlagsAll = event.Session.CollectorFlags
		} else {
			c.summary.CollectorFlagsAll &= event.Session.CollectorFlags
		}
		c.currentAppVersion = jhlog.ResolveSymbol(dict, event.Session.AppVersionRef)
		c.currentBuild = jhlog.ResolveSymbol(dict, event.Session.BuildRef)
		c.currentDevice = jhlog.ResolveSymbol(dict, event.Session.DeviceRef)
		c.currentSDK = fmt.Sprintf("api-%d", event.Session.SDKInt)
		c.currentProcess = firstNonEmpty(event.Session.ProcessName, "unknown")
		c.currentAndroid = jhlog.ResolveSymbol(dict, event.Session.AndroidReleaseRef)
		c.currentPatch = jhlog.ResolveSymbol(dict, event.Session.SecurityPatchRef)
		c.currentPrimaryABI = jhlog.ResolveSymbol(dict, event.Session.PrimaryABIRef)
		c.currentABIs = jhlog.ResolveSymbol(dict, event.Session.SupportedABIsRef)
		c.currentMaker = jhlog.ResolveSymbol(dict, event.Session.ManufacturerRef)
		c.currentBrand = jhlog.ResolveSymbol(dict, event.Session.BrandRef)
		c.currentHardware = jhlog.ResolveSymbol(dict, event.Session.HardwareRef)
		c.currentBoard = jhlog.ResolveSymbol(dict, event.Session.BoardRef)
		c.currentProduct = jhlog.ResolveSymbol(dict, event.Session.ProductRef)
		c.currentRootKnown = true
		c.currentRooted = event.Session.DeviceRooted
		c.currentCohortDirty = true
		c.summary.DeviceRootKnown = true
		c.summary.DeviceRooted = event.Session.DeviceRooted
		c.appVersions[c.currentAppVersion]++
		c.builds[c.currentBuild]++
		c.devices[c.currentDevice]++
		c.sdks[c.currentSDK]++
		c.processSamples[c.currentProcess]++
	case event.HTTP != nil:
		route := attrValue(jhlog.ResolveSymbol(dict, event.HTTP.RouteRef))
		service := attrValue(jhlog.ResolveSymbol(dict, event.HTTP.ServiceRef))
		initiator := attrValue(c.resolveOwnerRef(dict, event.HTTP.InitiatorRef))
		owner := c.currentAttrOwner
		context := c.eventContext("", owner)
		if !c.matchesFilters(route, context, nil, owner, initiator) {
			return
		}
		c.databaseCorrelation.addHTTP(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		c.summary.HTTPCount++
		if c.workerCorrelationOn {
			c.recordWorkerHTTP(event)
		}
		c.networkTotals.add(event.HTTP, event.Flags, c.currentLogIndex, event.TimeMS, true)
		routeStats := c.networkRoutes[route]
		if routeStats == nil {
			routeStats = &httpAggregate{}
			c.networkRoutes[route] = routeStats
		}
		routeStats.add(event.HTTP, event.Flags, c.currentLogIndex, event.TimeMS, true)
		routeStats.burst.add(c.currentLogIndex, event.TimeMS)
		if routeStats.ownerSample == "" || routeStats.ownerSample == "unknown" {
			routeStats.ownerSample = firstKnown(initiator, owner)
		}
		if routeStats.serviceSample == "" || routeStats.serviceSample == "unknown" {
			routeStats.serviceSample = service
		}
		if routeStats.initiatorSample == "" || routeStats.initiatorSample == "unknown" {
			routeStats.initiatorSample = initiator
		}
		callKey := networkCallKey{
			route: route, service: service, initiator: initiator,
			screen: context.Screen, operation: context.Operation, owner: context.Owner,
		}
		callStats := c.networkCalls[callKey]
		if callStats == nil {
			callStats = &httpAggregate{}
			c.networkCalls[callKey] = callStats
		}
		callStats.add(event.HTTP, event.Flags, c.currentLogIndex, event.TimeMS, false)
		if event.HTTP.StatusCode != 0 {
			c.networkStatusCodes[event.HTTP.StatusCode]++
		}
		if event.HTTP.FailurePhase > jhlog.HTTPFailurePhaseUnknown && int(event.HTTP.FailurePhase) < len(c.networkFailurePhases) {
			c.networkFailurePhases[event.HTTP.FailurePhase]++
		}
		if event.HTTP.FailureKind > jhlog.HTTPFailureKindUnknown && int(event.HTTP.FailureKind) < len(c.networkFailureKinds) {
			c.networkFailureKinds[event.HTTP.FailureKind]++
		}
		if int(event.HTTP.Protocol) < len(c.networkProtocols) {
			c.networkProtocols[event.HTTP.Protocol]++
		}
		if httpEventFailed(event.HTTP, event.Flags) {
			c.summary.HTTPFailed++
		}
		addOwner(c.ownerStats, firstKnown(initiator, owner), "http", event.HTTP.DurationMS, "")
		contextKey := c.contextKey("", owner)
		contextStats := c.ensureSignalContext(contextKey)
		contextStats.HTTPCount++
		contextStats.RouteSample = firstNonEmpty(contextStats.RouteSample, route)
		c.sampleSet(c.signalContextHTTPDurations, contextKey).add(event.HTTP.DurationMS)
		if httpEventFailed(event.HTTP, event.Flags) {
			contextStats.HTTPFailed++
		}
		failed := httpEventFailed(event.HTTP, event.Flags)
		slow := event.Flags&uint64(jhlog.FlagHTTPSlow) != 0
		if failed || slow {
			c.addProblemWindow(context, "http_slow_or_failed", event.HTTP.DurationMS, 1, event.HTTP.DurationMS)
		}
	case event.WebSocket != nil:
		route := attrValue(jhlog.ResolveSymbol(dict, event.WebSocket.RouteRef))
		owner := c.currentAttrOwner
		context := c.eventContext("", owner)
		if !c.matchesFilters(route, context, nil, owner) {
			return
		}
		c.markCohort()
		c.webSocketTotals.add(event.WebSocket)
		key := webSocketKey{route: route, screen: context.Screen, operation: context.Operation, owner: context.Owner}
		stats := c.webSocketConnections[key]
		if stats == nil {
			stats = &webSocketAggregate{}
			c.webSocketConnections[key] = stats
		}
		stats.add(event.WebSocket)
		addOwner(c.ownerStats, owner, "websocket", event.WebSocket.DurationMS, "")
		if event.WebSocket.Stage == jhlog.WebSocketStageFailed {
			c.addProblemWindow(context, "websocket_failure", event.WebSocket.DurationMS, 1, event.WebSocket.DurationMS)
		}
	case event.Database != nil:
		query := attrValue(jhlog.ResolveSymbol(dict, event.Database.QueryRef))
		source := attrValue(c.resolveOwnerRef(dict, event.Database.SourceRef))
		context := c.eventContext("", "")
		if !c.matchesFilters("", context, []string{source}, source) {
			return
		}
		c.markCohort()
		c.databaseTotals.add(event.Database, event.Flags, c.currentLogIndex, event.TimeMS)
		c.databaseTelemetry.add(event.Database)
		framework := databaseFrameworkName(event.Database.Framework)
		operation := databaseOperationName(event.Database.Operation)
		statementKey := canonicalDatabaseStatementKey(query, source, framework, operation)
		contextKey := databaseContextKey{
			statement: statementKey, source: source, framework: framework,
			screen: context.Screen, contextOwner: context.Owner,
			contextOperation: context.Operation, operationID: c.currentOperationID,
			process: c.currentProcess, processInstanceID: c.currentProcessID,
			sessionID: c.currentSessionID,
		}
		estimatedCalls := c.databaseStatements.add(
			statementKey, contextKey, event.Database, event.Flags, c.currentLogIndex, event.TimeMS,
		)
		c.databaseScenarios.add(
			statementKey, contextKey, event.Database, event.Flags, c.currentLogIndex,
			databaseEventTimeUS(event),
		)
		c.databaseCorrelation.addDatabase(contextKey, databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event, estimatedCalls)
		c.operationAnalysis.recordDatabaseStatement(
			c.currentOperationID, query, source, operation, event.Database, event.Flags,
		)
		addOwner(c.ownerStats, source, "database", event.Database.DurationUS/1_000, "")
		if event.Database.Outcome == jhlog.DatabaseOutcomeFailure ||
			(event.Flags&uint64(jhlog.FlagThreadMain) != 0 && event.Database.DurationUS >= 16_000) {
			c.addProblemWindow(context, "database_slow_or_failed", event.Database.DurationUS/1_000, 1, event.Database.DurationUS/1_000)
		}
	case event.DatabaseTransaction != nil:
		source := attrValue(c.resolveOwnerRef(dict, event.DatabaseTransaction.SourceRef))
		context := c.eventContext("", "")
		if !c.matchesFilters("", context, []string{source}, source) {
			return
		}
		c.markCohort()
		transactionKey := databaseTransactionScopeKey(
			c.currentProcessID, c.currentLogIndex, event.DatabaseTransaction.TransactionID,
		)
		c.databaseCorrelation.addTransaction(transactionKey, databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.databaseTransactions.add(
			event.DatabaseTransaction,
			event.Flags,
			c.currentLogIndex,
			source,
			context,
			c.currentProcess,
			c.currentProcessID,
			c.currentSessionID,
		)
		if event.DatabaseTransaction.Stage == jhlog.DatabaseTransactionTerminal {
			addOwner(c.ownerStats, source, "database_transaction", event.DatabaseTransaction.DurationUS/1_000, "")
		}
	case event.ProcessState != nil:
		c.androidAnalysis.addProcessState(event)
	case event.AndroidComponent != nil:
		component := attrValue(c.resolveOwnerRef(dict, event.AndroidComponent.ComponentRef))
		action := attrValue(jhlog.ResolveSymbol(dict, event.AndroidComponent.ActionRef))
		context := c.eventContext("", component)
		if c.matchesFilters("", context, []string{component}, component) {
			c.androidAnalysis.addComponent(component, action, event)
		}
	case event.BinderTransaction != nil:
		descriptor := ""
		if !event.BinderTransaction.DescriptorRef.IsUnknown() {
			descriptor = strings.TrimSpace(jhlog.ResolveSymbol(dict, event.BinderTransaction.DescriptorRef))
		}
		method := ""
		if !event.BinderTransaction.MethodRef.IsUnknown() {
			method = strings.TrimSpace(jhlog.ResolveSymbol(dict, event.BinderTransaction.MethodRef))
		}
		context := c.eventContext("", attrValue(descriptor))
		if c.matchesFilters("", context, []string{descriptor}, descriptor, method) {
			c.androidAnalysis.addBinder(descriptor, method, event)
		}
	case event.UIWindow != nil:
		c.runtimeAnalysis.gc.addUIWindow(event, c.currentLogIndex)
		screen := c.currentAttrScreen
		context := c.eventContext(screen, "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.databaseCorrelation.addUIWindow(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		stats := c.screenStats[screen]
		if stats == nil {
			stats = &ScreenStats{Screen: screen}
			c.screenStats[screen] = stats
		}
		stats.WindowCount++
		stats.WindowMS += event.UIWindow.WindowMS
		stats.Frames += event.UIWindow.FrameCount
		stats.JankyFrames += event.UIWindow.JankCount
		if fpsWindowReliable(event.UIWindow) {
			windowFPS := fps(event.UIWindow.FrameCount, event.UIWindow.WindowMS)
			stats.FPSMeasuredFrames += event.UIWindow.FrameCount
			stats.FPSMeasuredWindowMS += event.UIWindow.WindowMS
			stats.FPSMeasuredWindowCount++
			if stats.MinFPS == 0 || windowFPS < stats.MinFPS {
				stats.MinFPS = windowFPS
			}
			c.summary.UIFPSMeasuredFrames += event.UIWindow.FrameCount
			c.summary.UIFPSMeasuredWindowMS += event.UIWindow.WindowMS
			c.summary.UIFPSMeasuredWindowCount++
			if c.summary.UIMinFPS == 0 || windowFPS < c.summary.UIMinFPS {
				c.summary.UIMinFPS = windowFPS
			}
		}
		mergeFrameWindow(stats, event.UIWindow)
		c.summary.UIFrames += event.UIWindow.FrameCount
		c.summary.UIJank += event.UIWindow.JankCount
		c.summary.UIWindowMS += event.UIWindow.WindowMS
		contextStats := c.ensureSignalContext(c.contextKey(screen, ""))
		contextStats.UIWindows++
		contextStats.UIFrames += event.UIWindow.FrameCount
		contextStats.UIJank += event.UIWindow.JankCount
		problem := event.Flags&uint64(jhlog.FlagUIProblem) != 0
		if problem {
			c.addProblemWindow(
				context,
				"ui_jank",
				event.UIWindow.WindowMS,
				maxUint64(event.UIWindow.JankCount, 1),
				maxUint64(event.UIWindow.P95MS, event.UIWindow.P99MS),
			)
		}
	case event.Stall != nil:
		owner := c.currentAttrOwner
		stack := jhlog.ResolveSymbol(dict, event.Stall.StackRef)
		if c.isHeapDumpStall(event.TimeMS, owner) {
			owner = "jankhunter.heap_dump"
		}
		context := c.eventContext("", owner)
		if !c.matchesFilters("", context, nil, owner) {
			return
		}
		c.databaseCorrelation.addStall(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		c.summary.StallCount++
		if event.Stall.DurationMS > c.summary.StallMaxMS {
			c.summary.StallMaxMS = event.Stall.DurationMS
		}
		addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
		contextStats := c.ensureSignalContext(strings.Join([]string{context.Screen, context.Operation, context.Owner}, "\x00"))
		contextStats.StallCount++
		if event.Stall.DurationMS > contextStats.StallMaxMS {
			contextStats.StallMaxMS = event.Stall.DurationMS
		}
		c.addProblemWindow(context, "main_thread_stall", event.Stall.DurationMS, 1, event.Stall.DurationMS)
	case event.Context != nil:
		c.summary.ContextCount++
		c.currentNetwork = jhlog.NetworkName(event.Context.Network)
		c.currentCohortDirty = true
		c.markCohort()
		c.summary.BatteryLastPct = event.Context.BatteryPct
		c.summary.BatteryStateLast = event.Context.BatteryState
		c.summary.BatteryTempDeciC = event.Context.BatteryTempDeciC
		c.summary.AvailMemoryLastKB = event.Context.AvailMemoryKB
		c.summary.TotalMemoryKB = event.Context.TotalMemoryKB
		c.summary.FreeStorageKB = event.Context.FreeStorageKB
		c.summary.TotalStorageKB = event.Context.TotalStorageKB
		c.summary.NetworkMetered = event.Context.NetworkMetered
		c.summary.NetworkValidated = event.Context.NetworkValidated
		c.summary.NetworkVPN = event.Context.NetworkVPN
		if c.summary.BatteryMinPct == 0 || event.Context.BatteryPct < c.summary.BatteryMinPct {
			c.summary.BatteryMinPct = event.Context.BatteryPct
		}
		if c.summary.AvailMemoryMinKB == 0 || event.Context.AvailMemoryKB < c.summary.AvailMemoryMinKB {
			c.summary.AvailMemoryMinKB = event.Context.AvailMemoryKB
		}
		if event.Context.LowMemory {
			c.summary.LowMemoryCount++
		}
		c.recordTraffic(event.Context.RxBytes, event.Context.TxBytes)
		c.networkSamples[c.currentNetwork]++
	case event.Memory != nil:
		context := c.eventContext("", "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.markCohort()
		point := workerPoint{
			logIndex: c.currentLogIndex, timeMS: event.TimeMS, value: event.Memory.PSSKB, count: 1,
		}
		if c.workerCorrelationOn {
			c.workerMemory = append(c.workerMemory, point)
		} else {
			c.workerPriorMemory = point
			c.workerPriorMemorySet = true
		}
		c.summary.MemoryCount++
		if event.Memory.PSSKB > c.summary.MemoryMaxKB {
			c.summary.MemoryMaxKB = event.Memory.PSSKB
		}
		contextStats := c.ensureSignalContext(c.contextKey("", ""))
		if event.Memory.PSSKB > contextStats.MemoryMaxKB {
			contextStats.MemoryMaxKB = event.Memory.PSSKB
		}
	case event.ProcessExit != nil:
		process := firstKnown(jhlog.ResolveSymbol(dict, event.ProcessExit.ProcessRef), c.currentProcess)
		key := fmt.Sprintf("%d\x00%s", event.ProcessExit.Reason, process)
		stats := c.processExitStats[key]
		if stats == nil {
			label, _ := processExitReason(event.ProcessExit.Reason)
			stats = &ProcessExitStats{Reason: event.ProcessExit.Reason, ReasonLabel: label, Process: attrValue(process)}
			c.processExitStats[key] = stats
		}
		stats.Count++
		if event.ProcessExit.TimestampUnixMS >= stats.LatestTimestampUnixMS {
			stats.LatestTimestampUnixMS = event.ProcessExit.TimestampUnixMS
			stats.Importance = event.ProcessExit.Importance
		}
		stats.MaxPSSKB = maxUint64(stats.MaxPSSKB, event.ProcessExit.PSSKB)
		stats.MaxRSSKB = maxUint64(stats.MaxRSSKB, event.ProcessExit.RSSKB)
	case event.IO != nil:
		context := c.eventContext("", "")
		source := attrValue(c.resolveOwnerRef(dict, event.IO.SourceRef))
		if !c.matchesFilters("", context, nil, source, context.Owner) {
			return
		}
		c.databaseCorrelation.addIO(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, event)
		c.markCohort()
		if c.workerCorrelationOn {
			c.recordWorkerIO(event)
		}
		operation := ioOperationName(event.IO.Operation)
		mainThread := event.Flags&uint64(jhlog.FlagThreadMain) != 0
		key := strings.Join([]string{context.Screen, context.Operation, context.Owner}, "\x00") +
			fmt.Sprintf("\x00%s\x00%s\x00%t", source, operation, mainThread)
		stats := c.ioStats[key]
		if stats == nil {
			stats = &ioAggregate{stats: IOStats{
				Operation: operation, Source: source, MainThread: mainThread,
				Screen: context.Screen, ContextOperation: context.Operation, Owner: context.Owner,
			}}
			c.ioStats[key] = stats
		}
		stats.add(event.IO, event.Flags, c.currentLogIndex, event.TimeMS)
		c.ioAnalysis.add(
			event.IO,
			event.Flags,
			c.currentLogIndex,
			ioEventEndUS(event),
		)
	case event.Retained != nil:
		className := c.deobfuscate(jhlog.ResolveSymbol(dict, event.Retained.ClassRef))
		holder := c.resolveOwnerRef(dict, event.Retained.HolderRef)
		context := c.eventContext("", "")
		owner := context.Owner
		holder = firstKnown(holder, context.Owner)
		if !c.matchesFilters("", context, []string{className}, holder, owner) {
			return
		}
		c.markCohort()
		c.summary.Retained += event.Retained.Count
		stats := c.retainedClasses[className]
		if stats == nil {
			stats = &retainedClassStats{}
			c.retainedClasses[className] = stats
		}
		stats.count += event.Retained.Count
		if event.Retained.AgeMS > stats.maxAgeMs {
			stats.maxAgeMs = event.Retained.AgeMS
		}
		c.retainedAgeBuckets[retainedAgeBucket(event.Retained.AgeMS)] += event.Retained.Count
		c.addMemoryLeakSuspect(
			className,
			holder,
			context,
			event.Retained.AgeMS,
			event.Retained.Count,
			event.Retained.Evidence,
			true,
		)
		addOwner(c.ownerStats, className, "retained_object", event.Retained.AgeMS, "")
		c.addProblemWindow(
			context,
			"retained_object",
			event.Retained.AgeMS,
			maxUint64(event.Retained.Count, 1),
			event.Retained.AgeMS,
		)
	case event.LogSpam != nil:
		key := c.contextKey("", "")
		context := c.signalContextFromKey(key)
		source := jhlog.ResolveSymbol(dict, event.LogSpam.SourceRef)
		if !c.matchesFilters("", context, []string{source}, context.Owner) {
			return
		}
		c.markCohort()
		level := logLevelName(event.LogSpam.Level)
		logKey := key + "\x00" + source + "\x00" + level
		stats := c.logSpamStats[logKey]
		if stats == nil {
			stats = &LogSpamStats{
				Screen: context.Screen, Operation: context.Operation, Owner: context.Owner,
				Source: source, Level: level,
			}
			c.logSpamStats[logKey] = stats
		}
		stats.Count += event.LogSpam.Count
		contextStats := c.ensureSignalContext(key)
		contextStats.LogSpam += event.LogSpam.Count
		if event.LogSpam.Count >= canonicalLogSpamCount {
			c.addProblemWindow(
				context,
				"log_spam",
				canonicalLogSpamWindowMS,
				event.LogSpam.Count,
				event.LogSpam.Count,
			)
		}
	case event.Problem != nil:
		key := c.contextKey("", "")
		context := c.signalContextFromKey(key)
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		kind := jhlog.ResolveSymbol(dict, event.Problem.KindRef)
		c.addProblemWindow(context, kind, event.Problem.WindowMS, event.Problem.Count, event.Problem.MaxMS)
	case event.Worker != nil:
		c.recordWorker(dict, event)
	case event.RuntimeCall != nil:
		caller := c.currentAttrOwner
		callee := c.resolveOwnerRef(dict, event.RuntimeCall.CalleeRef)
		key := c.contextKey("", "")
		context := c.signalContextFromKey(key)
		if !c.matchesFilters("", context, []string{caller, callee}, caller, callee) {
			return
		}
		c.markCohort()
		callKey := key + "\x00" + caller + "\x00" + callee
		stats := c.runtimeCallStats[callKey]
		if stats == nil {
			stats = &RuntimeCallStats{
				Screen: context.Screen, Operation: context.Operation,
				Caller: caller, Callee: callee,
			}
			c.runtimeCallStats[callKey] = stats
		}
		stats.Count += event.RuntimeCall.Count
		stats.TotalMS += event.RuntimeCall.TotalMS
		if event.RuntimeCall.MaxMS > stats.MaxMS {
			stats.MaxMS = event.RuntimeCall.MaxMS
		}
	case event.Metric != nil:
		c.markCohort()
		name := jhlog.ResolveSymbol(dict, event.Metric.MetricRef)
		if event.Type == jhlog.EventCounter && event.Metric.MetricRef.Stable {
			name = c.resolveOwnerRef(dict, event.Metric.MetricRef)
		}
		context := c.eventContext("", "")
		c.databaseCorrelation.addGC(databaseTimelineContext{
			screen: context.Screen, operation: context.Operation, operationID: c.currentOperationID,
		}, name, event)
		if event.Type == jhlog.EventCounter && name == "jankhunter.heap_dump.created.count" && event.Metric.Value > 0 {
			c.lastHeapDumpMS = event.TimeMS
		}
		if event.Type == jhlog.EventGauge {
			mode := event.Metric.Mode
			if mode == jhlog.MetricModeUnknown {
				mode = metricModeForGauge(name)
			}
			c.gauge(name).add(event.Metric.Value, event.Metric.Count, event.Metric.Sum, event.Metric.Max, mode)
		} else {
			c.counterValues[name] += event.Metric.Value
		}
		if c.workerCorrelationOn {
			c.recordWorkerMetric(name, event)
		}
		c.runtimeAnalysis.addMetric(name, event, c.currentLogIndex)
	}
}

const databaseStatementGroupLimit = 4_096

func (c *collector) isHeapDumpStall(eventTimeMS uint64, owner string) bool {
	if c.lastHeapDumpMS == 0 || eventTimeMS < c.lastHeapDumpMS || !isLikelySystemClass(owner) {
		return false
	}
	return eventTimeMS-c.lastHeapDumpMS <= heapDumpStallAttributionWindowMS
}

func (c *collector) markCohort() {
	if c.currentCohortDirty {
		c.currentCohortKey = fmt.Sprintf(
			"app=%s build=%s sdk=%s device=%s process=%s network=%s root=%s",
			c.currentAppVersion,
			c.currentBuild,
			c.currentSDK,
			c.currentDevice,
			c.currentProcess,
			c.currentNetwork,
			rootCohortValue(c.currentRootKnown, c.currentRooted),
		)
		c.currentCohortDirty = false
	}
	c.cohortSamples[c.currentCohortKey]++
}

func (c *collector) resolveOwnerRef(dict map[uint64]string, ref jhlog.SymbolRef) string {
	if !ref.Stable {
		return c.deobfuscate(jhlog.ResolveSymbol(dict, ref))
	}
	if embedded := c.stableSymbols.embedded[ref.ID]; embedded != "" {
		return c.deobfuscate(embedded)
	}
	canonical := jhlog.ResolveSymbol(dict, ref)
	c.stableSymbols.unresolved[canonical] = struct{}{}
	return canonical
}

func (c *collector) validateStableSymbols() error {
	if len(c.stableSymbols.unresolved) != 0 {
		ids := make([]string, 0, len(c.stableSymbols.unresolved))
		for id := range c.stableSymbols.unresolved {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf(
			"log violates the self-contained stable-symbol contract: %d unresolved symbol(s), first is %s; collect a new log with the current Android SDK",
			len(ids), ids[0],
		)
	}
	return nil
}

func (c *collector) deobfuscate(value string) string {
	if c.nameMap == nil {
		return value
	}
	return c.nameMap.Deobfuscate(value)
}

func (c *collector) contextKey(screenOverride, ownerOverride string) string {
	return strings.Join([]string{
		firstKnown(screenOverride, c.currentAttrScreen),
		c.operationAnalysis.activeName(c.currentOperationID),
		firstKnown(ownerOverride, c.currentAttrOwner),
	}, "\x00")
}

func (c *collector) signalContextFromKey(key string) SignalContextStats {
	parts := strings.Split(key, "\x00")
	for len(parts) < 3 {
		parts = append(parts, "unknown")
	}
	return SignalContextStats{
		Screen:    attrValue(parts[0]),
		Operation: attrValue(parts[1]),
		Owner:     attrValue(parts[2]),
	}
}

func (c *collector) ensureSignalContext(key string) *SignalContextStats {
	stats := c.signalContextStats[key]
	if stats != nil {
		return stats
	}
	context := c.signalContextFromKey(key)
	stats = &SignalContextStats{
		Screen: context.Screen, Operation: context.Operation, Owner: context.Owner,
	}
	c.signalContextStats[key] = stats
	return stats
}

func (c *collector) addProblemWindow(context SignalContextStats, kind string, windowMS, count, maxMS uint64) {
	key := strings.Join([]string{context.Screen, context.Operation, context.Owner}, "\x00")
	problemKey := key + "\x00" + kind
	stats := c.problemStats[problemKey]
	if stats == nil {
		stats = &ProblemWindowStats{
			Screen: context.Screen, Operation: context.Operation, Owner: context.Owner, Kind: kind,
		}
		c.problemStats[problemKey] = stats
	}
	stats.Windows++
	stats.Count += count
	stats.TotalWindowMS += windowMS
	stats.MaxMS = maxUint64(stats.MaxMS, maxMS)
	contextStats := c.ensureSignalContext(key)
	contextStats.ProblemCount += count
	contextStats.ProblemMaxMS = maxUint64(contextStats.ProblemMaxMS, maxMS)
}

func (c *collector) sampleSet(target map[string]*uint64SampleSet, key string) *uint64SampleSet {
	set := target[key]
	if set == nil {
		set = &uint64SampleSet{}
		target[key] = set
	}
	return set
}

func (c *collector) gauge(name string) *gaugeStats {
	stats := c.gaugeValues[name]
	if stats == nil {
		stats = &gaugeStats{}
		c.gaugeValues[name] = stats
	}
	return stats
}

func (c *collector) addMemoryLeakSuspect(
	className,
	holder string,
	context SignalContextStats,
	ageMs,
	count uint64,
	evidence jhlog.RetentionEvidence,
	runtimeSignal bool,
) {
	className = attrValue(className)
	holder = firstKnown(holder, context.Owner, className)
	key := strings.Join([]string{className, holder, context.Screen, context.Operation}, "\x00")
	stats := c.memoryLeakStats[key]
	if stats == nil {
		stats = &memoryLeakStats{
			className: className,
			holder:    holder,
			screen:    context.Screen,
			operation: context.Operation,
		}
		c.memoryLeakStats[key] = stats
	}
	stats.count += count
	if runtimeSignal {
		switch evidence.Effective() {
		case jhlog.RetentionEvidenceAfterExplicitGC:
			stats.afterExplicitGCCount += count
		default:
			stats.timeOnlyCount += count
		}
	}
	if ageMs > stats.maxAgeMs {
		stats.maxAgeMs = ageMs
	}
}

func (c *collector) addHeapOnlyMemoryLeaks() {
	if c.heap == nil {
		return
	}
	for _, leak := range c.heap.Leaks {
		className := attrValue(c.deobfuscate(leak.ClassName))
		if className == "unknown" || c.hasMemoryLeakClass(className) {
			continue
		}
		count := leak.RetainedObjectCount
		if count == 0 {
			count = 1
		}
		holder := c.deobfuscate(firstKnown(leak.Holder, leak.HolderField))
		if !c.matchesFilters("", SignalContextStats{}, []string{className}, holder) {
			continue
		}
		c.addMemoryLeakSuspect(
			className,
			holder,
			SignalContextStats{},
			0,
			count,
			jhlog.RetentionEvidenceUnknown,
			false,
		)
		c.summary.Retained += count
		stats := c.retainedClasses[className]
		if stats == nil {
			stats = &retainedClassStats{}
			c.retainedClasses[className] = stats
		}
		stats.count += count
	}
}

func (c *collector) hasMemoryLeakClass(className string) bool {
	for _, stats := range c.memoryLeakStats {
		if stats != nil && stats.className == className {
			return true
		}
	}
	return false
}

func firstKnown(values ...string) string {
	for _, value := range values {
		value = attrValue(value)
		if value != "unknown" {
			return value
		}
	}
	return "unknown"
}

func attrValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "id:0" {
		return "unknown"
	}
	return value
}

func logLevelName(level uint64) string {
	switch level {
	case 2:
		return "verbose"
	case 3:
		return "debug"
	case 4:
		return "info"
	case 5:
		return "warn"
	case 6:
		return "error"
	case 7:
		return "assert"
	default:
		return fmt.Sprintf("level-%d", level)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func counterDelta(first, last uint64) uint64 {
	if last >= first {
		return last - first
	}
	return last
}

func (c *collector) finish() Summary {
	c.finalizeCollectionQuality()
	summary := c.summary
	if c.logsWithEvents > 0 {
		summary.DurationMS = c.totalLogDurationMS
	} else if c.seenEvent && c.lastTime >= c.firstTime {
		summary.DurationMS = c.lastTime - c.firstTime
	}
	if summary.LogCount > 1 && c.logsWithEvents > 1 {
		summary.Warnings = append(
			summary.Warnings,
			"Несколько логов считаются независимыми прогонами: длительность в обзоре равна сумме длительностей логов, а математическая временная шкала накладывает события по относительному времени.",
		)
	}
	summary.TrafficRxMax = c.totalTrafficRxBytes
	summary.TrafficTxMax = c.totalTrafficTxBytes

	networkAnalysis := NetworkAnalysis{}
	contextCounts := make(map[string]int, len(c.networkRoutes))
	for key, stats := range c.networkCalls {
		contextCounts[key.route]++
		networkAnalysis.Calls = append(networkAnalysis.Calls, networkCallStats(key, stats))
	}
	for route, stats := range c.networkRoutes {
		burstStatus := "exact_rolling_second"
		if stats.burst.approximate {
			burstStatus = "bounded_approximation"
		}
		maxConcurrency, peakConcurrencyAtMS := maxHTTPConcurrency(stats.intervals)
		row := RouteStats{
			Route:                 route,
			ServiceSample:         stats.serviceSample,
			InitiatorSample:       stats.initiatorSample,
			Count:                 stats.count,
			ContextCount:          contextCounts[route],
			Failures:              stats.failures,
			TransportFailures:     stats.transportFailures,
			HTTP4xx:               stats.http4xx,
			HTTP5xx:               stats.http5xx,
			Canceled:              stats.canceled,
			CacheHits:             stats.cacheHits,
			ReusedConnections:     stats.reusedConnections,
			KnownRequestBytes:     stats.knownRequestBytes,
			KnownResponseBytes:    stats.knownResponseBytes,
			Attempts:              stats.attempts,
			DNSAttempts:           stats.dnsAttempts,
			ConnectAttempts:       stats.connectAttempts,
			TLSAttempts:           stats.tlsAttempts,
			Retries:               stats.retries,
			Redirects:             stats.redirects,
			ConnectFailures:       stats.connectFailures,
			TLSFailures:           stats.tlsFailures,
			P50MS:                 stats.durations.percentile(0.50),
			P95MS:                 stats.durations.percentile(0.95),
			MaxMS:                 stats.durations.max,
			TotalDurationMS:       stats.durationTotal,
			AvgTTFBMS:             phaseAverage(stats, 5),
			MaxConcurrency:        maxConcurrency,
			PeakConcurrencyAtMS:   peakConcurrencyAtMS,
			Phases:                httpPhaseStats(stats),
			BytesRx:               stats.bytesRx,
			BytesTx:               stats.bytesTx,
			OwnerSample:           stats.ownerSample,
			BurstEstimateStatus:   burstStatus,
			PeakRequestsPerSecond: stats.burst.peak,
			PeakWindowStartMS:     stats.burst.peakWindowStartMS,
		}
		summary.Routes = append(summary.Routes, row)
	}
	summary.HTTPP95MS = c.networkTotals.durations.percentile(0.95)
	if c.networkTotals.count > 0 {
		networkAnalysis = c.finalizeNetworkAnalysis(networkAnalysis)
		summary.NetworkAnalysis = &networkAnalysis
	}
	if c.webSocketTotals.opened > 0 || c.webSocketTotals.closed > 0 || c.webSocketTotals.failures > 0 {
		summary.WebSocketAnalysis = c.finalizeWebSocketAnalysis()
	}
	if c.databaseTotals.overall.calls > 0 || c.databaseTransactions.events > 0 || c.databaseEvidence != nil {
		summary.DatabaseAnalysis = c.finalizeDatabaseAnalysis()
	}
	summary.DatabaseCoverage = buildDatabaseCoverage(summary, c.diagnostics)
	if workerAnalysis := c.finalizeWorkerAnalysis(); workerAnalysis != nil {
		summary.WorkerAnalysis = workerAnalysis
	}

	for _, stats := range c.screenStats {
		if stats.Frames > 0 {
			stats.JankRatePct = float64(stats.JankyFrames) * 100 / float64(stats.Frames)
		}
		stats.AvgFPS = fps(stats.FPSMeasuredFrames, stats.FPSMeasuredWindowMS)
		stats.FPSStatus = fpsMeasurementStatus(stats.Frames, stats.FPSMeasuredWindowCount)
		stats.FrameP50MS = jhlog.UIFrameHistogramQuantileMS(stats.FrameDurationBuckets, 50)
		stats.FrameP95MS = jhlog.UIFrameHistogramQuantileMS(stats.FrameDurationBuckets, 95)
		stats.FrameP99MS = jhlog.UIFrameHistogramQuantileMS(stats.FrameDurationBuckets, 99)
		summary.Screens = append(summary.Screens, *stats)
	}
	if summary.UIFrames > 0 {
		summary.UIJankPct = float64(summary.UIJank) * 100 / float64(summary.UIFrames)
	}
	summary.UIAvgFPS = fps(summary.UIFPSMeasuredFrames, summary.UIFPSMeasuredWindowMS)
	summary.UIFPSStatus = fpsMeasurementStatus(summary.UIFrames, summary.UIFPSMeasuredWindowCount)

	for _, stats := range c.ownerStats {
		summary.Owners = append(summary.Owners, *stats)
	}
	for key, stats := range c.signalContextStats {
		if durations := c.signalContextHTTPDurations[key]; durations != nil {
			stats.HTTPP95MS = durations.percentile(0.95)
		}
		if stats.UIFrames > 0 {
			stats.UIJankPct = float64(stats.UIJank) * 100 / float64(stats.UIFrames)
		}
		summary.SignalContexts = append(summary.SignalContexts, *stats)
	}
	for _, stats := range c.logSpamStats {
		summary.LogSpam = append(summary.LogSpam, *stats)
	}
	for _, stats := range c.problemStats {
		summary.ProblemWindows = append(summary.ProblemWindows, *stats)
	}
	for _, stats := range c.runtimeCallStats {
		summary.RuntimeCalls = append(summary.RuntimeCalls, *stats)
	}
	for _, stats := range c.processExitStats {
		summary.ProcessExits = append(summary.ProcessExits, *stats)
	}
	criticalIOCalls := make([]IOStats, 0, len(c.ioStats))
	for _, aggregate := range c.ioStats {
		criticalIOCalls = append(criticalIOCalls, aggregate.finalize())
	}
	sortIOOperations(criticalIOCalls)
	summary.IOAnalysis = c.ioAnalysis.finalize(criticalIOCalls)
	summary.AsyncAnalysis = c.runtimeAnalysis.async.finalize()
	summary.GCAnalysis = c.runtimeAnalysis.gc.finalize()
	summary.StartupAnalysis = c.runtimeAnalysis.startup.finalize()
	summary.OperationAnalysis = c.operationAnalysis.finalize()
	if androidAnalysis := c.androidAnalysis.finalize(summary.CollectionQuality); androidAnalysis.Available ||
		androidAnalysis.Coverage.CatalogAvailable {
		summary.AndroidComponents = androidAnalysis
		if androidAnalysis.Partial {
			summary.Warnings = append(
				summary.Warnings,
				"Анализ компонентов Android и IPC частичный: "+strings.Join(androidAnalysis.PartialReasons, "; ")+".",
			)
		}
	}
	if operations := summary.OperationAnalysis; operations != nil {
		if operations.MissingFinish > 0 || operations.MissingStart > 0 || operations.DuplicateStart > 0 ||
			operations.InconsistentLifecycle > 0 {
			summary.Warnings = append(summary.Warnings, fmt.Sprintf(
				"Жизненный цикл операций неполон: без завершения=%d, без начала=%d, повторных начал=%d, противоречий=%d.",
				operations.MissingFinish,
				operations.MissingStart,
				operations.DuplicateStart,
				operations.InconsistentLifecycle,
			))
		}
	}
	for name, value := range c.counterValues {
		summary.Counters = append(summary.Counters, NamedValue{Name: name, Value: value})
		if isJankStatsMetric(name) {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value})
		}
	}
	for name, values := range c.gaugeValues {
		value := values.value()
		extra := values.extra()
		summary.Gauges = append(summary.Gauges, NamedValue{Name: name, Value: value, Extra: extra})
		if isJankStatsMetric(name) {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value, Extra: extra})
		}
	}

	for name, value := range c.networkSamples {
		summary.Network = append(summary.Network, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.appVersions {
		summary.AppVersions = append(summary.AppVersions, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.builds {
		summary.Builds = append(summary.Builds, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.devices {
		summary.Devices = append(summary.Devices, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.sdks {
		summary.SDKs = append(summary.SDKs, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.cohortSamples {
		summary.Cohorts = append(summary.Cohorts, NamedValue{Name: name, Value: value})
	}
	for name, value := range c.processSamples {
		summary.Processes = append(summary.Processes, NamedValue{Name: name, Value: value})
	}
	c.addHeapOnlyMemoryLeaks()
	for name, stats := range c.retainedClasses {
		summary.RetainedClasses = append(summary.RetainedClasses, NamedValue{
			Name:  name,
			Value: stats.count,
			Extra: fmt.Sprintf("max_age_ms=%d", stats.maxAgeMs),
		})
	}
	for bucket, value := range c.retainedAgeBuckets {
		summary.RetainedAgeBuckets = append(summary.RetainedAgeBuckets, NamedValue{Name: bucket, Value: value})
	}
	summary.MemoryLeaks = buildMemoryLeakSuspects(
		c.memoryLeakStats,
		summary.LowMemoryCount,
		summary.MemoryMaxKB,
		c.heap,
		c.retentionDataQuality(),
	)
	if c.heap != nil {
		summary.Warnings = append(summary.Warnings, c.heap.Warnings...)
	}
	summary.Memory = append(summary.Memory, NamedValue{Name: "max_pss_kb", Value: summary.MemoryMaxKB, Extra: formatMB(summary.MemoryMaxKB)})
	if summary.AvailMemoryMinKB > 0 {
		summary.Memory = append(summary.Memory, NamedValue{Name: "min_available_kb", Value: summary.AvailMemoryMinKB, Extra: formatMB(summary.AvailMemoryMinKB)})
	}
	if summary.ContextCount > 0 {
		summary.Memory = append(summary.Memory, NamedValue{Name: "low_memory_samples", Value: uint64(summary.LowMemoryCount)})
	}
	summary.Environment = c.runEnvironment(summary)
	summary.Warnings = append(summary.Warnings, c.telemetryHealthWarnings(summary)...)
	summary.Warnings = append(summary.Warnings, c.filterWarnings(summary)...)

	sortRoutes(summary.Routes)
	if summary.NetworkAnalysis != nil {
		sortNetworkCalls(summary.NetworkAnalysis.Calls)
	}
	sortScreens(summary.Screens)
	sortOwners(summary.Owners)
	sortSignalContexts(summary.SignalContexts)
	sortLogSpam(summary.LogSpam)
	sortProblems(summary.ProblemWindows)
	sortRuntimeCalls(summary.RuntimeCalls)
	sortProcessExits(summary.ProcessExits)
	sortNamed(summary.AppVersions)
	sortNamed(summary.Builds)
	sortNamed(summary.Devices)
	sortNamed(summary.SDKs)
	sortNamed(summary.Cohorts)
	sortNamed(summary.Processes)
	sortNamed(summary.Network)
	sortNamed(summary.RetainedClasses)
	sortNamed(summary.RetainedAgeBuckets)
	sortMemoryLeaks(summary.MemoryLeaks)
	sortNamed(summary.JankStats)
	sortNamed(summary.Counters)
	sortNamed(summary.Gauges)
	summary.LogGrowth = buildLogGrowthSummary(c.streamResults)
	dependencyInjection := c.dependencyInjection
	// Every base aggregate has been copied into Summary. Drop the mutable collection maps before
	// materializing influence views and the code-problem registry so both representations do not
	// coexist at peak heap usage on large applications.
	c.releaseAggregationState()
	summary.Influence = BuildInfluence(summary, c.classGraph)
	summary.CodeProblems = BuildCodeProblemRegistry(summary)
	summary.AnalysisInputs = c.analysisInputCompleteness(summary)
	problemReport, problemErr := buildProblemReportWithCatalog(
		summary,
		DefaultProblemDetectorConfig(),
		dependencyInjection,
	)
	if problemErr != nil {
		summary.Warnings = append(summary.Warnings, "problem engine: "+problemErr.Error())
	} else {
		summary.ProblemSchemaVersion = ProblemSchemaVersion
		summary.ProblemSummary = problemReport.Summary
		summary.Problems = problemReport.Problems
		summary.ProblemIncidents = problemReport.Incidents
		summary.CategoryCoverage = problemReport.Coverage
		summary.Detectors = problemReport.Registry
	}
	summary.EvidenceQuality = BuildEvidenceQualityVector(summary)
	return summary
}

func (c *collector) releaseAggregationState() {
	c.nameMap = nil
	c.dependencyInjection = nil
	c.networkTotals = httpAggregate{}
	c.networkRoutes = nil
	c.networkCalls = nil
	c.networkStatusCodes = nil
	c.databaseTotals = databaseAggregate{}
	c.databaseStatements = databaseStatementStore{}
	c.workerCollectorState.release()
	c.runtimeAnalysis = runtimeAnalysisAccumulator{}
	c.operationAnalysis.release()
	c.androidAnalysis = nil
	c.databaseCorrelation = databaseCorrelationAccumulator{}
	c.screenStats = nil
	c.processExitStats = nil
	c.ioStats = nil
	c.ownerStats = nil
	c.signalContextStats = nil
	c.signalContextHTTPDurations = nil
	c.logSpamStats = nil
	c.problemStats = nil
	c.runtimeCallStats = nil
	c.counterValues = nil
	c.gaugeValues = nil
	c.appVersions = nil
	c.builds = nil
	c.devices = nil
	c.sdks = nil
	c.cohortSamples = nil
	c.networkSamples = nil
	c.processSamples = nil
	c.retainedClasses = nil
	c.retainedAgeBuckets = nil
	c.memoryLeakStats = nil
	c.qualitySnapshots = nil
	c.streamResults = nil
	c.stableSymbols.embedded = nil
}

func isJankStatsMetric(name string) bool {
	return strings.HasPrefix(name, "jankstats.")
}

func (c *collector) analysisInputCompleteness(summary Summary) AnalysisInputCompleteness {
	runtimeEvidence := summary.LogCount > 0 && summary.DataRecordCount > 0
	classGraph := summary.Influence.HasClassGraph
	diagnostics := c.diagnostics != nil && c.diagnostics.Available && c.diagnostics.ClassCount > 0
	missing := make([]string, 0, 4)
	if !runtimeEvidence {
		missing = append(missing, "события выполнения")
	}
	if !classGraph {
		missing = append(missing, "class-graph.jsonl")
	}
	if !diagnostics {
		missing = append(missing, "instrumentation-diagnostics.jsonl")
	}
	artifactIdentityVerified := len(c.artifactNamespace) == symbolNamespaceBytes
	if (classGraph || diagnostics) && !artifactIdentityVerified {
		missing = append(missing, "совпадающее пространство имён артефактов")
	}
	complete := len(missing) == 0
	status := "complete"
	explanation := "данные выполнения, статический граф классов и диагностика ASM подключены"
	if !complete {
		status = "partial"
		explanation = "часть входных данных анализа отсутствует; соответствующие выводы и дополнительные отчёты ограничены"
		if runtimeEvidence && !classGraph && !diagnostics {
			status = "runtime_only"
			explanation = "доступны данные выполнения, но статический граф, горячие пути, циклы и диагностика ASM неполны"
		}
	}
	return AnalysisInputCompleteness{
		Status:                     status,
		Complete:                   complete,
		RuntimeEvidence:            runtimeEvidence,
		ClassGraph:                 classGraph,
		InstrumentationDiagnostics: diagnostics,
		HeapEvidence:               c.heap != nil && len(c.heap.Sources) > 0,
		ArtifactDirectory:          c.artifactDirectory,
		ArtifactsAutoDiscovered:    c.artifactAuto,
		ArtifactIdentityVerified:   artifactIdentityVerified,
		Missing:                    missing,
		Explanation:                explanation,
	}
}

func validateArtifactNamespace(namespace []byte, header jhlog.SegmentHeader, source, directory string) error {
	if len(namespace) == 0 {
		return nil
	}
	if len(namespace) != symbolNamespaceBytes || !bytes.Equal(namespace, header.SymbolNamespace) {
		return fmt.Errorf(
			"Jank Hunter artifact bundle %q does not match .jhlog %q symbol namespace; rebuild the same app variant or pass its exact --artifacts-dir",
			directory,
			source,
		)
	}
	return nil
}

func (c *collector) telemetryHealthWarnings(summary Summary) []string {
	warnings := c.runtimeQualityWarnings()
	warnings = append(warnings, c.instrumentationQualityWarnings()...)
	warnings = append(warnings, c.attributionQualityWarnings(summary)...)
	return warnings
}

func (c *collector) runtimeQualityWarnings() []string {
	var warnings []string
	for _, item := range runtimeQualityCounterWarnings {
		if value := c.counterValues[item.name]; value > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: %s: %d.", item.label, value))
		}
	}
	quality := c.latestQualityTotals()
	dictionaryOverflow := uint64(c.dictionaryOverflow)
	if quality[jhlog.QualityDictionaryOverflowTotal] > dictionaryOverflow {
		dictionaryOverflow = quality[jhlog.QualityDictionaryOverflowTotal]
	}
	if dictionaryOverflow > 0 {
		warnings = append(warnings, fmt.Sprintf("Качество сбора: словарь .jhlog использовал overflow-ссылки: %d; соответствующие имена могли стать неразличимыми.", dictionaryOverflow))
	}
	exactAdmission := true
	for _, result := range c.streamResults {
		if result.Header.RequiredFeatures&jhlog.FeatureExactEventAdmission == 0 {
			exactAdmission = false
			break
		}
	}
	warnings = append(warnings, qualityCounterWarnings(quality, exactAdmission)...)
	return warnings
}

func (c *collector) latestQualityTotals() map[uint64]uint64 {
	totals := map[uint64]uint64{}
	for _, state := range c.qualitySnapshots {
		for id, value := range state.snapshot.Counters {
			totals[id] = saturatingUint64Sum(totals[id], value)
		}
	}
	return totals
}

func (c *collector) finalizeCollectionQuality() {
	if len(c.streamResults) == 0 {
		return
	}
	quality := CollectionQuality{
		Level:                   "high",
		Complete:                true,
		ChainValid:              true,
		ExactAdmission:          true,
		ProcessScopeConsistent:  true,
		RunCohortConsistent:     true,
		CounterInvariantsValid:  true,
		QualityProgressionValid: true,
		RuntimeGraphEnabled:     true,
		ChainIssues:             append([]string(nil), c.chainIssues...),
	}
	type processScopeConfig struct {
		scope                     jhlog.ProcessScope
		allowedCount              uint64
		fingerprint               string
		expectedCount             uint64
		expectedFingerprint       string
		rosterDeclarationComplete bool
	}
	processScopes := map[processScopeConfig]struct{}{}
	observedProcesses := map[string]struct{}{}
	runCohorts := map[jhlog.ID128]struct{}{}
	addReason := func(level, reason string) {
		quality.Complete = false
		quality.Level = lowerConfidenceLevel(quality.Level, level)
		quality.Reasons = append(quality.Reasons, reason)
	}
	addNotice := func(notice string) {
		quality.Complete = false
		quality.Notices = append(quality.Notices, notice)
	}

	for _, result := range c.streamResults {
		segmentDamaged := false
		if !result.Header.RunID.IsZero() {
			runCohorts[result.Header.RunID] = struct{}{}
		}
		if processName := strings.TrimSpace(result.Header.ProcessName); processName != "" {
			observedProcesses[processName] = struct{}{}
		}
		if result.Header.RequiredFeatures&jhlog.FeatureProcessScope == 0 {
			processScopes[processScopeConfig{}] = struct{}{}
			addReason("low", fmt.Sprintf(
				"сегмент %s не содержит обязательный process scope; охват процессов подтвердить невозможно",
				result.Source,
			))
		} else {
			processScopes[processScopeConfig{
				scope:                     result.Header.ProcessScope,
				allowedCount:              result.Header.AllowedProcessCount,
				fingerprint:               hex.EncodeToString(result.Header.ProcessScopeFingerprint),
				expectedCount:             result.Header.ExpectedProcessCount,
				expectedFingerprint:       hex.EncodeToString(result.Header.ExpectedProcessFingerprint),
				rosterDeclarationComplete: result.Header.ProcessRosterDeclarationComplete,
			}] = struct{}{}
		}
		if result.Header.RequiredFeatures&jhlog.FeatureExactEventAdmission == 0 {
			quality.ExactAdmission = false
			addReason("medium", fmt.Sprintf(
				"сегмент %s собран без EXACT admission; отсутствие потерь очереди нельзя гарантировать архитектурно",
				result.Source,
			))
		}
		if result.Header.RunID.IsZero() || result.Header.ProcessInstanceID.IsZero() || result.Header.SessionID.IsZero() {
			quality.ChainValid = false
			addReason("medium", fmt.Sprintf("identity сегмента %s неполна, поэтому принадлежность session не подтверждена", result.Source))
		}
		if result.Sealed && result.Status == jhlog.SegmentStatusClosedClean {
			quality.SealedSegments++
		} else {
			quality.UnsealedSegments++
			switch result.Status {
			case jhlog.SegmentStatusOpenWithTail, jhlog.SegmentStatusCorrupt:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s не запечатан и имеет статус %s (хвост %d байт)", result.Source, result.Status, result.TailBytes))
			case jhlog.SegmentStatusOpenClean:
				addNotice(fmt.Sprintf(
					"снимок активной сессии %s корректно прочитан до последнего зафиксированного чанка; FINAL seal появится после завершения runtime",
					result.Source,
				))
			default:
				segmentDamaged = true
				addReason("medium", fmt.Sprintf("сегмент %s не содержит FINAL seal (статус %s)", result.Source, result.Status))
			}
		}
		if result.LatestQuality == nil {
			quality.SegmentsWithoutQuality++
			addReason("medium", fmt.Sprintf("сегмент %s не содержит quality snapshot", result.Source))
		} else {
			quality.SegmentsWithQuality++
		}
		if result.SegmentEnd != nil {
			switch result.SegmentEnd.Reason {
			case jhlog.SegmentEndIOError:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s завершен после ошибки ввода-вывода", result.Source))
			case jhlog.SegmentEndSizeLimit:
				segmentDamaged = true
				addReason("medium", sizeLimitCollectionReason(result.Source))
			case jhlog.SegmentEndStorageBudget:
				segmentDamaged = true
				addReason("medium", fmt.Sprintf(
					"сегмент %s запечатан с storage_budget_exhausted: активный запуск исчерпал общий бюджет .jhlog; последующие события не собирались",
					result.Source,
				))
			case jhlog.SegmentEndNormal, jhlog.SegmentEndShutdown, jhlog.SegmentEndRotation:
			default:
				segmentDamaged = true
				addReason("low", fmt.Sprintf("сегмент %s завершен с неизвестной причиной %d", result.Source, uint64(result.SegmentEnd.Reason)))
			}
		}
		if segmentDamaged {
			quality.DamagedSegments++
		}
	}
	quality.RunCohortCount = uint64(len(runCohorts))
	if len(runCohorts) != 1 {
		quality.RunCohortConsistent = false
		addReason("low", fmt.Sprintf(
			"входные сегменты относятся к %d разным запускам приложения; all-process roster нельзя объединять между запусками",
			len(runCohorts),
		))
	}

	switch len(processScopes) {
	case 1:
		for scope := range processScopes {
			quality.ProcessScope = scope.scope.String()
			quality.AllowedProcessCount = scope.allowedCount
			quality.ProcessScopeFingerprint = scope.fingerprint
			quality.ExpectedProcessCount = scope.expectedCount
			quality.ExpectedProcessFingerprint = scope.expectedFingerprint
			quality.ProcessRosterDeclarationComplete = scope.rosterDeclarationComplete
			quality.ObservedProcessCount = uint64(len(observedProcesses))
			if scope.scope == jhlog.ProcessScopeUnknown {
				quality.ProcessScopeConsistent = false
			}
			quality.AllProcessesConfigured = scope.scope == jhlog.ProcessScopeAll
			if !scope.rosterDeclarationComplete {
				addReason("low", "runtime не смог полностью объявить process roster из Android manifest")
			} else {
				observedNames := make([]string, 0, len(observedProcesses))
				for processName := range observedProcesses {
					observedNames = append(observedNames, processName)
				}
				observedFingerprint := hex.EncodeToString(jhlog.ProcessRosterFingerprint(observedNames))
				quality.ProcessRosterComplete = quality.RunCohortConsistent &&
					uint64(len(observedProcesses)) == scope.expectedCount &&
					observedFingerprint == scope.expectedFingerprint
				if !quality.RunCohortConsistent {
					addReason("low", "состав процессов не доказан: процессы принадлежат разным группам запусков")
				} else if !quality.ProcessRosterComplete {
					addNotice(fmt.Sprintf(
						"наблюдается %d процессов из %d указанных в области сбора; отсутствующие процессы могли не запускаться либо их сегменты не были переданы, поэтому это неопределённость охвата, а не доказанная потеря",
						len(observedProcesses),
						scope.expectedCount,
					))
				}
			}
			switch scope.scope {
			case jhlog.ProcessScopeMainOnly:
				quality.Notices = append(
					quality.Notices,
					"сбор намеренно ограничен main-процессом; полнота относится только к этому scope",
				)
			case jhlog.ProcessScopeAllowlist:
				quality.Notices = append(quality.Notices, fmt.Sprintf(
					"сбор намеренно ограничен allowlist из %d процессов; полнота относится только к этому scope",
					scope.allowedCount,
				))
			}
		}
	case 0:
		quality.ProcessScope = jhlog.ProcessScopeUnknown.String()
		quality.ProcessScopeConsistent = false
		addReason("low", "process scope отсутствует во всех входных сегментах")
	default:
		quality.ProcessScope = "mixed"
		quality.ProcessScopeConsistent = false
		addReason("low", "входные сегменты используют разные process scope или разные process allowlist")
	}

	if len(c.chainIssues) > 0 {
		quality.ChainValid = false
		for _, issue := range c.chainIssues {
			addReason("low", issue)
		}
	}
	for _, issue := range qualityProgressionIssues(c.streamResults) {
		quality.ChainValid = false
		quality.QualityProgressionValid = false
		quality.ChainIssues = append(quality.ChainIssues, issue)
		addReason("low", issue)
	}

	counters := c.latestQualityTotals()
	quality.AcceptedEvents = counters[jhlog.QualityAcceptedEventTotal]
	quality.WrittenEvents = counters[jhlog.QualityWrittenEventTotal]
	quality.ReportedCommittedChunks = counters[jhlog.QualityCommittedChunkTotal]
	for _, result := range c.streamResults {
		quality.DecodedCommittedChunks = saturatingUint64Sum(
			quality.DecodedCommittedChunks,
			uint64(result.CommittedChunks),
		)
		quality.DecodedRuntimeGraphCalls = saturatingUint64Sum(
			quality.DecodedRuntimeGraphCalls,
			result.RuntimeGraphLogicalCalls,
		)
	}
	quality.RuntimeGraphInputEvents = counters[jhlog.QualityRuntimeGraphInputTotal]
	quality.RuntimeGraphEmittedEvents = counters[jhlog.QualityRuntimeGraphEmittedTotal]
	quality.RuntimeGraphStackMismatches = counters[jhlog.QualityRuntimeStackMismatch]
	quality.RuntimeGraphEnabled = counters[jhlog.QualityRuntimeGraphDisabled] == 0
	quality.ArchiveEvictedRuns = counters[jhlog.QualityArchiveEvictedRunTotal]
	quality.ArchiveEvictedSegments = counters[jhlog.QualityArchiveEvictedSegmentTotal]
	quality.ArchiveEvictedBytes = counters[jhlog.QualityArchiveEvictedBytesTotal]
	if quality.ArchiveEvictedRuns > 0 {
		addNotice(fmt.Sprintf(
			"циклическое хранение освободило %d байт: удалено %d завершённых запусков (%d сегментов); текущий run cohort сохранён целиком",
			quality.ArchiveEvictedBytes,
			quality.ArchiveEvictedRuns,
			quality.ArchiveEvictedSegments,
		))
	}
	quality.RuntimeGraphCompletenessRatio = 1
	if !quality.RuntimeGraphEnabled {
		quality.RuntimeGraphCompletenessRatio = 0
		if quality.RuntimeGraphInputEvents > 0 || quality.RuntimeGraphEmittedEvents > 0 || quality.DecodedRuntimeGraphCalls > 0 {
			addReason("low", fmt.Sprintf(
				"runtime-граф отмечен отключённым, но содержит input/emitted/decoded=%d/%d/%d; конфигурация и evidence противоречат друг другу",
				quality.RuntimeGraphInputEvents,
				quality.RuntimeGraphEmittedEvents,
				quality.DecodedRuntimeGraphCalls,
			))
		} else {
			quality.Notices = append(quality.Notices, fmt.Sprintf(
				"runtime-граф отключён конфигурацией в %d quality snapshot(s) и полностью исключён из индекса доверия",
				counters[jhlog.QualityRuntimeGraphDisabled],
			))
		}
	} else if quality.RuntimeGraphInputEvents > 0 {
		quality.RuntimeGraphCompletenessRatio =
			float64(quality.DecodedRuntimeGraphCalls) / float64(quality.RuntimeGraphInputEvents)
		if quality.RuntimeGraphEmittedEvents > quality.RuntimeGraphInputEvents {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"невозможное состояние runtime-графа: writer сообщает %d emitted при %d input",
				quality.RuntimeGraphEmittedEvents,
				quality.RuntimeGraphInputEvents,
			))
		}
		if quality.DecodedRuntimeGraphCalls > quality.RuntimeGraphInputEvents {
			quality.RuntimeGraphCompletenessRatio = 0
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"невозможное состояние runtime-графа: декодировано %d логических вызовов при %d входных",
				quality.DecodedRuntimeGraphCalls,
				quality.RuntimeGraphInputEvents,
			))
		} else if quality.RuntimeGraphCompletenessRatio < 1 {
			level := "medium"
			if quality.RuntimeGraphCompletenessRatio < 0.99 {
				level = "low"
			}
			addReason(level, fmt.Sprintf(
				"полнота runtime-графа %.2f%% (%d из %d логических вызовов)",
				quality.RuntimeGraphCompletenessRatio*100,
				quality.DecodedRuntimeGraphCalls,
				quality.RuntimeGraphInputEvents,
			))
		}
	} else if quality.RuntimeGraphEmittedEvents > 0 || quality.DecodedRuntimeGraphCalls > 0 {
		quality.RuntimeGraphCompletenessRatio = 0
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"невозможное состояние runtime-графа: reported=%d, decoded=%d при нулевом input counter",
			quality.RuntimeGraphEmittedEvents,
			quality.DecodedRuntimeGraphCalls,
		))
	}
	preAdmissionLoss := saturatingUint64Sum(
		counters[jhlog.QualityQueueFullTotal],
		counters[jhlog.QualityNotAcceptingTotal],
	)
	if !quality.ExactAdmission {
		preAdmissionLoss = saturatingUint64Sum(
			preAdmissionLoss,
			counters[jhlog.QualityWriterAdmissionContentionTotal],
		)
	}
	postAdmissionCounters := saturatingUint64Sum(
		counters[jhlog.QualityEventLostAfterIOTotal],
		counters[jhlog.QualityEventLostAfterSizeLimitTotal],
		counters[jhlog.QualityEventLostAfterStorageBudget],
		counters[jhlog.QualityOversizedRecordTotal],
	)
	acceptedGap := uint64(0)
	if quality.AcceptedEvents > quality.WrittenEvents {
		acceptedGap = quality.AcceptedEvents - quality.WrittenEvents
	} else if quality.WrittenEvents > quality.AcceptedEvents {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"невозможное состояние writer: записано %d событий при %d принятых",
			quality.WrittenEvents,
			quality.AcceptedEvents,
		))
	}
	if acceptedGap > postAdmissionCounters {
		postAdmissionCounters = acceptedGap
	}
	quality.KnownLostEvents = saturatingUint64Sum(preAdmissionLoss, postAdmissionCounters)
	quality.WriterBackpressureCount = counters[jhlog.QualityWriterBackpressureCount]
	quality.WriterBackpressureNanos = counters[jhlog.QualityWriterBackpressureNanos]
	quality.RuntimeHookFailures = counters[jhlog.QualityRuntimeHookFailureTotal]
	var classifiedRuntimeHookFailures uint64
	quality.RuntimeHookFailureDetails, quality.CriticalRuntimeHookFailures, classifiedRuntimeHookFailures =
		runtimeHookFailureDetails(counters)
	if classifiedRuntimeHookFailures > quality.RuntimeHookFailures {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"reason-coded hook failures=%d превышают общий runtime_hook_failure_total=%d",
			classifiedRuntimeHookFailures,
			quality.RuntimeHookFailures,
		))
	}
	if quality.ExactAdmission && counters[jhlog.QualityWriterAdmissionContentionTotal] > 0 {
		quality.Notices = append(quality.Notices, fmt.Sprintf(
			"EXACT writer ожидал admission lock %d раз (%s backpressure); все принятые события сохранены",
			counters[jhlog.QualityWriterAdmissionContentionTotal],
			formatDurationNanos(quality.WriterBackpressureNanos),
		))
	}
	if quality.KnownLostEvents > 0 {
		level := "medium"
		denominator := saturatingUint64Sum(quality.WrittenEvents, quality.KnownLostEvents)
		if quality.KnownLostEvents >= 100 || (denominator > 0 && float64(quality.KnownLostEvents)/float64(denominator) >= 0.01) {
			level = "low"
		}
		addReason(level, fmt.Sprintf("quality snapshots фиксируют потерю как минимум %d событий", quality.KnownLostEvents))
	}
	if c.summary.DataRecordCount > 0 && quality.AcceptedEvents == 0 && quality.WrittenEvents == 0 {
		quality.CounterInvariantsValid = false
		addReason("low", fmt.Sprintf(
			"%d data records присутствуют без accepted/written quality counters",
			c.summary.DataRecordCount,
		))
	}
	if quality.UnsealedSegments == 0 && quality.SegmentsWithoutQuality == 0 {
		if quality.RuntimeGraphEmittedEvents != quality.DecodedRuntimeGraphCalls {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"writer сообщает %d записанных runtime-вызовов, но декодировано %d",
				quality.RuntimeGraphEmittedEvents,
				quality.DecodedRuntimeGraphCalls,
			))
		}
		if quality.WrittenEvents != c.summary.DataRecordCount {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"writer сообщает %d записанных событий, но декодировано %d data records",
				quality.WrittenEvents,
				c.summary.DataRecordCount,
			))
		}
		if quality.ReportedCommittedChunks != quality.DecodedCommittedChunks {
			quality.CounterInvariantsValid = false
			addReason("low", fmt.Sprintf(
				"writer сообщает %d committed chunks, но декодировано %d",
				quality.ReportedCommittedChunks,
				quality.DecodedCommittedChunks,
			))
		}
	}
	if counters[jhlog.QualityWriterIOErrorTotal] > 0 || counters[jhlog.QualityFailedChunkTotal] > 0 {
		addReason("low", fmt.Sprintf(
			"writer сообщил ошибки I/O=%d и незаписанные чанки=%d",
			counters[jhlog.QualityWriterIOErrorTotal],
			counters[jhlog.QualityFailedChunkTotal],
		))
	}
	controlFailures := saturatingUint64Sum(
		counters[jhlog.QualityControlLaneFullTotal],
		counters[jhlog.QualityControlTimeoutTotal],
		counters[jhlog.QualityControlInterruptedTotal],
		counters[jhlog.QualityCloseTimeoutTotal],
	)
	quality.ControlFailures = controlFailures
	if controlFailures > 0 {
		addReason("medium", fmt.Sprintf("служебный канал writer сообщил %d сбоев или таймаутов", controlFailures))
	}
	quality.DictionaryOverflow = counters[jhlog.QualityDictionaryOverflowTotal]
	if uint64(c.dictionaryOverflow) > quality.DictionaryOverflow {
		quality.DictionaryOverflow = uint64(c.dictionaryOverflow)
	}
	quality.DictionaryTruncated = counters[jhlog.QualityDictionaryValueTruncated]
	if quality.DictionaryOverflow > 0 || quality.DictionaryTruncated > 0 {
		addReason("medium", fmt.Sprintf(
			"словарь деградировал: overflow=%d, truncated=%d",
			quality.DictionaryOverflow,
			quality.DictionaryTruncated,
		))
	}
	boundedEvidenceLoss := saturatingUint64Sum(
		counters[jhlog.QualityMetricCardinalityLoss],
		counters[jhlog.QualityRuntimeGraphShutdownLoss],
		counters[jhlog.QualityRuntimeGraphWriterRejectionLoss],
		counters[jhlog.QualityRuntimeStackMismatch],
		counters[jhlog.QualityHandlerContentionBypass],
		counters[jhlog.QualityRuntimeEventBufferCapacityLoss],
		counters[jhlog.QualityRuntimeEventRegistryCapacityLoss],
		counters[jhlog.QualityMethodCounterCardinalityLoss],
		counters[jhlog.QualityRuntimeEventWriterRejectionLoss],
		counters[jhlog.QualityLogSpamCardinalityLoss],
		counters[jhlog.QualityHandlerEntryLimit],
		counters[jhlog.QualityHandlerWrapperLimit],
		counters[jhlog.QualityLifecycleRegistryLimit],
		counters[jhlog.QualityObjectWatcherLimit],
		counters[jhlog.QualityJankStatsHandleLimit],
		counters[jhlog.QualityMetricFlushTimeout],
	)
	quality.BoundedEvidenceLoss = boundedEvidenceLoss
	graphEvidenceLoss := saturatingUint64Sum(
		counters[jhlog.QualityRuntimeGraphShutdownLoss],
		counters[jhlog.QualityRuntimeGraphWriterRejectionLoss],
	)
	graphEvidenceLoss = saturatingUint64Sum(graphEvidenceLoss, quality.RuntimeGraphStackMismatches)
	if boundedEvidenceLoss > graphEvidenceLoss {
		quality.OtherEvidenceLoss = boundedEvidenceLoss - graphEvidenceLoss
	}
	if boundedEvidenceLoss > 0 {
		addReason("medium", fmt.Sprintf("ограниченные runtime-реестры потеряли %d элементов evidence", boundedEvidenceLoss))
	}
	availabilityFailures := saturatingUint64Sum(
		counters[jhlog.QualityJankStatsDependencyMissing],
		counters[jhlog.QualityJankStatsInstallFailure],
	)
	if availabilityFailures > 0 {
		addNotice(fmt.Sprintf(
			"JankStats недоступен %d раз до активации; использован Choreographer fallback, транспорт событий не повреждён",
			availabilityFailures,
		))
	}
	if quality.CriticalRuntimeHookFailures > 0 {
		addReason("low", fmt.Sprintf(
			"fail-open границы runtime подавили %d сбоев, влияющих на evidence; причины: %s",
			quality.CriticalRuntimeHookFailures,
			runtimeHookFailureReasonSummary(quality.RuntimeHookFailureDetails, true),
		))
	}
	quality.ChainIssues = uniqueStrings(quality.ChainIssues)
	quality.Notices = uniqueStrings(quality.Notices)
	quality.Reasons = uniqueStrings(quality.Reasons)
	quality.DiagnosticCompletenessPercent, quality.DiagnosticCompletenessComponents =
		collectionDiagnosticCompleteness(quality)
	quality.DiagnosticCompletenessModel = diagnosticCompletenessModel
	quality.DiagnosticCompletenessLevel, quality.DiagnosticCompletenessExplanation =
		describeDiagnosticCompleteness(
			quality.DiagnosticCompletenessPercent,
			quality.DiagnosticCompletenessComponents,
		)
	c.summary.CollectionQuality = quality
	for _, reason := range quality.Reasons {
		c.summary.Warnings = append(c.summary.Warnings, "Качество сбора: "+reason+".")
	}
}

func collectionDiagnosticCompleteness(quality CollectionQuality) (float64, []DiagnosticCompletenessComponent) {
	components := make([]DiagnosticCompletenessComponent, 0, 4)
	activeWeight := 0.0
	earnedWeight := 0.0
	appendComponent := func(id, label string, weight, coverage float64, excluded bool, explanation string) {
		coverage = math.Max(0, math.Min(1, coverage))
		earned := 0.0
		missing := 0.0
		if !excluded {
			earned = weight * coverage
			missing = weight - earned
			activeWeight += weight
			earnedWeight += earned
		}
		components = append(components, DiagnosticCompletenessComponent{
			ID:              id,
			Label:           label,
			Weight:          weight,
			Excluded:        excluded,
			CoveragePercent: roundDiagnosticCompleteness(coverage * 100),
			EarnedPoints:    roundDiagnosticCompleteness(earned),
			MissingPoints:   roundDiagnosticCompleteness(missing),
			Explanation:     explanation,
		})
	}

	transportCoverage := 1.0
	transportExplanation := "Все принятые события записаны; известных пропусков журнала нет"
	if !quality.ExactAdmission {
		transportCoverage = 0
		transportExplanation = "Режим доставки не позволяет подтвердить полноту журнала; количественные оценки могут быть занижены"
	} else if quality.KnownLostEvents > 0 {
		transportTotal := saturatingUint64Sum(quality.WrittenEvents, quality.KnownLostEvents)
		if transportTotal == 0 {
			transportCoverage = 0
		} else {
			transportCoverage = float64(quality.WrittenEvents) / float64(transportTotal)
		}
		transportExplanation = "Часть событий журнала недоступна; количественные оценки могут быть занижены"
	}
	appendComponent("transport", "Доставка событий", 40, transportCoverage, false, transportExplanation)

	runtimeCoverage := quality.RuntimeGraphCompletenessRatio
	runtimeDenominator := saturatingUint64Sum(
		quality.RuntimeGraphInputEvents,
		quality.RuntimeGraphStackMismatches,
	)
	runtimeExplanation := "Все записанные связи вызовов доступны"
	if !quality.RuntimeGraphEnabled {
		runtimeCoverage = 1
		runtimeExplanation = "Граф вызовов во время выполнения отключён настройками и исключён из расчёта индекса"
	} else if runtimeDenominator == 0 {
		runtimeCoverage = 1
		runtimeExplanation = "Сбор связей вызовов включён, но вызовов в этом сценарии не зарегистрировано"
	} else {
		runtimeCoverage = float64(quality.DecodedRuntimeGraphCalls) / float64(runtimeDenominator)
		if runtimeCoverage < 1 || quality.RuntimeGraphStackMismatches > 0 {
			runtimeExplanation = "Часть связей вызовов недоступна; цепочки вызовов и количество повторов могут быть неполными"
		}
	}
	appendComponent(
		"runtime_graph",
		"Граф вызовов во время выполнения",
		20,
		runtimeCoverage,
		!quality.RuntimeGraphEnabled,
		runtimeExplanation,
	)

	processCoverage := 0.0
	processExplanation := fmt.Sprintf(
		"записано %d из %d потенциальных процессов; AndroidManifest не доказывает, что остальные процессы запускались",
		quality.ObservedProcessCount,
		quality.ExpectedProcessCount,
	)
	if quality.ProcessRosterComplete {
		processCoverage = 1
		processExplanation = fmt.Sprintf(
			"подтверждены все %d ожидаемых процессов",
			quality.ExpectedProcessCount,
		)
	} else if quality.ProcessRosterDeclarationComplete && quality.RunCohortConsistent &&
		quality.ProcessScopeConsistent && quality.ExpectedProcessCount > 0 &&
		quality.ObservedProcessCount < quality.ExpectedProcessCount {
		processCoverage = float64(quality.ObservedProcessCount) / float64(quality.ExpectedProcessCount)
	} else if !quality.ProcessRosterDeclarationComplete {
		processExplanation = "AndroidManifest не позволил подтвердить полный список ожидаемых процессов"
	} else if !quality.RunCohortConsistent {
		processExplanation = "входные сегменты относятся к разным прогонам"
	} else if !quality.ProcessScopeConsistent {
		processExplanation = "настройки охвата процессов не согласованы между сегментами"
	} else {
		processExplanation = "количество или состав процессов не совпадает с объявленным списком"
	}
	appendComponent("process_roster", "Охват процессов", 20, processCoverage, false, processExplanation)

	integrityIssues := make([]string, 0, 9)
	if !quality.ChainValid {
		integrityIssues = append(integrityIssues, "цепочка сегментов")
	}
	if !quality.CounterInvariantsValid {
		integrityIssues = append(integrityIssues, "инварианты счётчиков")
	}
	if !quality.QualityProgressionValid {
		integrityIssues = append(integrityIssues, "последовательность показателей качества")
	}
	if quality.DamagedSegments > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf("повреждённые/аварийные сегменты=%d", quality.DamagedSegments))
	}
	if quality.SegmentsWithoutQuality > 0 {
		integrityIssues = append(integrityIssues, "сегменты без сведений о качестве")
	}
	if quality.CriticalRuntimeHookFailures > 0 {
		integrityIssues = append(integrityIssues, "ошибки перехвата, способные скрыть часть событий")
	}
	if quality.DictionaryOverflow > 0 || quality.DictionaryTruncated > 0 {
		integrityIssues = append(integrityIssues, "неполный словарь имён")
	}
	if quality.ControlFailures > 0 {
		integrityIssues = append(integrityIssues, "ошибки служебных записей журнала")
	}
	if quality.OtherEvidenceLoss > 0 {
		integrityIssues = append(integrityIssues, "часть диагностических данных недоступна")
	}
	integrityCoverage := 1.0
	integrityExplanation := "цепочка сегментов, схема и счётчики согласованы"
	if len(integrityIssues) > 0 {
		integrityCoverage = 0
		integrityExplanation = "не доказаны: " + strings.Join(integrityIssues, "; ")
	}
	appendComponent("integrity", "Целостность доказательств", 20, integrityCoverage, false, integrityExplanation)
	if activeWeight == 0 {
		return 100, components
	}
	return roundDiagnosticCompleteness(earnedWeight * 100 / activeWeight), components
}

func describeDiagnosticCompleteness(score float64, components []DiagnosticCompletenessComponent) (string, string) {
	level, explanation := diagnosticCompletenessTier(score)
	missing := make([]string, 0, len(components))
	excluded := make([]string, 0, len(components))
	for _, component := range components {
		if component.Excluded {
			excluded = append(excluded, component.Label)
			continue
		}
		if component.MissingPoints > 0 {
			missing = append(missing, fmt.Sprintf(
				"%s: −%.2f из %.0f (%s)",
				component.Label,
				component.MissingPoints,
				component.Weight,
				component.Explanation,
			))
		}
	}
	if len(missing) == 0 {
		explanation += " Недостающих баллов нет."
	} else {
		explanation += " Почему не 100%: " + strings.Join(missing, "; ") + "."
	}
	if len(excluded) > 0 {
		explanation += " Отключены настройками и не входят в расчёт: " + strings.Join(excluded, ", ") + "."
	}
	return level, explanation
}

func diagnosticCompletenessTier(score float64) (string, string) {
	switch {
	case score >= 95:
		return "excellent", "Максимальная полнота: индекс 95–100%; все активные источники диагностических данных практически полностью подтверждены."
	case score >= 85:
		return "high", "Высокая полнота: индекс 85–94,99%; основные диагностические данные подтверждены, оставшиеся ограничения явно перечислены."
	case score >= 65:
		return "sufficient", "Достаточная полнота: индекс 65–84,99%; выводы применимы с учётом перечисленных ограничений."
	case score >= 40:
		return "limited", "Ограниченная полнота: индекс 40–64,99%; существенная часть активных диагностических данных не подтверждена."
	default:
		return "low", "Низкая полнота: индекс ниже 40%; отчёт нельзя использовать для уверенных выводов без повторного сбора."
	}
}

func roundDiagnosticCompleteness(value float64) float64 {
	return math.Round(value*100) / 100
}

func qualityProgressionIssues(results []jhlog.StreamResult) []string {
	chains := map[string][]jhlog.StreamResult{}
	for _, result := range results {
		chains[qualityIdentityKey(result)] = append(chains[qualityIdentityKey(result)], result)
	}
	var issues []string
	for _, chain := range chains {
		sort.Slice(chain, func(i, j int) bool {
			return chain[i].Header.SegmentIndex < chain[j].Header.SegmentIndex
		})
		for index := 1; index < len(chain); index++ {
			previous := chain[index-1]
			current := chain[index]
			if previous.LatestQuality == nil || current.LatestQuality == nil {
				continue
			}
			if err := jhlog.ValidateQualityProgression(*previous.LatestQuality, *current.LatestQuality); err != nil {
				issues = append(issues, fmt.Sprintf(
					"session %x имеет немонотонные quality snapshots между segment %d и %d: %v",
					current.Header.SessionID,
					previous.Header.SegmentIndex,
					current.Header.SegmentIndex,
					err,
				))
			}
		}
	}
	sort.Strings(issues)
	return uniqueStrings(issues)
}

func lowerConfidenceLevel(current, candidate string) string {
	if confidenceRank(candidate) < confidenceRank(current) {
		return candidate
	}
	return current
}

func saturatingUint64Sum(values ...uint64) uint64 {
	total := uint64(0)
	for _, value := range values {
		if math.MaxUint64-total < value {
			return math.MaxUint64
		}
		total += value
	}
	return total
}

func (c *collector) retentionDataQuality() retentionDataQuality {
	quality := c.latestQualityTotals()
	result := retentionDataQuality{}
	for _, reason := range []jhlog.QualityLossReason{
		jhlog.QualityLossQueueFull,
		jhlog.QualityLossNotAccepting,
		jhlog.QualityLossIOLost,
		jhlog.QualityLossOversized,
		jhlog.QualityLossSizeLimit,
		jhlog.QualityLossAdmissionContention,
		jhlog.QualityLossStorageBudget,
	} {
		result.runtimeLoss += quality[jhlog.EventQualityCounterID(jhlog.EventRetained, reason)]
	}
	if result.runtimeLoss > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(result.runtimeNotes, fmt.Sprintf("потеряно retained-событий: %d", result.runtimeLoss))
	}
	if watcherLoss := quality[jhlog.QualityObjectWatcherLimit]; watcherLoss > 0 {
		result.runtimeLoss += watcherLoss
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(
			result.runtimeNotes,
			fmt.Sprintf("наблюдатель удержания отклонил объектов из-за лимита: %d", watcherLoss),
		)
	}
	if lifecycleLoss := quality[jhlog.QualityLifecycleRegistryLimit]; lifecycleLoss > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(
			result.runtimeNotes,
			fmt.Sprintf("реестр lifecycle-наблюдения достиг лимита: %d", lifecycleLoss),
		)
	}
	if dropped := c.counterValues["jankhunter.events_dropped.count"]; dropped > 0 {
		result.runtimeMayBeIncomplete = true
		result.runtimeNotes = append(result.runtimeNotes, fmt.Sprintf("writer отбросил события неизвестных типов: %d", dropped))
	}
	for _, segment := range c.summary.CollectionSegments {
		if segment.Status == string(jhlog.SegmentStatusOpenWithTail) ||
			segment.Status == string(jhlog.SegmentStatusCorrupt) {
			result.runtimeMayBeIncomplete = true
			result.runtimeNotes = append(
				result.runtimeNotes,
				fmt.Sprintf("сегмент %s имеет статус %s и хвост %d байт", segment.Source, segment.Status, segment.TailBytes),
			)
		}
	}
	dictionaryLoss := quality[jhlog.QualityDictionaryOverflowTotal]
	if dictionaryLoss > 0 || c.dictionaryOverflow > 0 {
		result.dictionaryDegraded = true
		result.dictionaryNotes = append(
			result.dictionaryNotes,
			"имена retained-класса или держателя могли быть заменены overflow-ссылкой",
		)
	}
	if c.heap != nil && len(c.heap.Warnings) > 0 {
		result.heapDegraded = true
		for _, warning := range c.heap.Warnings {
			result.heapNotes = append(result.heapNotes, "HPROF: "+warning)
		}
	}
	result.runtimeNotes = uniqueStrings(result.runtimeNotes)
	result.dictionaryNotes = uniqueStrings(result.dictionaryNotes)
	result.heapNotes = uniqueStrings(result.heapNotes)
	return result
}

func qualityCounterWarnings(counters map[uint64]uint64, exactAdmission bool) []string {
	items := []struct {
		id    uint64
		label string
	}{
		{jhlog.QualityQueueFullTotal, "очередь событий была заполнена"},
		{jhlog.QualityNotAcceptingTotal, "события пришли после остановки приёма"},
		{jhlog.QualityControlLaneFullTotal, "служебная очередь writer была заполнена"},
		{jhlog.QualityControlTimeoutTotal, "служебные команды writer завершились по таймауту"},
		{jhlog.QualityControlInterruptedTotal, "служебные команды writer были прерваны"},
		{jhlog.QualityWriterIOErrorTotal, "writer встретил ошибки ввода-вывода"},
		{jhlog.QualityEventLostAfterIOTotal, "события потеряны после ошибки записи"},
		{jhlog.QualityEventLostAfterSizeLimitTotal, "события потеряны после достижения лимита session-файла"},
		{jhlog.QualityEventLostAfterStorageBudget, "storage_budget_exhausted: активный запуск исчерпал общий бюджет .jhlog"},
		{jhlog.QualityDictionaryValueTruncated, "значения словаря были усечены"},
		{jhlog.QualityOversizedRecordTotal, "слишком крупные записи не поместились в чанк"},
		{jhlog.QualityFailedChunkTotal, "чанки не удалось зафиксировать"},
		{jhlog.QualityRecoveryTotal, "writer выполнял восстановление после ошибки"},
		{jhlog.QualityCloseTimeoutTotal, "закрытие writer завершилось по таймауту"},
		{jhlog.QualityMetricCardinalityLoss, "метрики потеряны из-за лимита кардинальности"},
		{jhlog.QualityInvalidMetric, "некорректные метрики отклонены"},
		{jhlog.QualityRuntimeGraphShutdownLoss, "runtime-граф не успел завершить drain при shutdown"},
		{jhlog.QualityRuntimeGraphWriterRejectionLoss, "writer отклонил batch runtime-графа"},
		{jhlog.QualityRuntimeStackMismatch, "runtime-стек вызовов рассинхронизировался"},
		{jhlog.QualityHandlerContentionBypass, "Handler instrumentation была обойдена из-за конкуренции registry"},
		{jhlog.QualityRuntimeGraphDisabled, "runtime-граф явно отключён конфигурацией"},
		{jhlog.QualityRuntimeEventBufferCapacityLoss, "producer buffer method/log events был заполнен"},
		{jhlog.QualityRuntimeEventRegistryCapacityLoss, "реестр producer buffers method/log events был заполнен"},
		{jhlog.QualityMethodCounterCardinalityLoss, "method counters достигли лимита кардинальности"},
		{jhlog.QualityRuntimeEventWriterRejectionLoss, "writer отклонил batch method/log events"},
		{jhlog.QualityLogSpamCardinalityLoss, "агрегатор логов достиг лимита кардинальности"},
		{jhlog.QualityHandlerEntryLimit, "реестр Handler достиг лимита записей"},
		{jhlog.QualityHandlerWrapperLimit, "реестр Handler достиг лимита wrapper-объектов"},
		{jhlog.QualityLifecycleRegistryLimit, "реестр lifecycle-наблюдения достиг лимита объектов"},
		{jhlog.QualityObjectWatcherLimit, "наблюдатель удержания достиг лимита объектов"},
		{jhlog.QualityJankStatsHandleLimit, "реестр JankStats достиг лимита активных окон"},
		{jhlog.QualityMetricFlushTimeout, "агрегированные метрики не успели попасть в writer до таймаута"},
		{jhlog.QualityPreparedStatementRegistryEviction, "реестр prepared statement вытеснил активные записи из-за лимита ёмкости"},
		{jhlog.QualityPreparedStatementResolutionMiss, "execute-вызовы потеряли SQL-шаблон после вытеснения из реестра prepared statement"},
		{jhlog.QualityReceiverAsyncRegistryEviction, "реестр BroadcastReceiver.goAsync вытеснил незавершённые PendingResult из-за лимита ёмкости"},
		{jhlog.QualityReceiverAsyncResolutionMiss, "PendingResult.finish не удалось сопоставить с goAsync после вытеснения из реестра"},
	}
	if !exactAdmission {
		items = append(items, struct {
			id    uint64
			label string
		}{jhlog.QualityWriterAdmissionContentionTotal, "BEST_EFFORT writer обошёл admission из-за конкуренции producers"})
	}
	warnings := make([]string, 0, len(items)+1)
	for _, item := range items {
		if value := counters[item.id]; value > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: %s: %d.", item.label, value))
		}
	}
	accepted := counters[jhlog.QualityAcceptedEventTotal]
	written := counters[jhlog.QualityWrittenEventTotal]
	if accepted > written {
		warnings = append(warnings, fmt.Sprintf("Качество сбора: принято %d событий, но зафиксировано %d; разница: %d.", accepted, written, accepted-written))
	}
	return warnings
}

func runtimeHookFailureDetails(counters map[uint64]uint64) ([]RuntimeHookFailureDetail, uint64, uint64) {
	descriptors := []struct {
		id          uint64
		reason      string
		impact      string
		explanation string
	}{
		{jhlog.QualityRuntimeHookInstrumentationFailure, "instrumentation_hook", "evidence_loss", "инжектированный hook завершился через fail-open"},
		{jhlog.QualityRuntimeHookAsyncWrapperFailure, "async_wrapper", "evidence_loss", "обёртка Runnable, Callable или coroutine не записала evidence"},
		{jhlog.QualityRuntimeHookLifecycleFailure, "runtime_lifecycle", "evidence_loss", "операция запуска, остановки или flush runtime завершилась ошибкой"},
		{jhlog.QualityRuntimeHookCollectorFailure, "collector", "evidence_loss", "runtime collector подавил внутреннюю ошибку"},
		{jhlog.QualityRuntimeHookContextFailure, "context", "evidence_loss", "контекст экрана, операции или источника мог быть неполным"},
		{jhlog.QualityRuntimeHookSchedulerFailure, "scheduler", "evidence_loss", "служебная задача runtime не была выполнена штатно"},
		{jhlog.QualityJankStatsDependencyMissing, "jankstats_dependency_missing", "fallback", "AndroidX Metrics отсутствовал; использован Choreographer fallback"},
		{jhlog.QualityJankStatsInstallFailure, "jankstats_install", "fallback", "JankStats не установился; использован Choreographer fallback"},
		{jhlog.QualityJankStatsFrameFailure, "jankstats_frame", "evidence_loss", "активный JankStats не смог декодировать frame evidence"},
		{jhlog.QualityJankStatsControlFailure, "jankstats_control", "evidence_loss", "не удалось переключить состояние активного JankStats"},
		{jhlog.QualityRuntimeHookUnclassifiedFailure, "unclassified", "evidence_loss", "источник fail-open ошибки не был классифицирован"},
	}
	details := make([]RuntimeHookFailureDetail, 0, len(descriptors)+1)
	classified := uint64(0)
	critical := uint64(0)
	for _, descriptor := range descriptors {
		count := counters[descriptor.id]
		if count == 0 {
			continue
		}
		classified = saturatingUint64Sum(classified, count)
		if descriptor.impact == "evidence_loss" {
			critical = saturatingUint64Sum(critical, count)
		}
		details = append(details, RuntimeHookFailureDetail{
			Reason: descriptor.reason, Count: count, Impact: descriptor.impact, Explanation: descriptor.explanation,
		})
	}
	total := counters[jhlog.QualityRuntimeHookFailureTotal]
	if total > classified {
		unclassified := total - classified
		critical = saturatingUint64Sum(critical, unclassified)
		details = append(details, RuntimeHookFailureDetail{
			Reason: "unclassified", Count: unclassified, Impact: "evidence_loss",
			Explanation: "quality snapshot не содержит reason-coded разбивку для этой части ошибок",
		})
	}
	return details, critical, classified
}

func runtimeHookFailureReasonSummary(details []RuntimeHookFailureDetail, criticalOnly bool) string {
	parts := make([]string, 0, len(details))
	for _, detail := range details {
		if criticalOnly && detail.Impact != "evidence_loss" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", detail.Reason, detail.Count))
	}
	if len(parts) == 0 {
		return "нет"
	}
	return strings.Join(parts, ", ")
}

func formatDurationNanos(value uint64) string {
	if value < 1_000 {
		return fmt.Sprintf("%d нс", value)
	}
	if value < 1_000_000 {
		return fmt.Sprintf("%.3f мкс", float64(value)/1_000)
	}
	return fmt.Sprintf("%.3f мс", float64(value)/1_000_000)
}

func (c *collector) instrumentationQualityWarnings() []string {
	var warnings []string
	diagnostics := c.diagnostics
	if diagnostics == nil || !diagnostics.Available {
		// Build-time diagnostics are optional developer evidence. End users are expected to have
		// only a self-contained .jhlog, so their absence is not a collection-quality defect.
		return warnings
	} else {
		if diagnostics.ClassCount == 0 {
			warnings = append(warnings, "Качество сбора: ASM-диагностика пустая, значит instrument matcher не увидел классы или артефакт не был собран.")
		}
		if diagnostics.ClassCount > 0 && diagnostics.HookCount == 0 && diagnostics.AnnotatedMethodCount == 0 {
			warnings = append(warnings, "Качество сбора: ASM прошел по классам, но не нашел hooks или аннотации; проверьте include/exclude, версии библиотек и включенные bridge-флаги.")
		}
		if unsupported := unsupportedDecisionCount(diagnostics); unsupported > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: ASM встретил неподдержанные сигнатуры hooks: %d; часть телеметрии могла не попасть в лог.", unsupported))
		}
	}
	return warnings
}

func (c *collector) attributionQualityWarnings(summary Summary) []string {
	var warnings []string
	if totalProblemWindows(summary) > 0 && unknownProblemOwnerRate(summary.ProblemWindows) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство проблемных окон не имеют понятного owner; добавьте ownerHint/withOwner или проверьте охват ASM-инструментации.")
	}
	if len(summary.SignalContexts) > 0 && unknownSignalContextRate(summary.SignalContexts) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство сигналов не имеют экрана, операции или источника; проверьте автоматическое отслеживание экранов, @JankHunterOperation/traceOperation/withOwner и охват инструментирования.")
	}
	if summary.EventCount > 0 && datavalue.IsUnknown(c.currentDevice) {
		warnings = append(warnings, "Качество сбора: модель устройства не записана в session-событие; проверьте JankHunter init и device snapshot при старте runtime.")
	}
	if summary.EventCount > 0 && datavalue.IsUnknown(c.currentAppVersion) && datavalue.IsUnknown(c.currentBuild) {
		warnings = append(warnings, "Качество сбора: версия приложения не записана в session-событие; проверьте PackageInfo/versionName/versionCode на старте runtime.")
	}
	if summary.EventCount > 0 && len(summary.Processes) == 1 && datavalue.IsUnknown(summary.Processes[0].Name) {
		warnings = append(warnings, "Качество сбора: процесс неизвестен; проверьте session-события и mainProcessOnly/allowedProcesses.")
	}
	return warnings
}

func unsupportedDecisionCount(diagnostics *InstrumentationDiagnostics) uint64 {
	var total uint64
	if diagnostics == nil {
		return 0
	}
	for _, decision := range diagnostics.Decisions {
		if decision.Kind == "unsupported" || decision.Reason == "unsupported_signature" {
			total += decision.Count
		}
	}
	return total
}

func unknownProblemOwnerRate(problems []ProblemWindowStats) float64 {
	var total uint64
	var unknown uint64
	for _, problem := range problems {
		if problem.Count == 0 {
			continue
		}
		total += problem.Count
		if datavalue.IsUnknown(problem.Owner) {
			unknown += problem.Count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(unknown) / float64(total)
}

func unknownSignalContextRate(contexts []SignalContextStats) float64 {
	var total uint64
	var unknown uint64
	for _, context := range contexts {
		count := uint64(context.HTTPCount) + uint64(context.StallCount) + context.LogSpam + context.ProblemCount + uint64(context.UIWindows)
		if count == 0 {
			count = 1
		}
		total += count
		if datavalue.IsUnknown(context.Screen) &&
			datavalue.IsUnknown(context.Operation) &&
			datavalue.IsUnknown(context.Owner) {
			unknown += count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(unknown) / float64(total)
}

func (c *collector) filterWarnings(summary Summary) []string {
	if !filterActive(c.filter) {
		return nil
	}
	var globalSignals []string
	if summary.ContextCount > 0 {
		globalSignals = append(globalSignals, "контекст устройства")
	}
	if len(summary.Counters) > 0 || len(summary.Gauges) > 0 {
		globalSignals = append(globalSignals, "custom metrics")
	}
	if len(globalSignals) == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf(
			"Фильтр применен к событиям с маршрутом, экраном, источником или классом; %s не несут полного контекста выполнения и показаны глобально.",
			strings.Join(globalSignals, " и "),
		),
	}
}

func (c *collector) runEnvironment(summary Summary) RunEnvironment {
	device := unknownIfEmpty(c.currentDevice)
	manufacturer := unknownIfEmpty(c.currentMaker)
	brand := unknownIfEmpty(c.currentBrand)
	hardware := unknownIfEmpty(c.currentHardware)
	board := unknownIfEmpty(c.currentBoard)
	product := unknownIfEmpty(c.currentProduct)
	abi := unknownIfEmpty(c.currentPrimaryABI)
	abis := unknownIfEmpty(c.currentABIs)
	network := unknownIfEmpty(c.currentNetwork)
	app := unknownIfEmpty(c.currentAppVersion)
	build := unknownIfEmpty(c.currentBuild)
	process := unknownIfEmpty(c.currentProcess)

	return RunEnvironment{
		Title:    datavalue.HumanUnknown(device, "неизвестное устройство"),
		Subtitle: fmt.Sprintf("%s · %s · процесс %s", osValue(c.currentAndroid, c.currentSDK), appBuildValue(app, build), datavalue.HumanUnknown(process, "неизвестен")),
		Items: []InfoItem{
			{Label: "Батарея", Value: batteryValue(summary.BatteryLastPct), Detail: batteryDetail(summary)},
			{Label: "Сеть", Value: datavalue.HumanUnknown(network, "неизвестно"), Detail: networkDetail(summary)},
			{Label: "Свободная RAM", Value: formatDataSize(summary.AvailMemoryLastKB), Detail: memoryDetail(summary)},
			{Label: "Свободное хранилище", Value: formatDataSize(summary.FreeStorageKB), Detail: storageDetail(summary)},
			{Label: "Android", Value: osValue(c.currentAndroid, c.currentSDK), Detail: androidDetail(c.currentSDK, c.currentPatch)},
			{Label: "Рут-доступ", Value: rootValue(summary.DeviceRootKnown, summary.DeviceRooted), Detail: rootDetail(summary.DeviceRootKnown, summary.DeviceRooted)},
			{Label: "CPU ABI", Value: datavalue.HumanUnknown(abi, "неизвестно"), Detail: fmt.Sprintf("поддерживаются %s", datavalue.HumanUnknown(abis, "неизвестно"))},
			{Label: "Железо", Value: datavalue.HumanUnknown(hardware, "неизвестно"), Detail: fmt.Sprintf("плата %s · продукт %s", datavalue.HumanUnknown(board, "неизвестна"), datavalue.HumanUnknown(product, "неизвестен"))},
			{Label: "Бренд", Value: datavalue.HumanUnknown(manufacturer, "неизвестно"), Detail: fmt.Sprintf("бренд %s", datavalue.HumanUnknown(brand, "неизвестен"))},
		},
	}
}

func Compare(baseline, candidate Summary) Comparison {
	comparison := Comparison{Baseline: baseline, Candidate: candidate}
	confidence := confidence(baseline, candidate)
	baselineLogSpam := totalLogSpam(baseline)
	candidateLogSpam := totalLogSpam(candidate)
	baselineProblemWindows := totalProblemWindows(baseline)
	candidateProblemWindows := totalProblemWindows(candidate)
	comparison.Deltas = append(comparison.Deltas,
		observedDelta("HTTP p95", baseline.HTTPP95MS, candidate.HTTPP95MS, "мс", true, uint64(baseline.HTTPCount), uint64(candidate.HTTPCount), "HTTP-запросы не зафиксированы"),
		observedDeltaFloat("HTTP failure rate", percentCount(baseline.HTTPFailed, baseline.HTTPCount), percentCount(candidate.HTTPFailed, candidate.HTTPCount), "п.п.", true, uint64(baseline.HTTPCount), uint64(candidate.HTTPCount), "HTTP-запросы не зафиксированы"),
		observedDeltaFloat("UI jank rate", baseline.UIJankPct, candidate.UIJankPct, "п.п.", true, baseline.UIFrames, candidate.UIFrames, "UI-кадры не зафиксированы"),
		observedDeltaFloat("UI avg FPS", baseline.UIAvgFPS, candidate.UIAvgFPS, "FPS", false, baseline.UIFPSMeasuredFrames, candidate.UIFPSMeasuredFrames, "недостаточно непрерывных UI-кадров для оценки FPS"),
		delta("Main-thread stall max", baseline.StallMaxMS, candidate.StallMaxMS, "мс", true, minUint64(uint64(baseline.StallCount), uint64(candidate.StallCount))),
		observedDelta("Max PSS", baseline.MemoryMaxKB, candidate.MemoryMaxKB, "КБ", true, uint64(baseline.MemoryCount), uint64(candidate.MemoryCount), "PSS не измерялся"),
		observedDelta("Min available memory", baseline.AvailMemoryMinKB, candidate.AvailMemoryMinKB, "КБ", false, uint64(baseline.ContextCount), uint64(candidate.ContextCount), "снимки контекста памяти отсутствуют"),
		observedDelta("UID RX delta", baseline.TrafficRxMax, candidate.TrafficRxMax, "байт", true, uint64(baseline.ContextCount), uint64(candidate.ContextCount), "снимки сетевого контекста отсутствуют"),
		observedDelta("UID TX delta", baseline.TrafficTxMax, candidate.TrafficTxMax, "байт", true, uint64(baseline.ContextCount), uint64(candidate.ContextCount), "снимки сетевого контекста отсутствуют"),
		delta("Retained objects", baseline.Retained, candidate.Retained, "шт", true, minUint64(baseline.Retained, candidate.Retained)),
		durationRateDelta("Log spam", baselineLogSpam, candidateLogSpam, baseline.DurationMS, candidate.DurationMS, minUint64(baselineLogSpam, candidateLogSpam)),
		durationRateDelta("Problem windows", baselineProblemWindows, candidateProblemWindows, baseline.DurationMS, candidate.DurationMS, minUint64(baselineProblemWindows, candidateProblemWindows)),
		mixDelta("Process mix", baseline.Processes, candidate.Processes, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("App version mix", baseline.AppVersions, candidate.AppVersions, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("SDK mix", baseline.SDKs, candidate.SDKs, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("Device mix", baseline.Devices, candidate.Devices, minUint64(uint64(baseline.LogCount), uint64(candidate.LogCount))),
		mixDelta("Network mix", baseline.Network, candidate.Network, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		mixDelta("Cohort mix", baseline.Cohorts, candidate.Cohorts, minUint64(uint64(baseline.EventCount), uint64(candidate.EventCount))),
	)
	for i := range comparison.Deltas {
		comparison.Deltas[i].Confidence = confidence
		comparison.Deltas[i].Severity = adjustedSeverity(
			comparison.Deltas[i].Severity,
			confidence,
			comparison.Deltas[i].SampleSize,
		)
	}
	comparison.CohortWarnings = cohortWarnings(baseline, candidate)
	comparison.QualityWarnings = comparisonQualityWarnings(baseline, candidate)
	comparison.ExposureWarnings = durationComparisonWarnings(baseline, candidate)
	comparison.Database = compareDatabaseAnalysis(baseline, candidate)
	comparison.AndroidComponents = compareAndroidComponentAnalysis(baseline, candidate)
	comparison.OperationDeltas = compareOperationAnalysis(baseline, candidate)
	androidPartial := baseline.AndroidComponents != nil && baseline.AndroidComponents.Partial ||
		candidate.AndroidComponents != nil && candidate.AndroidComponents.Partial
	if comparison.AndroidComponents.Note != "" && (androidPartial ||
		!comparison.AndroidComponents.Comparable) && (baseline.AndroidComponents != nil || candidate.AndroidComponents != nil) {
		comparison.QualityWarnings = append(comparison.QualityWarnings, "Android Components/IPC: "+comparison.AndroidComponents.Note)
	}
	comparison.Warnings = append(append(append([]string{}, comparison.CohortWarnings...), comparison.QualityWarnings...), comparison.ExposureWarnings...)
	comparison.ProblemComparison = CompareProblems(baseline, candidate, len(comparison.CohortWarnings) == 0)
	return comparison
}

func mixDelta(name string, baseline, candidate []NamedValue, sampleSize uint64) Delta {
	baselineTotal := namedValueTotal(baseline)
	candidateTotal := namedValueTotal(candidate)
	before := namedShareSummary(baseline)
	after := namedShareSummary(candidate)
	result := Delta{
		Name:       name,
		Baseline:   before,
		Candidate:  after,
		Change:     "без существенных изменений",
		Severity:   "ok",
		Comparable: true,
		SampleSize: sampleSize,
		Interval:   sampleNote(sampleSize),
	}
	if baselineTotal == 0 || candidateTotal == 0 {
		if baselineTotal == 0 {
			result.Baseline = "нет данных"
		}
		if candidateTotal == 0 {
			result.Candidate = "нет данных"
		}
		return markDeltaUnavailable(result, baselineTotal, candidateTotal, "категориальный состав отсутствует хотя бы в одном прогоне")
	}
	distance := namedDistributionDistance(baseline, candidate)
	result.ComparisonNote = fmt.Sprintf("сравниваются доли категорий, а не абсолютное число служебных событий; суммарное различие долей %.1f п.п.", distance*100)
	severity := "ok"
	if distance > 0.05 {
		severity = "medium"
		result.Change = "доли изменились"
	}
	result.Severity = severity
	return result
}

func observedDelta(name string, before, after uint64, unit string, higherIsWorse bool, baselineSamples, candidateSamples uint64, absence string) Delta {
	result := delta(name, before, after, unit, higherIsWorse, minUint64(baselineSamples, candidateSamples))
	if baselineSamples > 0 && candidateSamples > 0 {
		return result
	}
	if baselineSamples == 0 {
		result.Baseline = "нет данных"
	}
	if candidateSamples == 0 {
		result.Candidate = "нет данных"
	}
	return markDeltaUnavailable(result, baselineSamples, candidateSamples, absence+" хотя бы в одном прогоне")
}

func observedDeltaFloat(name string, before, after float64, unit string, higherIsWorse bool, baselineSamples, candidateSamples uint64, absence string) Delta {
	result := deltaFloat(name, before, after, unit, higherIsWorse, minUint64(baselineSamples, candidateSamples))
	if baselineSamples > 0 && candidateSamples > 0 {
		return result
	}
	if baselineSamples == 0 {
		result.Baseline = "нет данных"
	}
	if candidateSamples == 0 {
		result.Candidate = "нет данных"
	}
	return markDeltaUnavailable(result, baselineSamples, candidateSamples, absence+" хотя бы в одном прогоне")
}

func markDeltaUnavailable(result Delta, baselineSamples, candidateSamples uint64, reason string) Delta {
	result.Change = "не сравнивается"
	result.Severity = "ok"
	result.Interval = fmt.Sprintf("база=%d, кандидат=%d", baselineSamples, candidateSamples)
	result.Comparable = false
	result.ComparisonNote = reason
	result.ChangeAbs = 0
	result.ChangePct = 0
	result.RegressionAbs = 0
	result.RegressionPct = 0
	result.SampleSize = minUint64(baselineSamples, candidateSamples)
	return result
}

func durationRateDelta(name string, before, after, baselineDurationMS, candidateDurationMS, sampleSize uint64) Delta {
	if baselineDurationMS == 0 || candidateDurationMS == 0 {
		baseline := "нет данных"
		candidate := "нет данных"
		if baselineDurationMS > 0 {
			baseline = fmt.Sprintf("%.2f шт/мин", float64(before)*60_000/float64(baselineDurationMS))
		}
		if candidateDurationMS > 0 {
			candidate = fmt.Sprintf("%.2f шт/мин", float64(after)*60_000/float64(candidateDurationMS))
		}
		return markDeltaUnavailable(Delta{Name: name, Baseline: baseline, Candidate: candidate, Unit: "шт/мин"}, baselineDurationMS, candidateDurationMS, "длительность хотя бы одного прогона неизвестна")
	}
	baselineRate := float64(before) * 60_000 / float64(baselineDurationMS)
	candidateRate := float64(after) * 60_000 / float64(candidateDurationMS)
	result := relativeDeltaFloat(name, baselineRate, candidateRate, "шт/мин", true, sampleSize)
	result.ComparisonNote = fmt.Sprintf("нормировано по длительности: %d и %d событий", before, after)
	return result
}

func relativeDeltaFloat(name string, before, after float64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	diff := after - before
	changePct := 0.0
	severity := "ok"
	regressionAbs := 0.0
	regressionPct := 0.0
	change := "0.0%"
	if before == 0 && after > 0 {
		change = "+new"
		if higherIsWorse {
			severity = "medium"
			regressionAbs = after
			regressionPct = 100
		}
	} else if before != 0 {
		changePct = diff * 100 / before
		change = fmt.Sprintf("%+.1f%%", changePct)
		if higherIsWorse && changePct > 0 {
			regressionAbs = diff
			regressionPct = changePct
		} else if !higherIsWorse && changePct < 0 {
			regressionAbs = math.Abs(diff)
			regressionPct = math.Abs(changePct)
		}
		if regressionPct >= 25 {
			severity = "high"
		} else if regressionPct >= 10 {
			severity = "medium"
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%.2f %s", before, unit),
		Candidate:      fmt.Sprintf("%.2f %s", after, unit),
		Change:         change,
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Comparable:     true,
		Unit:           unit,
		BaselineValue:  before,
		CandidateValue: after,
		ChangeAbs:      diff,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func totalLogSpam(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total += item.Count
	}
	return total
}

func percentCount(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func totalProblemWindows(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.ProblemWindows {
		total += uint64(item.Windows)
	}
	return total
}

func cohortWarnings(baseline, candidate Summary) []string {
	checks := []struct {
		name      string
		baseline  []NamedValue
		candidate []NamedValue
	}{
		{name: "версий приложения", baseline: baseline.AppVersions, candidate: candidate.AppVersions},
		{name: "SDK", baseline: baseline.SDKs, candidate: candidate.SDKs},
		{name: "устройств", baseline: baseline.Devices, candidate: candidate.Devices},
		{name: "процессов", baseline: baseline.Processes, candidate: candidate.Processes},
		{name: "сетей", baseline: baseline.Network, candidate: candidate.Network},
		{name: "когорт", baseline: baseline.Cohorts, candidate: candidate.Cohorts},
	}
	var warnings []string
	for _, check := range checks {
		if namedValueTotal(check.baseline) == 0 || namedValueTotal(check.candidate) == 0 {
			continue
		}
		before := namedShareSummary(check.baseline)
		after := namedShareSummary(check.candidate)
		if namedDistributionDistance(check.baseline, check.candidate) > 0.05 {
			warnings = append(warnings, fmt.Sprintf("Состав %s отличается: база [%s], кандидат [%s].", check.name, before, after))
		}
	}
	return warnings
}

func durationComparisonWarnings(baseline, candidate Summary) []string {
	if baseline.DurationMS == 0 || candidate.DurationMS == 0 {
		return []string{"Длительность хотя бы одного прогона неизвестна: частотные метрики не сравниваются."}
	}
	shorter := baseline.DurationMS
	longer := candidate.DurationMS
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	if float64(longer-shorter)/float64(shorter) <= 0.2 {
		return nil
	}
	return []string{fmt.Sprintf(
		"Длительность прогонов отличается больше чем на 20%%: база %s, кандидат %s. Максимумы и редкие события могли получить разную экспозицию.",
		humanDurationMS(baseline.DurationMS),
		humanDurationMS(candidate.DurationMS),
	)}
}

func humanDurationMS(value uint64) string {
	if value < 1000 {
		return fmt.Sprintf("%d мс", value)
	}
	return fmt.Sprintf("%.1f с", float64(value)/1000)
}

func confidence(baseline, candidate Summary) string {
	sampleLevel := sampleConfidence(baseline, candidate)
	return lowerConfidenceLevel(
		lowerConfidenceLevel(
			lowerConfidenceLevel(sampleLevel, collectionConfidenceCap(baseline)),
			comparisonScopeConfidenceCap(baseline, candidate),
		),
		collectionConfidenceCap(candidate),
	)
}

func comparisonScopeConfidenceCap(baseline, candidate Summary) string {
	base := baseline.CollectionQuality
	next := candidate.CollectionQuality
	if base.ProcessScope == "" || next.ProcessScope == "" {
		return "high"
	}
	if base.ProcessScope != next.ProcessScope || base.AllowedProcessCount != next.AllowedProcessCount ||
		base.ProcessScopeFingerprint != next.ProcessScopeFingerprint ||
		base.ExpectedProcessCount != next.ExpectedProcessCount ||
		base.ExpectedProcessFingerprint != next.ExpectedProcessFingerprint {
		return "low"
	}
	return "high"
}

func sampleConfidence(baseline, candidate Summary) string {
	minLogs := baseline.LogCount
	if candidate.LogCount < minLogs {
		minLogs = candidate.LogCount
	}
	minEvents := baseline.EventCount
	if candidate.EventCount < minEvents {
		minEvents = candidate.EventCount
	}
	sampleLevel := "low"
	switch {
	case minLogs >= 5 && minEvents >= 500:
		sampleLevel = "high"
	case minLogs >= 2 && minEvents >= 80:
		sampleLevel = "medium"
	}
	return sampleLevel
}

func collectionConfidenceCap(summary Summary) string {
	if summary.CollectionQuality.Level == "" {
		return "high"
	}
	return summary.CollectionQuality.Level
}

func comparisonQualityWarnings(baseline, candidate Summary) []string {
	var warnings []string
	baseScope := baseline.CollectionQuality
	candidateScope := candidate.CollectionQuality
	if baseScope.ProcessScope != "" && candidateScope.ProcessScope != "" &&
		(baseScope.ProcessScope != candidateScope.ProcessScope ||
			baseScope.AllowedProcessCount != candidateScope.AllowedProcessCount ||
			baseScope.ProcessScopeFingerprint != candidateScope.ProcessScopeFingerprint ||
			baseScope.ExpectedProcessCount != candidateScope.ExpectedProcessCount ||
			baseScope.ExpectedProcessFingerprint != candidateScope.ExpectedProcessFingerprint) {
		warnings = append(warnings, fmt.Sprintf(
			"Process scope отличается: база %s (%d), кандидат %s (%d); сравнение ограничено низким доверием.",
			baseScope.ProcessScope,
			baseScope.AllowedProcessCount,
			candidateScope.ProcessScope,
			candidateScope.AllowedProcessCount,
		))
	}
	for _, item := range []struct {
		label   string
		summary Summary
	}{
		{label: "базы", summary: baseline},
		{label: "кандидата", summary: candidate},
	} {
		quality := item.summary.CollectionQuality
		if quality.Level == "" || quality.Level == "high" {
			continue
		}
		if len(quality.Reasons) == 0 {
			warnings = append(warnings, fmt.Sprintf("Качество данных %s ограничивает доверие уровнем %s.", item.label, quality.Level))
			continue
		}
		for _, reason := range quality.Reasons {
			warnings = append(warnings, fmt.Sprintf("Качество данных %s: %s.", item.label, reason))
		}
	}
	return uniqueStrings(warnings)
}

type ownerStatKey struct {
	owner string
	kind  string
}

func addOwner(stats map[ownerStatKey]*OwnerStats, owner, kind string, duration uint64, stack string) {
	if owner == "" {
		owner = "unknown"
	}
	key := ownerStatKey{owner: owner, kind: kind}
	item := stats[key]
	if item == nil {
		item = &OwnerStats{Owner: owner, Kind: kind}
		stats[key] = item
	}
	item.Count++
	item.TotalMS += duration
	if duration > item.MaxMS {
		item.MaxMS = duration
	}
	if item.StackHint == "" {
		item.StackHint = stack
	}
}

func fps(frames uint64, windowMS uint64) float64 {
	if frames == 0 || windowMS == 0 {
		return 0
	}
	return float64(frames) * 1000 / float64(windowMS)
}

const (
	minimumReliableFPSFrames = 30
	fpsIdleTolerance         = 6
)

// FPS is meaningful only while the UI is continuously producing enough frames. A partial window
// can contain one quick frame and then stay open while the screen is idle; dividing that frame by
// the whole wall-clock interval produces a false near-zero FPS. Frame tails remain available for
// every sample, while FPS uses only windows whose cadence is compatible with recorded durations.
func fpsWindowReliable(window *jhlog.UIWindowEvent) bool {
	if window == nil || window.FrameCount < minimumReliableFPSFrames || window.WindowMS == 0 {
		return false
	}
	typicalFrameMS := maxUint64(window.P95MS, maxUint64((window.FrameDeadlineUS+999)/1000, 16))
	averageIntervalMS := float64(window.WindowMS) / float64(window.FrameCount)
	return averageIntervalMS <= float64(typicalFrameMS*fpsIdleTolerance)
}

func fpsMeasurementStatus(frames uint64, measuredWindows int) string {
	if measuredWindows > 0 {
		return "measured"
	}
	if frames < minimumReliableFPSFrames {
		return "insufficient_frames"
	}
	return "sparse_rendering"
}

func sortRoutes(routes []RouteStats) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].P95MS == routes[j].P95MS {
			return routes[i].Count > routes[j].Count
		}
		return routes[i].P95MS > routes[j].P95MS
	})
}

func sortNetworkCalls(calls []NetworkCallStats) {
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Count != calls[j].Count {
			return calls[i].Count > calls[j].Count
		}
		if calls[i].P95MS != calls[j].P95MS {
			return calls[i].P95MS > calls[j].P95MS
		}
		if calls[i].Route != calls[j].Route {
			return calls[i].Route < calls[j].Route
		}
		if calls[i].Initiator != calls[j].Initiator {
			return calls[i].Initiator < calls[j].Initiator
		}
		return calls[i].Owner < calls[j].Owner
	})
}

func sortScreens(screens []ScreenStats) {
	sort.Slice(screens, func(i, j int) bool {
		if screens[i].JankRatePct == screens[j].JankRatePct {
			return screens[i].FrameP95MS > screens[j].FrameP95MS
		}
		return screens[i].JankRatePct > screens[j].JankRatePct
	})
}

func sortProcessExits(exits []ProcessExitStats) {
	sort.Slice(exits, func(i, j int) bool {
		if exits[i].LatestTimestampUnixMS != exits[j].LatestTimestampUnixMS {
			return exits[i].LatestTimestampUnixMS > exits[j].LatestTimestampUnixMS
		}
		if exits[i].Reason != exits[j].Reason {
			return exits[i].Reason < exits[j].Reason
		}
		return exits[i].Process < exits[j].Process
	})
}

func sortIOOperations(operations []IOStats) {
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].MainThread != operations[j].MainThread {
			return operations[i].MainThread
		}
		if operations[i].TotalDurationUS != operations[j].TotalDurationUS {
			return operations[i].TotalDurationUS > operations[j].TotalDurationUS
		}
		if operations[i].Operation != operations[j].Operation {
			return operations[i].Operation < operations[j].Operation
		}
		if operations[i].Source != operations[j].Source {
			return operations[i].Source < operations[j].Source
		}
		if operations[i].Owner != operations[j].Owner {
			return operations[i].Owner < operations[j].Owner
		}
		if operations[i].Screen != operations[j].Screen {
			return operations[i].Screen < operations[j].Screen
		}
		return operations[i].ContextOperation < operations[j].ContextOperation
	})
}

func sortOwners(owners []OwnerStats) {
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].MaxMS == owners[j].MaxMS {
			return owners[i].TotalMS > owners[j].TotalMS
		}
		return owners[i].MaxMS > owners[j].MaxMS
	})
}

func sortSignalContexts(contexts []SignalContextStats) {
	sort.Slice(contexts, func(i, j int) bool {
		left := signalContextSeverityScore(contexts[i])
		right := signalContextSeverityScore(contexts[j])
		if left == right {
			return contexts[i].Operation < contexts[j].Operation
		}
		return left > right
	})
}

func signalContextSeverityScore(context SignalContextStats) uint64 {
	return context.ProblemCount*10_000 +
		uint64(context.StallCount)*5_000 +
		context.UIJank*100 +
		context.LogSpam*10 +
		uint64(context.HTTPFailed)*500 +
		context.HTTPP95MS +
		context.ProblemMaxMS
}

func sortLogSpam(items []LogSpamStats) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Source < items[j].Source
		}
		return items[i].Count > items[j].Count
	})
}

func sortProblems(items []ProblemWindowStats) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].MaxMS == items[j].MaxMS {
			return items[i].Count > items[j].Count
		}
		return items[i].MaxMS > items[j].MaxMS
	})
}

func sortRuntimeCalls(items []RuntimeCallStats) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i].TotalMS + items[i].MaxMS*10 + items[i].Count
		right := items[j].TotalMS + items[j].MaxMS*10 + items[j].Count
		if left == right {
			if items[i].Caller == items[j].Caller {
				return items[i].Callee < items[j].Callee
			}
			return items[i].Caller < items[j].Caller
		}
		return left > right
	})
}

func sortMemoryLeaks(items []MemoryLeakSuspect) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			if items[i].MaxAgeMS == items[j].MaxAgeMS {
				return items[i].ClassName < items[j].ClassName
			}
			return items[i].MaxAgeMS > items[j].MaxAgeMS
		}
		return items[i].Score > items[j].Score
	})
}

func sortNamed(values []NamedValue) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Value == values[j].Value {
			return values[i].Name < values[j].Name
		}
		return values[i].Value > values[j].Value
	})
}

func namedValueTotal(values []NamedValue) uint64 {
	var total uint64
	for _, value := range values {
		total += value.Value
	}
	return total
}

func namedShareSummary(values []NamedValue) string {
	total := namedValueTotal(values)
	if total == 0 {
		return "нет данных"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s:%.1f%% (n=%d)", humanSummaryName(value.Name), float64(value.Value)*100/float64(total), value.Value))
	}
	if len(parts) == 0 {
		return "нет данных"
	}
	return strings.Join(parts, ",")
}

func namedDistributionDistance(baseline, candidate []NamedValue) float64 {
	baselineTotal := namedValueTotal(baseline)
	candidateTotal := namedValueTotal(candidate)
	if baselineTotal == 0 || candidateTotal == 0 {
		return 0
	}
	shares := map[string][2]float64{}
	for _, value := range baseline {
		pair := shares[value.Name]
		pair[0] += float64(value.Value) / float64(baselineTotal)
		shares[value.Name] = pair
	}
	for _, value := range candidate {
		pair := shares[value.Name]
		pair[1] += float64(value.Value) / float64(candidateTotal)
		shares[value.Name] = pair
	}
	distance := 0.0
	for _, pair := range shares {
		distance += math.Abs(pair[0] - pair[1])
	}
	return distance / 2
}

func humanSummaryName(value string) string {
	value = datavalue.HumanUnknown(value, "неизвестно")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "неизвестно"
	}
	for i, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			fields[i] = datavalue.HumanUnknown(field, "неизвестно")
			continue
		}
		fields[i] = key + "=" + datavalue.HumanUnknown(raw, "неизвестно")
	}
	return strings.Join(fields, " ")
}

func retainedAgeBucket(ageMs uint64) string {
	switch {
	case ageMs < 10_000:
		return "<10s"
	case ageMs < 30_000:
		return "10s-30s"
	case ageMs < 60_000:
		return "30s-60s"
	default:
		return ">=60s"
	}
}

func delta(name string, before, after uint64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	change := "0"
	severity := "ok"
	changePct := 0.0
	changeAbs := signedUint64DeltaFloat(before, after)
	regressionAbs := 0.0
	regressionPct := 0.0
	if before == 0 && after > 0 {
		change = "+new"
		if higherIsWorse {
			severity = "medium"
			regressionAbs = float64(after)
			regressionPct = 100
		}
	} else if before > 0 {
		diff := signedUint64DeltaFloat(before, after)
		changePct = diff * 100 / float64(before)
		change = fmt.Sprintf("%+.1f%%", changePct)
		if higherIsWorse {
			if changePct > 0 {
				regressionAbs = diff
				regressionPct = changePct
			}
			if changePct >= 25 {
				severity = "high"
			} else if changePct >= 10 {
				severity = "medium"
			}
		} else {
			if changePct < 0 {
				regressionAbs = math.Abs(diff)
				regressionPct = math.Abs(changePct)
			}
			if changePct <= -25 {
				severity = "high"
			} else if changePct <= -10 {
				severity = "medium"
			}
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%d %s", before, unit),
		Candidate:      fmt.Sprintf("%d %s", after, unit),
		Change:         change,
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Comparable:     true,
		Unit:           unit,
		BaselineValue:  float64(before),
		CandidateValue: float64(after),
		ChangeAbs:      changeAbs,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func signedUint64DeltaFloat(before, after uint64) float64 {
	if after >= before {
		return float64(after - before)
	}
	return -float64(before - after)
}

func deltaFloat(name string, before, after float64, unit string, higherIsWorse bool, sampleSize uint64) Delta {
	diff := after - before
	severity := "ok"
	regressionAbs := 0.0
	regressionPct := 0.0
	changePct := 0.0
	if before != 0 {
		changePct = diff * 100 / before
	}
	if higherIsWorse {
		if diff > 0 {
			regressionAbs = diff
			if before == 0 {
				regressionPct = 100
			} else {
				regressionPct = math.Abs(changePct)
			}
		}
		if diff >= 3.0 {
			severity = "high"
		} else if diff >= 1.0 {
			severity = "medium"
		}
	} else {
		if diff < 0 {
			regressionAbs = math.Abs(diff)
			if before == 0 {
				regressionPct = 100
			} else {
				regressionPct = math.Abs(changePct)
			}
		}
		if diff <= -5.0 {
			severity = "high"
		} else if diff <= -2.0 {
			severity = "medium"
		}
	}
	return Delta{
		Name:           name,
		Baseline:       fmt.Sprintf("%.2f %s", before, unit),
		Candidate:      fmt.Sprintf("%.2f %s", after, unit),
		Change:         fmt.Sprintf("%+.2f %s", diff, unit),
		Severity:       severity,
		Interval:       sampleNote(sampleSize),
		Comparable:     true,
		Unit:           unit,
		BaselineValue:  before,
		CandidateValue: after,
		ChangeAbs:      diff,
		ChangePct:      changePct,
		RegressionAbs:  regressionAbs,
		RegressionPct:  regressionPct,
		SampleSize:     sampleSize,
	}
}

func adjustedSeverity(effectSeverity, confidence string, sampleSize uint64) string {
	if effectSeverity == "ok" {
		return "ok"
	}
	if confidence == "low" || sampleSize < 3 {
		if effectSeverity == "high" {
			return "medium"
		}
	}
	return effectSeverity
}

func sampleNote(sampleSize uint64) string {
	return fmt.Sprintf("выборка=%d", sampleSize)
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func formatMB(kb uint64) string {
	return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
}

func formatDataSize(kb uint64) string {
	if kb == 0 {
		return "неизвестно"
	}
	if kb >= 1024*1024 {
		return fmt.Sprintf("%.1f ГБ", float64(kb)/(1024*1024))
	}
	return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
}

func unknownIfEmpty(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func osValue(release string, sdk string) string {
	release = unknownIfEmpty(release)
	sdk = unknownIfEmpty(sdk)
	switch {
	case release == "unknown" && sdk == "unknown":
		return "Android неизвестен"
	case release == "unknown":
		return fmt.Sprintf("Android API %s", apiNumber(sdk))
	case sdk == "unknown":
		return fmt.Sprintf("Android %s", release)
	default:
		return fmt.Sprintf("Android %s", release)
	}
}

func appBuildValue(app string, build string) string {
	if app == "unknown" && build == "unknown" {
		return "версия приложения неизвестна"
	}
	if build == "unknown" {
		return app
	}
	if app == "unknown" {
		return fmt.Sprintf("версия неизвестна (%s)", build)
	}
	return fmt.Sprintf("%s (%s)", app, build)
}

func batteryValue(pct uint64) string {
	if pct == 0 {
		return "неизвестно"
	}
	return fmt.Sprintf("%d%%", pct)
}

func batteryDetail(summary Summary) string {
	parts := []string{batteryStateName(summary.BatteryStateLast)}
	if summary.BatteryTempDeciC != 0 {
		parts = append(parts, fmt.Sprintf("%.1f °C", float64(summary.BatteryTempDeciC)/10))
	}
	if summary.BatteryMinPct > 0 {
		parts = append(parts, fmt.Sprintf("мин. %d%%", summary.BatteryMinPct))
	}
	return strings.Join(parts, " · ")
}

func batteryStateName(state uint64) string {
	switch state {
	case 2:
		return "заряжается"
	case 3:
		return "разряжается"
	case 4:
		return "не заряжается"
	case 5:
		return "полный заряд"
	default:
		return "неизвестно"
	}
}

func networkDetail(summary Summary) string {
	return fmt.Sprintf(
		"валидирована %s · лимитная %s · VPN %s",
		yesNoRU(summary.NetworkValidated),
		yesNoRU(summary.NetworkMetered),
		yesNoRU(summary.NetworkVPN),
	)
}

func memoryDetail(summary Summary) string {
	parts := []string{}
	if summary.TotalMemoryKB > 0 {
		parts = append(parts, fmt.Sprintf("всего %s", formatDataSize(summary.TotalMemoryKB)))
	}
	if summary.AvailMemoryMinKB > 0 {
		parts = append(parts, fmt.Sprintf("мин. свободно %s", formatDataSize(summary.AvailMemoryMinKB)))
	}
	if summary.LowMemoryCount > 0 {
		parts = append(parts, fmt.Sprintf("сигналы low-memory %d", summary.LowMemoryCount))
	}
	if len(parts) == 0 {
		return "нет контекста памяти"
	}
	return strings.Join(parts, " · ")
}

func storageDetail(summary Summary) string {
	if summary.TotalStorageKB == 0 {
		return "раздел данных приложения"
	}
	return fmt.Sprintf("из %s раздел данных приложения", formatDataSize(summary.TotalStorageKB))
}

func androidDetail(sdk string, patch string) string {
	patch = unknownIfEmpty(patch)
	sdk = unknownIfEmpty(sdk)
	if patch == "unknown" {
		return fmt.Sprintf("API %s · патч безопасности неизвестен", apiNumber(sdk))
	}
	return fmt.Sprintf("API %s · патч безопасности %s", apiNumber(sdk), patch)
}

func apiNumber(sdk string) string {
	if sdk == "unknown" {
		return "неизвестен"
	}
	return strings.TrimPrefix(sdk, "api-")
}

func yesNoRU(value bool) string {
	if value {
		return "да"
	}
	return "нет"
}

func rootCohortValue(known bool, rooted bool) string {
	if !known {
		return "unknown"
	}
	if rooted {
		return "yes"
	}
	return "no"
}

func rootValue(known bool, rooted bool) string {
	if !known {
		return "неизвестно"
	}
	if rooted {
		return "да"
	}
	return "нет"
}

func rootDetail(known bool, rooted bool) string {
	if !known {
		return "нет сигнала о рут-доступе в метаданных сессии"
	}
	if rooted {
		return "обнаружены признаки рут-доступа"
	}
	return "признаки рут-доступа не найдены"
}
