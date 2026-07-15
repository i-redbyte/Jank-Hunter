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

const maxAggregateSamplesPerSignal = 20_000

const (
	legacyHTTPSlowThresholdMS = 1_000
	legacyUIP95ThresholdMS    = 32
	canonicalLogSpamWindowMS  = 5_000
	canonicalLogSpamCount     = 50
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
		collector.addStreamResult(result)
		collector.summary.Warnings = append(collector.summary.Warnings, result.Warnings...)
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
	if ownerMap, ok, err := loadOwnerMapObject(path, data); ok || err != nil {
		return ownerMap, err
	}
	return loadOwnerMapJSONL(path, data)
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

type ownerMapRecord struct {
	Format          int               `json:"format"`
	Kind            string            `json:"kind"`
	SymbolNamespace string            `json:"symbolNamespace"`
	Owners          map[string]string `json:"owners"`
	Entries         []ownerMapEntry   `json:"entries"`
	ID              string            `json:"id"`
	Owner           string            `json:"owner"`
	Name            string            `json:"name"`
	Value           string            `json:"value"`
}

type ownerMapEntry struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func loadOwnerMapObject(path string, data []byte) (*OwnerMap, bool, error) {
	var raw ownerMapRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, nil
	}
	if err := validateOwnerMapFormat(path, raw.Format); err != nil {
		return nil, true, err
	}
	out := &OwnerMap{Entries: map[string]string{}}
	if err := addOwnerMapMetadata(out, raw); err != nil {
		return nil, true, fmt.Errorf("%s: parse owner map: %w", path, err)
	}
	if err := addOwnerMapRecord(out.Entries, raw); err != nil {
		return nil, true, fmt.Errorf("%s: parse owner map: %w", path, err)
	}
	if err := validateLoadedOwnerMap(out); err != nil {
		return nil, true, fmt.Errorf("%s: parse owner map: %w", path, err)
	}
	return out, true, nil
}

