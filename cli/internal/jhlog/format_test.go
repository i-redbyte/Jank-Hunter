package jhlog

import (
	"os/exec"
	"testing"
)

func TestGeneratedWireContractIsCurrent(t *testing.T) {
	command := exec.Command("python3", "../../../wire/generate.py", "--check")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("wire generated sources are stale: %v\n%s", err, output)
	}
}

func TestResolveSymbolFormatsCanonicalStableID(t *testing.T) {
	tests := []struct {
		name string
		ref  SymbolRef
		want string
	}{
		{
			name: "unnamespaced",
			ref:  StableSymbol(0x0123456789abcdef),
			want: "stable:0x0123456789abcdef",
		},
		{
			name: "namespaced",
			ref:  StableSymbolInNamespace(0xabcdef, []byte{0xaa, 0xbb}),
			want: "stable:aabb:0x0000000000abcdef",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveSymbol(nil, test.ref); got != test.want {
				t.Fatalf("ResolveSymbol() = %q, want %q", got, test.want)
			}
		})
	}
}
