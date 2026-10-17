package analyze

// A shared bound covers all four primitive histories before their secondary
// interval indexes are built. It is independent of lifecycle admission.
const workerCorrelationEventLimit = 100000

func (c *collector) retainWorkerCorrelation() bool {
	if len(c.workerHTTP)+len(c.workerIO)+len(c.workerMemory)+len(c.workerMetrics) >= workerCorrelationEventLimit {
		c.workerCorrelationDropped = saturatingUint64Sum(c.workerCorrelationDropped, 1)
		return false
	}
	return true
}
func (c *collector) recordWorkerMemory(point workerPoint) {
	if c.retainWorkerCorrelation() {
		c.workerMemory = append(c.workerMemory, point)
	}
}
