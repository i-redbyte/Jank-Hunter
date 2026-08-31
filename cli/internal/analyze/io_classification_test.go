package analyze

import "testing"

func TestClassifyIOUsesProblemDetectorPriority(t *testing.T) {
	tests := []struct {
		name      string
		operation IOStats
		want      IOClassificationKind
		problem   bool
	}{
		{
			name:      "main thread sync wins over every other signal",
			operation: IOStats{Operation: "file_sync", MainThread: true, Count: 4, Failures: 4, MaxDurationUS: 1_000_000, MaxBytes: 32 << 20},
			want:      IOClassificationMainThreadSync,
			problem:   true,
		},
		{
			name:      "large main thread operation wins over duration",
			operation: IOStats{Operation: "file_read", MainThread: true, Count: 1, MaxDurationUS: 20_000, MaxBytes: 1 << 20},
			want:      IOClassificationMainThreadLarge,
			problem:   true,
		},
		{
			name:      "slow main thread operation",
			operation: IOStats{Operation: "file_read", MainThread: true, Count: 1, MaxDurationUS: 16_000},
			want:      IOClassificationMainThreadSlow,
			problem:   true,
		},
		{
			name:      "repeated failures win over background pressure",
			operation: IOStats{Operation: "content_read", Count: 4, Failures: 2, MaxDurationUS: 800_000, MaxBytes: 32 << 20},
			want:      IOClassificationRepeatedFailures,
			problem:   true,
		},
		{
			name:      "large background operation wins over storm",
			operation: IOStats{Operation: "file_write", Count: 50, KnownByteOperations: 50, MaxBytes: 16 << 20, PeakOperationsPerSecond: 5},
			want:      IOClassificationBackgroundLarge,
			problem:   true,
		},
		{
			name:      "slow background operation",
			operation: IOStats{Operation: "file_write", Count: 1, MaxDurationUS: 500_000},
			want:      IOClassificationBackgroundSlow,
			problem:   true,
		},
		{
			name:      "small operation storm requires complete byte evidence",
			operation: IOStats{Operation: "file_write", Count: 50, KnownByteOperations: 50, MaxBytes: 64 << 10, PeakOperationsPerSecond: 5},
			want:      IOClassificationSmallOperationStorm,
			problem:   true,
		},
		{
			name:      "ordinary main thread observation is not a finding",
			operation: IOStats{Operation: "content_read", MainThread: true, Count: 1, MaxDurationUS: 1_000},
			want:      IOClassificationMainThreadObservation,
			problem:   false,
		},
		{
			name:      "partial byte evidence cannot prove small operation storm",
			operation: IOStats{Operation: "file_write", Count: 50, KnownByteOperations: 49, MaxBytes: 4 << 10, PeakOperationsPerSecond: 5},
			want:      IOClassificationObservation,
			problem:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classification := ClassifyIO(test.operation)
			if classification.Kind != test.want || classification.IsProblem() != test.problem {
				t.Fatalf("classification = %+v, problem = %t; want kind %q, problem %t", classification, classification.IsProblem(), test.want, test.problem)
			}
		})
	}
}

func TestClassifyIOHonorsCustomDetectorThresholds(t *testing.T) {
	cfg := DefaultProblemDetectorConfig()
	cfg.IOMainThreadMS = 40

	classification := classifyIO(IOStats{
		Operation: "file_read", MainThread: true, Count: 1, MaxDurationUS: 20_000,
	}, cfg)

	if classification.Kind != IOClassificationMainThreadObservation || classification.IsProblem() {
		t.Fatalf("classification = %+v", classification)
	}
}
