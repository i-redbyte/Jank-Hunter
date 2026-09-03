package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type analysisOptionsBuilder struct {
	filter               analyze.Filter
	artifactsDir         string
	mappingPath          string
	classGraphPath       string
	diagnosticsPath      string
	diCatalogPath        string
	componentCatalogPath string
	databaseEvidencePath string
	artifactNS           []byte
}

func takeAnalysisOptionsBuilder(args []string) (analysisOptionsBuilder, []string, error) {
	filter, remaining, err := takeFilterFlags(args)
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	artifactsDir, remaining, err := takeStringFlag(remaining, "artifacts-dir", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	mappingPath, remaining, err := takeStringFlag(remaining, "mapping", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	classGraphPath, remaining, err := takeStringFlag(remaining, "class-graph", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	diagnosticsPath, remaining, err := takeStringFlag(remaining, "instrumentation-diagnostics", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	diCatalogPath, remaining, err := takeStringFlag(remaining, "di-catalog", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	componentCatalogPath, remaining, err := takeStringFlag(remaining, "android-components-catalog", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	databaseEvidencePath, remaining, err := takeStringFlag(remaining, "database-evidence", "")
	if err != nil {
		return analysisOptionsBuilder{}, nil, err
	}
	return analysisOptionsBuilder{
		filter:               filter,
		artifactsDir:         artifactsDir,
		mappingPath:          mappingPath,
		classGraphPath:       classGraphPath,
		diagnosticsPath:      diagnosticsPath,
		diCatalogPath:        diCatalogPath,
		componentCatalogPath: componentCatalogPath,
		databaseEvidencePath: databaseEvidencePath,
	}, remaining, nil
}

func (b analysisOptionsBuilder) buildForLogs(paths []string) (analyze.Options, error) {
	namespaces, err := logArtifactNamespaces(paths)
	if err != nil {
		return analyze.Options{}, err
	}
	return b.buildWithArtifactNamespaces(namespaces)
}

func (b analysisOptionsBuilder) buildWithArtifactNamespaces(namespaces map[string]struct{}) (analyze.Options, error) {
	explicitClassGraph := strings.TrimSpace(b.classGraphPath)
	explicitDiagnostics := strings.TrimSpace(b.diagnosticsPath)
	b, err := b.withExplicitArtifactsForNamespaces(namespaces)
	if err != nil {
		return analyze.Options{}, err
	}
	b, err = b.withExplicitArtifactSidecarsForNamespaces(
		explicitClassGraph,
		explicitDiagnostics,
		namespaces,
	)
	if err != nil {
		return analyze.Options{}, err
	}
	nameMapping, err := analyze.LoadNameMapping(b.mappingPath)
	if err != nil {
		return analyze.Options{}, err
	}
	classGraph, err := analyze.LoadClassGraph(b.classGraphPath)
	if err != nil {
		return analyze.Options{}, err
	}
	diagnostics, err := analyze.LoadInstrumentationDiagnostics(b.diagnosticsPath)
	if err != nil {
		return analyze.Options{}, err
	}
	diCatalog, err := analyze.LoadDependencyInjectionCatalog(b.diCatalogPath)
	if err != nil {
		return analyze.Options{}, err
	}
	componentCatalog, err := analyze.LoadAndroidComponentCatalog(b.componentCatalogPath)
	if err != nil {
		return analyze.Options{}, err
	}
	databaseEvidence, err := analyze.LoadDatabaseEvidence(b.databaseEvidencePath)
	if err != nil {
		return analyze.Options{}, err
	}
	return analyze.Options{
		Filter:                     b.filter,
		ObfuscationMap:             nameMapping,
		ClassGraph:                 classGraph,
		InstrumentationDiagnostics: diagnostics,
		DependencyInjectionCatalog: diCatalog,
		AndroidComponentCatalog:    componentCatalog,
		DatabaseEvidence:           databaseEvidence,
		ArtifactDirectory:          b.artifactsDir,
		ArtifactSymbolNamespace:    append([]byte(nil), b.artifactNS...),
	}, nil
}

func (b analysisOptionsBuilder) withExplicitArtifactSidecarsForNamespaces(
	classGraphPath string,
	diagnosticsPath string,
	namespaces map[string]struct{},
) (analysisOptionsBuilder, error) {
	inputs := [...]struct {
		label string
		path  string
	}{
		{label: "class-graph", path: classGraphPath},
		{label: "instrumentation-diagnostics", path: diagnosticsPath},
	}
	var verifiedNamespace []byte
	for _, input := range inputs {
		if input.path == "" {
			continue
		}
		metadataPath := filepath.Join(filepath.Dir(input.path), "artifact-metadata.json")
		info, err := os.Stat(metadataPath)
		if err != nil || info.IsDir() || info.Size() == 0 {
			b.artifactNS = nil
			return b, nil
		}
		namespace, err := analyze.ReadArtifactMetadataNamespace(metadataPath)
		if err != nil {
			return b, fmt.Errorf(
				"explicit --%s %q has unreadable artifact identity sidecar %q: %w",
				input.label,
				input.path,
				metadataPath,
				err,
			)
		}
		if len(namespaces) > 0 && !namespaceMatchesLogs(namespace, namespaces) {
			return b, fmt.Errorf(
				"explicit --%s %q does not match the input .jhlog symbol namespace",
				input.label,
				input.path,
			)
		}
		if verifiedNamespace != nil && !bytes.Equal(verifiedNamespace, namespace) {
			return b, fmt.Errorf("explicit analysis artifacts have different symbol namespaces")
		}
		verifiedNamespace = namespace
	}
	if verifiedNamespace != nil {
		b.artifactNS = append(b.artifactNS[:0], verifiedNamespace...)
	}
	return b, nil
}

func namespaceMatchesLogs(namespace []byte, namespaces map[string]struct{}) bool {
	if len(namespaces) != 1 {
		return false
	}
	_, matches := namespaces[string(namespace)]
	return matches
}

type androidArtifactBundle struct {
	directory        string
	metadata         string
	classGraph       string
	diagnostics      string
	diCatalog        string
	componentCatalog string
	symbolNamespace  []byte
}

func (b analysisOptionsBuilder) withExplicitArtifactsForNamespaces(
	namespaces map[string]struct{},
) (analysisOptionsBuilder, error) {
	directory := strings.TrimSpace(b.artifactsDir)
	if directory == "" {
		return b, nil
	}
	bundle, err := loadAndroidArtifactBundle(directory)
	if err != nil {
		return b, err
	}
	if len(namespaces) > 0 && !artifactNamespaceMatches(bundle, namespaces) {
		return b, fmt.Errorf(
			"artifact directory %q does not match the input .jhlog symbol namespace; rebuild the same app variant or pass its exact artifact directory",
			directory,
		)
	}
	b.artifactsDir = bundle.directory
	classGraphFromBundle := b.classGraphPath == ""
	diagnosticsFromBundle := b.diagnosticsPath == ""
	if b.classGraphPath == "" {
		b.classGraphPath = bundle.classGraph
	}
	if b.diagnosticsPath == "" {
		b.diagnosticsPath = bundle.diagnostics
	}
	if classGraphFromBundle && diagnosticsFromBundle {
		b.artifactNS = append([]byte(nil), bundle.symbolNamespace...)
	}
	if b.diCatalogPath == "" && bundle.diCatalog != "" {
		b.diCatalogPath = bundle.diCatalog
	}
	if b.componentCatalogPath == "" && bundle.componentCatalog != "" {
		b.componentCatalogPath = bundle.componentCatalog
	}
	return b, nil
}

func artifactNamespaceMatches(bundle androidArtifactBundle, namespaces map[string]struct{}) bool {
	if len(namespaces) == 0 {
		return true
	}
	return namespaceMatchesLogs(bundle.symbolNamespace, namespaces)
}

func loadAndroidArtifactBundle(directory string) (androidArtifactBundle, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return androidArtifactBundle{}, fmt.Errorf("resolve --artifacts-dir %q: %w", directory, err)
	}
	bundle := androidArtifactBundle{
		directory:  absolute,
		metadata:   filepath.Join(absolute, "artifact-metadata.json"),
		classGraph: filepath.Join(absolute, "class-graph.jsonl"),
		diagnostics: filepath.Join(
			absolute,
			"instrumentation-diagnostics.jsonl",
		),
	}
	for _, required := range []struct {
		label string
		path  string
	}{
		{label: "artifact-metadata.json", path: bundle.metadata},
		{label: "class-graph.jsonl", path: bundle.classGraph},
		{label: "instrumentation-diagnostics.jsonl", path: bundle.diagnostics},
	} {
		label := required.label
		path := required.path
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			return androidArtifactBundle{}, fmt.Errorf(
				"invalid Jank Hunter --artifacts-dir %q: required %s is missing or empty",
				directory,
				label,
			)
		}
	}
	namespace, err := analyze.ReadArtifactMetadataNamespace(bundle.metadata)
	if err != nil {
		return androidArtifactBundle{}, fmt.Errorf("invalid Jank Hunter --artifacts-dir %q: artifact-metadata.json identity cannot be read", directory)
	}
	bundle.symbolNamespace = append([]byte(nil), namespace...)
	diCatalog := filepath.Join(absolute, "di-catalog.jsonl")
	if info, statErr := os.Stat(diCatalog); statErr == nil && !info.IsDir() && info.Size() > 0 {
		bundle.diCatalog = diCatalog
	}
	componentCatalog := filepath.Join(absolute, "android-components-catalog.jsonl")
	if info, statErr := os.Stat(componentCatalog); statErr == nil && !info.IsDir() && info.Size() > 0 {
		bundle.componentCatalog = componentCatalog
	}
	return bundle, nil
}

func logArtifactNamespaces(paths []string) (map[string]struct{}, error) {
	namespaces := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		header, err := jhlog.ReadSessionHeader(path)
		if err != nil {
			return nil, err
		}
		namespaces[string(header.SymbolNamespace)] = struct{}{}
	}
	return namespaces, nil
}

func diagnosticsAvailable(options analyze.Options) bool {
	return options.InstrumentationDiagnostics != nil && options.InstrumentationDiagnostics.Available
}

func dependencyInjectionAvailable(options analyze.Options) bool {
	return options.DependencyInjectionCatalog != nil && options.DependencyInjectionCatalog.Available
}
