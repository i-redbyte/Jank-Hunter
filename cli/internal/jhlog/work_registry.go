package jhlog

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// A snapshot may span bounded packets. Missing packets never describe absent jobs.
type WorkRegistryEvent struct {
	Version, SnapshotID, QueryStartedMS, ObservedMS, IntervalMS        uint64
	PacketIndex, PacketCount, TotalObserved, RetainedJobs, Unavailable uint64
	Jobs                                                               []WorkRegistryJob
}

type WorkRegistryJob struct {
	UUID                                                      string
	InstanceID, State, RunAttempt, Generation, InitialDelayMS uint64
	PeriodMS, FlexMS                                          *uint64
	EarliestEligibleUnixMS, StopReason                        *int64
	Tags                                                      []string
	OmittedTags                                               uint64
	NetworkType                                               string
	Constraints                                               uint64
	ContentTriggers                                           []string
	OmittedContentTriggers                                    uint64
	Progress, Output                                          []WorkDataField
	OmittedProgressKeys, OmittedOutputKeys, TruncatedFields   uint64
}

type WorkDataField struct {
	Key, Type    string
	PayloadBytes *uint64
}

const (
	registryRowsPerPacket    = 8
	registryMaxJobs          = 128
	registryKnownConstraints = 0x3f
	registryHasPeriod        = 1
	registryHasEarliest      = 2
	registryHasStop          = 4
)

func validateWorkRegistry(p *WorkRegistryEvent) error {
	if p == nil || p.Version != 1 || p.SnapshotID == 0 || p.ObservedMS < p.QueryStartedMS || p.IntervalMS == 0 || p.Unavailable > 4 {
		return fmt.Errorf("invalid work registry header")
	}
	if p.RetainedJobs > registryMaxJobs || p.TotalObserved < p.RetainedJobs || p.TotalObserved > math.MaxInt32 {
		return fmt.Errorf("invalid registry counts")
	}
	packets := (p.RetainedJobs + registryRowsPerPacket - 1) / registryRowsPerPacket
	if packets == 0 {
		packets = 1
	}
	if p.PacketCount != packets || p.PacketIndex >= packets {
		return fmt.Errorf("invalid registry packet index/count")
	}
	expected := p.RetainedJobs - p.PacketIndex*registryRowsPerPacket
	if expected > registryRowsPerPacket {
		expected = registryRowsPerPacket
	}
	if uint64(len(p.Jobs)) != expected || p.Unavailable != 0 && (p.TotalObserved != 0 || p.RetainedJobs != 0) {
		return fmt.Errorf("inconsistent registry coverage")
	}
	seen := map[string]bool{}
	for _, r := range p.Jobs {
		if seen[r.UUID] {
			return fmt.Errorf("duplicate registry UUID")
		}
		seen[r.UUID] = true
		if err := validateRegistryJob(r); err != nil {
			return err
		}
	}
	return nil
}

func validateRegistryJob(r WorkRegistryJob) error {
	if len(r.UUID) != 36 || r.UUID[8] != '-' || r.UUID[13] != '-' || r.UUID[18] != '-' || r.UUID[23] != '-' {
		return fmt.Errorf("invalid work UUID")
	}
	uuid, err := hex.DecodeString(strings.ReplaceAll(r.UUID, "-", ""))
	if err != nil || len(uuid) != 16 {
		return fmt.Errorf("invalid work UUID")
	}
	if r.State > 5 || r.RunAttempt > math.MaxInt32 || r.Generation > math.MaxInt32 || r.InitialDelayMS > math.MaxInt64 || r.Constraints & ^uint64(registryKnownConstraints) != 0 {
		return fmt.Errorf("unsupported work state/count/constraints")
	}
	if (r.PeriodMS == nil) != (r.FlexMS == nil) {
		return fmt.Errorf("incomplete periodic configuration")
	}
	if r.PeriodMS != nil && (*r.PeriodMS == 0 || *r.PeriodMS > math.MaxInt64 || *r.FlexMS > *r.PeriodMS) {
		return fmt.Errorf("invalid periodic configuration")
	}
	if r.StopReason != nil && (*r.StopReason < math.MinInt32 || *r.StopReason > math.MaxInt32) {
		return fmt.Errorf("invalid stop reason")
	}
	if r.EarliestEligibleUnixMS != nil && r.State != 0 {
		return fmt.Errorf("earliest eligibility is only known for enqueued work")
	}
	if len(r.Tags) > 8 || len(r.ContentTriggers) > 4 || len(r.Progress) > 8 || len(r.Output) > 8 || len(r.NetworkType) > 64 {
		return fmt.Errorf("registry field limit")
	}
	for _, v := range append(append([]string{}, r.Tags...), r.ContentTriggers...) {
		if len(v) > 512 {
			return fmt.Errorf("registry text limit")
		}
	}
	for _, fields := range [][]WorkDataField{r.Progress, r.Output} {
		for _, f := range fields {
			if len(f.Key) > 512 || len(f.Type) > 32 || f.PayloadBytes != nil && *f.PayloadBytes > math.MaxInt64 {
				return fmt.Errorf("registry Data metadata limit")
			}
		}
	}
	for _, v := range []uint64{r.OmittedTags, r.OmittedContentTriggers, r.OmittedProgressKeys, r.OmittedOutputKeys, r.TruncatedFields} {
		if v > math.MaxInt32 {
			return fmt.Errorf("registry omission count overflow")
		}
	}
	return nil
}

