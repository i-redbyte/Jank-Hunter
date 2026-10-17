package analyze

import (
	"sort"
	"strconv"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	workerProgressLimit      = 50000
	workerProgressBytesLimit = 16 << 20
	workerProgressPerAttempt = 32
)

func (c *collector) recordWorkerProgress(event jhlog.Event) {
	value := event.WorkerProgress
	if value == nil {
		return
	}
	c.workerSeenInLog = true
	c.markCohort()
	estimate := 128 + len(c.currentProcess)
	for _, field := range value.Fields {
		estimate += len(field.Key) + len(field.Type) + 32
	}
	if len(c.workerProgress) >= workerProgressLimit || c.workerProgressBytes+estimate > workerProgressBytesLimit {
		c.workerProgressDropped++
		return
	}
	c.workerProgressBytes += estimate
	c.workerProgress = append(c.workerProgress, workerRawProgress{
		logIndex: c.currentLogIndex, process: c.currentProcess, timeMS: value.ObservedElapsedNanos / 1_000_000,
		instanceID: value.InstanceID, runAttempt: uint32(value.RunAttempt), generation: uint32(value.Generation),
		stage: value.Stage, omittedUpdates: value.OmittedUpdates, omittedKeys: value.OmittedKeys,
		truncatedFields: value.TruncatedFields, fields: value.Fields,
	})
}

type progressIndexPoint struct {
	time  uint64
	index int
	xor   int
}
type progressIntervalIndex struct{ starts, ends []progressIndexPoint }

func (index *progressIntervalIndex) prepare() {
	sort.Slice(index.starts, func(i, j int) bool { return index.starts[i].time < index.starts[j].time })
	sort.Slice(index.ends, func(i, j int) bool { return index.ends[i].time < index.ends[j].time })
	xor := 0
	for i := range index.starts {
		xor ^= index.starts[i].index
		index.starts[i].xor = xor
	}
	xor = 0
	for i := range index.ends {
		xor ^= index.ends[i].index
		index.ends[i].xor = xor
	}
}

func (index *progressIntervalIndex) uniqueAt(at uint64) (int, bool) {
	starts := sort.Search(len(index.starts), func(i int) bool { return index.starts[i].time > at })
	ends := sort.Search(len(index.ends), func(i int) bool { return index.ends[i].time >= at })
	if starts-ends != 1 {
		return 0, false
	}
	result := index.starts[starts-1].xor
	if ends > 0 {
		result ^= index.ends[ends-1].xor
	}
	return result, true
}

func workerAttemptIntervals(attempts []WorkerAttempt, allowMissingStart bool) map[attemptJoinKey]*progressIntervalIndex {
	indices := make(map[attemptJoinKey]*progressIntervalIndex)
	for i, attempt := range attempts {
		if (attempt.StartMS == nil && (!allowMissingStart || attempt.EndMS == nil)) ||
			(attempt.StartMS != nil && attempt.EndMS != nil && *attempt.EndMS < *attempt.StartMS) {
			continue
		}
		instance, err := strconv.ParseUint(attempt.InstanceID, 10, 64)
		if err != nil {
			continue
		}
		key := attemptJoinKey{attempt.Scope, instance, attempt.RunAttempt, attempt.Generation}
		index := indices[key]
		if index == nil {
			index = &progressIntervalIndex{}
			indices[key] = index
		}
		start := uint64(0)
		if attempt.StartMS != nil {
			start = *attempt.StartMS
		}
		index.starts = append(index.starts, progressIndexPoint{time: start, index: i})
		if attempt.EndMS != nil {
			index.ends = append(index.ends, progressIndexPoint{time: *attempt.EndMS, index: i})
		}
	}
	for _, index := range indices {
		index.prepare()
	}
	return indices
}

// Only a unique active interval with an observed start can own a direct request.
func attachWorkerProgress(attempts []WorkerAttempt, progress []workerRawProgress) uint64 {
	indices := workerAttemptIntervals(attempts, false)
	var unmatched uint64
	for _, value := range progress {
		key := attemptJoinKey{value.scope, value.instanceID, value.runAttempt, value.generation}
		index := indices[key]
		if index == nil {
			unmatched++
			continue
		}
		position, unique := index.uniqueAt(value.timeMS)
		if !unique {
			unmatched++
			continue
		}
		attempt := &attempts[position]
		if value.stage == 2 {
			if value.omittedUpdates > attempt.ProgressOmittedUpdates {
				attempt.ProgressOmittedUpdates = value.omittedUpdates
			}
			continue
		}
		if len(attempt.ProgressRequests) >= workerProgressPerAttempt {
			unmatched++
			continue
		}
		attempt.ProgressRequests = append(attempt.ProgressRequests, WorkerProgressRequest{
			ObservedMS: value.timeMS, Fields: value.fields, OmittedKeys: value.omittedKeys,
			TruncatedFields: value.truncatedFields,
		})
	}
	return unmatched
}
