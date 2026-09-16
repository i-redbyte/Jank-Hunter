package mathanalysis

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func writeLoopAttributionFixture(t testing.TB, mode string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "attribution.jhlog")
	f, w, err := jhlog.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []string{"FirstOwner", "ActualLoopOwner", "OtherOwner", "GET /loop", "GET /initial"} {
		kind := jhlog.DictOwner
		if i >= 3 {
			kind = jhlog.DictRoute
		}
		entry := jhlog.DictionaryEntry{Kind: kind, ID: uint64(i + 1), Value: value}
		if err := w.WriteEvent(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatal(err)
		}
	}
	write := func(time, owner, route uint64) {
		event := jhlog.Event{Type: jhlog.EventHTTP, TimeMS: time, Attribution: jhlog.AttributionContext{Present: owner != 0, Owner: jhlog.LocalSymbol(owner)}, HTTP: &jhlog.HTTPEvent{RouteRef: jhlog.LocalSymbol(route), DurationMS: 150, DNSMS: 5, ConnectMS: 7, Status: jhlog.Status5xx}}
		if err := w.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "route_change" {
		write(100, 2, 5)
	} else {
		write(100, 1, 4)
	}
	for bucket := uint64(4); bucket <= 48; bucket += 4 {
		for offset := uint64(0); offset < 3; offset++ {
			owner := uint64(2)
			if offset == 2 {
				if mode == "mixed" {
					owner = 3
				}
				if mode == "missing" {
					owner = 0
				}
			}
			write(bucket*DefaultBucketMS+100+offset*20, owner, 4)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func loopAttributionSignals(t testing.TB, path string) *networkLoopCollector {
	t.Helper()
	c := newNetworkLoopCollector(analyze.Options{}, newTimelineScale(0, 49*DefaultBucketMS, true))
	symbols := newMathSymbolResolver(analyze.Options{})
	if err := jhlog.StreamFile(path, func(event jhlog.Event, dict map[uint64]string) error {
		symbols.observe(event)
		c.add(event, dict, symbols)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, signal := range c.signals {
		signal.points = make([]float64, c.bucketSize)
		signal.sparse.writeDense(signal.points)
	}
	return c
}

func TestNetworkLoopOwnerComesFromDetectedBursts(t *testing.T) {
	for _, mode := range []string{"before", "mixed", "missing"} {
		c := loopAttributionSignals(t, writeLoopAttributionFixture(t, mode))
		for _, prefix := range []string{"route:", "dns:route:", "connect:route:", "failure:route:"} {
			finding, ok := analyzeNetworkLoopSignal(c.signals[prefix+"GET /loop"], DefaultBucketMS)
			if !ok {
				t.Fatalf("%s/%s: loop not detected", mode, prefix)
			}
			want := "ActualLoopOwner"
			wantStatus := "observed"
			if mode != "before" {
				want = ""
				wantStatus = "ambiguous"
				if mode == "missing" {
					wantStatus = "unknown"
				}
			}
			if finding.Owner != want || finding.OwnerAttributionStatus != wantStatus || finding.RouteAttributionStatus != "observed" {
				t.Errorf("%s/%s: owner=%q want%q, confidence%.2f", mode, prefix, finding.Owner, want, finding.Confidence)
			}
			for _, node := range finding.Path.Nodes {
				if strings.Contains(node, "FirstOwner") || (want == "" && (strings.Contains(node, "ActualLoopOwner") || strings.Contains(node, "OtherOwner"))) {
					t.Errorf("%s: path invents a single source: %v", mode, finding.Path.Nodes)
					break
				}
			}
		}
	}
}

func TestNetworkLoopChangingBurstOwnersRemainAmbiguous(t *testing.T) {
	signal := &networkLoopSignal{name: "route", kind: "route", points: make([]float64, 49), tokens: map[int]map[string]int{}}
	for i := 0; i <= 48; i += 4 {
		owner := "A"
		if i >= 24 {
			owner = "B"
		}
		signal.points[i] = 3
		signal.tokens[i] = map[string]int{"route:GET /loop": 3, "owner:" + owner: 3}
	}
	finding, ok := analyzeNetworkLoopSignal(signal, DefaultBucketMS)
	if !ok || finding.Owner != "" || finding.OwnerAttributionStatus != "ambiguous" || finding.Route != "GET /loop" {
		t.Fatalf("changing source collapsed into one owner: %+v", finding)
	}
}

func TestNetworkLoopRouteComesFromDetectedBursts(t *testing.T) {
	c := loopAttributionSignals(t, writeLoopAttributionFixture(t, "route_change"))
	finding, ok := analyzeNetworkLoopSignal(c.signals["owner:ActualLoopOwner"], DefaultBucketMS)
	if !ok || finding.Route != "GET /loop" || finding.Owner != "ActualLoopOwner" {
		t.Fatalf("first route replaced actual burst context: %+v", finding)
	}
	if strings.Contains(strings.Join(finding.Path.Nodes, " "), "GET /initial") {
		t.Fatal("old route remained in explanation")
	}
}

func TestNetworkLoopContextIsNotInferredFromTruncatedMotif(t *testing.T) {
	signal := &networkLoopSignal{name: "global", kind: "failure", points: make([]float64, 49), tokens: map[int]map[string]int{}}
	for i := 0; i <= 48; i += 4 {
		signal.points[i] = 4
		signal.tokens[i] = map[string]int{"dns_high": 4, "connect_high": 4, "reconnect_high": 4, "http_failed": 4, "route:GET /one": 3, "route:GET /two": 1, "owner:A": 3, "owner:B": 1}
	}
	finding, ok := analyzeNetworkLoopSignal(signal, DefaultBucketMS)
	if !ok || finding.Route != "" || finding.Owner != "" {
		t.Fatalf("top-five motif hid conflicting context: %+v", finding)
	}
	for _, node := range finding.Path.Nodes {
		if strings.HasPrefix(node, "маршрут:") || strings.HasPrefix(node, "место запуска:") {
			t.Fatalf("mixed context escaped through path: %v", finding.Path.Nodes)
		}
	}
}

func TestNetworkLoopUnknownContextsDoNotBecomeOneBlamedOwner(t *testing.T) {
	for _, unknown := range []string{"unknown", "", "неизвестно"} {
		scores := causalOwnerScores(nil, []CausalEdge{{From: "owner:" + unknown, To: "state:" + markovNetworkSlow, Kind: "owner-state", Confidence: 0.9}}, []NetworkLoopFinding{{Owner: unknown, Confidence: 0.9}})
		if len(scores) != 0 {
			t.Errorf("unknown context treated as one source: %+v", scores)
		}
	}
}
