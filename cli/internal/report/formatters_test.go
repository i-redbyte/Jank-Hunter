package report

import "testing"

var dataSizeSink string

func TestHumanDataSizeBytesUsesCompactBinaryUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bytes uint64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 Б"},
		{name: "bytes", bytes: 1023, want: "1023 Б"},
		{name: "kilobytes", bytes: 1024, want: "1.0 КБ"},
		{name: "megabytes", bytes: 88_139_074, want: "84.1 МБ"},
		{name: "gigabytes", bytes: 3 * 1024 * 1024 * 1024, want: "3.0 ГБ"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := humanDataSizeBytes(test.bytes); got != test.want {
				t.Fatalf("humanDataSizeBytes(%d) = %q, want %q", test.bytes, got, test.want)
			}
		})
	}
}

func TestHumanDataSizeBytesAllocatesOnlyResult(t *testing.T) {
	allocations := testing.AllocsPerRun(1_000, func() {
		dataSizeSink = humanDataSizeBytes(88_139_074)
	})
	if allocations > 1 {
		t.Fatalf("humanDataSizeBytes allocations = %.1f, want at most one result string", allocations)
	}
}
