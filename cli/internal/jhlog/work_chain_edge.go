package jhlog

import "fmt"

// A then() call declared this edge. It does not prove that enqueue accepted either request.
type WorkChainEdgeEvent struct {
	Version, ParentMost, ParentLeast uint64
	ChildMost, ChildLeast            uint64
	ObservedElapsedNanos             uint64 `json:",string"`
	CallerSource                     string
	SourceTruncated                  bool
}

func (v *WorkChainEdgeEvent) ParentUUID() string {
	return (WorkerIdentityEvent{UUIDMost: v.ParentMost, UUIDLeast: v.ParentLeast}).UUID()
}

func (v *WorkChainEdgeEvent) ChildUUID() string {
	return (WorkerIdentityEvent{UUIDMost: v.ChildMost, UUIDLeast: v.ChildLeast}).UUID()
}

func validWorkChainEdge(v *WorkChainEdgeEvent) bool {
	return v != nil && v.Version == 1 && len(v.CallerSource) > 0 && len(v.CallerSource) <= 512
}

func (e eventPayloadEncoder) encodeWorkChainEdge(v *WorkChainEdgeEvent) error {
	if !validWorkChainEdge(v) {
		return fmt.Errorf("invalid work chain edge")
	}
	var flags uint64
	if v.SourceTruncated {
		flags = 1
	}
	if err := e.writeValues(v.Version, v.ParentMost, v.ParentLeast, v.ChildMost, v.ChildLeast, v.ObservedElapsedNanos, flags); err != nil {
		return err
	}
	return e.threadString(v.CallerSource)
}

func decodeWorkChainEdge(r *recordReader, event *Event) error {
	var scratch [7]uint64
	v, err := readPayloadValues(r, scratch[:], "schema", "parent UUID most", "parent UUID least", "child UUID most", "child UUID least", "observed nanos", "flags")
	if err != nil {
		return err
	}
	if v[6]&^uint64(1) != 0 {
		return fmt.Errorf("unknown work chain flags")
	}
	source, err := readThreadString(r, 512)
	if err != nil {
		return err
	}
	p := &WorkChainEdgeEvent{Version: v[0], ParentMost: v[1], ParentLeast: v[2], ChildMost: v[3], ChildLeast: v[4], ObservedElapsedNanos: v[5], CallerSource: source, SourceTruncated: v[6]&1 != 0}
	if !validWorkChainEdge(p) {
		return fmt.Errorf("invalid work chain edge")
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkChainEdge: p}
	return nil
}
