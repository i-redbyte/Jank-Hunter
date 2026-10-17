package report

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkSchedulesExplainSourcesAndEscapeUntrustedNames(t *testing.T) {
	period, flex := uint64(1800000), uint64(300000)
	s := analyze.Summary{WorkSubmissions: &analyze.WorkSubmissionAnalysis{Observations: []analyze.WorkSubmissionObservation{{UUID: "id", Scope: "s", Submission: &jhlog.WorkSubmissionEvent{Version: 4, Periodic: true, HasUnique: true, UniqueName: "</script><img src=x onerror=alert(1)>", CallerSource: "app.schedule", RequestedPeriodMS: &period, RequestedFlexMS: &flex}}}}}
	path := filepath.Join(t.TempDir(), "schedule.html")
	if err := WriteInspectWithOptions(path, s, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "data-work-schedules", "Что запросило приложение", "Окно выбора запуска (flex)", "Откуда взялось окно", "data-schedule-zone")
	payload, err := workSchedulesJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "</script>") {
		t.Fatal("schedule metadata escaped JSON script")
	}
}

func TestWorkScheduleFullEvidencePreview(t *testing.T) {
	n := func(v uint64) *uint64 { return &v }
	when := uint64(time.Date(2026, time.October, 16, 13, 0, 0, 0, time.FixedZone("MSK", 3*3600)).UnixMilli())
	eligible := int64(when + 7200000)
	s := analyze.Summary{Title: "Демонстрация расписаний - искусственные данные", DurationMS: 7200000,
		WorkSubmissions: &analyze.WorkSubmissionAnalysis{}, WorkRegistry: &analyze.WorkRegistryAnalysis{}, WorkerAnalysis: &analyze.WorkerAnalysis{Instances: 2, Executions: 8, Started: 8, Finished: 8, Success: 8, PeriodicInstances: 2, RunSamples: 8, RunP50MS: 20000, RunP95MS: 20000, RunMaxMS: 20000, TotalRunMS: 160000, MaxRunning: 1}}
	for i, name := range []string{"DEMO_ConfigWorker", "DEMO_TokenWorker"} {
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
		s.WorkSubmissions.Observations = append(s.WorkSubmissions.Observations, analyze.WorkSubmissionObservation{UUID: id, Scope: "demo-session", Process: "example.app", TimeBasis: "observation_clock", TimeMS: when,
			Submission: &jhlog.WorkSubmissionEvent{Version: 4, ObservedElapsedNanos: 1000000000, Periodic: true, HasUnique: true, UniqueName: name, CallerSource: "example.Scheduler.schedule", RequestedPeriodMS: n(1800000), RequestedFlexMS: n(300000), RequestedInitialDelayMS: n(600000)},
		})
		period := uint64(1800000)
		if i == 0 {
			period = 3600000
		}
		s.WorkRegistry.Snapshots = append(s.WorkRegistry.Snapshots, analyze.WorkRegistrySnapshot{Scope: "demo-session", Process: "example.app", Complete: true, ObservedElapsedMS: 7100000, ObservedUnixMS: n(when + 7100000), AgeAtCaptureMS: n(10000), IntervalMS: 30000, Jobs: []jhlog.WorkRegistryJob{{UUID: id, Generation: 1, State: 0, InitialDelayMS: 600000, PeriodMS: &period, FlexMS: n(300000), NetworkType: "CONNECTED", EarliestEligibleUnixMS: &eligible}}})
		for j := uint64(0); j < 4; j++ {
			at := uint64(600000) + j*1800000 + uint64(i)*30000
			s.WorkerAnalysis.Attempts = append(s.WorkerAnalysis.Attempts, analyze.WorkerAttempt{UUID: id, Scope: "demo-session", Process: "example.app", Worker: "example." + name, Generation: 1, StartMS: n(at), EndMS: n(at + 20000), StartUnixMS: n(when + at), EndUnixMS: n(when + at + 20000), DurationMS: n(20000), Outcome: "success", Coverage: "start_and_finish_observed"})
		}
	}
	// A single complete query observes both jobs; do not invent duplicate snapshot IDs.
	snapshot := s.WorkRegistry.Snapshots[0]
	snapshot.SnapshotID = 1
	snapshot.Jobs = append(snapshot.Jobs, s.WorkRegistry.Snapshots[1].Jobs...)
	snapshot.TotalObserved, snapshot.RetainedJobs = 2, 2
	snapshot.ExpectedPackets, snapshot.ReceivedPackets = 1, 1
	s.WorkRegistry.Snapshots = []analyze.WorkRegistrySnapshot{snapshot}
	path := filepath.Join(t.TempDir(), "demo.html")
	if preview := os.Getenv("JH_SCHEDULE_PREVIEW_HTML"); preview != "" {
		path = preview
	}
	if err := WriteWorkerInspectWithOptions(path, s, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	assertHTMLContains(t, path, "DEMO_ConfigWorker", "data-work-schedules", "GenerationCadences", "ParametersDiffer")
}
