package mathanalysis

import "testing"

func TestDetectChangePointsFindsStepFunction(t *testing.T) {
	timeline := []TimelineBucket{
		httpBucket(0, 100),
		httpBucket(1000, 100),
		httpBucket(2000, 100),
		httpBucket(3000, 520),
		httpBucket(4000, 540),
		httpBucket(5000, 530),
	}

	points := detectChangePoints(timeline)
	if len(points) == 0 {
		t.Fatalf("detectChangePoints() returned no points")
	}

	point := points[0]
	if point.Signal != "HTTP p95" {
		t.Fatalf("Signal = %q, want HTTP p95", point.Signal)
	}
	if point.TimeMS != 3000 {
		t.Fatalf("TimeMS = %d, want 3000", point.TimeMS)
	}
	if point.BeforeMedian != 100 || point.AfterMedian != 530 {
		t.Fatalf("unexpected medians: before=%f after=%f", point.BeforeMedian, point.AfterMedian)
	}
	if point.Severity != "high" {
		t.Fatalf("Severity = %q, want high", point.Severity)
	}
	if point.NearbyRoute != "GET /feed" || point.NearbyOwner != "FeedRepository.refresh" {
		t.Fatalf("nearby context missing: %+v", point)
	}
}

func TestCompareChangePointsReportsAppearedAndDisappeared(t *testing.T) {
	baseline := []ChangePoint{{
		Signal: "UI доля jank",
		TimeMS: 2000,
		Score:  4,
	}}
	candidate := []ChangePoint{{
		Signal:   "HTTP p95",
		TimeMS:   3000,
		Delta:    300,
		Unit:     "мс",
		Score:    6,
		Severity: "high",
	}}

	deltas := compareChangePoints(baseline, candidate)
	if len(deltas) != 2 {
		t.Fatalf("len(deltas) = %d, want 2: %+v", len(deltas), deltas)
	}
	if deltas[0].Status != "появилась" || deltas[0].Severity != "high" {
		t.Fatalf("unexpected first delta: %+v", deltas[0])
	}
	if deltas[1].Status != "исчезла" {
		t.Fatalf("unexpected second delta: %+v", deltas[1])
	}
}

func httpBucket(startMS uint64, p95 uint64) TimelineBucket {
	return TimelineBucket{
		StartMS:           startMS,
		EndMS:             startMS + DefaultBucketMS,
		HTTPCount:         3,
		HTTPP95DurationMS: p95,
		RouteSample:       "GET /feed",
		OwnerSample:       "FeedRepository.refresh",
		NetworkSample:     "wifi",
	}
}
