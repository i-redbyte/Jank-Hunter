package analyze

import (
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type WorkRegistryAnalysis struct {
	HistoryOmitted bool
	Snapshots      []WorkRegistrySnapshot
	DroppedPackets uint64
}
type WorkRegistrySnapshot struct {
	DataMetadataOmitted                                       bool
	Scope, Process                                            string
	SnapshotID, QueryStartedMS, ObservedElapsedMS, IntervalMS uint64
	ObservedUnixMS                                            *uint64
	AgeAtCaptureMS                                            *uint64
	RecordedTimeMS                                            uint64
	TotalObserved, RetainedJobs, Unavailable, ExpectedPackets uint64
	ReceivedPackets                                           int
	Complete                                                  bool
	CoverageReason                                            string
	Jobs                                                      []jhlog.WorkRegistryJob
}
type registryRawPacket struct {
	scope, process string
	unix           *uint64
	recorded       uint64
	packet         *jhlog.WorkRegistryEvent
}
type workRegistryCollectorState struct {
	windowEnds                    map[string]uint64
	scope, process                string
	anchorElapsedMS, anchorUnixMS uint64
	packets                       []registryRawPacket
	retainedRows, retainedBytes   int
	dropped                       uint64
}

func (c *collector) startDiagnosticSegment(header jhlog.SegmentHeader) {
	s := &c.workRegistry
	s.scope = hex.EncodeToString([]byte(workerStreamIdentity(jhlog.StreamResult{Header: header})))
	if header.RunID == (jhlog.ID128{}) && header.ProcessInstanceID == (jhlog.ID128{}) && header.SessionID == (jhlog.ID128{}) {
		s.scope = fmt.Sprintf("log:%d", c.currentLogIndex)
	}
	s.process = header.ProcessName
	s.anchorElapsedMS = header.SegmentStartElapsedUS / 1000
	s.anchorUnixMS = header.SegmentStartUnixMS
}

func (c *collector) recordWorkRegistry(event jhlog.Event) {
	s := &c.workRegistry
	p := event.WorkRegistry
	estimate := registryRetainedBytes(p)
	if len(s.packets) >= 12500 || s.retainedRows+len(p.Jobs) > 100000 || s.retainedBytes+estimate > 32<<20 {
		s.dropped++
		return
	}
	s.retainedRows += len(p.Jobs)
	s.retainedBytes += estimate
	process := s.process
	if process == "" {
		process = c.currentProcess
	}
	s.packets = append(s.packets, registryRawPacket{s.scope, process, observationUnixMS(s.anchorElapsedMS, s.anchorUnixMS, p.ObservedMS), event.TimeMS, p})
}

func observationUnixMS(anchorElapsed, anchorUnix, observed uint64) *uint64 {
	if anchorUnix == 0 {
		return nil
	}
	var value uint64
	if observed >= anchorElapsed {
		delta := observed - anchorElapsed
		if anchorUnix > math.MaxUint64-delta {
			return nil
		}
		value = anchorUnix + delta
	} else {
		delta := anchorElapsed - observed
		if delta > anchorUnix {
			return nil
		}
		value = anchorUnix - delta
	}
	return &value
}

func registryRetainedBytes(p *jhlog.WorkRegistryEvent) int {
	// Conservative allowance for structs, pointers and slice backing arrays, plus string contents.
	size := 256
	for _, r := range p.Jobs {
		size += 512 + len(r.UUID) + len(r.NetworkType)
		for _, v := range r.Tags {
			size += 64 + len(v)
		}
		for _, v := range r.ContentTriggers {
			size += 64 + len(v)
		}
		for _, fields := range [][]jhlog.WorkDataField{r.Progress, r.Output} {
			for _, f := range fields {
				size += 96 + len(f.Key) + len(f.Type)
			}
		}
	}
	return size
}

type registryAssembly struct {
	snapshot WorkRegistrySnapshot
	first    *jhlog.WorkRegistryEvent
	packets  map[uint64]*jhlog.WorkRegistryEvent
	conflict bool
}

func (s *workRegistryCollectorState) finalize() *WorkRegistryAnalysis {
	if len(s.packets) == 0 && s.dropped == 0 {
		return nil
	}
	groups := map[string]*registryAssembly{}
	for _, raw := range s.packets {
		p := raw.packet
		key := fmt.Sprintf("%s/%d", raw.scope, p.SnapshotID)
		group := groups[key]
		if group == nil {
			group = &registryAssembly{snapshot: WorkRegistrySnapshot{Scope: raw.scope, Process: raw.process, SnapshotID: p.SnapshotID, QueryStartedMS: p.QueryStartedMS, ObservedElapsedMS: p.ObservedMS, IntervalMS: p.IntervalMS,
				ObservedUnixMS: raw.unix, RecordedTimeMS: raw.recorded, TotalObserved: p.TotalObserved, RetainedJobs: p.RetainedJobs, Unavailable: p.Unavailable, ExpectedPackets: p.PacketCount}, first: p, packets: map[uint64]*jhlog.WorkRegistryEvent{}}
			groups[key] = group
		}
		first := group.first
		if p.QueryStartedMS != first.QueryStartedMS || p.ObservedMS != first.ObservedMS || p.IntervalMS != first.IntervalMS || p.TotalObserved != first.TotalObserved || p.RetainedJobs != first.RetainedJobs || p.Unavailable != first.Unavailable || p.PacketCount != first.PacketCount {
			group.conflict = true
		}
		if previous := group.packets[p.PacketIndex]; previous != nil {
			if !reflect.DeepEqual(previous, p) {
				group.conflict = true
			}
		} else {
			group.packets[p.PacketIndex] = p
		}
		if !reflect.DeepEqual(group.snapshot.ObservedUnixMS, raw.unix) {
			group.snapshot.ObservedUnixMS = nil
		}
	}
	result := &WorkRegistryAnalysis{DroppedPackets: s.dropped}
	for _, g := range groups {
		snapshot := g.snapshot
		if end, ok := s.windowEnds[snapshot.Scope]; ok && end >= snapshot.ObservedElapsedMS {
			age := end - snapshot.ObservedElapsedMS
			snapshot.AgeAtCaptureMS = &age
		}
		snapshot.ReceivedPackets = len(g.packets)
		seen := map[string]bool{}
		for i := uint64(0); i < snapshot.ExpectedPackets; i++ {
			if packet := g.packets[i]; packet != nil {
				for _, job := range packet.Jobs {
					if seen[job.UUID] {
						g.conflict = true
						continue
					}
					seen[job.UUID] = true
					snapshot.Jobs = append(snapshot.Jobs, job)
				}
			}
		}
		switch {
		case g.conflict:
			snapshot.CoverageReason = "conflicting_packets"
		case uint64(len(g.packets)) != snapshot.ExpectedPackets:
			snapshot.CoverageReason = "missing_packets"
		case snapshot.Unavailable != 0:
			snapshot.CoverageReason = "source_unavailable"
		case uint64(len(snapshot.Jobs)) != snapshot.RetainedJobs:
			snapshot.CoverageReason = "invalid_rows"
		case snapshot.TotalObserved > snapshot.RetainedJobs:
			snapshot.CoverageReason = "source_limit"
		default:
			snapshot.Complete = true
		}
		result.Snapshots = append(result.Snapshots, snapshot)
	}
	sort.Slice(result.Snapshots, func(i, j int) bool {
		a, b := result.Snapshots[i], result.Snapshots[j]
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.ObservedElapsedMS < b.ObservedElapsedMS
	})
	return result
}

func (s *workRegistryCollectorState) finishSegment(result jhlog.StreamResult) {
	if len(s.packets) == 0 || s.packets[len(s.packets)-1].scope != s.scope || result.LatestQuality == nil {
		return
	}
	end := result.LatestQuality.CapturedElapsedUS / 1000
	if end == 0 {
		var ok bool
		end, ok = result.LatestQuality.Counters[jhlog.QualityCollectionWindowEndElapsedMS]
		if !ok {
			return
		}
	}
	if s.windowEnds == nil {
		s.windowEnds = make(map[string]uint64)
	}
	if end > s.windowEnds[s.scope] {
		s.windowEnds[s.scope] = end
	}
}
