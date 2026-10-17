package jhlog

import (
	"fmt"
	"math"
	"unicode/utf8"
)

// WorkerFailureEvent describes a thrown exception, independently of the attempt's final result.
type WorkerFailureEvent struct {
	Version              uint64
	InstanceID           uint64
	RunAttempt           uint64
	Generation           uint64
	ObservedElapsedNanos uint64 `json:",string"`
	ExceptionClass       string
	Stack                string
	StackTruncated       bool
}

func validWorkerFailure(v *WorkerFailureEvent) bool {
	return v != nil && v.Version == 1 && v.InstanceID != 0 &&
		v.RunAttempt <= math.MaxUint32 && v.Generation <= math.MaxUint32 &&
		len(v.ExceptionClass) > 0 && len(v.ExceptionClass) <= 128 &&
		len(v.Stack) <= 1024 && utf8.ValidString(v.ExceptionClass) && utf8.ValidString(v.Stack)
}

func (e eventPayloadEncoder) encodeWorkerFailure(v *WorkerFailureEvent) error {
	if !validWorkerFailure(v) {
		return fmt.Errorf("invalid worker failure")
	}
	var flags uint64
	if v.StackTruncated {
		flags = 1
	}
	if err := e.writeValues(v.Version, v.InstanceID, v.RunAttempt, v.Generation, v.ObservedElapsedNanos, flags); err != nil {
		return err
	}
	if err := e.threadString(v.ExceptionClass); err != nil {
		return err
	}
	return e.threadString(v.Stack)
}

func decodeWorkerFailure(r *recordReader, event *Event) error {
	var scratch [6]uint64
	v, err := readPayloadValues(r, scratch[:], "schema", "instance ID", "attempt", "generation", "observed nanos", "flags")
	if err != nil {
		return err
	}
	if v[5]&^uint64(1) != 0 {
		return fmt.Errorf("unknown worker failure flags")
	}
	class, err := readThreadString(r, 128)
	if err != nil {
		return err
	}
	stack, err := readThreadString(r, 1024)
	if err != nil {
		return err
	}
	p := &WorkerFailureEvent{Version: v[0], InstanceID: v[1], RunAttempt: v[2], Generation: v[3],
		ObservedElapsedNanos: v[4], ExceptionClass: class, Stack: stack, StackTruncated: v[5]&1 != 0}
	if !validWorkerFailure(p) {
		return fmt.Errorf("invalid worker failure")
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkerFailure: p}
	return nil
}
