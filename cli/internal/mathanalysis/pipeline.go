package mathanalysis

import (
	"fmt"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type mathInputAnalysis struct {
	budget           *collectionBudget
	Timeline         []TimelineBucket
	Series           []Series
	Scale            timelineScale
	TimelineGroups   int
	RobustSamples    robustSampleMap
	RouteDefinitions []periodicDefinition
	NetworkLoops     []NetworkLoopFinding
}

func analyzeMathInputs(paths []string, options analyze.Options) (mathInputAnalysis, error) {
	return analyzeMathInputsWithBudget(paths, options, newCollectionBudgetWithWork(options.MathMemoryLimitBytes, options.MathSpectralWorkLimitOperations))
}

func analyzeMathInputsWithBudget(paths []string, options analyze.Options, budget *collectionBudget) (mathInputAnalysis, error) {
	metadata := budget.account("run metadata")
	defer metadata.close()
	if !metadata.reserveItems(len(paths), 1024) {
		return mathInputAnalysis{}, budget.err()
	}
	for _, path := range paths {
		if !metadata.reserve(uint64(len(path)) * 2) {
			return mathInputAnalysis{}, budget.err()
		}
	}
	scale, normalizer, robustSamples, err := detectScaleAndCollectRobust(paths, options, budget)
	if err != nil {
		return mathInputAnalysis{}, err
	}
	timeline, series, routeDefinitions, networkLoops, err := collectBucketedMathInputs(paths, options, scale, normalizer, budget)
	if err != nil {
		return mathInputAnalysis{}, err
	}
	return mathInputAnalysis{
		budget:           budget,
		Timeline:         timeline,
		Series:           series,
		Scale:            scale,
		TimelineGroups:   len(normalizer.baseByKey),
		RobustSamples:    robustSamples,
		RouteDefinitions: routeDefinitions,
		NetworkLoops:     networkLoops,
	}, nil
}

type runTimelineRange struct {
	minMS   uint64
	maxMS   uint64
	hasData bool
}

type runTimelineNormalizer struct {
	keyByPath map[string]string
	baseByKey map[string]uint64
}

func detectScaleAndCollectRobust(paths []string, options analyze.Options, budget *collectionBudget) (timelineScale, runTimelineNormalizer, robustSampleMap, error) {
	filter := normalizeTimelineFilter(options.Filter)
	robust := &robustCollector{
		account: budget.account("robust samples"),
		samples: robustSampleMap{},
	}
	normalizer := newRunTimelineNormalizer(paths)
	ranges := make(map[string]runTimelineRange, len(paths))
	hasData := false
	inputs, err := analyze.OrderedSessionInputs(paths)
	if err != nil {
		return timelineScale{}, runTimelineNormalizer{}, nil, err
	}
	stalls := mathStallLifecycle{account: budget.account("stall lifecycle")}
	defer stalls.account.close()
	for index, input := range inputs {
		path := input.Path
		runKey := normalizer.key(path)
		symbols := newMathSymbolResolver(options)
		symbols.account = budget.account("stable symbols")
		consume := func(event jhlog.Event, dict map[uint64]string) error {
			symbols.observe(event)
			if err := budget.err(); err != nil {
				return err
			}
			if event.Session == nil && filter.Active() && !mathEventMatchesFilter(event, dict, filter, symbols) {
				return nil
			}
			if timeMS, ok := mathScaleEventTimeMS(event, dict, analyze.Filter{}, symbols); ok || event.Session != nil {
				runRange := ranges[runKey]
				if !runRange.hasData || timeMS < runRange.minMS {
					runRange.minMS = timeMS
				}
				if !runRange.hasData || timeMS > runRange.maxMS {
					runRange.maxMS = timeMS
				}
				runRange.hasData = true
				ranges[runKey] = runRange
				hasData = true
			}
			robust.add(event, dict, symbols)
			return budget.err()
		}
		streamResult, streamErr := stalls.streamWithResult(path, symbols, consume)
		if streamResult.Sealed && streamResult.LatestQuality != nil {
			r := ranges[runKey]
			end := streamResult.LatestQuality.CapturedElapsedUS / 1000
			if end > 0 {
				end--
			}
			if r.hasData && end > r.maxMS {
				r.maxMS = end
				ranges[runKey] = r
			}
		}
		symbols.account.close()
		if err := streamErr; err != nil {
			return timelineScale{}, runTimelineNormalizer{}, nil, err
		}
		if mathSessionEnds(inputs, index) {
			if err := stalls.finish(consume); err != nil {
				return timelineScale{}, runTimelineNormalizer{}, nil, err
			}
		}
	}
	var maxDurationMS uint64
	for runKey, runRange := range ranges {
		if !runRange.hasData {
			continue
		}
		normalizer.baseByKey[runKey] = runRange.minMS
		durationMS := safeCounterDelta(runRange.minMS, runRange.maxMS)
		if durationMS > maxDurationMS {
			maxDurationMS = durationMS
		}
	}
	return newTimelineScale(0, maxDurationMS, hasData), normalizer, robust.samples, nil
}

func collectBucketedMathInputs(paths []string, options analyze.Options, scale timelineScale, normalizer runTimelineNormalizer, budget *collectionBudget) ([]TimelineBucket, []Series, []periodicDefinition, []NetworkLoopFinding, error) {
	results := budget.account("derived series")
	coverage := httpCoverageCollector{account: budget.account("HTTP coverage")}
	defer coverage.account.close()
	filter := normalizeTimelineFilter(options.Filter)
	collectorOptions := options
	collectorOptions.Filter = analyze.Filter{}
	timelineCollector := &timelineCollector{
		account: budget.account("timeline buckets"), results: results,
		scale:   scale,
		buckets: map[uint64]*timelineBucketAgg{},
	}
	routeCollector := newRouteSeriesCollector(collectorOptions, scale)
	networkCollector := newNetworkLoopCollector(collectorOptions, scale)
	networkCollector.budget = budget
	routeCollector.account, routeCollector.results = budget.account("route series"), results
	networkCollector.account, networkCollector.results = budget.account("network signals"), results
	defer timelineCollector.account.close()
	timelineCollector.traffic.account = budget.account("UID traffic intervals")
	defer timelineCollector.traffic.account.close()
	defer routeCollector.account.close()
	defer networkCollector.account.close()
	inputs, err := analyze.OrderedSessionInputs(paths)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stalls := mathStallLifecycle{account: budget.account("stall lifecycle")}
	defer stalls.account.close()
	for index, input := range inputs {
		path := input.Path
		timelineState := &timelineStreamState{}
		coverage.begin(path, input.Header)
		timelineCollector.traffic.tracker.StartSegment(input.Header)
		symbols := newMathSymbolResolver(options)
		symbols.account = budget.account("stable symbols")
		consume := func(event jhlog.Event, dict map[uint64]string) error {
			symbols.observe(event)
			coverage.observe(event)
			if err := budget.err(); err != nil {
				return err
			}
			if filter.Active() && !mathEventMatchesFilter(event, dict, filter, symbols) {
				return nil
			}
			normalizedEvent := event
			normalizedEvent.TimeMS = normalizer.normalize(path, event.TimeMS)
			timelineCollector.add(normalizedEvent, dict, timelineState, symbols)
			routeCollector.add(normalizedEvent, dict, symbols)
			networkCollector.add(normalizedEvent, dict, symbols)
			return budget.err()
		}
		streamResult, streamErr := stalls.streamWithResult(path, symbols, consume)
		timelineCollector.traffic.tracker.EndSegment(streamResult)
		coverage.end(streamResult)
		symbols.account.close()
		if err := streamErr; err != nil {
			return nil, nil, nil, nil, err
		}
		if mathSessionEnds(inputs, index) {
			if err := stalls.finish(consume); err != nil {
				return nil, nil, nil, nil, err
			}
		}
	}
	timeline := timelineCollector.finish()
	coverage.apply(timeline, normalizer)
	if err := budget.err(); err != nil {
		return nil, nil, nil, nil, err
	}
	series := timelineSeriesWithAccount(timeline, scale.bucketMSOrDefault(), results)
	routeCollector.timeline = timeline
	networkCollector.timeline = timeline
	routeDefinitions := routeCollector.definitions(3)
	networkLoops := selectNetworkLoops(networkCollector.findings())
	if err := budget.err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return timeline, series, routeDefinitions, networkLoops, nil
}

func newRunTimelineNormalizer(paths []string) runTimelineNormalizer {
	normalizer := runTimelineNormalizer{
		keyByPath: make(map[string]string, len(paths)),
		baseByKey: make(map[string]uint64, len(paths)),
	}
	for _, path := range paths {
		normalizer.keyByPath[path] = timelineRunKey(path)
	}
	return normalizer
}

func (n runTimelineNormalizer) key(path string) string {
	if key := n.keyByPath[path]; key != "" {
		return key
	}
	return timelinePathKey(path)
}

func (n runTimelineNormalizer) normalize(path string, timeMS uint64) uint64 {
	baseMS, ok := n.baseByKey[n.key(path)]
	if !ok {
		return 0
	}
	if timeMS <= baseMS {
		return 0
	}
	return timeMS - baseMS
}

func timelineRunKey(path string) string {
	header, err := jhlog.ReadSessionHeader(path)
	if err != nil {
		return timelinePathKey(path)
	}
	var zero jhlog.ID128
	if header.RunID != zero {
		return fmt.Sprintf("run:%x", header.RunID[:])
	}
	if header.SessionID != zero {
		return fmt.Sprintf("session:%x", header.SessionID[:])
	}
	return timelinePathKey(path)
}

func timelinePathKey(path string) string {
	canonical, err := filepath.Abs(path)
	if err != nil {
		canonical = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}
	return "path:" + canonical
}
