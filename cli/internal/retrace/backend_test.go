package retrace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func integrationBackend(t *testing.T) Backend {
	t.Helper()
	jar, bridge := os.Getenv("JANK_HUNTER_TEST_R8_JAR"), os.Getenv("JANK_HUNTER_TEST_RETRACE_BRIDGE")
	if jar == "" || bridge == "" {
		t.Skip("official JVM integration requires built offline Retrace bundle")
	}
	return Backend{R8Jar: jar, BridgeJar: bridge}
}

func TestOfficialRealR8InlineGolden(t *testing.T) {
	backend := integrationBackend(t)
	fixture := filepath.Join("..", "analyze", "testdata", "r8-inline-9.0.32")
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(data))
	}
	results, err := backend.Run(context.Background(), filepath.Join(fixture, "mapping.txt"), mappingDigest(t, filepath.Join(fixture, "mapping.txt")), []Request{StackRequest{Text: read("stack.txt")}})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, group := range results[0].(StackResult).Groups {
		if group.Ambiguous || len(group.Alternatives) != 1 {
			t.Fatalf("unexpected ambiguity: %+v", group)
		}
		lines = append(lines, group.Alternatives[0]...)
	}
	if got, want := strings.Join(lines, "\n"), read("expected.txt"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestOfficialFieldsKeepAmbiguityAndUseExactDeclaredType(t *testing.T) {
	backend := integrationBackend(t)
	mapping := filepath.Join(t.TempDir(), "mapping.txt")
	if err := os.WriteFile(mapping, []byte("original.Holder -> a:\n    java.lang.Object first -> b\n    java.lang.String second -> b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := backend.Run(context.Background(), mapping, mappingDigest(t, mapping), []Request{
		FieldRequest{Owner: "a", Name: "b"},
		FieldRequest{Owner: "a", Name: "b", Descriptor: "Ljava/lang/String;"},
		FieldRequest{Owner: "a", Name: "missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := results[0].(FieldResult)
	if !ambiguous.Ambiguous || len(ambiguous.Alternatives) != 2 {
		t.Fatalf("lost ambiguity: %+v", ambiguous)
	}
	exact := results[1].(FieldResult)
	want := FieldResult{Alternatives: []FieldCandidate{{Known: true, Owner: "original.Holder", Name: "second", Descriptor: "Ljava/lang/String;"}}}
	if !reflect.DeepEqual(exact, want) {
		t.Fatalf("typed field: %+v", exact)
	}
	unknown := results[2].(FieldResult)
	if len(unknown.Alternatives) != 1 || unknown.Alternatives[0].Known {
		t.Fatalf("invented field: %+v", unknown)
	}
}

func TestRequestBoundsAndMissingOwner(t *testing.T) {
	for _, request := range []Request{nil, FieldRequest{Name: "a"}, StackRequest{Text: strings.Repeat("x", maxString+1)}, StackRequest{Text: string([]byte{0xff})}} {
		if _, err := encode([]Request{request}); err == nil {
			t.Fatalf("accepted invalid request: %T", request)
		}
	}
}

func TestDecoderRejectsTruncationTrailingAndCardinality(t *testing.T) {
	valid := new(bytes.Buffer)
	for _, value := range []any{protocolMagic, uint32(1), byte(1), uint32(1), byte(0), uint32(1), uint32(1), uint32(1), byte('x')} {
		if err := binary.Write(valid, binary.BigEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	requests := []Request{StackRequest{Text: "x"}}
	if _, err := decode(valid.Bytes(), requests); err != nil {
		t.Fatal(err)
	}
	for length := 0; length < valid.Len(); length++ {
		if _, err := decode(valid.Bytes()[:length], requests); err == nil {
			t.Fatalf("accepted truncation at %d", length)
		}
	}
	if _, err := decode(append(valid.Bytes(), 0), requests); err == nil {
		t.Fatal("accepted trailing bytes")
	}
	corrupt := bytes.Clone(valid.Bytes())
	binary.BigEndian.PutUint32(corrupt[9:13], maxItems+1)
	if _, err := decode(corrupt, requests); err == nil {
		t.Fatal("accepted oversized cardinality")
	}
}

func TestWrongOfficialJarFailsBeforeJavaStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r8.jar")
	if err := os.WriteFile(path, []byte("different version"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (Backend{Java: "must-not-execute", R8Jar: path}).Run(context.Background(), "unused", "", []Request{StackRequest{Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("wrong artifact accepted: %v", err)
	}
}

func TestOutputLimitCannotBeBypassedByCopyFastPath(t *testing.T) {
	output := limitedBuffer{limit: 4}
	source := struct{ io.Reader }{strings.NewReader("oversized")}
	if _, err := io.Copy(&output, source); err == nil {
		t.Fatal("io.Copy bypassed output byte bound")
	}
	if output.Len() > 4 {
		t.Fatal("output retained bytes beyond limit")
	}
}

func mappingDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestOfficialRealR8OverloadAmbiguityMatchesCliGolden(t *testing.T) {
	backend := integrationBackend(t)
	fixture := filepath.Join("..", "analyze", "testdata", "r8-overloads-9.0.32")
	mapping := filepath.Join(fixture, "mapping.txt")
	stack, err := os.ReadFile(filepath.Join(fixture, "stack.txt"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(fixture, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	results, err := backend.Run(context.Background(), mapping, mappingDigest(t, mapping), []Request{
		StackRequest{Text: strings.TrimSpace(string(stack))},
		StackRequest{Text: "\tat a.a.a(Overloads.java:2)"},
		FieldRequest{Owner: "a.a", Name: "a", Descriptor: "J"},
		FieldRequest{Owner: "a.a", Name: "b", Descriptor: "Ljava/lang/String;"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := results[0].(StackResult)
	if len(ambiguous.Groups) != 1 || !ambiguous.Groups[0].Ambiguous || len(ambiguous.Groups[0].Alternatives) != 2 {
		t.Fatalf("lost overload alternatives: %+v", ambiguous)
	}
	var got []string
	for _, alternative := range ambiguous.Groups[0].Alternatives {
		for _, line := range alternative {
			got = append(got, strings.TrimSpace(line))
		}
	}
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(string(golden)), "\n") {
		want = append(want, strings.Replace(strings.TrimSpace(line), "<OR> ", "", 1))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("official CLI/API differential mismatch: %q != %q", got, want)
	}
	exact := results[1].(StackResult)
	if len(exact.Groups) != 1 || exact.Groups[0].Ambiguous || len(exact.Groups[0].Alternatives) != 1 || !strings.Contains(exact.Groups[0].Alternatives[0][0], "second(Overloads.java:9)") {
		t.Fatalf("line disambiguation: %+v", exact)
	}
	for index, expected := range []FieldCandidate{{Known: true, Owner: "fixture.Overloads", Name: "firstField", Descriptor: "J"}, {Known: true, Owner: "fixture.Overloads", Name: "secondField", Descriptor: "Ljava/lang/String;"}} {
		field := results[index+2].(FieldResult)
		if field.Ambiguous || len(field.Alternatives) != 1 || field.Alternatives[0] != expected {
			t.Fatalf("actual R8 field mapping: %+v", field)
		}
	}
}

func TestOfficialRejectsMalformedOrUnsupportedMapping(t *testing.T) {
	backend := integrationBackend(t)
	for name, text := range map[string]string{
		"truncated_member": "original.Holder -> a:\n    1:1:void broken( -> b\n",
		"future_version":   "# {\"id\":\"com.android.tools.r8.mapping\",\"version\":\"99.0\"}\noriginal.Holder -> a:\n    1:1:void method():7:7 -> b\n",
	} {
		t.Run(name, func(t *testing.T) {
			mapping := filepath.Join(t.TempDir(), "mapping.txt")
			if err := os.WriteFile(mapping, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := backend.Run(context.Background(), mapping, mappingDigest(t, mapping), []Request{StackRequest{Text: "\tat a.b(SourceFile:1)"}})
			if err == nil {
				t.Fatal("unsupported mapping produced apparently exact output")
			}
		})
	}
}

// Metadata cases follow the official format contract:
// https://r8.googlesource.com/r8/+/refs/heads/main/doc/retrace.md
// These are authored format fixtures, distinct from the real R8 compilation goldens above.
func TestOfficialMetadataKeepsCrossFrameContext(t *testing.T) {
	backend := integrationBackend(t)
	for _, test := range []struct{ name, mapping, stack, want string }{
		{
			name: "outline context",
			mapping: "# {\"id\":\"com.android.tools.r8.mapping\",\"version\":\"2.2\"}\n" +
				"generated.Outline -> a:\n    1:2:int extract() -> a\n    # {\"id\":\"com.android.tools.r8.outline\"}\n" +
				"app.Screen -> b:\n    4:4:int render(int):98:98 -> s\n    5:5:int render(int):100:100 -> s\n    27:27:int render(int):0:0 -> s\n" +
				"    # {\"id\":\"com.android.tools.r8.outlineCallsite\",\"positions\":{\"1\":4,\"2\":5},\"outline\":\"La;a()I\"}\n",
			stack: "\tat a.a(:1)\n\tat b.s(:27)",
			want:  "at app.Screen.render(Screen.java:98)",
		},
		{
			name: "synthesized lambda wrapper",
			mapping: "# {\"id\":\"com.android.tools.r8.mapping\",\"version\":\"1.0\"}\n" +
				"generated.Callback -> a:\n    4:4:void app.Screen.lambda$render$0():25:25 -> b\n    4:4:void run():2:2 -> b\n" +
				"    # {\"id\":\"com.android.tools.r8.synthesized\"}\n" +
				"app.Screen -> c:\n# {\"id\":\"sourceFile\",\"fileName\":\"Screen.kt\"}\n",
			stack: "\tat a.b(SourceFile:4)",
			want:  "at app.Screen.lambda$render$0(Screen.kt:25)",
		},
		{
			name: "exception rewrite context",
			mapping: "# {\"id\":\"com.android.tools.r8.mapping\",\"version\":\"2.0\"}\n" +
				"app.Screen -> a:\n    4:4:void app.Binding.read():23:23 -> b\n    4:4:void render():7:7 -> b\n" +
				"    # {\"id\":\"com.android.tools.r8.rewriteFrame\",\"conditions\":[\"throws(Ljava/lang/NullPointerException;)\"],\"actions\":[\"removeInnerFrames(1)\"]}\n",
			stack: "java.lang.NullPointerException\n\tat a.b(:4)",
			want:  "java.lang.NullPointerException\nat app.Screen.render(Screen.java:7)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapping := filepath.Join(t.TempDir(), "mapping.txt")
			if err := os.WriteFile(mapping, []byte(test.mapping), 0600); err != nil {
				t.Fatal(err)
			}
			results, err := backend.Run(context.Background(), mapping, mappingDigest(t, mapping), []Request{StackRequest{Text: test.stack}})
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, group := range results[0].(StackResult).Groups {
				if group.Ambiguous || len(group.Alternatives) > 1 {
					t.Fatalf("unexpected alternatives: %+v", group)
				}
				for _, chain := range group.Alternatives {
					for _, line := range chain {
						lines = append(lines, strings.TrimSpace(line))
					}
				}
			}
			if got := strings.Join(lines, "\n"); got != test.want {
				t.Fatalf("metadata context lost:\ngot %s\nwant %s", got, test.want)
			}
		})
	}
}
