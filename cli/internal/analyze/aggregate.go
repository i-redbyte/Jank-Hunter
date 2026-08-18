package analyze

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	collectionTrustScoreModel        = "evidence-v2:active-components-normalized;transport=40,runtime_graph=20,process_roster=20,integrity=20"
)

type qualityCounterWarning struct {
	name  string
	label string
}

var runtimeQualityCounterWarnings = []qualityCounterWarning{
	{"jankhunter.events_dropped.count", "очередь writer отбросила события"},
	{"jankhunter.writer_io_error.count", "writer видел ошибки записи"},
	{"jankhunter.writer_event_lost_on_io.count", "writer потерял события после ошибки записи"},
	{"jankhunter.metric_aggregation.dropped.count", "агрегатор метрик отбросил ключи из-за лимита кардинальности"},
	{"jankhunter.log_spam.dropped_keys.count", "агрегатор спама логами отбросил ключи из-за лимита кардинальности"},
	{"jankhunter.runtime_call_graph.dropped.count", "runtime-граф вызовов отбросил ребра из-за лимита или рассинхронизации стека"},
	{"jankhunter.handler_wrapper.dropped_entries.count", "реестр Handler-оберток отбросил записи из-за лимита"},
	{"jankhunter.handler_wrapper.dropped_wrappers.count", "реестр Handler-оберток отбросил wrapper из-за лимита"},
	{"jankhunter.activity_tracker.unavailable.count", "Activity lifecycle tracker не подключился, поэтому screen мог остаться неизвестным"},
}

