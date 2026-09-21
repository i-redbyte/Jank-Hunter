package mathanalysis

import (
	"encoding/json"
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"math/rand"
	"path/filepath"
	"testing"
)

func writeHTTPCountCoverageFixture(t testing.TB, enabled, legacy, loss bool) string {
	t.Helper()
	h := jhlog.DefaultSegmentHeader()
	h.RunID = jhlog.ID128{1}
	h.ProcessInstanceID = jhlog.ID128{1}
	h.SessionID = jhlog.ID128{1}
	h.CollectorStartElapsedUS = 0
	h.SegmentStartElapsedUS = 0
	if legacy {
		h.RequiredFeatures &^= 1 << 30
	}
	path := filepath.Join(t.TempDir(), "coverage.jhlog")
	f, w, err := jhlog.CreateWithHeader(path, h)
	if err != nil {
		t.Fatal(err)
	}
	flags := uint64(0)
	if enabled && !legacy {
		flags = 1 << 11
	}
	events := []jhlog.Event{
		{Type: jhlog.EventSession, TimeMS: 1, Session: &jhlog.SessionEvent{CollectorFlags: flags}},
		{Type: jhlog.EventMemory, TimeMS: 2000, Memory: &jhlog.MemoryEvent{PSSKB: 100}},
	}
	if enabled {
		events = append(events, jhlog.Event{Type: jhlog.EventHTTP, TimeMS: 4000, HTTP: &jhlog.HTTPEvent{Status: jhlog.Status2xx}})
	}
	for _, e := range events {
		if err := w.WriteEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	counters := map[uint64]uint64{jhlog.QualityCollectionWindowStartElapsedMS: 1, jhlog.QualityCollectionWindowEndElapsedMS: 6001}
	if legacy {
		counters = map[uint64]uint64{}
	}
	if loss {
		counters[jhlog.QualityQueueFullTotal] = 1
	}
	w.SetQualitySnapshot(jhlog.QualitySnapshot{CapturedElapsedUS: 6001000, Counters: counters})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHTTPCountCoverageDistinguishesQuietDisabledLegacyAndLoss(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		enabled, legacy, loss bool
		state                 string
		present               bool
	}{
		{"quiet", true, false, false, "observed", true},
		{"disabled", false, false, false, "unsupported", false},
		{"legacy", true, true, false, "missing", false},
		{"loss", true, false, true, "missing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := writeHTTPCountCoverageFixture(t, tc.enabled, tc.legacy, tc.loss)
			for _, filter := range []analyze.Filter{{}, {RouteContains: "never-matches"}} {
				got, err := analyzeMathInputs([]string{input}, analyze.Options{Filter: filter})
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Timeline) < 6 {
					t.Fatalf("quiet collection duration disappeared: %d buckets", len(got.Timeline))
				}
				raw, _ := json.Marshal(got.Timeline[2])
				var b map[string]any
				json.Unmarshal(raw, &b)
				if b["HTTPCountState"] != tc.state {
					t.Errorf("state=%v want %s", b["HTTPCountState"], tc.state)
				}
				found := false
				for _, s := range got.Series {
					if s.Name == "HTTP запросы" {
						found = true
						if s.Present[2] != tc.present || s.Points[2] != 0 {
							t.Errorf("false quiet observation: %+v", s)
						}
					}
				}
				if tc.present && !found {
					t.Error("observed zero series missing")
				}
			}
		})
	}
}

