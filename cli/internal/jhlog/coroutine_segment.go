package jhlog

import "fmt"

// CoroutineSegmentEvent is one observed resume interval, not exclusive thread ownership.
type CoroutineSegmentEvent struct {
	Version, Token, Generation       uint64
	JavaThreadID, ExitJavaThreadID   uint64
	StartedElapsedMS, EndedElapsedMS uint64 `json:",string"`
	State                            uint64
	EntryTID, ExitTID                uint64
	Owner                            string
}

func validCoroutineSegment(v *CoroutineSegmentEvent) bool {
	if v == nil || v.Token == 0 || v.JavaThreadID == 0 || v.EndedElapsedMS < v.StartedElapsedMS ||
		len(v.Owner) == 0 || len(v.Owner) > 512 {
		return false
	}
	if v.Version == 3 {
		return v.State == 4 && v.EntryTID != 0 && v.ExitJavaThreadID == 0 && v.ExitTID == 0
	}
	return (v.Version == 1 || v.Version == 2) && v.State <= 3 && v.ExitJavaThreadID != 0 &&
		(v.Version != 2 || v.EntryTID != 0 && v.ExitTID != 0)
}

func (e eventPayloadEncoder) encodeCoroutineSegment(v *CoroutineSegmentEvent) error {
	if !validCoroutineSegment(v) {
		return fmt.Errorf("invalid coroutine segment")
	}
	if err := e.writeValues(v.Version, v.Token, v.Generation, v.JavaThreadID, v.ExitJavaThreadID,
		v.StartedElapsedMS, v.EndedElapsedMS, v.State); err != nil {
		return err
	}
	if v.Version >= 2 {
		if err := e.writeValues(v.EntryTID, v.ExitTID); err != nil {
			return err
		}
	}
	return e.threadString(v.Owner)
}

func decodeCoroutineSegment(r *recordReader, event *Event) error {
	var scratch [8]uint64
	v, err := readPayloadValues(r, scratch[:], "schema", "token", "generation", "entry Java ID", "exit Java ID",
		"start elapsed ms", "end elapsed ms", "state")
	if err != nil {
		return err
	}
	var entryTID, exitTID uint64
	if v[0] == 2 || v[0] == 3 {
		var tidScratch [2]uint64
		tids, tidErr := readPayloadValues(r, tidScratch[:], "entry Linux TID", "exit Linux TID")
		if tidErr != nil {
			return tidErr
		}
		entryTID, exitTID = tids[0], tids[1]
	}
	owner, err := readThreadString(r, 512)
	if err != nil {
		return err
	}
	p := &CoroutineSegmentEvent{Version: v[0], Token: v[1], Generation: v[2], JavaThreadID: v[3],
		ExitJavaThreadID: v[4], StartedElapsedMS: v[5], EndedElapsedMS: v[6], State: v[7],
		EntryTID: entryTID, ExitTID: exitTID, Owner: owner}
	if !validCoroutineSegment(p) {
		return fmt.Errorf("invalid coroutine segment")
	}
	event.DiagnosticEvent = &DiagnosticEvent{CoroutineSegment: p}
	return nil
}
