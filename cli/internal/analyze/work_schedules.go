package analyze

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const scheduleRowLimit = 512
const scheduleHistoryLimit = 16
const scheduleAttemptLimit = 128
const scheduleFindingLimit = 256

type WorkScheduleAnalysis struct {
	Rows                                                          []WorkScheduleRow
	Findings                                                      []WorkScheduleFinding
	OmittedRows, OmittedHistory, OmittedAttempts, OmittedFindings uint64
	RegistryUnavailable, IncompleteSnapshots, DroppedEvidence     uint64
	RegistryUnavailableReasons                                    map[uint64]uint64
	EmptyCompleteSnapshots                                        uint64
	CollectionLevel                                               string
	CollectionKnownLost                                           uint64
	HistoryOmitted                                                bool
}
type WorkScheduleRow struct {
	UUID, Scope, Process, Name, Worker           string
	Requested                                    *jhlog.WorkSubmissionEvent
	Requests                                     []WorkSubmissionObservation
	RegistryHistory                              []WorkScheduleSnapshot
	Registered                                   *jhlog.WorkRegistryJob
	RegistryObservedMS                           uint64
	RegistryObservedUnixMS                       *uint64
	RegistryAgeMS                                *uint64
	RegistryIntervalMS                           uint64
	RegistryComplete                             bool
	RegistryBeforeRequest                        bool
	ParametersDiffer                             bool
	Attempts                                     []WorkScheduleAttempt
	OrdinaryStarts, RetryStarts, AmbiguousStarts uint64
	Cadence                                      WorkScheduleCadence
	CadenceGeneration                            uint32
	GenerationCadences                           []WorkScheduleGeneration
	Duration                                     WorkScheduleCadence
}
type WorkScheduleSnapshot struct {
	ObservedMS            uint64
	ObservedUnixMS, AgeMS *uint64
	Complete              bool
	Job                   jhlog.WorkRegistryJob
}
type WorkScheduleCadence struct{ Count, MinMS, MedianMS, P95MS, MaxMS uint64 }
type WorkScheduleFinding struct {
	Kind, Scope, Process, Name, OtherName string
	Count, MinGapMS, MaxGapMS             uint64
	FirstMS, LastMS                       uint64
	TimeBasis                             string
	UUIDs                                 []string
	SameRequirements                      bool
	Proximity                             []WorkScheduleProximity
}
type workScheduleKey struct{ scope, process, uuid string }
type schedulePoint struct {
	key   workScheduleKey
	at    uint64
	basis string
}

