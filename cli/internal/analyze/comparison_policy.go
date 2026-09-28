package analyze

import "math"

const zeroEventUpperBound95Numerator = 3.0

func zeroEventUpperRate95(exposure uint64) (float64, bool) {
	if exposure == 0 || exposure > maxExactMetricInteger {
		return 0, false
	}
	return zeroEventUpperBound95Numerator / float64(exposure), true
}

// ComparisonChange describes an effect inside an eligible scope, not causality.
type ComparisonChange string

const (
	ChangeInsufficientData ComparisonChange = "insufficient_data"
	ChangeNotComparable    ComparisonChange = "not_comparable"
	ChangeUnchanged        ComparisonChange = "unchanged"
	ChangeImproved         ComparisonChange = "improved"
	ChangeRegressed        ComparisonChange = "regressed"
	ChangeMixed            ComparisonChange = "mixed"
)

type measurementState uint8

const (
	measurementUnknown measurementState = iota
	measurementObserved
)

type comparisonMeasurement struct {
	Value   float64
	Samples uint64
	State   measurementState
}
type comparisonBand struct{ Absolute, Relative float64 }
type comparisonEffect struct {
	Change             ComparisonChange
	Absolute, Relative float64
	RelativeKnown      bool
}

// compareMeasurement is descriptive only. The caller owns scope eligibility and
// domain-specific sample/precision gates; a practical band does not prove equivalence.
func compareMeasurement(before, after comparisonMeasurement, band comparisonBand, eligible, higherIsWorse bool) comparisonEffect {
	result := comparisonEffect{Change: ChangeInsufficientData}
	if !eligible {
		result.Change = ChangeNotComparable
		return result
	}
	if before.State != measurementObserved || after.State != measurementObserved ||
		before.Samples == 0 || after.Samples == 0 || !finiteComparisonValue(before.Value) ||
		!finiteComparisonValue(after.Value) || !finiteComparisonValue(band.Absolute) ||
		!finiteComparisonValue(band.Relative) || band.Absolute < 0 || band.Relative < 0 {
		return result
	}
	result.Absolute = after.Value - before.Value
	if !finiteComparisonValue(result.Absolute) {
		return comparisonEffect{Change: ChangeInsufficientData}
	}
	if before.Value != 0 {
		result.Relative = result.Absolute / math.Abs(before.Value)
		result.RelativeKnown = finiteComparisonValue(result.Relative)
		if !result.RelativeKnown {
			result.Relative = 0
		}
	}
	tolerance := math.Max(band.Absolute, math.Abs(before.Value)*band.Relative)
	if !finiteComparisonValue(tolerance) {
		return comparisonEffect{Change: ChangeInsufficientData}
	}
	result.Change = ChangeUnchanged
	if math.Abs(result.Absolute) > tolerance {
		result.Change = ChangeImproved
		if (result.Absolute > 0) == higherIsWorse {
			result.Change = ChangeRegressed
		}
	}
	return result
}
func finiteComparisonValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func welchDifferenceInterval95(baseline, candidate []float64, center float64) (EffectInterval, bool) {
	if len(baseline) < 2 || len(candidate) < 2 || !finiteComparisonValue(center) {
		return EffectInterval{}, false
	}
	leftVariance, leftOK := sampleVariance(baseline)
	rightVariance, rightOK := sampleVariance(candidate)
	if !leftOK || !rightOK {
		return EffectInterval{}, false
	}
	leftTerm := leftVariance / float64(len(baseline))
	rightTerm := rightVariance / float64(len(candidate))
	standardError := math.Sqrt(leftTerm + rightTerm)
	if !finiteComparisonValue(standardError) {
		return EffectInterval{}, false
	}
	degrees := float64(len(baseline) + len(candidate) - 2)
	denominator := leftTerm*leftTerm/float64(len(baseline)-1) + rightTerm*rightTerm/float64(len(candidate)-1)
	if denominator > 0 {
		degrees = (leftTerm + rightTerm) * (leftTerm + rightTerm) / denominator
	}
	critical := studentTCritical95(degrees)
	margin := critical * standardError
	return EffectInterval{Lower: center - margin, Upper: center + margin, ConfidenceLevel: 0.95, BaselineGroups: len(baseline), CandidateGroups: len(candidate), Method: "welch_unpaired"}, true
}

func sampleVariance(values []float64) (float64, bool) {
	var mean, sumSquares float64
	for index, value := range values {
		if !finiteComparisonValue(value) {
			return 0, false
		}
		delta := value - mean
		mean += delta / float64(index+1)
		sumSquares += delta * (value - mean)
	}
	variance := sumSquares / float64(len(values)-1)
	return variance, finiteComparisonValue(variance) && variance >= 0
}

func studentTCritical95(degrees float64) float64 {
	values := [...]float64{0, 12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228, 2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086, 2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042}
	if degrees < 1 {
		return values[1]
	}
	index := int(math.Floor(degrees))
	if index < len(values) {
		return values[index]
	}
	if degrees < 60 {
		return 2.000
	}
	if degrees < 120 {
		return 1.980
	}
	return 1.960
}

func intervalAwareChange(interval EffectInterval, baseline float64, band comparisonBand, higherIsWorse bool) ComparisonChange {
	tolerance := math.Max(band.Absolute, math.Abs(baseline)*band.Relative)
	if !finiteComparisonValue(tolerance) || interval.Lower > interval.Upper {
		return ChangeInsufficientData
	}
	if interval.Lower > tolerance {
		if higherIsWorse {
			return ChangeRegressed
		}
		return ChangeImproved
	}
	if interval.Upper < -tolerance {
		if higherIsWorse {
			return ChangeImproved
		}
		return ChangeRegressed
	}
	if interval.Lower >= -tolerance && interval.Upper <= tolerance {
		return ChangeUnchanged
	}
	return ChangeInsufficientData
}

// A finite-state reducer: each input contributes a bit, so reduction is
// commutative and cannot cancel opposing changes. No per-observation storage.
type comparisonOutcome uint8

const (
	outcomeImproved comparisonOutcome = 1 << iota
	outcomeRegressed
	outcomeUnchanged
	outcomeUnknown
	outcomeIneligible
)

func (o *comparisonOutcome) add(change ComparisonChange) {
	switch change {
	case ChangeImproved:
		*o |= outcomeImproved
	case ChangeRegressed:
		*o |= outcomeRegressed
	case ChangeMixed:
		*o |= outcomeImproved | outcomeRegressed
	case ChangeUnchanged:
		*o |= outcomeUnchanged
	case ChangeNotComparable:
		*o |= outcomeIneligible
	default:
		*o |= outcomeUnknown
	}
}
func (o comparisonOutcome) change() ComparisonChange {
	switch o & (outcomeImproved | outcomeRegressed) {
	case outcomeImproved | outcomeRegressed:
		return ChangeMixed
	case outcomeImproved:
		return ChangeImproved
	case outcomeRegressed:
		return ChangeRegressed
	}
	if o&outcomeUnknown != 0 || o == 0 {
		return ChangeInsufficientData
	}
	if o&outcomeUnchanged != 0 {
		return ChangeUnchanged
	}
	return ChangeNotComparable
}
