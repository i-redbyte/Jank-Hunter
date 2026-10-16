package jhlog

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerCatalogRoundTripIncludesNeverRunClassesAndCoverage(t *testing.T) {
	catalog := &WorkerCatalogEvent{Schema: 1, Status: "available", Scope: "application-and-packaged-dependencies-pre-r8", Namespace: strings.Repeat("a", 32), DynamicModules: "outside-base-scope", UnresolvedClasses: 3, OmittedWorkers: 2, Classes: []string{"app.NeverRun"}}
	path := filepath.Join(t.TempDir(), "catalog.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkerCatalog, DiagnosticEvent: &DiagnosticEvent{WorkerCatalog: catalog}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || !reflect.DeepEqual(log.Events[0].WorkerCatalog, catalog) {
		t.Fatalf("catalog roundtrip: %+v", log.Events)
	}
}

func TestWorkerCatalogRejectsMalformedOrUnboundedClaims(t *testing.T) {
	valid := WorkerCatalogEvent{Schema: 1, Status: "available", Scope: "application-and-packaged-dependencies-pre-r8", Namespace: strings.Repeat("a", 32), DynamicModules: "outside-base-scope"}
	cases := []WorkerCatalogEvent{valid, valid, valid, valid}
	cases[0].Classes = []string{"app.Worker", "app.Worker"}
	cases[1].Schema = 2
	cases[2].Classes = []string{strings.Repeat("A", 1025)}
	cases[3].Status = "missing"
	cases[3].Classes = []string{"app.Worker"}
	for _, catalog := range cases {
		var out bytes.Buffer
		if err := (eventPayloadEncoder{writer: &out}).encodeWorkerCatalog(&catalog); err == nil {
			t.Fatalf("accepted %+v", catalog)
		}
	}
}

func TestWorkerCatalogMinifiedAndroidFixture(t *testing.T) {
	log, err := readLog("../../../wire/testdata/worker-catalog-r8-5.1.0.jhlog")
	if err != nil {
		t.Fatal(err)
	}
	catalogs := 0
	for _, event := range log.Events {
		if event.DiagnosticEvent == nil || event.WorkerCatalog == nil {
			continue
		}
		catalogs++
		c := event.WorkerCatalog
		found := false
		for _, name := range c.Classes {
			if name == "com.example.jhsmoke.WorkerExecutionProbe$NeverRunWorker" {
				found = true
			}
		}
		if !found || c.Status != "available" || c.OmittedWorkers != 0 {
			t.Fatalf("catalog lost unused source class: %+v", c)
		}
	}
	if catalogs != 1 || !log.Result.Sealed {
		t.Fatalf("catalogs=%d sealed=%v", catalogs, log.Result.Sealed)
	}
}