// Bounded derived view. Raw histories remain available in the existing diagnostic tables.
func BuildWorkSchedules(s Summary) WorkScheduleAnalysis {
	a := WorkScheduleAnalysis{CollectionLevel: s.CollectionQuality.Level, CollectionKnownLost: s.CollectionQuality.KnownLostEvents}
	rows := map[workScheduleKey]*WorkScheduleRow{}
	keys := make([]workScheduleKey, 0)
	rowFor := func(key workScheduleKey) *WorkScheduleRow {
		if r := rows[key]; r != nil {
			return r
		}
		if len(rows) >= scheduleRowLimit {
			a.OmittedRows++
			return nil
		}
		r := &WorkScheduleRow{UUID: key.uuid, Scope: key.scope, Process: key.process, Name: key.uuid}
		rows[key] = r
		keys = append(keys, key)
		return r
	}
	// Process is part of identity. Old events with an unavailable process remain in their own scope.
	if s.WorkSubmissions != nil {
		a.HistoryOmitted = s.WorkSubmissions.HistoryOmitted
		a.DroppedEvidence = saturatingUint64Sum(a.DroppedEvidence, saturatingUint64Sum(s.WorkSubmissions.DroppedCalls, s.WorkSubmissions.OmittedRequests))
		for _, v := range s.WorkSubmissions.Observations {
			if v.Submission == nil {
				continue
			}
			r := rowFor(workScheduleKey{v.Scope, v.Process, v.UUID})
			if r == nil {
				continue
			}
			if v.Submission.HasUnique && !v.Submission.NameTruncated && v.Submission.UniqueName != "" {
				r.Name = v.Submission.UniqueName
			}
			if r.Requested == nil || v.Submission.ObservedElapsedNanos >= r.Requested.ObservedElapsedNanos {
				r.Requested = v.Submission
			}
			if len(r.Requests) < scheduleHistoryLimit {
				r.Requests = append(r.Requests, v)
			} else {
				a.OmittedHistory++
			}
		}
	}
	if s.WorkRegistry != nil {
		a.HistoryOmitted = a.HistoryOmitted || s.WorkRegistry.HistoryOmitted
		a.DroppedEvidence = saturatingUint64Sum(a.DroppedEvidence, s.WorkRegistry.DroppedPackets)
		for _, snap := range s.WorkRegistry.Snapshots {
			if snap.Unavailable != 0 {
				a.RegistryUnavailable++
				if a.RegistryUnavailableReasons == nil {
					a.RegistryUnavailableReasons = map[uint64]uint64{}
				}
				a.RegistryUnavailableReasons[snap.Unavailable]++
				continue
			}
			if snap.Complete && snap.TotalObserved == 0 {
				a.EmptyCompleteSnapshots++
			}
			if !snap.Complete {
				a.IncompleteSnapshots++
			}
			for _, job := range snap.Jobs {
				r := rowFor(workScheduleKey{snap.Scope, snap.Process, job.UUID})
				if r == nil {
					continue
				}
				history := WorkScheduleSnapshot{snap.ObservedElapsedMS, snap.ObservedUnixMS, snap.AgeAtCaptureMS, snap.Complete, job}
				if len(r.RegistryHistory) < scheduleHistoryLimit {
					r.RegistryHistory = append(r.RegistryHistory, history)
				} else {
					a.OmittedHistory++
				}
				if r.Registered == nil || snap.ObservedElapsedMS >= r.RegistryObservedMS {
					j := job
					r.Registered = &j
					r.RegistryObservedMS = snap.ObservedElapsedMS
					r.RegistryObservedUnixMS = snap.ObservedUnixMS
					r.RegistryAgeMS = snap.AgeAtCaptureMS
					r.RegistryComplete = snap.Complete
					r.RegistryIntervalMS = snap.IntervalMS
				}
			}
		}
	}
	if s.WorkerAnalysis != nil {
		a.DroppedEvidence = saturatingUint64Sum(a.DroppedEvidence, s.WorkerAnalysis.DroppedAttempts)
		for _, v := range s.WorkerAnalysis.Attempts {
			if v.UUID == "" {
				continue
			}
			r := rowFor(workScheduleKey{v.Scope, v.Process, v.UUID})
			if r == nil {
				continue
			}
			if v.Worker != "" && v.Worker != "unknown" {
				r.Worker = v.Worker
				if r.Name == r.UUID {
					r.Name = v.Worker
				}
			}
			if v.Ambiguous || v.IdentityAmbiguous {
				r.AmbiguousStarts++
				continue
			}
			if len(r.Attempts) < scheduleAttemptLimit {
				r.Attempts = append(r.Attempts, WorkScheduleAttempt{v.StartUnixMS, v.EndUnixMS, v.StartMS, v.EndMS, v.DurationMS, v.RunAttempt, v.Generation, v.Outcome})
			} else {
				a.OmittedAttempts++
			}
		}
	}
	points := make([]schedulePoint, 0)
	eligibility := make([]schedulePoint, 0)
	for _, key := range keys {
		r := rows[key]
		sort.SliceStable(r.Requests, func(i, j int) bool {
			return r.Requests[i].Submission.ObservedElapsedNanos < r.Requests[j].Submission.ObservedElapsedNanos
		})
		sort.SliceStable(r.RegistryHistory, func(i, j int) bool { return r.RegistryHistory[i].ObservedMS < r.RegistryHistory[j].ObservedMS })
		sort.SliceStable(r.Attempts, func(i, j int) bool { return scheduleAttemptTime(r.Attempts[i]) < scheduleAttemptTime(r.Attempts[j]) })
		var durations []uint64
		gapsByGeneration := map[uint32][]uint64{}
		var previous *WorkScheduleAttempt
		for i := range r.Attempts {
			v := &r.Attempts[i]
			if v.DurationMS != nil {
				durations = append(durations, *v.DurationMS)
			}
			if v.StartMS == nil {
				continue
			}
			if v.RunAttempt > 0 {
				r.RetryStarts++
				continue
			}
			r.OrdinaryStarts++
			if _, ok := gapsByGeneration[v.Generation]; !ok {
				gapsByGeneration[v.Generation] = nil
			}
			if v.Generation > r.CadenceGeneration {
				r.CadenceGeneration = v.Generation
			}
			if previous != nil && previous.Generation == v.Generation && *v.StartMS > *previous.StartMS {
				gapsByGeneration[v.Generation] = append(gapsByGeneration[v.Generation], *v.StartMS-*previous.StartMS)
			}
			previous = v
			points = append(points, schedulePoint{key, *v.StartMS, "elapsed"})
		}
		for gen, gaps := range gapsByGeneration {
			stats := scheduleDistribution(gaps)
			r.GenerationCadences = append(r.GenerationCadences, WorkScheduleGeneration{gen, stats})
			if gen == r.CadenceGeneration {
				r.Cadence = stats
			}
		}
		sort.Slice(r.GenerationCadences, func(i, j int) bool { return r.GenerationCadences[i].Generation < r.GenerationCadences[j].Generation })
		r.Duration = scheduleDistribution(durations)
		if p, j := r.Requested, r.Registered; p != nil && j != nil {
			r.RegistryBeforeRequest = r.RegistryObservedMS < p.ObservedElapsedNanos/1000000
			r.ParametersDiffer = scheduleRequestedDiffers(p, j)
		}
		if j := r.Registered; j != nil && j.State == 0 && j.EarliestEligibleUnixMS != nil && *j.EarliestEligibleUnixMS > 0 && r.RegistryComplete && r.RegistryAgeMS != nil && *r.RegistryAgeMS <= r.RegistryIntervalMS {
			eligibility = append(eligibility, schedulePoint{key, uint64(*j.EarliestEligibleUnixMS), "unix"})
		}
	}
	// Adjacent observations only: no quadratic all-pairs scan, no claim of exhaustive clustering.
	addScheduleNeighbors(&a, rows, points, "adjacent_starts")
	addScheduleNeighbors(&a, rows, eligibility, "nearby_eligibility")
	addScheduleChurn(&a, s.WorkSubmissions)
	for _, key := range keys {
		a.Rows = append(a.Rows, *rows[key])
	}
	sort.SliceStable(a.Rows, func(i, j int) bool {
		if a.Rows[i].Name != a.Rows[j].Name {
			return a.Rows[i].Name < a.Rows[j].Name
		}
		if a.Rows[i].Scope != a.Rows[j].Scope {
			return a.Rows[i].Scope < a.Rows[j].Scope
		}
		return a.Rows[i].UUID < a.Rows[j].UUID
	})
	return a
}
func scheduleDistribution(values []uint64) WorkScheduleCadence {
	if len(values) == 0 {
		return WorkScheduleCadence{}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return WorkScheduleCadence{uint64(len(values)), values[0], scheduleMedian(values), values[(len(values)*95+99)/100-1], values[len(values)-1]}
}
func scheduleRequestedDiffers(p *jhlog.WorkSubmissionEvent, j *jhlog.WorkRegistryJob) bool {
	if p.Periodic != (j.PeriodMS != nil) {
		return true
	}
	if p.RequestedPeriodMS != nil && p.RequestedFlexMS == nil && j.PeriodMS != nil && j.FlexMS != nil && *j.FlexMS != *j.PeriodMS {
		return true
	}
	if p.RequestedPeriodMS != nil && j.PeriodMS != nil && *p.RequestedPeriodMS != *j.PeriodMS {
		return true
	}
	if p.RequestedFlexMS != nil && j.FlexMS != nil && *p.RequestedFlexMS != *j.FlexMS {
		return true
	}
	return p.RequestedInitialDelayMS != nil && *p.RequestedInitialDelayMS != j.InitialDelayMS
}
func addScheduleFinding(a *WorkScheduleAnalysis, f WorkScheduleFinding) {
	if len(a.Findings) >= scheduleFindingLimit {
		a.OmittedFindings++
		return
	}
	a.Findings = append(a.Findings, f)
}
func addScheduleNeighbors(a *WorkScheduleAnalysis, rows map[workScheduleKey]*WorkScheduleRow, points []schedulePoint, kind string) {
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].key.scope != points[j].key.scope {
			return points[i].key.scope < points[j].key.scope
		}
		if points[i].key.process != points[j].key.process {
			return points[i].key.process < points[j].key.process
		}
		return points[i].at < points[j].at
	})
	type pair struct{ first, second workScheduleKey }
	findings := map[pair]int{}
	for i := 1; i < len(points); i++ {
		x, y := points[i-1], points[i]
		if x.key.scope != y.key.scope || x.key.process != y.key.process || x.key.uuid == y.key.uuid || y.at-x.at > 900000 {
			continue
		}
		left, right := rows[x.key], rows[y.key]
		if left.Name == right.Name {
			continue
		}
		k := pair{x.key, y.key}
		if k.first.uuid > k.second.uuid {
			k.first, k.second = k.second, k.first
		}
		gap := y.at - x.at
		if n, ok := findings[k]; ok {
			f := &a.Findings[n]
			f.Count++
			addScheduleProximity(f, gap)
			if gap < f.MinGapMS {
				f.MinGapMS = gap
			}
			if gap > f.MaxGapMS {
				f.MaxGapMS = gap
			}
			f.LastMS = y.at
			continue
		}
		same := false
		if l, r := left.Registered, right.Registered; l != nil && r != nil {
			same = l.NetworkType != "" && l.NetworkType == r.NetworkType && l.Constraints&48 == 0 && r.Constraints&48 == 0 && l.Constraints == r.Constraints && left.RegistryComplete && right.RegistryComplete && len(l.ContentTriggers) == 0 && len(r.ContentTriggers) == 0 && l.OmittedContentTriggers == 0 && r.OmittedContentTriggers == 0
		}
		if len(a.Findings) < scheduleFindingLimit {
			findings[k] = len(a.Findings)
		}
		addScheduleFinding(a, WorkScheduleFinding{Kind: kind, Scope: x.key.scope, Process: x.key.process, Name: left.Name, OtherName: right.Name, Count: 1, MinGapMS: gap, MaxGapMS: gap, FirstMS: x.at, LastMS: y.at, TimeBasis: x.basis, UUIDs: []string{x.key.uuid, y.key.uuid}, SameRequirements: same, Proximity: scheduleProximity(gap)})
	}
}
func addScheduleChurn(a *WorkScheduleAnalysis, source *WorkSubmissionAnalysis) {
	if source == nil {
		return
	}
	type group struct{ scope, process, name string }
	groups := map[group][]WorkSubmissionObservation{}
	order := []group{}
	for _, v := range source.Observations {
		p := v.Submission
		if p == nil || !p.HasUnique || p.NameTruncated || p.UniqueName == "" || p.Policy != jhlog.WorkSubmissionReplace && p.Policy != jhlog.WorkSubmissionCancelAndReenqueue {
			continue
		}
		k := group{v.Scope, v.Process, p.UniqueName}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], v)
	}
	for _, k := range order {
		events := groups[k]
		sort.SliceStable(events, func(i, j int) bool {
			return events[i].Submission.ObservedElapsedNanos < events[j].Submission.ObservedElapsedNanos
		})
		for start := 0; start < len(events); {
			end := start + 1
			firstNanos := events[start].Submission.ObservedElapsedNanos
			first := firstNanos / 1000000
			ids := map[string]bool{events[start].UUID: true}
			for end < len(events) && events[end].Submission.ObservedElapsedNanos-firstNanos <= 1000000000 {
				ids[events[end].UUID] = true
				end++
			}
			if end-start >= 2 && len(ids) >= 2 {
				uuids := []string{}
				for id := range ids {
					uuids = append(uuids, id)
				}
				sort.Strings(uuids)
				if len(uuids) > 16 {
					uuids = uuids[:16]
				}
				// Old logs/tests may lack the producer observation clock: never mix wall and record clocks.
				last := events[end-1].Submission.ObservedElapsedNanos / 1000000
				addScheduleFinding(a, WorkScheduleFinding{Kind: "replacement_churn", Scope: k.scope, Process: k.process, Name: k.name, Count: uint64(end - start), FirstMS: first, LastMS: last, MaxGapMS: last - first, TimeBasis: "elapsed", UUIDs: uuids})
			}
			start = end
		}
	}
}

