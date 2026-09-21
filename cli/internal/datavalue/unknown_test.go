package datavalue

import "testing"

var unknownAllocationSink bool

func TestIsUnknownNormalizesCaseAndWhitespace(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: " UNKNOWN  BUILD ", want: true},
		{value: "\tКонтекст\u00a0выполнения  НЕДОСТУПЕН\n", want: true},
		{value: "Версия приложения неизвестна", want: true},
		{value: "com.example.UnknownPresenter", want: false},
		{value: "unknown.method", want: false},
	}
	for _, test := range tests {
		if got := IsUnknown(test.value); got != test.want {
			t.Fatalf("IsUnknown(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}

func TestIsUnknownCanonicalInputsDoNotAllocate(t *testing.T) {
	allocations := testing.AllocsPerRun(1_000, func() {
		unknownAllocationSink = IsUnknown("com.example.FeedPresenter")
		unknownAllocationSink = IsUnknown("UNKNOWN BUILD")
		unknownAllocationSink = IsUnknown("Контекст выполнения недоступен")
	})
	if allocations != 0 {
		t.Fatalf("unknown-value checks allocate %.2f objects, want zero", allocations)
	}
}
