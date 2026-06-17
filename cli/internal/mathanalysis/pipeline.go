package mathanalysis

import (
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
	scale, robustSamples, err := detectScaleAndCollectRobust(paths, options)
	if err != nil {
		return mathInputAnalysis{}, err
	}
	timeline, series, routeDefinitions, networkLoops, err := collectBucketedMathInputs(paths, options, scale)
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

func detectScaleAndCollectRobust(paths []string, options analyze.Options) (timelineScale, robustSampleMap, error) {
	filter := normalizeTimelineFilter(options.Filter)
	robust := &robustCollector{
		filter:   filter,
		ownerMap: options.OwnerMap,
		samples:  robustSampleMap{},
	}
	var minMS uint64
	var maxMS uint64
	hasData := false
	for _, path := range paths {
		if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
			if timeMS, ok := mathScaleEventTimeMS(event, dict, filter, options.OwnerMap); ok {
				if !hasData || timeMS < minMS {
					minMS = timeMS
				}
				if !hasData || timeMS > maxMS {
					maxMS = timeMS
				}
				hasData = true
			}
			robust.add(event, dict)
			return nil
		}); err != nil {
			return timelineScale{}, nil, err
		}
	}
	return newTimelineScale(minMS, maxMS, hasData), robust.samples, nil
}

func collectBucketedMathInputs(paths []string, options analyze.Options, scale timelineScale) ([]TimelineBucket, []Series, []periodicDefinition, []NetworkLoopFinding, error) {
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
			timelineCollector.add(event, dict, timelineState)
			routeCollector.add(event, dict)
			networkCollector.add(event, dict)
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
