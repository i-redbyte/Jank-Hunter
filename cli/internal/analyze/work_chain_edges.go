package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

// Edges are declarations seen through public API calls, not a snapshot of WorkManager's database.
type WorkChainAnalysis struct {
	Edges          []WorkChainEdge
	Roots          []WorkChainRoot
	CapturedEdges  uint64
	CapturedRoots  uint64
	DroppedEdges   uint64
	DroppedRoots   uint64
	HistoryOmitted bool
}

type WorkChainEdge struct {
	ParentUUID, ChildUUID, Scope, Process, TimeBasis string
	TimeMS                                           uint64
	Edge                                             *jhlog.WorkChainEdgeEvent
}

type WorkChainRoot struct {
	UUID, Scope, Process, TimeBasis string
	TimeMS                          uint64
	Root                            *jhlog.WorkSubmissionEvent
}

type workChainState struct {
	edges         []WorkChainEdge
	roots         []WorkChainRoot
	retainedBytes int
	dropped       uint64
	droppedRoots  uint64
}

func (c *collector) recordWorkChainEdge(event jhlog.Event) {
	s := &c.workChainEdges
	p := event.WorkChainEdge
	estimate := 256 + len(p.CallerSource) + len(c.currentProcess) + len(c.workRegistry.scope)
	if len(s.edges)+len(s.roots) >= 50000 || s.retainedBytes+estimate > 16<<20 {
		s.dropped = saturatingUint64Sum(s.dropped, 1)
		return
	}
	s.retainedBytes += estimate
	v := WorkChainEdge{ParentUUID: p.ParentUUID(), ChildUUID: p.ChildUUID(), Scope: c.workRegistry.scope,
		Process: c.currentProcess, TimeBasis: "record_time", TimeMS: event.TimeMS, Edge: p}
	if at := observationUnixMS(c.workRegistry.anchorElapsedMS, c.workRegistry.anchorUnixMS, p.ObservedElapsedNanos/1000000); at != nil {
		v.TimeMS = *at
		v.TimeBasis = "observation_clock"
	}
	s.edges = append(s.edges, v)
}

func (c *collector) recordWorkChainRoot(event jhlog.Event) {
	s := &c.workChainEdges
	p := event.WorkChainRoot
	estimate := 256 + len(p.UniqueName) + len(p.CallerSource) + len(c.currentProcess) + len(c.workRegistry.scope)
	if len(s.edges)+len(s.roots) >= 50000 || s.retainedBytes+estimate > 16<<20 {
		s.droppedRoots = saturatingUint64Sum(s.droppedRoots, 1)
		return
	}
	s.retainedBytes += estimate
	v := WorkChainRoot{UUID: p.UUID(), Scope: c.workRegistry.scope, Process: c.currentProcess,
		TimeBasis: "record_time", TimeMS: event.TimeMS, Root: p}
	if at := observationUnixMS(c.workRegistry.anchorElapsedMS, c.workRegistry.anchorUnixMS, p.ObservedElapsedNanos/1000000); at != nil {
		v.TimeMS = *at
		v.TimeBasis = "observation_clock"
	}
	s.roots = append(s.roots, v)
}

func (s *workChainState) finalize() *WorkChainAnalysis {
	if len(s.edges)+len(s.roots) == 0 && s.dropped == 0 && s.droppedRoots == 0 {
		return nil
	}
	return &WorkChainAnalysis{Edges: s.edges, Roots: s.roots, CapturedEdges: uint64(len(s.edges)),
		CapturedRoots: uint64(len(s.roots)), DroppedEdges: s.dropped, DroppedRoots: s.droppedRoots}
}
