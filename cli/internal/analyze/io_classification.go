package analyze

import "math/bits"

type IOClassificationKind string

const (
	IOClassificationObservation           IOClassificationKind = "observation"
	IOClassificationMainThreadObservation IOClassificationKind = "main_thread_observation"
	IOClassificationMainThreadSync        IOClassificationKind = "main_thread_sync"
	IOClassificationMainThreadLarge       IOClassificationKind = "main_thread_large"
	IOClassificationMainThreadSlow        IOClassificationKind = "main_thread_slow"
	IOClassificationRepeatedFailures      IOClassificationKind = "repeated_failures"
	IOClassificationBackgroundLarge       IOClassificationKind = "background_large"
	IOClassificationSmallOperationStorm   IOClassificationKind = "small_operation_storm"
	IOClassificationBackgroundSlow        IOClassificationKind = "background_slow"
)

type IOClassification struct {
	Kind         IOClassificationKind
	failureRate  float64
	byteCoverage float64
	signals      ioProblemSignals
}

func (classification IOClassification) IsProblem() bool {
	return classification.signals != 0
}

func (classification IOClassification) has(signal ioProblemSignals) bool {
	return classification.signals&signal != 0
}

func (classification IOClassification) signalCount() int {
	return bits.OnesCount8(uint8(classification.signals))
}

type ioProblemSignals uint8

const (
	ioSignalMainThreadSlow ioProblemSignals = 1 << iota
	ioSignalMainThreadSync
	ioSignalMainThreadLarge
	ioSignalBackgroundSlow
	ioSignalBackgroundLarge
	ioSignalSmallOperationStorm
	ioSignalRepeatedFailures
)

func ClassifyIO(operation IOStats) IOClassification {
	return classifyIO(operation, DefaultProblemDetectorConfig())
}

func classifyIO(operation IOStats, cfg ProblemDetectorConfig) IOClassification {
	failureRate := problemRatio(operation.Failures, operation.Count)
	classification := IOClassification{
		Kind:         IOClassificationObservation,
		failureRate:  failureRate,
		byteCoverage: problemRatio(operation.KnownByteOperations, operation.Count),
	}

	if operation.MainThread {
		classification.Kind = IOClassificationMainThreadObservation
		classification.add(ioSignalMainThreadSlow, operation.MaxDurationUS >= cfg.IOMainThreadMS*1_000)
		classification.add(ioSignalMainThreadSync, operation.Operation == "file_sync")
		classification.add(ioSignalMainThreadLarge, operation.MaxBytes >= cfg.IOLargeMainBytes)
	} else {
		classification.add(ioSignalBackgroundSlow, operation.MaxDurationUS >= cfg.IOBackgroundMS*1_000)
		classification.add(ioSignalBackgroundLarge, operation.MaxBytes >= cfg.IOLargeBackgroundBytes)
	}

	classification.add(
		ioSignalSmallOperationStorm,
		operation.Count >= cfg.IOStormMinCount &&
			float64(operation.PeakOperationsPerSecond) >= cfg.IOStormRate &&
			operation.KnownByteOperations == operation.Count &&
			operation.MaxBytes <= cfg.IOSmallOperationBytes,
	)
	classification.add(
		ioSignalRepeatedFailures,
		operation.Failures >= cfg.IOFailureMinCount && failureRate >= cfg.IOFailureRate,
	)

	classification.Kind = classification.primaryKind()
	return classification
}

func (classification *IOClassification) add(signal ioProblemSignals, present bool) {
	if present {
		classification.signals |= signal
	}
}

func (classification IOClassification) primaryKind() IOClassificationKind {
	switch {
	case classification.has(ioSignalMainThreadSync):
		return IOClassificationMainThreadSync
	case classification.has(ioSignalMainThreadLarge):
		return IOClassificationMainThreadLarge
	case classification.has(ioSignalMainThreadSlow):
		return IOClassificationMainThreadSlow
	case classification.has(ioSignalRepeatedFailures):
		return IOClassificationRepeatedFailures
	case classification.has(ioSignalBackgroundLarge):
		return IOClassificationBackgroundLarge
	case classification.has(ioSignalSmallOperationStorm):
		return IOClassificationSmallOperationStorm
	case classification.has(ioSignalBackgroundSlow):
		return IOClassificationBackgroundSlow
	default:
		return classification.Kind
	}
}