func InspectFilesWithOptions(title string, paths []string, options Options) (Summary, error) {
	collector := newCollector(title, len(paths), options)
	for _, path := range paths {
		collector.startLog()
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
		if err := validateOwnerMapNamespace(options.OwnerMap, result.Header, result.Source); err != nil {
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

func LoadOwnerMap(path string) (*OwnerMap, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadOwnerMapJSONL(path, data)
}

// ReadOwnerMapNamespace validates and returns only the bounded first metadata record. Artifact
// discovery uses it to avoid loading every symbol entry from every build variant into memory.
func ReadOwnerMapNamespace(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineData := []byte(line)
		var envelope ownerMapEnvelope
		if err := json.Unmarshal(lineData, &envelope); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := validateOwnerMapFormat(path, envelope.Format); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if envelope.Kind != "metadata" {
			return nil, fmt.Errorf("%s: parse owner map line %d: metadata must be the first record", path, lineNumber)
		}
		var raw ownerMapMetadataRecord
		if err := decodeOwnerMapRecord(lineData, &raw); err != nil {
			return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
		}
		namespace, err := decodeOwnerMapNamespace(raw.SymbolNamespace)
		if err != nil {
			return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
		}
		return namespace, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%s: owner map has no metadata record", path)
}

// LoadOwnerMaps loads and combines module-local owner maps into the single
// process-wide stable-symbol namespace used by a .jhlog session. Every map is
// validated independently before it participates in the merge.
func LoadOwnerMaps(paths []string) (*OwnerMap, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	merged := &OwnerMap{Entries: make(map[string]string)}
	entrySources := make(map[string]string)
	namespaceSource := ""
	for _, path := range paths {
		if path == "" {
			return nil, fmt.Errorf("owner map path must not be empty")
		}
		ownerMap, err := LoadOwnerMap(path)
		if err != nil {
			return nil, fmt.Errorf("load owner map %q: %w", path, err)
		}
		if ownerMap == nil {
			return nil, fmt.Errorf("load owner map %q: empty owner map", path)
		}
		if namespaceSource == "" {
			merged.SymbolNamespace = append([]byte(nil), ownerMap.SymbolNamespace...)
			namespaceSource = path
		} else if !bytes.Equal(merged.SymbolNamespace, ownerMap.SymbolNamespace) {
			return nil, fmt.Errorf(
				"owner maps %q and %q use different symbolNamespace values: %s and %s",
				namespaceSource,
				path,
				hexOrEmpty(merged.SymbolNamespace),
				hexOrEmpty(ownerMap.SymbolNamespace),
			)
		}

		ids := make([]string, 0, len(ownerMap.Entries))
		for id := range ownerMap.Entries {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			owner := ownerMap.Entries[id]
			if existing, ok := merged.Entries[id]; ok {
				if existing != owner {
					return nil, fmt.Errorf(
						"owner maps %q and %q contain conflicting stable ID %q: %q and %q",
						entrySources[id],
						path,
						id,
						existing,
						owner,
					)
				}
				continue
			}
			merged.Entries[id] = owner
			entrySources[id] = path
		}
	}
	if err := validateLoadedOwnerMap(merged); err != nil {
		return nil, fmt.Errorf("merge owner maps: %w", err)
	}
	return merged, nil
}

type ownerMapEnvelope struct {
	Format int    `json:"format"`
	Kind   string `json:"kind"`
}

type ownerMapMetadataRecord struct {
	Format                  int             `json:"format"`
	Kind                    string          `json:"kind"`
	Variant                 string          `json:"variant"`
	IDAlgorithm             string          `json:"idAlgorithm"`
	IDEncoding              string          `json:"idEncoding"`
	GeneratedOwners         bool            `json:"generatedOwners"`
	SymbolNamespace         string          `json:"symbolNamespace"`
	IncludeWholeApplication bool            `json:"includeWholeApplication"`
	Hooks                   map[string]bool `json:"hooks"`
	AndroidNamespace        string          `json:"androidNamespace"`
	IncludePackages         []string        `json:"includePackages"`
	ExcludePackages         []string        `json:"excludePackages"`
}

type ownerMapEntryRecord struct {
	Format     int    `json:"format"`
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	ClassName  string `json:"class"`
	MethodName string `json:"method"`
	Descriptor string `json:"descriptor"`
}

func loadOwnerMapJSONL(path string, data []byte) (*OwnerMap, error) {
	out := &OwnerMap{Entries: map[string]string{}}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	recordNumber := 0
	metadataSeen := false
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		recordNumber++
		lineData := []byte(line)
		var envelope ownerMapEnvelope
		if err := json.Unmarshal(lineData, &envelope); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := validateOwnerMapFormat(path, envelope.Format); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		switch envelope.Kind {
		case "metadata":
			if metadataSeen || recordNumber != 1 {
				return nil, fmt.Errorf("%s: parse owner map line %d: metadata must be the first and only metadata record", path, lineNumber)
			}
			var raw ownerMapMetadataRecord
			if err := decodeOwnerMapRecord(lineData, &raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
			if err := addOwnerMapMetadata(out, raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
			metadataSeen = true
		case "entry":
			if !metadataSeen {
				return nil, fmt.Errorf("%s: parse owner map line %d: entry appears before metadata", path, lineNumber)
			}
			var raw ownerMapEntryRecord
			if err := decodeOwnerMapRecord(lineData, &raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
			if err := addOwnerMapEntry(out.Entries, raw); err != nil {
				return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
			}
		default:
			return nil, fmt.Errorf("%s: parse owner map line %d: unsupported record kind %q", path, lineNumber, envelope.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := validateLoadedOwnerMap(out); err != nil {
		return nil, fmt.Errorf("%s: parse owner map: %w", path, err)
	}
	return out, nil
}

func decodeOwnerMapRecord(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func addOwnerMapMetadata(out *OwnerMap, raw ownerMapMetadataRecord) error {
	decoded, err := decodeOwnerMapNamespace(raw.SymbolNamespace)
	if err != nil {
		return err
	}
	if len(out.SymbolNamespace) > 0 && !bytes.Equal(out.SymbolNamespace, decoded) {
		return fmt.Errorf("conflicting symbolNamespace metadata")
	}
	out.SymbolNamespace = decoded
	return nil
}

func decodeOwnerMapNamespace(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("metadata record has no symbolNamespace")
	}
	if len(value) != ownerMapNamespaceBytes*2 {
		return nil, fmt.Errorf("symbolNamespace must contain exactly %d lowercase hexadecimal bytes", ownerMapNamespaceBytes)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, fmt.Errorf("symbolNamespace must contain lowercase hexadecimal bytes")
	}
	return decoded, nil
}

func validateLoadedOwnerMap(ownerMap *OwnerMap) error {
	if len(ownerMap.SymbolNamespace) != ownerMapNamespaceBytes {
		return fmt.Errorf("owner map metadata symbolNamespace must contain exactly %d bytes", ownerMapNamespaceBytes)
	}
	return nil
}

const ownerMapNamespaceBytes = 16

func addOwnerMapEntry(out map[string]string, entry ownerMapEntryRecord) error {
	if !isCanonicalStableOwnerID(entry.ID) {
		return fmt.Errorf("owner map id %q is not canonical; expected stable:0x followed by 16 lowercase hexadecimal digits", entry.ID)
	}
	name := strings.TrimSpace(entry.Owner)
	if name == "" {
		return fmt.Errorf("owner map entry %q has no owner", entry.ID)
	}
	if existing, ok := out[entry.ID]; ok {
		if existing != name {
			return fmt.Errorf("conflicting owner map entry %q: %q and %q", entry.ID, existing, name)
		}
		return nil
	}
	out[entry.ID] = name
	return nil
}

func validateOwnerMapFormat(path string, got int) error {
	return validateArtifactFormat(path, "owner map", got, OwnerMapFormat)
}

type collector struct {
	summary             Summary
	filter              Filter
	ownerMap            *OwnerMap
	nameMap             *NameMapping
	classGraph          *ClassGraph
	diagnostics         *InstrumentationDiagnostics
	heap                *HeapEvidence
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

	httpDurations  uint64SampleSet
	routeDurations map[string]*uint64SampleSet
	routeFailures  map[string]int
	routeRx        map[string]uint64
	routeTx        map[string]uint64
	routeTTFB      map[string]uint64
	routeTTFBCount map[string]uint64
	routeOwner     map[string]string
	routeBursts    map[string]*routeBurstAccumulator

	screenStats        map[string]*ScreenStats
	processExitStats   map[string]*ProcessExitStats
	ioStats            map[string]*IOStats
	ownerStats         map[ownerStatKey]*OwnerStats
	flowStats          map[string]*FlowStats
	flowHTTPDurations  map[string]*uint64SampleSet
	logSpamStats       map[string]*LogSpamStats
	problemStats       map[string]*ProblemWindowStats
	runtimeCallStats   map[string]*RuntimeCallStats
	counterValues      map[string]uint64
	gaugeValues        map[string]*gaugeStats
	appVersions        map[string]uint64
	builds             map[string]uint64
	devices            map[string]uint64
	sdks               map[string]uint64
	cohortSamples      map[string]uint64
	networkSamples     map[string]uint64
	processSamples     map[string]uint64
	retainedClasses    map[string]*retainedClassStats
	retainedAgeBuckets map[string]uint64
	memoryLeakStats    map[string]*memoryLeakStats

	currentAppVersion string
	currentBuild      string
	currentDevice     string
	currentSDK        string
	currentProcess    string
	currentNetwork    string
	currentAndroid    string
	currentPatch      string
	currentPrimaryABI string
	currentABIs       string
	currentMaker      string
	currentBrand      string
	currentHardware   string
	currentBoard      string
	currentProduct    string
	currentRootKnown  bool
	currentRooted     bool
	currentLogIndex   uint64
	currentAttrScreen string
	currentAttrOwner  string
	currentAttrFlow   string
	currentAttrStep   string
	stableSymbols     stableSymbolResolver
}

type stableSymbolResolver struct {
	embedded         map[uint64]string
	unresolved       map[string]struct{}
	externalResolved bool
	external         bool
	requireExplicit  bool
}

func newCollector(title string, logCount int, options Options) *collector {
	return &collector{
		summary:            Summary{Title: title, LogCount: logCount},
		filter:             normalizeFilter(options.Filter),
		ownerMap:           options.OwnerMap,
		nameMap:            options.ObfuscationMap,
		classGraph:         DeobfuscateClassGraph(options.ClassGraph, options.ObfuscationMap),
		diagnostics:        options.InstrumentationDiagnostics,
		heap:               DeobfuscateHeapEvidence(options.HeapEvidence, options.ObfuscationMap),
		artifactDirectory:  options.ArtifactDirectory,
		artifactAuto:       options.ArtifactsAutoDiscovered,
		artifactNamespace:  append([]byte(nil), options.ArtifactSymbolNamespace...),
		routeDurations:     map[string]*uint64SampleSet{},
		routeFailures:      map[string]int{},
		routeRx:            map[string]uint64{},
		routeTx:            map[string]uint64{},
		routeTTFB:          map[string]uint64{},
		routeTTFBCount:     map[string]uint64{},
		routeOwner:         map[string]string{},
		routeBursts:        map[string]*routeBurstAccumulator{},
		screenStats:        map[string]*ScreenStats{},
		processExitStats:   map[string]*ProcessExitStats{},
		ioStats:            map[string]*IOStats{},
		ownerStats:         map[ownerStatKey]*OwnerStats{},
		flowStats:          map[string]*FlowStats{},
		flowHTTPDurations:  map[string]*uint64SampleSet{},
		logSpamStats:       map[string]*LogSpamStats{},
		problemStats:       map[string]*ProblemWindowStats{},
		runtimeCallStats:   map[string]*RuntimeCallStats{},
		counterValues:      map[string]uint64{},
		qualitySnapshots:   map[string]segmentQualityState{},
		gaugeValues:        map[string]*gaugeStats{},
		appVersions:        map[string]uint64{},
		builds:             map[string]uint64{},
		devices:            map[string]uint64{},
		sdks:               map[string]uint64{},
		cohortSamples:      map[string]uint64{},
		networkSamples:     map[string]uint64{},
		processSamples:     map[string]uint64{},
		retainedClasses:    map[string]*retainedClassStats{},
		retainedAgeBuckets: map[string]uint64{},
		memoryLeakStats:    map[string]*memoryLeakStats{},
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
		currentAttrFlow:    "unknown",
		currentAttrStep:    "unknown",
		stableSymbols: stableSymbolResolver{
			embedded:        map[uint64]string{},
			unresolved:      map[string]struct{}{},
			external:        options.ExternalSymbols,
			requireExplicit: options.RequireExplicitExternalSymbols,
		},
	}
}

func (c *collector) startLog() {
	c.currentLogIndex++
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
	c.currentAttrFlow = "unknown"
	c.currentAttrStep = "unknown"
}

type segmentQualityState struct {
	segmentIndex uint64
	snapshot     jhlog.QualitySnapshot
}

func (c *collector) addStreamResult(result jhlog.StreamResult) {
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
	c.currentAttrFlow = attrValue(jhlog.ResolveSymbol(dict, context.Flow))
	c.currentAttrStep = attrValue(jhlog.ResolveSymbol(dict, context.Step))
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
	case jhlog.IOOperationDatabaseRead:
		return "database_read"
	case jhlog.IOOperationDatabaseWrite:
		return "database_write"
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
		return fmt.Sprintf("last=%d samples=%d", s.last, s.count)
	case jhlog.MetricModeState:
		return fmt.Sprintf("state=%d samples=%d", s.last, s.count)
	case jhlog.MetricModeBooleanRate:
		return fmt.Sprintf("true_pct=%d true=%d samples=%d", s.value(), s.total, s.count)
	default:
		return fmt.Sprintf("avg=%d max=%d samples=%d", s.value(), s.max, s.count)
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
	flow                 string
	step                 string
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

func (c *collector) eventContext(screenOverride, ownerOverride, flowOverride, stepOverride string) FlowStats {
	return c.flowContextFromKey(c.contextKey(screenOverride, ownerOverride, flowOverride, stepOverride))
}

func (c *collector) matchesFilters(route string, context FlowStats, classCandidates []string, ownerCandidates ...string) bool {
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
	switch {
	case event.Session != nil:
		c.summary.CollectorSessions++
		c.summary.CollectorFlagsAny |= event.Session.CollectorFlags
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
		c.summary.DeviceRootKnown = true
		c.summary.DeviceRooted = event.Session.DeviceRooted
		c.appVersions[c.currentAppVersion]++
		c.builds[c.currentBuild]++
		c.devices[c.currentDevice]++
		c.sdks[c.currentSDK]++
		c.processSamples[c.currentProcess]++
	case event.HTTP != nil:
		route := jhlog.ResolveSymbol(dict, event.HTTP.RouteRef)
		owner := c.currentAttrOwner
		context := c.eventContext("", owner, "", "")
		if !c.matchesFilters(route, context, nil, owner) {
			return
		}
		c.markCohort()
		c.summary.HTTPCount++
		c.httpDurations.add(event.HTTP.DurationMS)
		c.sampleSet(c.routeDurations, route).add(event.HTTP.DurationMS)
		c.routeBurst(route).add(c.currentLogIndex, event.TimeMS)
		c.routeRx[route] += event.HTTP.RxBytes
		c.routeTx[route] += event.HTTP.TxBytes
		c.routeTTFB[route] += event.HTTP.TTFBMS
		c.routeTTFBCount[route]++
		if c.routeOwner[route] == "" {
			c.routeOwner[route] = owner
		}
		if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
			c.summary.HTTPFailed++
			c.routeFailures[route]++
		}
		addOwner(c.ownerStats, owner, "http", event.HTTP.DurationMS, "")
		flowKey := c.flowKey("", owner)
		flow := c.ensureFlow(flowKey)
		flow.HTTPCount++
		flow.RouteSample = firstNonEmpty(flow.RouteSample, route)
		c.sampleSet(c.flowHTTPDurations, flowKey).add(event.HTTP.DurationMS)
		if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
			flow.HTTPFailed++
		}
		failed := event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx
		slow := event.Flags&uint64(jhlog.FlagHTTPSlow) != 0
		if failed || slow {
			c.addProblemWindow(context, "http_slow_or_failed", event.HTTP.DurationMS, 1, event.HTTP.DurationMS)
		}
	case event.UIWindow != nil:
		screen := c.currentAttrScreen
		context := c.eventContext(screen, "", "", "")
		if !c.matchesFilters("", context, nil) {
			return
		}
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
		flowKey := c.flowKey(screen, "")
		flow := c.ensureFlow(flowKey)
		flow.UIWindows++
		flow.UIFrames += event.UIWindow.FrameCount
		flow.UIJank += event.UIWindow.JankCount
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
		flowOverride := ""
		stepOverride := ""
		if c.isHeapDumpStall(event.TimeMS, owner) {
			owner = "jankhunter.heap_dump"
			flowOverride = "jankhunter.diagnostics"
			stepOverride = "heap_dump"
		}
		context := c.eventContext("", owner, flowOverride, stepOverride)
		if !c.matchesFilters("", context, nil, owner) {
			return
		}
		c.markCohort()
		c.summary.StallCount++
		if event.Stall.DurationMS > c.summary.StallMaxMS {
			c.summary.StallMaxMS = event.Stall.DurationMS
		}
		addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
		flowKey := c.contextKey(context.Screen, context.Owner, context.Flow, context.Step)
		flow := c.ensureFlow(flowKey)
		flow.StallCount++
		if event.Stall.DurationMS > flow.StallMaxMS {
			flow.StallMaxMS = event.Stall.DurationMS
		}
		c.addProblemWindow(context, "main_thread_stall", event.Stall.DurationMS, 1, event.Stall.DurationMS)
	case event.Context != nil:
		c.summary.ContextCount++
		c.currentNetwork = jhlog.NetworkName(event.Context.Network)
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
		context := c.eventContext("", "", "", "")
		if !c.matchesFilters("", context, nil) {
			return
		}
		c.markCohort()
		c.summary.MemoryCount++
		if event.Memory.PSSKB > c.summary.MemoryMaxKB {
			c.summary.MemoryMaxKB = event.Memory.PSSKB
		}
		flow := c.ensureFlow(c.flowKey("", ""))
		if event.Memory.PSSKB > flow.MemoryMaxKB {
			flow.MemoryMaxKB = event.Memory.PSSKB
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
		context := c.eventContext("", "", "", "")
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		operation := ioOperationName(event.IO.Operation)
		mainThread := event.Flags&uint64(jhlog.FlagThreadMain) != 0
		key := c.contextKey(context.Screen, context.Owner, context.Flow, context.Step) + fmt.Sprintf("\x00%s\x00%t", operation, mainThread)
		stats := c.ioStats[key]
		if stats == nil {
			stats = &IOStats{
				Operation: operation, MainThread: mainThread,
				Screen: context.Screen, Flow: context.Flow, Step: context.Step, Owner: context.Owner,
			}
			c.ioStats[key] = stats
		}
		stats.Count++
		stats.TotalDurationUS = saturatingUint64Sum(stats.TotalDurationUS, event.IO.DurationUS)
		stats.MaxDurationUS = maxUint64(stats.MaxDurationUS, event.IO.DurationUS)
		stats.Bytes = saturatingUint64Sum(stats.Bytes, event.IO.Bytes)
	case event.Retained != nil:
		className := c.deobfuscate(jhlog.ResolveSymbol(dict, event.Retained.ClassRef))
		holder := c.resolveOwnerRef(dict, event.Retained.HolderRef)
		context := c.eventContext("", "", "", "")
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
		key := c.contextKey("", "", "", "")
		context := c.flowContextFromKey(key)
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
				Screen: context.Screen,
				Flow:   context.Flow,
				Step:   context.Step,
				Owner:  context.Owner,
				Source: source,
				Level:  level,
			}
			c.logSpamStats[logKey] = stats
		}
		stats.Count += event.LogSpam.Count
		flow := c.ensureFlow(key)
		flow.LogSpam += event.LogSpam.Count
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
		key := c.contextKey("", "", "", "")
		context := c.flowContextFromKey(key)
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		kind := jhlog.ResolveSymbol(dict, event.Problem.KindRef)
		c.addProblemWindow(context, kind, event.Problem.WindowMS, event.Problem.Count, event.Problem.MaxMS)
	case event.RuntimeCall != nil:
		caller := c.currentAttrOwner
		callee := c.resolveOwnerRef(dict, event.RuntimeCall.CalleeRef)
		key := c.contextKey("", "", "", "")
		context := c.flowContextFromKey(key)
		if !c.matchesFilters("", context, []string{caller, callee}, caller, callee) {
			return
		}
		c.markCohort()
		callKey := key + "\x00" + caller + "\x00" + callee
		stats := c.runtimeCallStats[callKey]
		if stats == nil {
			stats = &RuntimeCallStats{
				Screen: context.Screen,
				Flow:   context.Flow,
				Step:   context.Step,
				Caller: caller,
				Callee: callee,
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
	}
}

func (c *collector) isHeapDumpStall(eventTimeMS uint64, owner string) bool {
	if c.lastHeapDumpMS == 0 || eventTimeMS < c.lastHeapDumpMS || !isLikelySystemClass(owner) {
		return false
	}
	return eventTimeMS-c.lastHeapDumpMS <= heapDumpStallAttributionWindowMS
}

func (c *collector) markCohort() {
	c.cohortSamples[fmt.Sprintf(
		"app=%s build=%s sdk=%s device=%s process=%s network=%s root=%s",
		c.currentAppVersion,
		c.currentBuild,
		c.currentSDK,
		c.currentDevice,
		c.currentProcess,
		c.currentNetwork,
		rootCohortValue(c.currentRootKnown, c.currentRooted),
	)]++
}

func (c *collector) resolveOwnerRef(dict map[uint64]string, ref jhlog.SymbolRef) string {
	if !ref.Stable {
		return c.deobfuscate(ResolveOwnerAlias(c.ownerMap, jhlog.ResolveSymbol(dict, ref)))
	}
	if embedded := c.stableSymbols.embedded[ref.ID]; embedded != "" {
		return c.deobfuscate(embedded)
	}
	canonical := jhlog.ResolveSymbol(dict, ref)
	resolved := ResolveOwnerAlias(c.ownerMap, canonical)
	if resolved != canonical {
		c.stableSymbols.externalResolved = true
		return c.deobfuscate(resolved)
	}
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
			"log contains %d unresolved external stable symbol(s), first is %s; rerun with --external-symbols and --artifacts-dir <build/generated/jankhunter/variant>, or collect a new log with JankHunterSymbolMode.EMBEDDED",
			len(ids), ids[0],
		)
	}
	if c.stableSymbols.requireExplicit && c.stableSymbols.externalResolved && !c.stableSymbols.external {
		return fmt.Errorf("log uses external stable symbols; rerun with --external-symbols and the matching --artifacts-dir (or --owner-map)")
	}
	return nil
}

func (c *collector) deobfuscate(value string) string {
	if c.nameMap == nil {
		return value
	}
	return c.nameMap.Deobfuscate(value)
}

func (c *collector) flowKey(screenOverride, ownerOverride string) string {
	return c.contextKey(screenOverride, ownerOverride, "", "")
}

func (c *collector) contextKey(screenOverride, ownerOverride, flowOverride, stepOverride string) string {
	return strings.Join([]string{
		firstKnown(screenOverride, c.currentAttrScreen),
		firstKnown(flowOverride, c.currentAttrFlow),
		firstKnown(stepOverride, c.currentAttrStep),
		firstKnown(ownerOverride, c.currentAttrOwner),
	}, "\x00")
}

func (c *collector) flowContextFromKey(key string) FlowStats {
	parts := strings.Split(key, "\x00")
	for len(parts) < 4 {
		parts = append(parts, "unknown")
	}
	return FlowStats{
		Screen: attrValue(parts[0]),
		Flow:   attrValue(parts[1]),
		Step:   attrValue(parts[2]),
		Owner:  attrValue(parts[3]),
	}
}

func (c *collector) ensureFlow(key string) *FlowStats {
	stats := c.flowStats[key]
	if stats != nil {
		return stats
	}
	context := c.flowContextFromKey(key)
	stats = &FlowStats{
		Screen: context.Screen,
		Flow:   context.Flow,
		Step:   context.Step,
		Owner:  context.Owner,
	}
	c.flowStats[key] = stats
	return stats
}

func (c *collector) addProblemWindow(context FlowStats, kind string, windowMS, count, maxMS uint64) {
	key := c.contextKey(context.Screen, context.Owner, context.Flow, context.Step)
	problemKey := key + "\x00" + kind
	stats := c.problemStats[problemKey]
	if stats == nil {
		stats = &ProblemWindowStats{
			Screen: context.Screen,
			Flow:   context.Flow,
			Step:   context.Step,
			Owner:  context.Owner,
			Kind:   kind,
		}
		c.problemStats[problemKey] = stats
	}
	stats.Windows++
	stats.Count += count
	stats.TotalWindowMS += windowMS
	stats.MaxMS = maxUint64(stats.MaxMS, maxMS)
	flow := c.ensureFlow(key)
	flow.ProblemCount += count
	flow.ProblemMaxMS = maxUint64(flow.ProblemMaxMS, maxMS)
}

func (c *collector) sampleSet(target map[string]*uint64SampleSet, key string) *uint64SampleSet {
	set := target[key]
	if set == nil {
		set = &uint64SampleSet{}
		target[key] = set
	}
	return set
}

func (c *collector) routeBurst(route string) *routeBurstAccumulator {
	stats := c.routeBursts[route]
	if stats == nil {
		stats = &routeBurstAccumulator{}
		c.routeBursts[route] = stats
	}
	return stats
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
	context FlowStats,
	ageMs,
	count uint64,
	evidence jhlog.RetentionEvidence,
	runtimeSignal bool,
) {
	className = attrValue(className)
	holder = firstKnown(holder, context.Owner, className)
	key := strings.Join([]string{className, holder, context.Screen, context.Flow, context.Step}, "\x00")
	stats := c.memoryLeakStats[key]
	if stats == nil {
		stats = &memoryLeakStats{
			className: className,
			holder:    holder,
			screen:    context.Screen,
			flow:      context.Flow,
			step:      context.Step,
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
		if !c.matchesFilters("", FlowStats{}, []string{className}, holder) {
			continue
		}
		c.addMemoryLeakSuspect(
			className,
			holder,
			FlowStats{},
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
			"Несколько логов считаются независимыми прогонами: длительность в обзоре равна сумме длительностей логов, а math timeline накладывает события по относительному времени.",
		)
	}
	summary.TrafficRxMax = c.totalTrafficRxBytes
	summary.TrafficTxMax = c.totalTrafficTxBytes

	for route, set := range c.routeDurations {
		ttfbAvg := uint64(0)
		if c.routeTTFBCount[route] > 0 {
			ttfbAvg = c.routeTTFB[route] / c.routeTTFBCount[route]
		}
		burst := c.routeBursts[route]
		burstStatus := "exact_rolling_second"
		if burst != nil && burst.approximate {
			burstStatus = "bounded_approximation"
		}
		row := RouteStats{
			Route:               route,
			Count:               set.seen,
			Failures:            c.routeFailures[route],
			P50MS:               set.percentile(0.50),
			P95MS:               set.percentile(0.95),
			MaxMS:               set.max,
			AvgTTFBMS:           ttfbAvg,
			BytesRx:             c.routeRx[route],
			BytesTx:             c.routeTx[route],
			OwnerSample:         c.routeOwner[route],
			BurstEstimateStatus: burstStatus,
		}
		if burst != nil {
			row.PeakRequestsPerSecond = burst.peak
			row.PeakWindowStartMS = burst.peakWindowStartMS
		}
		summary.Routes = append(summary.Routes, row)
	}
	summary.HTTPP95MS = c.httpDurations.percentile(0.95)

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
	for key, stats := range c.flowStats {
		if durations := c.flowHTTPDurations[key]; durations != nil {
			stats.HTTPP95MS = durations.percentile(0.95)
		}
		if stats.UIFrames > 0 {
			stats.UIJankPct = float64(stats.UIJank) * 100 / float64(stats.UIFrames)
		}
		summary.Flows = append(summary.Flows, *stats)
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
	for _, stats := range c.ioStats {
		summary.IOOperations = append(summary.IOOperations, *stats)
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
	sortScreens(summary.Screens)
	sortOwners(summary.Owners)
	sortFlows(summary.Flows)
	sortLogSpam(summary.LogSpam)
	sortProblems(summary.ProblemWindows)
	sortRuntimeCalls(summary.RuntimeCalls)
	sortProcessExits(summary.ProcessExits)
	sortIOOperations(summary.IOOperations)
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
	// Every base aggregate has been copied into Summary. Drop the mutable collection maps before
	// materializing influence views and the code-problem registry so both representations do not
	// coexist at peak heap usage on large applications.
	c.releaseAggregationState()
	summary.Influence = BuildInfluence(summary, c.classGraph)
	summary.CodeProblems = BuildCodeProblemRegistry(summary)
	summary.AnalysisInputs = c.analysisInputCompleteness(summary)
	problemReport, problemErr := BuildProblemReport(summary)
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
	c.ownerMap = nil
	c.nameMap = nil
	c.routeDurations = nil
	c.routeFailures = nil
	c.routeRx = nil
	c.routeTx = nil
	c.routeTTFB = nil
	c.routeTTFBCount = nil
	c.routeOwner = nil
	c.routeBursts = nil
	c.screenStats = nil
	c.processExitStats = nil
	c.ioStats = nil
	c.ownerStats = nil
	c.flowStats = nil
	c.flowHTTPDurations = nil
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
	symbolMode := "embedded"
	if c.stableSymbols.externalResolved {
		symbolMode = "external"
	}
	missing := make([]string, 0, 4)
	if !runtimeEvidence {
		missing = append(missing, "runtime events")
	}
	if !classGraph {
		missing = append(missing, "class-graph.jsonl")
	}
	if !diagnostics {
		missing = append(missing, "instrumentation-diagnostics.jsonl")
	}
	artifactIdentityVerified := len(c.artifactNamespace) == ownerMapNamespaceBytes
	if (classGraph || diagnostics) && !artifactIdentityVerified {
		missing = append(missing, "matching artifact symbolNamespace")
	}
	complete := len(missing) == 0
	status := "complete"
	explanation := "runtime evidence, статический class graph и ASM diagnostics подключены"
	if !complete {
		status = "partial"
		explanation = "часть аналитических входов отсутствует; соответствующие выводы и companion reports ограничены"
		if runtimeEvidence && !classGraph && !diagnostics {
			status = "runtime_only"
			explanation = "доступны runtime evidence, но статический граф, hot paths, cycles и ASM diagnostics неполны"
		}
	}
	return AnalysisInputCompleteness{
		Status:                     status,
		Complete:                   complete,
		RuntimeEvidence:            runtimeEvidence,
		SymbolsResolved:            len(c.stableSymbols.unresolved) == 0,
		SymbolMode:                 symbolMode,
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
	if len(namespace) != ownerMapNamespaceBytes || !bytes.Equal(namespace, header.SymbolNamespace) {
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
					addReason("low", "process roster не доказан: процессы принадлежат разным run cohort")
				} else if !quality.ProcessRosterComplete {
					addNotice(fmt.Sprintf(
						"наблюдается %d процессов из %d объявленных в configured scope; отсутствующие процессы могли не запускаться либо их сегменты не были переданы, поэтому это неопределённость охвата, а не доказанная потеря",
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
	quality.TrustScorePercent, quality.TrustComponents = collectionTrustScore(quality)
	quality.TrustScoreModel = collectionTrustScoreModel
	quality.TrustLevel, quality.TrustLevelExplanation = describeCollectionTrust(
		quality.TrustScorePercent,
		quality.TrustComponents,
	)
	c.summary.CollectionQuality = quality
	for _, reason := range quality.Reasons {
		c.summary.Warnings = append(c.summary.Warnings, "Качество сбора: "+reason+".")
	}
}

func collectionTrustScore(quality CollectionQuality) (float64, []CollectionTrustComponent) {
	components := make([]CollectionTrustComponent, 0, 4)
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
		components = append(components, CollectionTrustComponent{
			ID:              id,
			Label:           label,
			Weight:          weight,
			Excluded:        excluded,
			CoveragePercent: roundTrustValue(coverage * 100),
			EarnedPoints:    roundTrustValue(earned),
			MissingPoints:   roundTrustValue(missing),
			Explanation:     explanation,
		})
	}

	transportCoverage := 1.0
	transportExplanation := fmt.Sprintf(
		"EXACT admission; записано %d событий, известных потерь нет",
		quality.WrittenEvents,
	)
	if !quality.ExactAdmission {
		transportCoverage = 0
		transportExplanation = "BEST_EFFORT admission не доказывает отсутствие событий, потерянных до регистрации"
	} else if quality.KnownLostEvents > 0 {
		transportTotal := saturatingUint64Sum(quality.WrittenEvents, quality.KnownLostEvents)
		if transportTotal == 0 {
			transportCoverage = 0
		} else {
			transportCoverage = float64(quality.WrittenEvents) / float64(transportTotal)
		}
		transportExplanation = fmt.Sprintf(
			"записано %d из как минимум %d событий; известно потеряно %d",
			quality.WrittenEvents,
			transportTotal,
			quality.KnownLostEvents,
		)
	}
	appendComponent("transport", "Доставка событий", 40, transportCoverage, false, transportExplanation)

	runtimeCoverage := quality.RuntimeGraphCompletenessRatio
	runtimeDenominator := saturatingUint64Sum(
		quality.RuntimeGraphInputEvents,
		quality.RuntimeGraphStackMismatches,
	)
	runtimeExplanation := fmt.Sprintf(
		"декодировано %d из %d входных runtime-вызовов",
		quality.DecodedRuntimeGraphCalls,
		runtimeDenominator,
	)
	if !quality.RuntimeGraphEnabled {
		runtimeCoverage = 1
		runtimeExplanation = "Runtime-граф отключён конфигурацией и исключён из расчёта индекса"
	} else if runtimeDenominator == 0 {
		runtimeCoverage = 1
		runtimeExplanation = "runtime-граф включён; входных вызовов и признаков их потери не зарегистрировано"
	} else {
		runtimeCoverage = float64(quality.DecodedRuntimeGraphCalls) / float64(runtimeDenominator)
		if quality.RuntimeGraphStackMismatches > 0 {
			runtimeExplanation = fmt.Sprintf(
				"декодировано %d из как минимум %d runtime-вызовов; stack mismatch=%d",
				quality.DecodedRuntimeGraphCalls,
				runtimeDenominator,
				quality.RuntimeGraphStackMismatches,
			)
		}
	}
	appendComponent(
		"runtime_graph",
		"Runtime-граф",
		20,
		runtimeCoverage,
		!quality.RuntimeGraphEnabled,
		runtimeExplanation,
	)

	processCoverage := 0.0
	processExplanation := fmt.Sprintf(
		"наблюдается %d из %d потенциальных процессов configured scope; manifest не доказывает запуск отсутствующих процессов",
		quality.ObservedProcessCount,
		quality.ExpectedProcessCount,
	)
	if quality.ProcessRosterComplete {
		processCoverage = 1
		processExplanation = fmt.Sprintf(
			"подтверждены все %d процессов configured scope %s",
			quality.ExpectedProcessCount,
			quality.ProcessScope,
		)
	} else if quality.ProcessRosterDeclarationComplete && quality.RunCohortConsistent &&
		quality.ProcessScopeConsistent && quality.ExpectedProcessCount > 0 &&
		quality.ObservedProcessCount < quality.ExpectedProcessCount {
		processCoverage = float64(quality.ObservedProcessCount) / float64(quality.ExpectedProcessCount)
	} else if !quality.ProcessRosterDeclarationComplete {
		processExplanation = "Android manifest не позволил доказать полный список процессов configured scope"
	} else if !quality.RunCohortConsistent {
		processExplanation = "входные сегменты относятся к разным run cohort"
	} else if !quality.ProcessScopeConsistent {
		processExplanation = "process scope или allowlist не согласованы между сегментами"
	} else {
		processExplanation = "количество или fingerprint процессов не совпадает с объявленным roster"
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
		integrityIssues = append(integrityIssues, "монотонность quality")
	}
	if quality.DamagedSegments > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf("повреждённые/аварийные сегменты=%d", quality.DamagedSegments))
	}
	if quality.SegmentsWithoutQuality > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf("сегменты без quality=%d", quality.SegmentsWithoutQuality))
	}
	if quality.CriticalRuntimeHookFailures > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf(
			"hook failures с влиянием на evidence=%d (%s)",
			quality.CriticalRuntimeHookFailures,
			runtimeHookFailureReasonSummary(quality.RuntimeHookFailureDetails, true),
		))
	}
	if quality.DictionaryOverflow > 0 || quality.DictionaryTruncated > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf(
			"dictionary overflow/truncated=%d/%d",
			quality.DictionaryOverflow,
			quality.DictionaryTruncated,
		))
	}
	if quality.ControlFailures > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf("control failures=%d", quality.ControlFailures))
	}
	if quality.OtherEvidenceLoss > 0 {
		integrityIssues = append(integrityIssues, fmt.Sprintf("потери прочих runtime evidence=%d", quality.OtherEvidenceLoss))
	}
	integrityCoverage := 1.0
	integrityExplanation := "digest chain, quality progression, schema и счётчики согласованы"
	if len(integrityIssues) > 0 {
		integrityCoverage = 0
		integrityExplanation = "не доказаны: " + strings.Join(integrityIssues, "; ")
	}
	appendComponent("integrity", "Целостность доказательств", 20, integrityCoverage, false, integrityExplanation)
	if activeWeight == 0 {
		return 100, components
	}
	return roundTrustValue(earnedWeight * 100 / activeWeight), components
}

func describeCollectionTrust(score float64, components []CollectionTrustComponent) (string, string) {
	level, explanation := collectionTrustTier(score)
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
		explanation += " Отключены конфигурацией и не входят в denominator: " + strings.Join(excluded, ", ") + "."
	}
	return level, explanation
}

func collectionTrustTier(score float64) (string, string) {
	switch {
	case score >= 95:
		return "excellent", "Максимальное доверие: индекс 95–100%; все активные источники evidence практически полностью подтверждены."
	case score >= 85:
		return "high", "Высокое доверие: индекс 85–94,99%; основные evidence подтверждены, оставшиеся ограничения явно перечислены."
	case score >= 65:
		return "sufficient", "Достаточное доверие: индекс 65–84,99%; выводы применимы с учётом перечисленных ограничений."
	case score >= 40:
		return "limited", "Ограниченное доверие: индекс 40–64,99%; существенная часть активного evidence не подтверждена."
	default:
		return "low", "Низкое доверие: индекс ниже 40%; отчёт нельзя использовать для уверенных выводов без повторного сбора."
	}
}

func roundTrustValue(value float64) float64 {
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
		{jhlog.QualityRuntimeHookContextFailure, "context", "evidence_loss", "контекст screen, owner или flow мог быть неполным"},
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
		warnings = append(warnings, "Качество сбора: большинство проблемных окон не имеют понятного owner; добавьте ownerHint/withOwner или проверьте owner-map.")
	}
	if len(summary.Flows) > 0 && unknownFlowContextRate(summary.Flows) >= 0.8 {
		warnings = append(warnings, "Качество сбора: большинство сценариев не имеют screen/flow/step/owner; проверьте автотрекинг Activity, @JankHunterTrace/withFlow/withOwner и ASM owner-map.")
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

func unknownFlowContextRate(flows []FlowStats) float64 {
	var total uint64
	var unknown uint64
	for _, flow := range flows {
		count := uint64(flow.HTTPCount) + uint64(flow.StallCount) + flow.LogSpam + flow.ProblemCount + uint64(flow.UIWindows)
		if count == 0 {
			count = 1
		}
		total += count
		if datavalue.IsUnknown(flow.Screen) &&
			datavalue.IsUnknown(flow.Flow) &&
			datavalue.IsUnknown(flow.Step) &&
			datavalue.IsUnknown(flow.Owner) {
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
		return operations[i].Owner < operations[j].Owner
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

func sortFlows(flows []FlowStats) {
	sort.Slice(flows, func(i, j int) bool {
		left := flowSeverityScore(flows[i])
		right := flowSeverityScore(flows[j])
		if left == right {
			return flows[i].Flow < flows[j].Flow
		}
		return left > right
	})
}

func flowSeverityScore(flow FlowStats) uint64 {
	return flow.ProblemCount*10_000 +
		uint64(flow.StallCount)*5_000 +
		flow.UIJank*100 +
		flow.LogSpam*10 +
		uint64(flow.HTTPFailed)*500 +
		flow.HTTPP95MS +
		flow.ProblemMaxMS
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