func scheduleMedian(sorted []uint64) uint64 {
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return sorted[mid-1] + (sorted[mid]-sorted[mid-1])/2
}

type WorkScheduleAttempt struct {
	StartUnixMS, EndUnixMS, StartMS, EndMS, DurationMS *uint64
	RunAttempt, Generation                             uint32
	Outcome                                            string
}

func scheduleAttemptTime(a WorkScheduleAttempt) uint64 {
	if a.StartMS != nil {
		return *a.StartMS
	}
	if a.EndMS != nil {
		return *a.EndMS
	}
	return 0
}

type WorkScheduleGeneration struct {
	Generation uint32
	Cadence    WorkScheduleCadence
}

type WorkScheduleProximity struct{ LimitMS, Count, MinGapMS, MaxGapMS uint64 }

func scheduleProximity(gap uint64) []WorkScheduleProximity {
	f := WorkScheduleFinding{Proximity: []WorkScheduleProximity{{LimitMS: 60000}, {LimitMS: 300000}, {LimitMS: 900000}}}
	addScheduleProximity(&f, gap)
	return f.Proximity
}
func addScheduleProximity(f *WorkScheduleFinding, gap uint64) {
	for i := range f.Proximity {
		b := &f.Proximity[i]
		if gap > b.LimitMS {
			continue
		}
		if b.Count == 0 || gap < b.MinGapMS {
			b.MinGapMS = gap
		}
		if gap > b.MaxGapMS {
			b.MaxGapMS = gap
		}
		b.Count++
	}
}
