package jhlog

import (
	"fmt"
	"math"
)

type WorkSubmissionPolicy uint64

const (
	WorkSubmissionNone WorkSubmissionPolicy = iota
	WorkSubmissionKeep
	WorkSubmissionReplace
	WorkSubmissionAppend
	WorkSubmissionAppendOrReplace
	WorkSubmissionUpdate
	WorkSubmissionCancelAndReenqueue
)
const (
	submissionUniqueFlag          uint64 = 1
	submissionPeriodicFlag        uint64 = 2
	submissionNameTruncatedFlag   uint64 = 4
	submissionSourceTruncatedFlag uint64 = 8
	submissionKnownFlags                 = submissionUniqueFlag | submissionPeriodicFlag | submissionNameTruncatedFlag
)

// An enqueue call returned an Operation. Neither acceptance nor a schedule is implied.
type WorkSubmissionEvent struct {
	CallerSource                                                string
	SourceTruncated                                             bool
	NameTruncated                                               bool
	Version, UUIDMost, UUIDLeast                                uint64
	ObservedElapsedNanos                                        uint64 `json:",string"`
	Periodic, HasUnique                                         bool
	UniqueName                                                  string
	Policy                                                      WorkSubmissionPolicy
	OmittedRequests                                             uint64
	BackoffPolicy                                               uint64
	RequestedBackoffMS                                          uint64 `json:",string"`
	QuotaPolicy                                                 uint64
	RequestedPeriodMS, RequestedFlexMS, RequestedInitialDelayMS *uint64
}