func loadOwnerMapJSONL(path string, data []byte) (*OwnerMap, error) {
	out := &OwnerMap{Entries: map[string]string{}}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw ownerMapRecord
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := validateOwnerMapFormat(path, raw.Format); err != nil {
			return nil, fmt.Errorf("parse owner map line %d: %w", lineNumber, err)
		}
		if err := addOwnerMapMetadata(out, raw); err != nil {
			return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
		}
		if err := addOwnerMapRecord(out.Entries, raw); err != nil {
			return nil, fmt.Errorf("%s: parse owner map line %d: %w", path, lineNumber, err)
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

func addOwnerMapMetadata(out *OwnerMap, raw ownerMapRecord) error {
	if raw.Kind != "metadata" {
		if raw.SymbolNamespace != "" {
			return fmt.Errorf("symbolNamespace is only allowed on the metadata record")
		}
		return nil
	}
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

func addOwnerMapRecord(out map[string]string, raw ownerMapRecord) error {
	for id, owner := range raw.Owners {
		if err := addOwnerMapEntry(out, ownerMapEntry{ID: id, Owner: owner}); err != nil {
			return err
		}
	}
	for _, entry := range raw.Entries {
		if err := addOwnerMapEntry(out, entry); err != nil {
			return err
		}
	}
	if raw.ID != "" || raw.Kind == "entry" {
		if err := addOwnerMapEntry(out, ownerMapEntry{
			ID:    raw.ID,
			Owner: raw.Owner,
			Name:  raw.Name,
			Value: raw.Value,
		}); err != nil {
			return err
		}
	}
	return nil
}

func addOwnerMapEntry(out map[string]string, entry ownerMapEntry) error {
	if !isCanonicalStableOwnerID(entry.ID) {
		return fmt.Errorf("owner map id %q is not canonical; expected stable:0x followed by 16 lowercase hexadecimal digits", entry.ID)
	}
	name := strings.TrimSpace(firstNonEmpty(entry.Owner, entry.Name, entry.Value))
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
	logCanonical        map[string]struct{}
	logLegacy           map[string]*ProblemWindowStats
	totalTrafficRxBytes uint64
	totalTrafficTxBytes uint64
	dictionaryOverflow  int
	qualitySnapshots    map[string]segmentQualityState
	streamResults       []jhlog.StreamResult
	chainIssues         []string

	httpDurations  uint64SampleSet
	routeDurations map[string]*uint64SampleSet
	routeFailures  map[string]int
	routeRx        map[string]uint64
	routeTx        map[string]uint64
	routeTTFB      map[string]uint64
	routeTTFBCount map[string]uint64
	routeOwner     map[string]string

	screenStats        map[string]*ScreenStats
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
		routeDurations:     map[string]*uint64SampleSet{},
		routeFailures:      map[string]int{},
		routeRx:            map[string]uint64{},
		routeTx:            map[string]uint64{},
		routeTTFB:          map[string]uint64{},
		routeTTFBCount:     map[string]uint64{},
		routeOwner:         map[string]string{},
		screenStats:        map[string]*ScreenStats{},
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
		logCanonical:       map[string]struct{}{},
		logLegacy:          map[string]*ProblemWindowStats{},
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
	clear(c.logCanonical)
	clear(c.logLegacy)
}

func (c *collector) finishLog() {
	c.mergeLegacyProblems()
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
		Source:            result.Source,
		Version:           result.Version,
		Status:            string(result.Status),
		Sealed:            result.Sealed,
		TailBytes:         result.TailBytes,
		TotalRecords:      result.TotalRecords,
		DataRecords:       result.DataRecords,
		DictionaryRecords: result.DictionaryRecords,
		ControlRecords:    result.ControlRecords,
		RunID:             fmt.Sprintf("%x", result.Header.RunID[:]),
		ProcessInstanceID: fmt.Sprintf("%x", result.Header.ProcessInstanceID[:]),
		SessionID:         fmt.Sprintf("%x", result.Header.SessionID[:]),
		SegmentIndex:      result.Header.SegmentIndex,
		ProcessName:       result.Header.ProcessName,
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
		if result.Version != jhlog.FormatVersion {
			issues = append(issues, fmt.Sprintf("лог %s не имеет проверяемой v9 identity/segment chain", result.Source))
			continue
		}
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
	case jhlog.SegmentEndNormal, jhlog.SegmentEndShutdown:
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
	values       []uint64
	seen         int
	max          uint64
	approximated bool
}

func (s *uint64SampleSet) add(value uint64) {
	s.seen++
	if value > s.max {
		s.max = value
	}
	if len(s.values) < maxAggregateSamplesPerSignal {
		s.values = append(s.values, value)
		return
	}
	s.approximated = true
	index := deterministicAggregateReservoirIndex(s.seen)
	if index < maxAggregateSamplesPerSignal {
		s.values[index] = value
	}
}

func (s *uint64SampleSet) sortedValues() []uint64 {
	if len(s.values) == 0 {
		return nil
	}
	values := append([]uint64(nil), s.values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

func (s *uint64SampleSet) sampled() int {
	return len(s.values)
}

func (s *uint64SampleSet) isApproximated() bool {
	return s.approximated || s.seen > len(s.values)
}

type gaugeStats struct {
	count uint64
	total uint64
	max   uint64
	last  uint64
	mode  jhlog.MetricMode
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
		"process.exit.last.reason",
		"process.exit.last.importance",
		"memory.trim.last_level":
		return jhlog.MetricModeState
	case "battery.charging",
		"device.power_save_mode",
		"device.interactive",
		"device.idle_mode",
		"network.request.connection_released":
		return jhlog.MetricModeBooleanRate
	}
	if strings.HasPrefix(metric, "process.exit.last.reason_") && strings.HasSuffix(metric, ".count") {
		return jhlog.MetricModeLast
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
	heapOnlyCount        uint64
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

func deterministicAggregateReservoirIndex(seen int) int {
	x := uint64(seen)*2862933555777941757 + 3037000493
	return int(x % uint64(seen))
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
		c.currentAppVersion = resolveEventSymbol(dict, event.Session.AppVersionRef, event.Session.AppVersionID)
		c.currentBuild = resolveEventSymbol(dict, event.Session.BuildRef, event.Session.BuildID)
		c.currentDevice = resolveEventSymbol(dict, event.Session.DeviceRef, event.Session.DeviceID)
		c.currentSDK = fmt.Sprintf("api-%d", event.Session.SDKInt)
		c.currentProcess = firstNonEmpty(event.Session.ProcessName, jhlog.Resolve(dict, event.Session.ProcessID))
		c.currentAndroid = resolveEventSymbol(dict, event.Session.AndroidReleaseRef, event.Session.AndroidReleaseID)
		c.currentPatch = resolveEventSymbol(dict, event.Session.SecurityPatchRef, event.Session.SecurityPatchID)
		c.currentPrimaryABI = resolveEventSymbol(dict, event.Session.PrimaryABIRef, event.Session.PrimaryABIID)
		c.currentABIs = resolveEventSymbol(dict, event.Session.SupportedABIsRef, event.Session.SupportedABIsID)
		c.currentMaker = resolveEventSymbol(dict, event.Session.ManufacturerRef, event.Session.ManufacturerID)
		c.currentBrand = resolveEventSymbol(dict, event.Session.BrandRef, event.Session.BrandID)
		c.currentHardware = resolveEventSymbol(dict, event.Session.HardwareRef, event.Session.HardwareID)
		c.currentBoard = resolveEventSymbol(dict, event.Session.BoardRef, event.Session.BoardID)
		c.currentProduct = resolveEventSymbol(dict, event.Session.ProductRef, event.Session.ProductID)
		c.currentRootKnown = true
		c.currentRooted = event.Session.DeviceRooted
		c.summary.DeviceRootKnown = true
		c.summary.DeviceRooted = event.Session.DeviceRooted
		c.appVersions[c.currentAppVersion]++
		c.builds[c.currentBuild]++
		c.devices[c.currentDevice]++
		c.sdks[c.currentSDK]++
		c.processSamples[c.currentProcess]++
	case event.Flow != nil:
		// A flow transition describes only itself. Atomic envelope attribution was
		// applied above and is never carried into the next event.
		context := c.eventContext("", "", "", "")
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		c.ensureFlow(c.flowKey("", ""))
	case event.HTTP != nil:
		route := resolveEventSymbol(dict, event.HTTP.RouteRef, event.HTTP.RouteID)
		owner := c.resolveOwnerRef(dict, firstEventSymbol(event.HTTP.OwnerRef, event.HTTP.OwnerID))
		context := c.eventContext("", owner, "", "")
		if !c.matchesFilters(route, context, nil, owner) {
			return
		}
		c.markCohort()
		c.summary.HTTPCount++
		c.httpDurations.add(event.HTTP.DurationMS)
		c.sampleSet(c.routeDurations, route).add(event.HTTP.DurationMS)
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
		classified := event.Flags&uint64(jhlog.FlagHTTPClassified) != 0
		slow := event.Flags&uint64(jhlog.FlagHTTPSlow) != 0
		if failed || slow || (!classified && event.HTTP.DurationMS >= legacyHTTPSlowThresholdMS) {
			c.addProblemWindow(context, "http_slow_or_failed", event.HTTP.DurationMS, 1, event.HTTP.DurationMS)
		}
	case event.UIWindow != nil:
		screen := resolveEventSymbol(dict, event.UIWindow.ScreenRef, event.UIWindow.ScreenID)
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
		windowFPS := fps(event.UIWindow.FrameCount, event.UIWindow.WindowMS)
		if stats.MinFPS == 0 || windowFPS < stats.MinFPS {
			stats.MinFPS = windowFPS
		}
		if event.UIWindow.P95MS > stats.P95MS {
			stats.P95MS = event.UIWindow.P95MS
		}
		if event.UIWindow.P99MS > stats.MaxP99MS {
			stats.MaxP99MS = event.UIWindow.P99MS
		}
		c.summary.UIFrames += event.UIWindow.FrameCount
		c.summary.UIJank += event.UIWindow.JankCount
		c.summary.UIWindowMS += event.UIWindow.WindowMS
		if c.summary.UIMinFPS == 0 || windowFPS < c.summary.UIMinFPS {
			c.summary.UIMinFPS = windowFPS
		}
		flowKey := c.flowKey(screen, "")
		flow := c.ensureFlow(flowKey)
		flow.UIWindows++
		flow.UIFrames += event.UIWindow.FrameCount
		flow.UIJank += event.UIWindow.JankCount
		classified := event.Flags&uint64(jhlog.FlagUIClassified) != 0
		problem := event.Flags&uint64(jhlog.FlagUIProblem) != 0
		if problem || (!classified && (event.UIWindow.JankCount > 0 || event.UIWindow.P95MS >= legacyUIP95ThresholdMS)) {
			c.addProblemWindow(
				context,
				"ui_jank",
				event.UIWindow.WindowMS,
				maxUint64(event.UIWindow.JankCount, 1),
				event.UIWindow.P95MS,
			)
		}
	case event.Stall != nil:
		owner := c.resolveOwnerRef(dict, firstEventSymbol(event.Stall.OwnerRef, event.Stall.OwnerID))
		stack := resolveEventSymbol(dict, event.Stall.StackRef, event.Stall.StackID)
		context := c.eventContext("", owner, "", "")
		if !c.matchesFilters("", context, nil, owner) {
			return
		}
		c.markCohort()
		c.summary.StallCount++
		if event.Stall.DurationMS > c.summary.StallMaxMS {
			c.summary.StallMaxMS = event.Stall.DurationMS
		}
		addOwner(c.ownerStats, owner, "main_thread_stall", event.Stall.DurationMS, stack)
		flowKey := c.flowKey("", owner)
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
	case event.Retained != nil:
		className := c.deobfuscate(resolveEventSymbol(dict, event.Retained.ClassRef, event.Retained.ClassID))
		holder := c.resolveOwnerRef(dict, firstEventSymbol(event.Retained.HolderRef, event.Retained.HolderID))
		owner := c.resolveOwner(dict, event.Retained.OwnerID)
		context := c.eventContext(
			jhlog.Resolve(dict, event.Retained.ScreenID),
			owner,
			jhlog.Resolve(dict, event.Retained.FlowID),
			jhlog.Resolve(dict, event.Retained.StepID),
		)
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
		key := c.contextKey(
			jhlog.Resolve(dict, event.LogSpam.ScreenID),
			c.resolveOwner(dict, event.LogSpam.OwnerID),
			jhlog.Resolve(dict, event.LogSpam.FlowID),
			jhlog.Resolve(dict, event.LogSpam.StepID),
		)
		context := c.flowContextFromKey(key)
		source := resolveEventSymbol(dict, event.LogSpam.SourceRef, event.LogSpam.SourceID)
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
		key := c.contextKey(
			jhlog.Resolve(dict, event.Problem.ScreenID),
			c.resolveOwner(dict, event.Problem.OwnerID),
			jhlog.Resolve(dict, event.Problem.FlowID),
			jhlog.Resolve(dict, event.Problem.StepID),
		)
		context := c.flowContextFromKey(key)
		if !c.matchesFilters("", context, nil, context.Owner) {
			return
		}
		c.markCohort()
		kind := resolveEventSymbol(dict, event.Problem.KindRef, event.Problem.KindID)
		if isLegacyDerivedProblemKind(kind) {
			c.addLegacyProblem(context, kind, event.Problem.WindowMS, event.Problem.Count, event.Problem.MaxMS)
		} else {
			c.addProblemWindow(context, kind, event.Problem.WindowMS, event.Problem.Count, event.Problem.MaxMS)
		}
	case event.RuntimeCall != nil:
		caller := c.resolveOwnerRef(dict, firstEventSymbol(event.RuntimeCall.CallerRef, event.RuntimeCall.CallerID))
		callee := c.resolveOwnerRef(dict, firstEventSymbol(event.RuntimeCall.CalleeRef, event.RuntimeCall.CalleeID))
		key := c.contextKey(
			jhlog.Resolve(dict, event.RuntimeCall.ScreenID),
			caller,
			jhlog.Resolve(dict, event.RuntimeCall.FlowID),
			jhlog.Resolve(dict, event.RuntimeCall.StepID),
		)
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
		name := resolveEventSymbol(dict, event.Metric.MetricRef, event.Metric.MetricID)
		if event.Type == jhlog.EventCounter && event.Metric.MetricRef.Stable {
			name = c.resolveOwnerRef(dict, event.Metric.MetricRef)
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

func (c *collector) resolveOwner(dict map[uint64]string, id uint64) string {
	return c.deobfuscate(ResolveOwnerAlias(c.ownerMap, jhlog.Resolve(dict, id)))
}

func (c *collector) resolveOwnerRef(dict map[uint64]string, ref jhlog.SymbolRef) string {
	if !ref.Stable {
		return c.deobfuscate(jhlog.ResolveSymbol(dict, ref))
	}
	if embedded := c.stableSymbols.embedded[ref.StableID]; embedded != "" {
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

func firstEventSymbol(ref jhlog.SymbolRef, legacyID uint64) jhlog.SymbolRef {
	if !ref.IsUnknown() {
		return ref
	}
	return jhlog.LocalSymbol(legacyID)
}

func resolveEventSymbol(dict map[uint64]string, ref jhlog.SymbolRef, legacyID uint64) string {
	return jhlog.ResolveSymbol(dict, firstEventSymbol(ref, legacyID))
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
	problemKey := c.accumulateProblem(c.problemStats, context, kind, windowMS, count, maxMS)
	c.logCanonical[problemKey] = struct{}{}
	flow := c.ensureFlow(c.contextKey(context.Screen, context.Owner, context.Flow, context.Step))
	flow.ProblemCount += count
	flow.ProblemMaxMS = maxUint64(flow.ProblemMaxMS, maxMS)
}

func (c *collector) addLegacyProblem(context FlowStats, kind string, windowMS, count, maxMS uint64) {
	c.accumulateProblem(c.logLegacy, context, kind, windowMS, count, maxMS)
}

func (c *collector) accumulateProblem(
	target map[string]*ProblemWindowStats,
	context FlowStats,
	kind string,
	windowMS,
	count,
	maxMS uint64,
) string {
	key := c.contextKey(context.Screen, context.Owner, context.Flow, context.Step)
	problemKey := key + "\x00" + kind
	stats := target[problemKey]
	if stats == nil {
		stats = &ProblemWindowStats{
			Screen: context.Screen,
			Flow:   context.Flow,
			Step:   context.Step,
			Owner:  context.Owner,
			Kind:   kind,
		}
		target[problemKey] = stats
	}
	stats.Windows++
	stats.Count += count
	stats.TotalWindowMS += windowMS
	stats.MaxMS = maxUint64(stats.MaxMS, maxMS)
	return problemKey
}

func (c *collector) mergeLegacyProblems() {
	for problemKey, legacy := range c.logLegacy {
		if _, canonical := c.logCanonical[problemKey]; canonical {
			continue
		}
		stats := c.problemStats[problemKey]
		if stats == nil {
			c.problemStats[problemKey] = legacy
		} else {
			stats.Windows += legacy.Windows
			stats.Count += legacy.Count
			stats.TotalWindowMS += legacy.TotalWindowMS
			stats.MaxMS = maxUint64(stats.MaxMS, legacy.MaxMS)
		}
		flow := c.ensureFlow(c.contextKey(legacy.Screen, legacy.Owner, legacy.Flow, legacy.Step))
		flow.ProblemCount += legacy.Count
		flow.ProblemMaxMS = maxUint64(flow.ProblemMaxMS, legacy.MaxMS)
	}
}

func isLegacyDerivedProblemKind(kind string) bool {
	switch kind {
	case "http_slow_or_failed", "main_thread_stall", "ui_jank", "retained_object", "log_spam":
		return true
	default:
		return false
	}
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
	} else {
		stats.heapOnlyCount += count
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
		durations := set.sortedValues()
		ttfbAvg := uint64(0)
		if c.routeTTFBCount[route] > 0 {
			ttfbAvg = c.routeTTFB[route] / c.routeTTFBCount[route]
		}
		summary.Routes = append(summary.Routes, RouteStats{
			Route:          route,
			Count:          set.seen,
			Sampled:        set.sampled(),
			Failures:       c.routeFailures[route],
			P50MS:          percentileSorted(durations, 0.50),
			P95MS:          percentileSorted(durations, 0.95),
			P95Approximate: set.isApproximated(),
			MaxMS:          set.max,
			AvgTTFBMS:      ttfbAvg,
			BytesRx:        c.routeRx[route],
			BytesTx:        c.routeTx[route],
			OwnerSample:    c.routeOwner[route],
		})
	}
	summary.HTTPP95MS = percentileSorted(c.httpDurations.sortedValues(), 0.95)
	summary.HTTPP95Approximate = c.httpDurations.isApproximated()

	for _, stats := range c.screenStats {
		if stats.Frames > 0 {
			stats.JankRatePct = float64(stats.JankyFrames) * 100 / float64(stats.Frames)
		}
		stats.AvgFPS = fps(stats.Frames, stats.WindowMS)
		summary.Screens = append(summary.Screens, *stats)
	}
	if summary.UIFrames > 0 {
		summary.UIJankPct = float64(summary.UIJank) * 100 / float64(summary.UIFrames)
	}
	summary.UIAvgFPS = fps(summary.UIFrames, summary.UIWindowMS)

	for _, stats := range c.ownerStats {
		summary.Owners = append(summary.Owners, *stats)
	}
	for key, stats := range c.flowStats {
		if durations := c.flowHTTPDurations[key]; durations != nil {
			stats.HTTPP95MS = percentileSorted(durations.sortedValues(), 0.95)
			stats.HTTPP95Approximate = durations.isApproximated()
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
	for name, value := range c.counterValues {
		summary.Counters = append(summary.Counters, NamedValue{Name: name, Value: value})
		if strings.HasPrefix(name, "jankstats.") {
			summary.JankStats = append(summary.JankStats, NamedValue{Name: name, Value: value})
		}
	}
	for name, values := range c.gaugeValues {
		value := values.value()
		extra := values.extra()
		summary.Gauges = append(summary.Gauges, NamedValue{Name: name, Value: value, Extra: extra})
		if strings.HasPrefix(name, "jankstats.") {
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
	summary.Warnings = append(summary.Warnings, c.sampleWarnings(summary)...)
	summary.Warnings = append(summary.Warnings, c.filterWarnings(summary)...)

	sortRoutes(summary.Routes)
	sortScreens(summary.Screens)
	sortOwners(summary.Owners)
	sortFlows(summary.Flows)
	sortLogSpam(summary.LogSpam)
	sortProblems(summary.ProblemWindows)
	sortRuntimeCalls(summary.RuntimeCalls)
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
	summary.Influence = BuildInfluence(summary, c.classGraph)
	summary.CodeProblems = BuildCodeProblemRegistry(summary)
	return summary
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
	warnings = append(warnings, qualityCounterWarnings(quality)...)
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
		Level:       "high",
		Complete:    true,
		ChainValid:  true,
		ChainIssues: append([]string(nil), c.chainIssues...),
	}
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
		if result.Version != jhlog.FormatVersion {
			quality.ChainValid = false
			addReason("low", fmt.Sprintf("лог %s использует legacy-формат без v9 seal и identity", result.Source))
			continue
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
				addReason("low", fmt.Sprintf("сегмент %s не запечатан и имеет статус %s (хвост %d байт)", result.Source, result.Status, result.TailBytes))
			case jhlog.SegmentStatusOpenClean:
				addNotice(fmt.Sprintf(
					"снимок активной сессии %s корректно прочитан до последнего зафиксированного чанка; FINAL seal появится после завершения runtime",
					result.Source,
				))
			default:
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
				addReason("low", fmt.Sprintf("сегмент %s завершен после ошибки ввода-вывода", result.Source))
			case jhlog.SegmentEndSizeLimit:
				addReason("medium", sizeLimitCollectionReason(result.Source))
			case jhlog.SegmentEndNormal, jhlog.SegmentEndShutdown:
			default:
				addReason("low", fmt.Sprintf("сегмент %s завершен с неизвестной причиной %d", result.Source, uint64(result.SegmentEnd.Reason)))
			}
		}
	}

	if len(c.chainIssues) > 0 {
		quality.ChainValid = false
		for _, issue := range c.chainIssues {
			addReason("low", issue)
		}
	}

	counters := c.latestQualityTotals()
	quality.AcceptedEvents = counters[jhlog.QualityAcceptedEventTotal]
	quality.WrittenEvents = counters[jhlog.QualityWrittenEventTotal]
	preAdmissionLoss := saturatingUint64Sum(
		counters[jhlog.QualityQueueFullTotal],
		counters[jhlog.QualityNotAcceptingTotal],
	)
	postAdmissionCounters := saturatingUint64Sum(
		counters[jhlog.QualityEventLostAfterIOTotal],
		counters[jhlog.QualityEventLostAfterSizeLimitTotal],
		counters[jhlog.QualityOversizedRecordTotal],
	)
	acceptedGap := uint64(0)
	if quality.AcceptedEvents > quality.WrittenEvents {
		acceptedGap = quality.AcceptedEvents - quality.WrittenEvents
	}
	if acceptedGap > postAdmissionCounters {
		postAdmissionCounters = acceptedGap
	}
	quality.KnownLostEvents = saturatingUint64Sum(preAdmissionLoss, postAdmissionCounters)
	if quality.KnownLostEvents > 0 {
		level := "medium"
		denominator := saturatingUint64Sum(quality.WrittenEvents, quality.KnownLostEvents)
		if quality.KnownLostEvents >= 100 || (denominator > 0 && float64(quality.KnownLostEvents)/float64(denominator) >= 0.01) {
			level = "low"
		}
		addReason(level, fmt.Sprintf("quality snapshots фиксируют потерю как минимум %d событий", quality.KnownLostEvents))
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
		counters[jhlog.QualityRuntimeGraphCapacityLoss],
		counters[jhlog.QualityRuntimeStackMismatch],
		counters[jhlog.QualityLogSpamCardinalityLoss],
		counters[jhlog.QualityHandlerEntryLimit],
		counters[jhlog.QualityHandlerWrapperLimit],
		counters[jhlog.QualityLifecycleRegistryLimit],
		counters[jhlog.QualityObjectWatcherLimit],
		counters[jhlog.QualityJankStatsHandleLimit],
		counters[jhlog.QualityMetricFlushTimeout],
	)
	if boundedEvidenceLoss > 0 {
		addReason("medium", fmt.Sprintf("ограниченные runtime-реестры потеряли %d элементов evidence", boundedEvidenceLoss))
	}
	if c.summary.DataRecordCount > uint64(c.summary.EventCount) {
		addReason("medium", fmt.Sprintf(
			"%d data records не были декодированы как известные semantic events",
			c.summary.DataRecordCount-uint64(c.summary.EventCount),
		))
	}
	quality.ChainIssues = uniqueStrings(quality.ChainIssues)
	quality.Notices = uniqueStrings(quality.Notices)
	quality.Reasons = uniqueStrings(quality.Reasons)
	c.summary.CollectionQuality = quality
	for _, reason := range quality.Reasons {
		c.summary.Warnings = append(c.summary.Warnings, "Качество сбора: "+reason+".")
	}
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

func qualityCounterWarnings(counters map[uint64]uint64) []string {
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
		{jhlog.QualityDictionaryValueTruncated, "значения словаря были усечены"},
		{jhlog.QualityOversizedRecordTotal, "слишком крупные записи не поместились в чанк"},
		{jhlog.QualityFailedChunkTotal, "чанки не удалось зафиксировать"},
		{jhlog.QualityRecoveryTotal, "writer выполнял восстановление после ошибки"},
		{jhlog.QualityCloseTimeoutTotal, "закрытие writer завершилось по таймауту"},
		{jhlog.QualityMetricCardinalityLoss, "метрики потеряны из-за лимита кардинальности"},
		{jhlog.QualityInvalidMetric, "некорректные метрики отклонены"},
		{jhlog.QualityRuntimeGraphCapacityLoss, "runtime-граф достиг лимита рёбер"},
		{jhlog.QualityRuntimeStackMismatch, "runtime-стек вызовов рассинхронизировался"},
		{jhlog.QualityLogSpamCardinalityLoss, "агрегатор логов достиг лимита кардинальности"},
		{jhlog.QualityHandlerEntryLimit, "реестр Handler достиг лимита записей"},
		{jhlog.QualityHandlerWrapperLimit, "реестр Handler достиг лимита wrapper-объектов"},
		{jhlog.QualityLifecycleRegistryLimit, "реестр lifecycle-наблюдения достиг лимита объектов"},
		{jhlog.QualityObjectWatcherLimit, "наблюдатель удержания достиг лимита объектов"},
		{jhlog.QualityJankStatsHandleLimit, "реестр JankStats достиг лимита активных окон"},
		{jhlog.QualityMetricFlushTimeout, "агрегированные метрики не успели попасть в writer до таймаута"},
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

func (c *collector) sampleWarnings(summary Summary) []string {
	var warnings []string
	if summary.HTTPP95Approximate {
		warnings = append(warnings, fmt.Sprintf("HTTP p95 рассчитан по reservoir-сэмплу: использовано %d из %d запросов.", c.httpDurations.sampled(), c.httpDurations.seen))
	}
	var approximateRoutes int
	var totalRoutes int
	for _, route := range summary.Routes {
		if route.P95Approximate {
			approximateRoutes++
			totalRoutes += route.Count
		}
	}
	if approximateRoutes > 0 {
		warnings = append(warnings, fmt.Sprintf("P95 маршрутов приблизительный для %d маршрутов; суммарно %d запросов ограничены reservoir-сэмплингом.", approximateRoutes, totalRoutes))
	}
	return warnings
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
	comparison.Deltas = append(comparison.Deltas,
		delta("HTTP p95", baseline.HTTPP95MS, candidate.HTTPP95MS, "мс", true, minUint64(uint64(baseline.HTTPCount), uint64(candidate.HTTPCount))),
		delta("HTTP failures", uint64(baseline.HTTPFailed), uint64(candidate.HTTPFailed), "шт", true, minUint64(uint64(baseline.HTTPCount), uint64(candidate.HTTPCount))),
		deltaFloat("UI jank rate", baseline.UIJankPct, candidate.UIJankPct, "п.п.", true, minUint64(baseline.UIFrames, candidate.UIFrames)),
		deltaFloat("UI avg FPS", baseline.UIAvgFPS, candidate.UIAvgFPS, "FPS", false, minUint64(baseline.UIFrames, candidate.UIFrames)),
		delta("Main-thread stall max", baseline.StallMaxMS, candidate.StallMaxMS, "мс", true, minUint64(uint64(baseline.StallCount), uint64(candidate.StallCount))),
		delta("Max PSS", baseline.MemoryMaxKB, candidate.MemoryMaxKB, "КБ", true, minUint64(uint64(baseline.MemoryCount), uint64(candidate.MemoryCount))),
		delta("Min available memory", baseline.AvailMemoryMinKB, candidate.AvailMemoryMinKB, "КБ", false, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("UID RX delta", baseline.TrafficRxMax, candidate.TrafficRxMax, "байт", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("UID TX delta", baseline.TrafficTxMax, candidate.TrafficTxMax, "байт", true, minUint64(uint64(baseline.ContextCount), uint64(candidate.ContextCount))),
		delta("Retained objects", baseline.Retained, candidate.Retained, "шт", true, minUint64(baseline.Retained, candidate.Retained)),
		delta("Log spam", totalLogSpam(baseline), totalLogSpam(candidate), "шт", true, minUint64(uint64(len(baseline.LogSpam)), uint64(len(candidate.LogSpam)))),
		delta("Problem windows", totalProblemWindows(baseline), totalProblemWindows(candidate), "шт", true, minUint64(uint64(len(baseline.ProblemWindows)), uint64(len(candidate.ProblemWindows)))),
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
	comparison.Warnings = append(append([]string{}, comparison.CohortWarnings...), comparison.QualityWarnings...)
	return comparison
}

func mixDelta(name string, baseline, candidate []NamedValue, sampleSize uint64) Delta {
	before := namedSummary(baseline)
	after := namedSummary(candidate)
	severity := "ok"
	change := "без изменений"
	if before != after {
		severity = "medium"
		change = "изменилось"
	}
	return Delta{
		Name:       name,
		Baseline:   before,
		Candidate:  after,
		Change:     change,
		Severity:   severity,
		SampleSize: sampleSize,
	}
}

func totalLogSpam(summary Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total += item.Count
	}
	return total
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
		before := namedSummary(check.baseline)
		after := namedSummary(check.candidate)
		if before != after {
			warnings = append(warnings, fmt.Sprintf("Состав %s отличается: база [%s], кандидат [%s].", check.name, before, after))
		}
	}
	return warnings
}

func confidence(baseline, candidate Summary) string {
	sampleLevel := sampleConfidence(baseline, candidate)
	return lowerConfidenceLevel(
		lowerConfidenceLevel(sampleLevel, collectionConfidenceCap(baseline)),
		collectionConfidenceCap(candidate),
	)
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

func percentileSorted(values []uint64, p float64) uint64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(values))*p)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func fps(frames uint64, windowMS uint64) float64 {
	if frames == 0 || windowMS == 0 {
		return 0
	}
	return float64(frames) * 1000 / float64(windowMS)
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
			return screens[i].P95MS > screens[j].P95MS
		}
		return screens[i].JankRatePct > screens[j].JankRatePct
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

func namedSummary(values []NamedValue) string {
	if len(values) == 0 {
		return "неизвестно"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s:%d", humanSummaryName(value.Name), value.Value))
	}
	return strings.Join(parts, ",")
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
			regressionPct = math.Abs(changePct)
		}
		if diff >= 3.0 {
			severity = "high"
		} else if diff >= 1.0 {
			severity = "medium"
		}
	} else {
		if diff < 0 {
			regressionAbs = math.Abs(diff)
			regressionPct = math.Abs(changePct)
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
