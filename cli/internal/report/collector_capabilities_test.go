package report

import (
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestCollectorCapabilityStatus(t *testing.T) {
	tests := []struct {
		name    string
		summary analyze.Summary
		want    string
	}{
		{
			name: "enabled in every session",
			summary: analyze.Summary{
				CollectorFlagsAny: uint64(jhlog.CollectorIOTracing),
				CollectorFlagsAll: uint64(jhlog.CollectorIOTracing),
			},
			want: "enabled",
		},
		{
			name: "enabled in some sessions",
			summary: analyze.Summary{
				CollectorFlagsAny: uint64(jhlog.CollectorIOTracing),
			},
			want: "partial",
		},
		{
			name:    "disabled",
			summary: analyze.Summary{},
			want:    "disabled",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, _ := collectorCapabilityStatus(test.summary, jhlog.CollectorIOTracing)
			if status != test.want {
				t.Fatalf("status = %q, want %q", status, test.want)
			}
		})
	}
}

func TestCollectorCapabilitiesDistinguishDisabledFromEnabledWithoutEvents(t *testing.T) {
	summary := analyze.Summary{
		CollectorSessions: 1,
		CollectorFlagsAny: uint64(jhlog.CollectorKnownMask &^ jhlog.CollectorIOTracing),
		CollectorFlagsAll: uint64(jhlog.CollectorKnownMask &^ jhlog.CollectorIOTracing),
	}
	capabilities := collectorCapabilities(summary)
	if len(capabilities) != len(collectorCapabilityDefinitions) {
		t.Fatalf("collector capabilities = %d, want %d", len(capabilities), len(collectorCapabilityDefinitions))
	}
	byLabel := make(map[string]collectorCapability, len(capabilities))
	for _, capability := range capabilities {
		byLabel[capability.Label] = capability
	}
	if got := byLabel["Типизированные I/O операции"].Status; got != "disabled" {
		t.Fatalf("I/O status = %q, want disabled", got)
	}
	stalls := byLabel["Зависания главного потока"]
	if stalls.Status != "enabled" {
		t.Fatalf("stall status = %q, want enabled", stalls.Status)
	}
	if !strings.Contains(stalls.Observation, "порог зависания не был превышен") {
		t.Fatalf("enabled empty collector was described as missing: %q", stalls.Observation)
	}
}

func TestCollectorCapabilitiesRequireSessionFlags(t *testing.T) {
	if got := collectorCapabilities(analyze.Summary{}); got != nil {
		t.Fatalf("capabilities without a session = %+v, want nil", got)
	}
}

func TestCollectorCapabilityDefinitionsCoverEveryKnownFlagExactlyOnce(t *testing.T) {
	var covered jhlog.CollectorFlag
	for _, definition := range collectorCapabilityDefinitions {
		if covered&definition.flag != 0 {
			t.Fatalf("collector flag 0x%x has more than one definition", definition.flag)
		}
		if definition.label == "" || definition.description == "" || definition.observation == nil {
			t.Fatalf("collector flag 0x%x has an incomplete definition", definition.flag)
		}
		covered |= definition.flag
	}
	if covered != jhlog.CollectorKnownMask {
		t.Fatalf("covered collector mask = 0x%x, want 0x%x", covered, jhlog.CollectorKnownMask)
	}
}