func (v *WorkSubmissionEvent) UUID() string {
	return (WorkerIdentityEvent{UUIDMost: v.UUIDMost, UUIDLeast: v.UUIDLeast}).UUID()
}
func validWorkSubmission(v *WorkSubmissionEvent) bool {
	if v == nil || (v.Version < 1 || v.Version > 4) || len(v.UniqueName) > 512 || len(v.CallerSource) > 512 || (v.NameTruncated && !v.HasUnique) {
		return false
	}
	if v.Version == 1 && (v.CallerSource != "" || v.SourceTruncated) || v.Version >= 2 && v.CallerSource == "" {
		return false
	}
	if v.BackoffPolicy > 2 || v.QuotaPolicy > 2 || (v.BackoffPolicy == 0 && v.RequestedBackoffMS != 0) ||
		(v.Version < 3 && (v.BackoffPolicy != 0 || v.RequestedBackoffMS != 0 || v.QuotaPolicy != 0)) {
		return false
	}
	if !validScheduleHints(v) {
		return false
	}
	if !v.HasUnique {
		return v.UniqueName == "" && v.Policy == WorkSubmissionNone
	}
	if v.Periodic {
		return v.Policy == WorkSubmissionKeep || v.Policy == WorkSubmissionReplace || v.Policy == WorkSubmissionUpdate || v.Policy == WorkSubmissionCancelAndReenqueue
	}
	return v.Policy >= WorkSubmissionKeep && v.Policy <= WorkSubmissionAppendOrReplace
}
func (e eventPayloadEncoder) encodeWorkSubmission(v *WorkSubmissionEvent) error {
	if !validWorkSubmission(v) {
		return fmt.Errorf("invalid work submission intent")
	}
	var flags uint64
	if v.SourceTruncated {
		flags |= submissionSourceTruncatedFlag
	}
	if v.NameTruncated {
		flags |= submissionNameTruncatedFlag
	}
	if v.HasUnique {
		flags |= submissionUniqueFlag
	}
	if v.Periodic {
		flags |= submissionPeriodicFlag
	}
	if err := e.writeValues(v.Version, v.UUIDMost, v.UUIDLeast, v.ObservedElapsedNanos, flags, v.OmittedRequests, uint64(v.Policy)); err != nil {
		return err
	}
	if v.HasUnique {
		if err := e.threadString(v.UniqueName); err != nil {
			return err
		}
	}
	if v.Version >= 2 {
		if err := e.threadString(v.CallerSource); err != nil {
			return err
		}
	}
	if v.Version >= 3 {
		if err := e.writeValues(v.BackoffPolicy, v.RequestedBackoffMS, v.QuotaPolicy); err != nil {
			return err
		}
	}
	if v.Version == 4 {
		fields := []*uint64{v.RequestedPeriodMS, v.RequestedFlexMS, v.RequestedInitialDelayMS}
		var mask uint64
		for i, f := range fields {
			if f != nil {
				mask |= 1 << i
			}
		}
		if err := e.writeValues(mask); err != nil {
			return err
		}
		for _, f := range fields {
			if f != nil {
				if err := e.writeValues(*f); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func decodeWorkSubmission(r *recordReader, event *Event) error {
	var scratch [7]uint64
	v, err := readPayloadValues(r, scratch[:], "schema", "UUID most", "UUID least", "observed nanos", "submission flags", "omitted requests", "policy")
	if err != nil {
		return err
	}
	knownFlags := submissionKnownFlags
	if v[0] >= 2 {
		knownFlags |= submissionSourceTruncatedFlag
	}
	if v[4]&^knownFlags != 0 {
		return fmt.Errorf("unknown work submission flags")
	}
	p := &WorkSubmissionEvent{SourceTruncated: v[4]&submissionSourceTruncatedFlag != 0, NameTruncated: v[4]&submissionNameTruncatedFlag != 0, Version: v[0], UUIDMost: v[1], UUIDLeast: v[2], ObservedElapsedNanos: v[3], HasUnique: v[4]&submissionUniqueFlag != 0, Periodic: v[4]&submissionPeriodicFlag != 0, OmittedRequests: v[5], Policy: WorkSubmissionPolicy(v[6])}
	if p.HasUnique {
		p.UniqueName, err = readThreadString(r, 512)
		if err != nil {
			return err
		}
	}
	if p.Version >= 2 {
		p.CallerSource, err = readThreadString(r, 512)
		if err != nil {
			return err
		}
	}
	if p.Version >= 3 {
		var hints [3]uint64
		values, err := readPayloadValues(r, hints[:], "backoff policy", "requested backoff ms", "quota policy")
		if err != nil {
			return err
		}
		p.BackoffPolicy, p.RequestedBackoffMS, p.QuotaPolicy = values[0], values[1], values[2]
	}
	if p.Version == 4 {
		var maskBuf [1]uint64
		values, err := readPayloadValues(r, maskBuf[:], "schedule mask")
		if err != nil {
			return err
		}
		mask := values[0]
		if mask == 0 || mask&^7 != 0 {
			return fmt.Errorf("invalid schedule mask")
		}
		fields := []**uint64{&p.RequestedPeriodMS, &p.RequestedFlexMS, &p.RequestedInitialDelayMS}
		for i, f := range fields {
			if mask&(1<<i) != 0 {
				var buf [1]uint64
				v, err := readPayloadValues(r, buf[:], "requested schedule ms")
				if err != nil {
					return err
				}
				n := v[0]
				*f = &n
			}
		}
	}
	if !validWorkSubmission(p) {
		return fmt.Errorf("invalid work submission intent")
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkSubmission: p}
	return nil
}

func validScheduleHints(v *WorkSubmissionEvent) bool {
	has := v.RequestedPeriodMS != nil || v.RequestedFlexMS != nil || v.RequestedInitialDelayMS != nil
	if has != (v.Version == 4) {
		return false
	}
	if (v.RequestedPeriodMS != nil || v.RequestedFlexMS != nil) && !v.Periodic {
		return false
	}
	if v.RequestedFlexMS != nil && v.RequestedPeriodMS == nil {
		return false
	}
	for _, n := range []*uint64{v.RequestedPeriodMS, v.RequestedFlexMS, v.RequestedInitialDelayMS} {
		if n != nil && *n > math.MaxInt64 {
			return false
		}
	}
	return true
}
