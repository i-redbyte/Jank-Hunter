package analyze

import (
	"encoding/hex"
	"strconv"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const workerIdentityLimit = 50000

type workerIdentityKey struct{ log, instance uint64 }
type workerIdentityValue struct {
	most, least uint64
	ambiguous   bool
}
type workerIdentityState struct {
	bindings map[workerIdentityKey]workerIdentityValue
	dropped  uint64
}

func (c *collector) recordWorkerIdentity(event jhlog.Event) {
	s := &c.workerIdentity
	if s.bindings == nil {
		s.bindings = make(map[workerIdentityKey]workerIdentityValue)
	}
	p := event.WorkerIdentity
	key := workerIdentityKey{c.currentLogIndex, p.InstanceID}
	previous, exists := s.bindings[key]
	if exists {
		c.workerSeenInLog = true
		previous.ambiguous = previous.ambiguous || previous.most != p.UUIDMost || previous.least != p.UUIDLeast
		s.bindings[key] = previous
	} else if len(s.bindings) < workerIdentityLimit {
		c.workerSeenInLog = true
		s.bindings[key] = workerIdentityValue{most: p.UUIDMost, least: p.UUIDLeast}
	} else {
		s.dropped++
	}
}

type scopedWorkerIdentity struct{ scope, instance string }

func (c *collector) bindWorkerAttempts(analysis *WorkerAnalysis) {
	identities := make(map[scopedWorkerIdentity]workerIdentityValue, len(c.workerIdentity.bindings))
	for key, value := range c.workerIdentity.bindings {
		scoped := scopedWorkerIdentity{hex.EncodeToString([]byte(c.workerScope(key.log))), strconv.FormatUint(key.instance, 10)}
		if previous, ok := identities[scoped]; ok {
			value.ambiguous = value.ambiguous || previous.ambiguous || previous.most != value.most || previous.least != value.least
		}
		identities[scoped] = value
	}
	for i := range analysis.Attempts {
		a := &analysis.Attempts[i]
		if value, ok := identities[scopedWorkerIdentity{a.Scope, a.InstanceID}]; ok {
			a.IdentityAmbiguous = value.ambiguous
			if !value.ambiguous {
				a.UUID = (jhlog.WorkerIdentityEvent{UUIDMost: value.most, UUIDLeast: value.least}).UUID()
			}
		}
	}
	analysis.DroppedIdentityBindings = c.workerIdentity.dropped
}
