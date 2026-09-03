package jhlog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterDoesNotOwnDomainPayloadCodecs(t *testing.T) {
	required := []string{
		"event_payload.go",
		"event_payload_app.go",
		"event_payload_control.go",
		"event_payload_database.go",
		"event_payload_runtime.go",
		"event_validation.go",
	}
	for _, name := range required {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("domain codec %s is missing: %v", name, err)
		}
	}

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "writer.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{
		"encodeEventPayload":          true,
		"encodeEventPayloadWithState": true,
		"writeDatabaseTransactionID":  true,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if forbidden[function.Name.Name] || strings.HasPrefix(function.Name.Name, "validate") {
			t.Fatalf("writer.go still owns domain function %s", function.Name.Name)
		}
	}
}

func TestPayloadDispatchRemainsSmall(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, name, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "encodeEventPayloadWithState" {
				continue
			}
			lines := fileSet.Position(function.End()).Line - fileSet.Position(function.Pos()).Line + 1
			if lines > 16 {
				t.Fatalf("encodeEventPayloadWithState spans %d lines, want <= 16", lines)
			}
			return
		}
	}
	t.Fatal("encodeEventPayloadWithState not found")
}

func TestReaderDoesNotOwnDomainPayloadDecoder(t *testing.T) {
	for _, name := range []string{
		"event_payload_decoder.go",
		"event_payload_decoder_app.go",
		"event_payload_decoder_control.go",
		"event_payload_decoder_core.go",
		"event_payload_decoder_database.go",
		"event_payload_decoder_runtime.go",
	} {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("domain decoder %s is missing: %v", name, err)
		}
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "reader.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "decodeEventPayload" {
			t.Fatal("reader.go still owns decodeEventPayload")
		}
	}
	decoder, err := parser.ParseFile(fileSet, "event_payload_decoder.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range decoder.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "decodeEventPayload" {
			continue
		}
		lines := fileSet.Position(function.End()).Line - fileSet.Position(function.Pos()).Line + 1
		if lines > 80 {
			t.Fatalf("decodeEventPayload spans %d lines, want <= 80", lines)
		}
		return
	}
	t.Fatal("decodeEventPayload not found")
}
