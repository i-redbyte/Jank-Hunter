package mathanalysis

import (
	"fmt"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type mathInputAnalysis struct {
	Timeline         []TimelineBucket
	Series           []Series
	Scale            timelineScale
	RobustSamples    robustSampleMap
	RouteDefinitions []periodicDefinition
	NetworkLoops     []NetworkLoopFinding
}

func analyzeMathInputs(paths []string, options analyze.Options) (mathInputAnalysis, error) {
	scale, normalizer, robustSamples, err := detectScaleAndCollectRobust(paths, options)
	if err != nil {
		return mathInputAnalysis{}, err
	}
	timeline, series, routeDefinitions, networkLoops, err := collectBucketedMathInputs(paths, options, scale, normalizer)
	if err != nil {
		return mathInputAnalysis{}, err
	}
	return mathInputAnalysis{
		Timeline:         timeline,
		Series:           series,
		Scale:            scale,
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

func detectScaleAndCollectRobust(paths []string, options analyze.Options) (timelineScale, runTimelineNormalizer, robustSampleMap, error) {
	filter := normalizeTimelineFilter(options.Filter)
	robust := &robustCollector{
		filter:   filter,
		ownerMap: options.OwnerMap,
		samples:  robustSampleMap{},
	}
	normalizer := newRunTimelineNormalizer(paths)
	ranges := make(map[string]runTimelineRange, len(paths))
	hasData := false
	for _, path := range paths {
		runKey := normalizer.key(path)
		if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			if timeMS, ok := mathScaleEventTimeMS(event, dict, filter, options.OwnerMap); ok {
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
			robust.add(event, dict)
			return nil
		}); err != nil {
			return timelineScale{}, runTimelineNormalizer{}, nil, err
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

func collectBucketedMathInputs(paths []string, options analyze.Options, scale timelineScale, normalizer runTimelineNormalizer) ([]TimelineBucket, []Series, []periodicDefinition, []NetworkLoopFinding, error) {
	timelineCollector := &timelineCollector{
		filter:   normalizeTimelineFilter(options.Filter),
		ownerMap: options.OwnerMap,
		scale:    scale,
		buckets:  map[uint64]*timelineBucketAgg{},
	}
	routeCollector := newRouteSeriesCollector(options, scale)
	networkCollector := newNetworkLoopCollector(options, scale)
	for _, path := range paths {
		timelineState := &timelineStreamState{}
		if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			normalizedEvent := event
			normalizedEvent.TimeMS = normalizer.normalize(path, event.TimeMS)
			timelineCollector.add(normalizedEvent, dict, timelineState)
			routeCollector.add(normalizedEvent, dict)
			networkCollector.add(normalizedEvent, dict)
			return nil
		}); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	timeline := timelineCollector.finish()
	series := timelineSeries(timeline, scale.bucketMSOrDefault())
	routeDefinitions := routeCollector.definitions(3)
	networkLoops := selectNetworkLoops(networkCollector.findings())
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