func TestHTTPCountLossCannotCreateRoutePeriodOrNetworkLoop(t *testing.T) {
	h := jhlog.DefaultSegmentHeader()
	h.RunID = jhlog.ID128{1}
	h.ProcessInstanceID = jhlog.ID128{1}
	h.SessionID = jhlog.ID128{1}
	path := filepath.Join(t.TempDir(), "lost-loop.jhlog")
	f, w, err := jhlog.CreateWithHeader(path, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{CollectorFlags: 1 << 11}}); err != nil {
		t.Fatal(err)
	}
	for i := uint64(0); i < 60; i += 4 {
		for j := uint64(0); j < 3; j++ {
			if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: i*1000 + j, HTTP: &jhlog.HTTPEvent{DurationMS: 50, Status: jhlog.Status2xx}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.SetQualitySnapshot(jhlog.QualitySnapshot{CapturedElapsedUS: 60000000, Counters: map[uint64]uint64{jhlog.QualityQueueFullTotal: 1}})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err := analyzeMathInputs([]string{path}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.NetworkLoops) > 0 {
		t.Error("loss fabricated a complete periodic network signal")
	}
	for _, d := range got.RouteDefinitions {
		_, observed := longestPeriodicRun(d.points, d.present)
		if observed != 0 {
			t.Errorf("route has %d invented observations", observed)
		}
	}
}

func completeHTTPTestHeader() jhlog.SegmentHeader {
	h := jhlog.DefaultSegmentHeader()
	h.RunID = jhlog.ID128{1}
	h.ProcessInstanceID = jhlog.ID128{1}
	h.SessionID = jhlog.ID128{1}
	return h
}

func TestHTTPUnknownCountCannotBeAHealthyRecovery(t *testing.T) {
	timeline := []TimelineBucket{
		{StartMS: 0, EndMS: 1000, HasObservation: true, HTTPCount: 1, HTTPFailed: 1, HTTPCountState: CountObserved},
		{StartMS: 1000, EndMS: 2000, HasObservation: true, HasMemoryPSS: true, MemoryPSSKB: 100, HTTPCountState: CountMissing},
		{StartMS: 2000, EndMS: 3000, HasObservation: true, HTTPCountState: CountObserved},
	}
	model := buildMarkovModel(timeline, nil)
	if len(model.States) != 2 || model.TransitionEventCount != 0 || model.States[1].State == markovRecovering {
		t.Fatalf("unknown HTTP interval became recovery: %+v", model)
	}
}

func writeHTTPCoverageSegments(t testing.TB, process byte, indices []uint64, enabled bool) []string {
	t.Helper()
	var paths []string
	var digest []byte
	for n, index := range indices {
		h := completeHTTPTestHeader()
		h.ProcessInstanceID = jhlog.ID128{process}
		h.SessionID = jhlog.ID128{process}
		h.SegmentIndex = index
		h.PreviousSegmentDigest = digest
		if index > 0 && len(digest) == 0 {
			h.PreviousSegmentDigest = make([]byte, 32)
		}
		h.SegmentStartElapsedUS = uint64(n) * 3000000
		path := filepath.Join(t.TempDir(), "segment.jhlog")
		f, w, err := jhlog.CreateWithHeader(path, h)
		if err != nil {
			t.Fatal(err)
		}
		flags := uint64(0)
		if enabled {
			flags = uint64(jhlog.CollectorHTTP)
		}
		if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventSession, TimeMS: uint64(n)*3000 + 1, Session: &jhlog.SessionEvent{CollectorFlags: flags}}); err != nil {
			t.Fatal(err)
		}
		counters := map[uint64]uint64{jhlog.QualityCollectionWindowStartElapsedMS: 1}
		if n+1 == len(indices) {
			counters[jhlog.QualityCollectionWindowEndElapsedMS] = uint64(n+1)*3000 + 1
		}
		w.SetQualitySnapshot(jhlog.QualitySnapshot{CapturedElapsedUS: uint64(n+1)*3000000 + 1000, Counters: counters})
		reason := jhlog.SegmentEndNormal
		if n+1 < len(indices) {
			reason = jhlog.SegmentEndRotation
		}
		if err := w.CloseWithReason(reason); err != nil {
			t.Fatal(err)
		}
		digest = w.SegmentDigest()
		f.Close()
		paths = append(paths, path)
	}
	return paths
}

func TestHTTPCountCoverageRequiresContinuousAllProcessInput(t *testing.T) {
	for _, tc := range []struct {
		name          string
		indices       []uint64
		secondProcess bool
		want          string
	}{
		{"rotation", []uint64{0, 1}, false, CountObserved},
		{"missing_segment", []uint64{0, 2}, false, CountMissing},
		{"missing_prefix", []uint64{1, 2}, false, CountMissing},
		{"other_process_disabled", []uint64{0, 1}, true, CountMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := writeHTTPCoverageSegments(t, 1, tc.indices, true)
			if tc.secondProcess {
				paths = append(paths, writeHTTPCoverageSegments(t, 2, []uint64{0, 1}, false)...)
			}
			for _, reverse := range []bool{false, true} {
				if reverse {
					for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
						paths[i], paths[j] = paths[j], paths[i]
					}
				}
				inputs, err := analyzeMathInputs(paths, analyze.Options{})
				if err != nil {
					t.Fatal(err)
				}
				if len(inputs.Timeline) != 6 {
					t.Fatalf("len=%d", len(inputs.Timeline))
				}
				for _, b := range inputs.Timeline {
					if b.HTTPCountState != tc.want {
						t.Fatalf("reverse=%v: %+v want %s", reverse, b, tc.want)
					}
				}
			}
		})
	}
}

