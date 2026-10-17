package jhlog

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// WorkerProgressEvent records a WorkManager progress request, not proof of persistence.
// Stage 2 carries the producer's omitted-request count at the attempt boundary.
type WorkerProgressEvent struct {
	Version, Stage, InstanceID, RunAttempt, Generation, ObservedElapsedNanos uint64
	OmittedUpdates, OmittedKeys, TruncatedFields                             uint64
	Fields                                                                   []WorkDataField
}

func validWorkerProgress(v *WorkerProgressEvent) bool {
	if v == nil || v.Version != 1 || v.Stage < 1 || v.Stage > 2 || v.InstanceID == 0 ||
		v.RunAttempt > math.MaxUint32 || v.Generation > math.MaxUint32 ||
		v.OmittedUpdates > math.MaxUint32 || v.OmittedKeys > math.MaxUint32 ||
		v.TruncatedFields > math.MaxUint32 || len(v.Fields) > 8 {
		return false
	}
	if v.Stage == 2 && (len(v.Fields) != 0 || v.OmittedKeys != 0 || v.TruncatedFields != 0) {
		return false
	}
	if v.Stage == 1 && v.OmittedUpdates != 0 {
		return false
	}
	for _, f := range v.Fields {
		if len(f.Key) > 512 || len(f.Type) > 32 || !utf8.ValidString(f.Key) || !utf8.ValidString(f.Type) {
			return false
		}
	}
	return true
}

func (e eventPayloadEncoder) encodeWorkerProgress(v *WorkerProgressEvent) error {
	if !validWorkerProgress(v) {
		return fmt.Errorf("invalid worker progress")
	}
	if err := e.writeValues(v.Version, v.Stage, v.InstanceID, v.RunAttempt, v.Generation,
		v.ObservedElapsedNanos, v.OmittedUpdates, v.OmittedKeys, v.TruncatedFields, uint64(len(v.Fields))); err != nil {
		return err
	}
	for _, f := range v.Fields {
		if err := e.threadString(f.Key); err != nil {
			return err
		}
		if err := e.threadString(f.Type); err != nil {
			return err
		}
		known := uint64(0)
		if f.PayloadBytes != nil {
			known = 1
		}
		if err := e.writeValues(known); err != nil {
			return err
		}
		if known != 0 {
			if err := e.writeValues(*f.PayloadBytes); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeWorkerProgress(r *recordReader, event *Event) error {
	var scratch [9]uint64
	v, err := readPayloadValues(r, scratch[:], "schema", "stage", "instance ID", "attempt", "generation",
		"observed nanos", "omitted updates", "omitted keys", "truncated fields")
	if err != nil {
		return err
	}
	fields, err := decodeWorkData(r)
	if err != nil {
		return err
	}
	p := &WorkerProgressEvent{Version: v[0], Stage: v[1], InstanceID: v[2], RunAttempt: v[3], Generation: v[4],
		ObservedElapsedNanos: v[5], OmittedUpdates: v[6], OmittedKeys: v[7], TruncatedFields: v[8], Fields: fields}
	if !validWorkerProgress(p) {
		return fmt.Errorf("invalid worker progress")
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkerProgress: p}
	return nil
}
