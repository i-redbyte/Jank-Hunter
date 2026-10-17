package analyze

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestWorkerCorrelationSharedBudgetDoesNotDropLifecycle(t *testing.T) {
	c := newCollector("bounded", 1, Options{})
	c.workerHTTP = make([]workerCostInterval, 100000)
	c.workerCorrelationOn = true
	c.recordWorkerIO(jhlog.Event{IO: &jhlog.IOEvent{DurationUS: 1000}})
	c.recordWorkerMetric(workerMetricDeviceCPU, jhlog.Event{Metric: &jhlog.MetricEvent{Value: 100}})
	c.add(nil, jhlog.Event{Type: jhlog.EventMemory, Memory: &jhlog.MemoryEvent{PSSKB: 100}})
	if len(c.workerIO) != 0 || len(c.workerMetrics) != 0 || len(c.workerMemory) != 0 {
		t.Fatal("correlation arrays exceed shared budget")
	}
	if c.workerCorrelationDropped != 3 {
		t.Fatalf("loss invisible: %d", c.workerCorrelationDropped)
	}
	c.workerEvents = []workerRawEvent{{stage: jhlog.WorkerStageStarted, timeMS: 10, instanceID: 1, worker: "app.Work"}, {stage: jhlog.WorkerStageFinished, timeMS: 20, instanceID: 1, worker: "app.Work", outcome: jhlog.WorkerOutcomeSuccess}}
	a := c.finalizeWorkerAnalysis()
	if a.Started != 1 || a.Success != 1 || a.DroppedCorrelationEvents != 3 {
		t.Fatalf("budget confused lifecycle and correlation: %+v", a)
	}
	c.workerCollectorState.release()
	if c.workerCorrelationDropped != 0 {
		t.Fatal("loss count leaked across release")
	}
}
func TestWorkerCorrelationPriorMemoryUsesSameBudget(t *testing.T) {
	c := &collector{}
	c.workerMetrics = make([]workerMetricPoint, 100000)
	c.workerPriorMemorySet = true
	c.enableWorkerCorrelation()
	if len(c.workerMemory) != 0 || c.workerCorrelationDropped != 1 || c.workerPriorMemorySet {
		t.Fatal("prior sample bypassed budget")
	}
}

func BenchmarkWorkerCorrelationFullBudgetAdmission(b *testing.B) {
	c := &collector{}
	c.workerHTTP = make([]workerCostInterval, workerCorrelationEventLimit)
	event := jhlog.Event{IO: &jhlog.IOEvent{DurationUS: 1000}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.recordWorkerIO(event)
	}
}
