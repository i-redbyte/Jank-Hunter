package analyze

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func writeAcquisitionRotation(t testing.TB, run byte, segments int, device string) []string {
	t.Helper()
	directory := t.TempDir()
	var paths []string
	var previous []byte
	for segment := 0; segment < segments; segment++ {
		path := filepath.Join(directory, fmt.Sprintf("run%d-%d.jhlog", run, segment))
		header := collectionTestHeader(run*10, uint64(segment))
		header.RunID[0] = run
		header.ProcessInstanceID[0] = run + 100
		header.PreviousSegmentDigest = previous
		file, writer, err := jhlog.CreateWithHeader(path, header)
		if err != nil {
			t.Fatal(err)
		}
		write := func(event jhlog.Event) {
			t.Helper()
			if err := writer.WriteEvent(event); err != nil {
				t.Fatal(err)
			}
		}
		write(jhlog.Event{Type: jhlog.EventDictionary, Dictionary: &jhlog.DictionaryEntry{Kind: jhlog.DictGeneric, ID: 1, Value: device}})
		write(jhlog.Event{Type: jhlog.EventSession, TimeMS: uint64(1000 + segment*1000/segments*10), Session: &jhlog.SessionEvent{SDKInt: 35, ProcessName: "main", DeviceRef: jhlog.LocalSymbol(1)}})
		for index := segment * 1000 / segments; index < (segment+1)*1000/segments; index++ {
			write(jhlog.Event{Type: jhlog.EventHTTP, TimeMS: uint64(1000 + index*10), HTTP: &jhlog.HTTPEvent{DurationMS: 20, Status: jhlog.Status2xx}})
		}
		reason := jhlog.SegmentEndNormal
		if segment+1 < segments {
			reason = jhlog.SegmentEndRotation
		}
		if err := writer.CloseWithReason(reason); err != nil {
			t.Fatal(err)
		}
		previous = writer.SegmentDigest()
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func TestActualRotatedFilesCannotIncreaseReplication(t *testing.T) {
	for _, segments := range []int{1, 2, 5} {
		paths := writeAcquisitionRotation(t, 1, segments, "device-a")
		summary, err := InspectFilesWithOptions("rotation", paths, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if summary.HTTPCount != 1000 || AcquisitionEvidenceFor(summary).IndependentGroups != 1 || sampleConfidence(summary, summary) != "low" {
			t.Fatalf("%d segments changed replication or workload: %d HTTP, %+v", segments, summary.HTTPCount, AcquisitionEvidenceFor(summary))
		}
	}
}

func TestRotationCannotChangeEnvironmentCohortWeights(t *testing.T) {
	inspect := func(firstSegments int) Summary {
		paths := append(writeAcquisitionRotation(t, 1, firstSegments, "device-a"), writeAcquisitionRotation(t, 2, 1, "device-b")...)
		summary, err := InspectFilesWithOptions("cohorts", paths, Options{})
		if err != nil {
			t.Fatal(err)
		}
		return summary
	}
	one, five := inspect(1), inspect(5)
	if !reflect.DeepEqual(one.Devices, five.Devices) || !reflect.DeepEqual(one.SDKs, five.SDKs) || !reflect.DeepEqual(one.Processes, five.Processes) {
		t.Fatalf("same observations acquire different cohort weights: devices %v -> %v; sdk %v -> %v", one.Devices, five.Devices, one.SDKs, five.SDKs)
	}
	if len(cohortWarnings(one, five)) != 0 {
		t.Fatalf("rotation fabricates cohort mismatch: %v", cohortWarnings(one, five))
	}
}

func BenchmarkAcquisitionInspection(b *testing.B) {
	for _, tc := range []struct {
		name           string
		runs, segments int
	}{{"one", 1, 1}, {"rotated5", 1, 5}, {"independent10", 10, 1}} {
		b.Run(tc.name, func(b *testing.B) {
			var paths []string
			for run := 1; run <= tc.runs; run++ {
				paths = append(paths, writeAcquisitionRotation(b, byte(run), tc.segments, "device")...)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				summary, err := InspectFilesWithOptions("benchmark", paths, Options{})
				if err != nil {
					b.Fatal(err)
				}
				if summary.HTTPCount != tc.runs*1000 {
					b.Fatal("lost observations")
				}
			}
		})
	}
}