func TestHTTPCountCoverageDoesNotRoundStartDown(t *testing.T) {
	c := httpCoverageCollector{}
	h := completeHTTPTestHeader()
	c.begin("fixture", h)
	c.observe(jhlog.Event{Type: jhlog.EventSession, TimeUS: 500, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorHTTP)}})
	c.end(jhlog.StreamResult{Header: h, Sealed: true, Status: jhlog.SegmentStatusClosedClean, LatestQuality: &jhlog.QualitySnapshot{CapturedElapsedUS: 2000000, Counters: map[uint64]uint64{jhlog.QualityCollectionWindowStartElapsedMS: 1, jhlog.QualityCollectionWindowEndElapsedMS: 2000}}, SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndNormal}})
	timeline := []TimelineBucket{{StartMS: 0, EndMS: 1000}, {StartMS: 1000, EndMS: 2000}}
	c.apply(timeline, runTimelineNormalizer{})
	if timeline[0].HTTPCountState != CountMissing || timeline[1].HTTPCountState != CountObserved {
		t.Fatalf("sub-millisecond collection boundary rounded into coverage: %+v", timeline)
	}
}

func TestHTTPCountGapCannotCreateChangePointOrExactIntegral(t *testing.T) {
	var timeline []TimelineBucket
	for i := 0; i < 7; i++ {
		b := TimelineBucket{StartMS: uint64(i) * 1000, EndMS: uint64(i+1) * 1000, HTTPCountState: CountObserved}
		if i == 3 {
			b.HTTPCountState = CountMissing
		}
		if i > 3 {
			b.HTTPCount = 5
			b.HTTPFailed = 5
		}
		timeline = append(timeline, b)
	}
	for _, p := range detectChangePoints(timeline) {
		if p.Signal == "HTTP ошибки" {
			t.Error("change point crosses missing count")
		}
	}
	incomplete := computeIntegralScoresForRuns(timeline, nil, 1)
	for _, s := range incomplete {
		if s.ID == "network_failure_burn" {
			t.Error("unknown counts generated exact integral")
		}
	}
	timeline[3].HTTPCountState = CountObserved
	complete := computeIntegralScoresForRuns(timeline, nil, 1)
	for _, d := range compareIntegralScores(incomplete, complete) {
		if d.ID == "network_failure_burn" {
			t.Error("unavailable integral became baseline zero")
		}
	}
}

func TestHTTPCountCoverageUsesGateWindowNotSnapshotLifetime(t *testing.T) {
	c := httpCoverageCollector{}
	h := completeHTTPTestHeader()
	c.begin("fixture", h)
	c.observe(jhlog.Event{Type: jhlog.EventSession, Session: &jhlog.SessionEvent{CollectorFlags: uint64(jhlog.CollectorHTTP)}})
	c.end(jhlog.StreamResult{Header: h, Sealed: true, Status: jhlog.SegmentStatusClosedClean,
		LatestQuality: &jhlog.QualitySnapshot{CapturedElapsedUS: 6000000, Counters: map[uint64]uint64{0x204b: 2000, 0x204c: 3000}},
		SegmentEnd:    &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndNormal}})
	timeline := make([]TimelineBucket, 6)
	for i := range timeline {
		timeline[i].StartMS = uint64(i) * 1000
		timeline[i].EndMS = uint64(i+1) * 1000
	}
	c.apply(timeline, runTimelineNormalizer{})
	for i, b := range timeline {
		if (b.HTTPCountState == CountObserved) != (i == 2) {
			t.Fatalf("bucket%d uses writer lifetime instead of collection: %s", i, b.HTTPCountState)
		}
	}
}

