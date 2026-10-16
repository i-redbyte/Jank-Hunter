package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

// Documents retain their source log and process; different process/session inventories are not merged.
type WorkerCatalogAnalysis struct {
	Observations     []WorkerCatalogObservation
	DroppedDocuments uint64
}
type WorkerCatalogObservation struct {
	LogIndex uint64
	Process  string
	TimeMS   uint64
	Catalog  *jhlog.WorkerCatalogEvent
}

func (c *collector) recordWorkerCatalog(event jhlog.Event) {
	if c.workerCatalog == nil {
		c.workerCatalog = &WorkerCatalogAnalysis{}
	}
	if len(c.workerCatalog.Observations) >= 32 {
		c.workerCatalog.DroppedDocuments++
		return
	}
	c.workerCatalog.Observations = append(c.workerCatalog.Observations, WorkerCatalogObservation{c.currentLogIndex, c.currentProcess, event.TimeMS, event.WorkerCatalog})
}
