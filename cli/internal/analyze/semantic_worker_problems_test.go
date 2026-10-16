package analyze

import (
	"strings"
	"testing"
)

func typedWorkerFinding(t *testing.T, worker WorkerStats) *ProblemFinding {
	t.Helper()
	report, err := BuildProblemReport(Summary{DurationMS: 60_000, WorkerAnalysis: &WorkerAnalysis{
		Started: 1, Finished: worker.Finished, Workers: []WorkerStats{worker},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return findingByDetector(report.Problems, "cpu.worker_execution")
}

func TestWorkerStartOnMainThreadDoesNotProveExecutionThere(t *testing.T) {
	finding := typedWorkerFinding(t, WorkerStats{
		Worker: "app.SyncWorker", Instances: 1, Executions: 1, Started: 1, MainThreadStarts: 1,
	})
	if finding == nil || finding.Subcategory != "worker_main_thread" ||
		!strings.Contains(finding.Title, "событие начала") ||
		!strings.Contains(finding.WhatHappened, "это не измерение потока выполнения всей задачи") ||
		!evidenceNamed(finding.Evidence, "События начала на главном потоке") ||
		evidenceNamed(finding.Evidence, "Максимум завершённой попытки") ||
		!warningsContain(finding.Limitations, "только к событию начала") ||
		!strings.Contains(finding.Recommendations[0].Action, "если да") {
		t.Fatalf("start callback was reported as main-thread work: %+v", finding)
	}
}

func TestLongWorkerAttemptIsThresholdSignalWithExpectedDurationUnknown(t *testing.T) {
	finding := typedWorkerFinding(t, WorkerStats{
		Worker: "app.SyncWorker", Instances: 1, Executions: 1, Started: 1, Finished: 1,
		RunSamples: 1, RunMaxMS: 12_000,
	})
	if finding == nil || finding.Subcategory != "worker_long" ||
		!strings.Contains(finding.Title, "порога проверки 10 с") ||
		!evidenceNamed(finding.Evidence, "Максимум завершённой попытки") ||
		!warningsContain(finding.Limitations, "не доказывает дефект") {
		t.Fatalf("duration threshold was reported as proven defect: %+v", finding)
	}
}

func TestIncompleteWorkerLifecycleAndNormalRetriesDoNotCreateApplicationFinding(t *testing.T) {
	for _, worker := range []WorkerStats{
		{Worker: "app.SyncWorker", MissingStart: 1, MissingFinish: 1},
		{Worker: "app.SyncWorker", Retries: 1},
		{Worker: "app.SyncWorker", Cancelled: 1},
	} {
		if finding := typedWorkerFinding(t, worker); finding != nil {
			t.Fatalf("collection gap or routine outcome became an application problem: %+v", finding)
		}
	}
}

func TestAggregateWorkerExecutionsDoNotClaimRetryOfOneJob(t *testing.T) {
	finding := typedWorkerFinding(t, WorkerStats{
		Worker: "app.SyncWorker", Instances: 10, Executions: 10, Started: 10,
		Finished: 10, RunSamples: 10, TotalRunMS: 31_000, RunMaxMS: 3_100,
	})
	if finding == nil || finding.Subcategory != "worker_repeated" ||
		!strings.Contains(finding.Title, "выполнения класса") ||
		!warningsContain(finding.Limitations, "разные UUID одного класса") ||
		strings.Contains(finding.Title, "повторные попытки") {
		t.Fatalf("class aggregate misrepresented as one job's retries: %+v", finding)
	}
}

func TestLegacyWorkerBoundaryTreatsRetryAndCancellationAsOutcomes(t *testing.T) {
	for _, outcome := range []string{"retry", "cancelled"} {
		report, err := BuildProblemReport(Summary{DurationMS: 60_000, RuntimeCalls: []RuntimeCallStats{{
			Caller: "jankhunter.semantic.v1.worker." + outcome + ".background",
			Callee: "app.SyncWorker.doWork", Count: 1, TotalMS: 4_000, MaxMS: 4_000,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if finding := findingByDetector(report.Problems, "cpu.worker_execution"); finding != nil {
			t.Fatalf("routine %s outcome became a problem: %+v", outcome, finding)
		}
	}
}