func TestARTQuietHTTPCountCoverage(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		path := fmt.Sprintf("../../../wire/testdata/http-state-%t-5.1.0.jhlog", enabled)
		inputs, err := analyzeMathInputs([]string{path}, analyze.Options{})
		if err != nil {
			t.Fatal(err)
		}
		expected := CountUnsupported
		if enabled {
			expected = CountObserved
		}
		covered := 0
		for _, b := range inputs.Timeline {
			if b.HTTPCountState == expected {
				covered++
				if b.HTTPCount != 0 {
					t.Fatal("quiet ART session has HTTP events")
				}
			}
		}
		if covered == 0 {
			t.Fatalf("enabled=%t no complete quiet interval in ART fixture", enabled)
		}
	}
}

func TestVerifiedQuietHTTPBucketsRemainObservations(t *testing.T) {
	inputs, err := analyzeMathInputs([]string{writeHTTPCountCoverageFixture(t, true, false, false)}, analyze.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !inputs.Timeline[2].HasObservation {
		t.Fatal("verified quiet count is still classified as an unobserved bucket")
	}
}

func TestHTTPCountCoverageSweepMatchesPerSourceReference(t *testing.T) {
	type interval struct {
		source     int
		start, end uint64
		enabled    bool
	}
	for seed := int64(1); seed <= 64; seed++ {
		rng := rand.New(rand.NewSource(seed))
		c := httpCoverageCollector{epochs: map[httpEpochKey]*httpCoverageEpoch{}}
		var intervals []interval
		for source := 0; source < 3; source++ {
			start := uint64(1)
			for epoch := 0; epoch < 4; epoch++ {
				end := start + uint64(2000+rng.Intn(5000))
				enabled := rng.Intn(3) != 0
				intervals = append(intervals, interval{source, start, end, enabled})
				key := httpEpochKey{run: jhlog.ID128{1}, process: jhlog.ID128{byte(source + 1)}, session: jhlog.ID128{byte(epoch + 1)}}
				c.epochs[key] = &httpCoverageEpoch{source: fmt.Sprint(source), start: start * 1000, end: end * 1000, windowStartMS: start, windowEndMS: end,
					known: true, enabled: enabled, started: true, previous: jhlog.StreamResult{SegmentEnd: &jhlog.SegmentEndEvent{Reason: jhlog.SegmentEndNormal}}}
				start = end + uint64(rng.Intn(1500))
			}
		}
		timeline := make([]TimelineBucket, 30)
		for i := range timeline {
			timeline[i] = TimelineBucket{StartMS: uint64(i) * 1000, EndMS: uint64(i+1) * 1000}
		}
		c.apply(timeline, runTimelineNormalizer{})
		for _, b := range timeline {
			observed, unsupported := 0, 0
			for source := 0; source < 3; source++ {
				for _, r := range intervals {
					if r.source == source && r.start <= b.StartMS && r.end >= b.EndMS {
						if r.enabled {
							observed++
						} else {
							unsupported++
						}
					}
				}
			}
			expected := CountMissing
			if observed == 3 {
				expected = CountObserved
			}
			if unsupported == 3 {
				expected = CountUnsupported
			}
			if b.HTTPCountState != expected {
				t.Fatalf("seed%d bucket%d: %s want%s", seed, b.StartMS, b.HTTPCountState, expected)
			}
		}
	}
}

func TestMissingHTTPCountCoverageSkipsIneligibleNetworkWork(t *testing.T) {
	budget := newCollectionBudgetWithWork(0, 1)
	collector := newNetworkLoopCollector(analyze.Options{}, timelineScale{bucketMS: 1000, bucketCount: 50000, hasData: true})
	collector.budget = budget
	collector.timeline = make([]TimelineBucket, 50000)
	for i := range collector.timeline {
		collector.timeline[i].HTTPCountState = CountMissing
	}
	for _, index := range []int{1, 5, 9} {
		collector.addPoint("route:test", "test", "route", index, 1)
	}
	if got := collector.findings(); len(got) != 0 || budget.err() != nil || budget.workUsed != 0 {
		t.Fatal("unobserved count candidate consumed spectral quota")
	}
}