func (e eventPayloadEncoder) encodeWorkRegistry(p *WorkRegistryEvent) error {
	if err := validateWorkRegistry(p); err != nil {
		return err
	}
	if err := e.writeValues(p.Version, p.SnapshotID, p.QueryStartedMS, p.ObservedMS, p.IntervalMS, p.PacketIndex, p.PacketCount, p.TotalObserved, p.RetainedJobs, p.Unavailable, uint64(len(p.Jobs))); err != nil {
		return err
	}
	for _, r := range p.Jobs {
		if err := e.registryJob(r); err != nil {
			return err
		}
	}
	return nil
}

func (e eventPayloadEncoder) registryJob(r WorkRegistryJob) error {
	if err := e.threadString(r.UUID); err != nil {
		return err
	}
	presence := uint64(0)
	if r.PeriodMS != nil {
		presence |= registryHasPeriod
	}
	if r.EarliestEligibleUnixMS != nil {
		presence |= registryHasEarliest
	}
	if r.StopReason != nil {
		presence |= registryHasStop
	}
	if err := e.writeValues(r.InstanceID, r.State, r.RunAttempt, r.Generation, r.InitialDelayMS, presence); err != nil {
		return err
	}
	if r.PeriodMS != nil {
		if err := e.writeValues(*r.PeriodMS, *r.FlexMS); err != nil {
			return err
		}
	}
	for _, v := range []*int64{r.EarliestEligibleUnixMS, r.StopReason} {
		if v != nil {
			if err := e.writeValues(uint64(*v<<1) ^ uint64(*v>>63)); err != nil {
				return err
			}
		}
	}
	if err := e.registryStrings(r.Tags); err != nil {
		return err
	}
	if err := e.writeValues(r.OmittedTags); err != nil {
		return err
	}
	if err := e.threadString(r.NetworkType); err != nil {
		return err
	}
	if err := e.writeValues(r.Constraints); err != nil {
		return err
	}
	if err := e.registryStrings(r.ContentTriggers); err != nil {
		return err
	}
	if err := e.writeValues(r.OmittedContentTriggers); err != nil {
		return err
	}
	for _, fields := range [][]WorkDataField{r.Progress, r.Output} {
		if err := e.writeValues(uint64(len(fields))); err != nil {
			return err
		}
		for _, f := range fields {
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
	}
	return e.writeValues(r.OmittedProgressKeys, r.OmittedOutputKeys, r.TruncatedFields)
}

func (e eventPayloadEncoder) registryStrings(values []string) error {
	if err := e.writeValues(uint64(len(values))); err != nil {
		return err
	}
	for _, v := range values {
		if err := e.threadString(v); err != nil {
			return err
		}
	}
	return nil
}

func decodeWorkRegistry(r *recordReader, event *Event) error {
	var values [11]uint64
	for i := range values {
		v, err := r.readUvarint()
		if err != nil {
			return err
		}
		values[i] = v
	}
	p := &WorkRegistryEvent{Version: values[0], SnapshotID: values[1], QueryStartedMS: values[2], ObservedMS: values[3], IntervalMS: values[4], PacketIndex: values[5], PacketCount: values[6], TotalObserved: values[7], RetainedJobs: values[8], Unavailable: values[9]}
	if values[10] > registryRowsPerPacket {
		return fmt.Errorf("registry row count exceeds limit")
	}
	for i := uint64(0); i < values[10]; i++ {
		row, err := decodeRegistryJob(r)
		if err != nil {
			return err
		}
		p.Jobs = append(p.Jobs, row)
	}
	if err := validateWorkRegistry(p); err != nil {
		return err
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkRegistry: p}
	return nil
}

func decodeRegistryJob(r *recordReader) (WorkRegistryJob, error) {
	var row WorkRegistryJob
	var err error
	if row.UUID, err = readThreadString(r, 36); err != nil {
		return row, err
	}
	var v [6]uint64
	for i := range v {
		if v[i], err = r.readUvarint(); err != nil {
			return row, err
		}
	}
	row.InstanceID, row.State, row.RunAttempt, row.Generation, row.InitialDelayMS = v[0], v[1], v[2], v[3], v[4]
	if v[5] & ^uint64(7) != 0 {
		return row, fmt.Errorf("unknown registry presence bits")
	}
	if v[5]&registryHasPeriod != 0 {
		period, e := r.readUvarint()
		if e != nil {
			return row, e
		}
		flex, e := r.readUvarint()
		if e != nil {
			return row, e
		}
		row.PeriodMS = &period
		row.FlexMS = &flex
	}
	for i, bit := range []uint64{registryHasEarliest, registryHasStop} {
		if v[5]&bit != 0 {
			raw, e := r.readUvarint()
			if e != nil {
				return row, e
			}
			decoded := int64(raw>>1) ^ -int64(raw&1)
			if i == 0 {
				row.EarliestEligibleUnixMS = &decoded
			} else {
				row.StopReason = &decoded
			}
		}
	}
	if row.Tags, err = decodeRegistryStrings(r, 8); err != nil {
		return row, err
	}
	if row.OmittedTags, err = r.readUvarint(); err != nil {
		return row, err
	}
	if row.NetworkType, err = readThreadString(r, 64); err != nil {
		return row, err
	}
	if row.Constraints, err = r.readUvarint(); err != nil {
		return row, err
	}
	if row.ContentTriggers, err = decodeRegistryStrings(r, 4); err != nil {
		return row, err
	}
	if row.OmittedContentTriggers, err = r.readUvarint(); err != nil {
		return row, err
	}
	if row.Progress, err = decodeWorkData(r); err != nil {
		return row, err
	}
	if row.Output, err = decodeWorkData(r); err != nil {
		return row, err
	}
	if row.OmittedProgressKeys, err = r.readUvarint(); err != nil {
		return row, err
	}
	if row.OmittedOutputKeys, err = r.readUvarint(); err != nil {
		return row, err
	}
	if row.TruncatedFields, err = r.readUvarint(); err != nil {
		return row, err
	}
	return row, nil
}

func decodeRegistryStrings(r *recordReader, limit uint64) ([]string, error) {
	n, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, fmt.Errorf("registry list exceeds limit")
	}
	var result []string
	for i := uint64(0); i < n; i++ {
		s, e := readThreadString(r, 512)
		if e != nil {
			return nil, e
		}
		result = append(result, s)
	}
	return result, nil
}

func decodeWorkData(r *recordReader) ([]WorkDataField, error) {
	n, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	if n > 8 {
		return nil, fmt.Errorf("Data field list exceeds limit")
	}
	var fields []WorkDataField
	for i := uint64(0); i < n; i++ {
		key, e := readThreadString(r, 512)
		if e != nil {
			return nil, e
		}
		kind, e := readThreadString(r, 32)
		if e != nil {
			return nil, e
		}
		known, e := r.readUvarint()
		if e != nil {
			return nil, e
		}
		if known > 1 {
			return nil, fmt.Errorf("invalid Data size presence")
		}
		f := WorkDataField{Key: key, Type: kind}
		if known != 0 {
			size, e := r.readUvarint()
			if e != nil {
				return nil, e
			}
			f.PayloadBytes = &size
		}
		fields = append(fields, f)
	}
	return fields, nil
}
