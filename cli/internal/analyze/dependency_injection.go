package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const DependencyInjectionDisclaimer = "Build-time DI-связь. Это не ссылка удержания, не runtime-вызов и не доказательство утечки. DI-данные не влияют на score, severity или evidence."

type DependencyInjectionCatalog struct {
	Available  bool
	Source     string
	Variant    string
	Classes    []DependencyInjectionClass
	Edges      []DependencyInjectionEdge
	Frameworks []DependencyInjectionFrameworkSummary
	Warnings   []string
}

type DependencyInjectionClass struct {
	Name       string
	Framework  string
	Roles      []string
	Generated  bool
	Scopes     []string
	Components []string
}

type DependencyInjectionEdge struct {
	Consumer      string
	Dependency    string
	Framework     string
	InjectionKind string
	Site          string
	Qualifiers    []string
	Resolution    string
}

type DependencyInjectionFrameworkSummary struct {
	Name    string
	Classes int
	Edges   int
}

type DependencyInjectionReport struct {
	Available        bool
	Source           string
	Variant          string
	Disclaimer       string
	ClassCount       int
	EdgeCount        int
	ShownClassCount  int
	ShownEdgeCount   int
	ClassesTruncated bool
	EdgesTruncated   bool
	Frameworks       []DependencyInjectionFrameworkSummary
	Classes          []DependencyInjectionReportClass
	Edges            []DependencyInjectionReportEdge
	Warnings         []string
}

type DependencyInjectionReportClass struct {
	DependencyInjectionClass
	Observed []string
}

type DependencyInjectionReportEdge struct {
	DependencyInjectionEdge
	ConsumerObserved   bool
	DependencyObserved bool
}

type dependencyInjectionRecord struct {
	Format         int      `json:"format"`
	Kind           string   `json:"kind"`
	Variant        string   `json:"variant"`
	Semantics      string   `json:"semantics"`
	EdgeDirection  string   `json:"edgeDirection"`
	RuntimeTracing *bool    `json:"runtimeTracing"`
	AffectsScore   *bool    `json:"affectsScore"`
	Name           string   `json:"name"`
	Framework      string   `json:"framework"`
	Roles          []string `json:"roles"`
	Generated      bool     `json:"generated"`
	Scopes         []string `json:"scopes"`
	Components     []string `json:"components"`
	Consumer       string   `json:"consumer"`
	Dependency     string   `json:"dependency"`
	InjectionKind  string   `json:"injectionKind"`
	Site           string   `json:"site"`
	Qualifiers     []string `json:"qualifiers"`
	Resolution     string   `json:"resolution"`
}

func LoadDependencyInjectionCatalog(path string) (*DependencyInjectionCatalog, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	classRecords := map[string]DependencyInjectionClass{}
	edgeRecords := map[string]DependencyInjectionEdge{}
	var variant string
	metadataSeen := false
	recordCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), dependencyInjectionMaxLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		recordCount++
		if recordCount > dependencyInjectionMaxRecords {
			return nil, fmt.Errorf("%s: DI catalog exceeds the record limit %d", path, dependencyInjectionMaxRecords)
		}
		var record dependencyInjectionRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse DI catalog line %d: %w", lineNumber, err)
		}
		if err := validateArtifactFormat(path, "dependency injection catalog", record.Format, DependencyInjectionCatalogFormat); err != nil {
			return nil, fmt.Errorf("parse DI catalog line %d: %w", lineNumber, err)
		}
		if recordCount == 1 && record.Kind != "metadata" {
			return nil, fmt.Errorf("parse DI catalog line %d: metadata must be the first record", lineNumber)
		}
		switch record.Kind {
		case "metadata":
			if metadataSeen {
				return nil, fmt.Errorf("parse DI catalog line %d: duplicate metadata record", lineNumber)
			}
			if err := validateDependencyInjectionMetadata(record); err != nil {
				return nil, fmt.Errorf("parse DI catalog line %d: %w", lineNumber, err)
			}
			metadataSeen = true
			variant = strings.TrimSpace(record.Variant)
		case "class":
			classRecord, err := dependencyInjectionClassFromRecord(record)
			if err != nil {
				return nil, fmt.Errorf("parse DI catalog line %d: %w", lineNumber, err)
			}
			key := classRecord.Name + "\x00" + classRecord.Framework
			if previous, ok := classRecords[key]; ok {
				classRecord.Roles = mergeDependencyInjectionStrings(previous.Roles, classRecord.Roles)
				classRecord.Scopes = mergeDependencyInjectionStrings(previous.Scopes, classRecord.Scopes)
				classRecord.Components = mergeDependencyInjectionStrings(previous.Components, classRecord.Components)
				classRecord.Generated = previous.Generated || classRecord.Generated
			}
			classRecords[key] = classRecord
		case "edge":
			edge, err := dependencyInjectionEdgeFromRecord(record)
			if err != nil {
				return nil, fmt.Errorf("parse DI catalog line %d: %w", lineNumber, err)
			}
			if edge.Consumer == edge.Dependency {
				continue
			}
			edgeRecords[dependencyInjectionEdgeKey(edge)] = edge
		default:
			return nil, fmt.Errorf("parse DI catalog line %d: unsupported record kind %q", lineNumber, record.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !metadataSeen {
		return nil, fmt.Errorf("%s: DI catalog metadata is missing", path)
	}

	catalog := &DependencyInjectionCatalog{
		Available: true,
		Source:    path,
		Variant:   variant,
		Classes:   make([]DependencyInjectionClass, 0, len(classRecords)),
		Edges:     make([]DependencyInjectionEdge, 0, len(edgeRecords)),
	}
	for _, classRecord := range classRecords {
		catalog.Classes = append(catalog.Classes, classRecord)
	}
	for _, edge := range edgeRecords {
		catalog.Edges = append(catalog.Edges, edge)
	}
	sort.Slice(catalog.Classes, func(i, j int) bool {
		if catalog.Classes[i].Name == catalog.Classes[j].Name {
			return catalog.Classes[i].Framework < catalog.Classes[j].Framework
		}
		return catalog.Classes[i].Name < catalog.Classes[j].Name
	})
	sort.Slice(catalog.Edges, func(i, j int) bool {
		left, right := catalog.Edges[i], catalog.Edges[j]
		if left.Consumer != right.Consumer {
			return left.Consumer < right.Consumer
		}
		if left.Dependency != right.Dependency {
			return left.Dependency < right.Dependency
		}
		if left.InjectionKind != right.InjectionKind {
			return left.InjectionKind < right.InjectionKind
		}
		return left.Site < right.Site
	})
	catalog.Frameworks = dependencyInjectionFrameworkSummaries(catalog.Classes, catalog.Edges)
	if dependencyInjectionFrameworkPresent(catalog.Frameworks, "koin") {
		catalog.Warnings = append(
			catalog.Warnings,
			"Koin: каталог покрывает аннотации и KSP-generated bindings; произвольный runtime DSL намеренно не интерпретируется.",
		)
	}
	return catalog, nil
}

func BuildDependencyInjectionReport(catalog *DependencyInjectionCatalog, summary Summary) DependencyInjectionReport {
	if catalog == nil || !catalog.Available {
		return DependencyInjectionReport{}
	}
	observed := dependencyInjectionObservedClasses(summary)
	classes := make([]DependencyInjectionReportClass, 0, len(catalog.Classes))
	for _, classRecord := range catalog.Classes {
		classes = append(classes, DependencyInjectionReportClass{
			DependencyInjectionClass: classRecord,
			Observed:                 dependencyInjectionObservations(classRecord.Name, observed),
		})
	}
	sort.SliceStable(classes, func(i, j int) bool {
		leftObserved := len(classes[i].Observed) > 0
		rightObserved := len(classes[j].Observed) > 0
		if leftObserved != rightObserved {
			return leftObserved
		}
		if classes[i].Name == classes[j].Name {
			return classes[i].Framework < classes[j].Framework
		}
		return classes[i].Name < classes[j].Name
	})

	edges := make([]DependencyInjectionReportEdge, 0, len(catalog.Edges))
	for _, edge := range catalog.Edges {
		edges = append(edges, DependencyInjectionReportEdge{
			DependencyInjectionEdge: edge,
			ConsumerObserved:        len(dependencyInjectionObservations(edge.Consumer, observed)) > 0,
			DependencyObserved:      len(dependencyInjectionObservations(edge.Dependency, observed)) > 0,
		})
	}
	sort.SliceStable(edges, func(i, j int) bool {
		leftObserved := edges[i].ConsumerObserved || edges[i].DependencyObserved
		rightObserved := edges[j].ConsumerObserved || edges[j].DependencyObserved
		if leftObserved != rightObserved {
			return leftObserved
		}
		left, right := edges[i].DependencyInjectionEdge, edges[j].DependencyInjectionEdge
		if left.Consumer != right.Consumer {
			return left.Consumer < right.Consumer
		}
		if left.Dependency != right.Dependency {
			return left.Dependency < right.Dependency
		}
		return left.Site < right.Site
	})

	report := DependencyInjectionReport{
		Available:  true,
		Source:     catalog.Source,
		Variant:    catalog.Variant,
		Disclaimer: DependencyInjectionDisclaimer,
		ClassCount: len(catalog.Classes),
		EdgeCount:  len(catalog.Edges),
		Frameworks: append([]DependencyInjectionFrameworkSummary(nil), catalog.Frameworks...),
		Warnings:   append([]string(nil), catalog.Warnings...),
	}
	if len(classes) > dependencyInjectionReportClassLimit {
		report.ClassesTruncated = true
		classes = classes[:dependencyInjectionReportClassLimit]
	}
	if len(edges) > dependencyInjectionReportEdgeLimit {
		report.EdgesTruncated = true
		edges = edges[:dependencyInjectionReportEdgeLimit]
	}
	report.Classes = classes
	report.Edges = edges
	report.ShownClassCount = len(classes)
	report.ShownEdgeCount = len(edges)
	return report
}

func validateDependencyInjectionMetadata(record dependencyInjectionRecord) error {
	if record.Semantics != "build_time_di" {
		return fmt.Errorf("unsupported DI semantics %q", record.Semantics)
	}
	if record.EdgeDirection != "consumer_to_dependency" {
		return fmt.Errorf("unsupported DI edge direction %q", record.EdgeDirection)
	}
	if record.RuntimeTracing == nil || *record.RuntimeTracing {
		return fmt.Errorf("DI catalog must declare runtimeTracing=false")
	}
	if record.AffectsScore == nil || *record.AffectsScore {
		return fmt.Errorf("DI catalog must declare affectsScore=false")
	}
	return nil
}

func dependencyInjectionClassFromRecord(record dependencyInjectionRecord) (DependencyInjectionClass, error) {
	name := normalizeClassName(record.Name)
	if name == "" {
		return DependencyInjectionClass{}, fmt.Errorf("DI class name is empty")
	}
	if !validDependencyInjectionFramework(record.Framework) {
		return DependencyInjectionClass{}, fmt.Errorf("unsupported DI framework %q", record.Framework)
	}
	return DependencyInjectionClass{
		Name:       name,
		Framework:  record.Framework,
		Roles:      normalizeDependencyInjectionStrings(record.Roles),
		Generated:  record.Generated,
		Scopes:     normalizeDependencyInjectionStrings(record.Scopes),
		Components: normalizeDependencyInjectionStrings(record.Components),
	}, nil
}

func dependencyInjectionEdgeFromRecord(record dependencyInjectionRecord) (DependencyInjectionEdge, error) {
	edge := DependencyInjectionEdge{
		Consumer:      normalizeClassName(record.Consumer),
		Dependency:    normalizeClassName(record.Dependency),
		Framework:     strings.TrimSpace(record.Framework),
		InjectionKind: strings.TrimSpace(record.InjectionKind),
		Site:          strings.TrimSpace(record.Site),
		Qualifiers:    normalizeDependencyInjectionStrings(record.Qualifiers),
		Resolution:    strings.TrimSpace(record.Resolution),
	}
	if edge.Consumer == "" || edge.Dependency == "" {
		return DependencyInjectionEdge{}, fmt.Errorf("DI edge consumer and dependency are required")
	}
	if !validDependencyInjectionFramework(edge.Framework) {
		return DependencyInjectionEdge{}, fmt.Errorf("unsupported DI framework %q", edge.Framework)
	}
	if !validDependencyInjectionKind(edge.InjectionKind) {
		return DependencyInjectionEdge{}, fmt.Errorf("unsupported DI injection kind %q", edge.InjectionKind)
	}
	if edge.Site == "" {
		return DependencyInjectionEdge{}, fmt.Errorf("DI edge site is empty")
	}
	if !validDependencyInjectionResolution(edge.Resolution) {
		return DependencyInjectionEdge{}, fmt.Errorf("unsupported DI resolution %q", edge.Resolution)
	}
	return edge, nil
}

func dependencyInjectionObservedClasses(summary Summary) map[string]map[string]struct{} {
	observed := map[string]map[string]struct{}{}
	add := func(className, label string) {
		className = normalizeClassName(className)
		if className == "" {
			return
		}
		for _, candidate := range dependencyInjectionClassAndAncestors(className) {
			labels := observed[candidate]
			if labels == nil {
				labels = map[string]struct{}{}
				observed[candidate] = labels
			}
			labels[label] = struct{}{}
		}
	}
	for _, problem := range summary.CodeProblems {
		add(problem.ClassName, "есть отдельный runtime-сигнал")
	}
	for _, leak := range summary.MemoryLeaks {
		add(leak.ClassName, "класс отдельно присутствует в memory-анализе")
	}
	for _, node := range summary.Influence.TopNodes {
		add(node.ClassName, "класс отдельно присутствует в графе влияния")
	}
	for _, call := range summary.RuntimeCalls {
		add(classFromOwner(call.Caller), "есть отдельный runtime-вызов")
		add(classFromOwner(call.Callee), "есть отдельный runtime-вызов")
	}
	for _, owner := range summary.Owners {
		add(classFromOwner(owner.Owner), "есть отдельная runtime-атрибуция")
	}
	return observed
}

func dependencyInjectionObservations(className string, observed map[string]map[string]struct{}) []string {
	className = normalizeClassName(className)
	labels := map[string]struct{}{}
	for _, candidate := range dependencyInjectionClassAndAncestors(className) {
		for label := range observed[candidate] {
			labels[label] = struct{}{}
		}
	}
	out := make([]string, 0, len(labels))
	for label := range labels {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func dependencyInjectionClassAndAncestors(className string) []string {
	className = normalizeClassName(className)
	if className == "" {
		return nil
	}
	values := []string{className}
	for strings.Contains(className, "$") {
		className = className[:strings.LastIndex(className, "$")]
		values = append(values, className)
	}
	return values
}

func dependencyInjectionFrameworkSummaries(
	classes []DependencyInjectionClass,
	edges []DependencyInjectionEdge,
) []DependencyInjectionFrameworkSummary {
	byName := map[string]DependencyInjectionFrameworkSummary{}
	for _, classRecord := range classes {
		item := byName[classRecord.Framework]
		item.Name = classRecord.Framework
		item.Classes++
		byName[classRecord.Framework] = item
	}
	for _, edge := range edges {
		item := byName[edge.Framework]
		item.Name = edge.Framework
		item.Edges++
		byName[edge.Framework] = item
	}
	out := make([]DependencyInjectionFrameworkSummary, 0, len(byName))
	for _, item := range byName {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func dependencyInjectionEdgeKey(edge DependencyInjectionEdge) string {
	return strings.Join([]string{
		edge.Consumer,
		edge.Dependency,
		edge.Framework,
		edge.InjectionKind,
		edge.Site,
		strings.Join(edge.Qualifiers, "\x1f"),
		edge.Resolution,
	}, "\x00")
}

func normalizeDependencyInjectionStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "/", "."))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeDependencyInjectionStrings(left, right []string) []string {
	return normalizeDependencyInjectionStrings(append(append([]string(nil), left...), right...))
}

func validDependencyInjectionFramework(value string) bool {
	switch value {
	case "dagger2", "hilt", "koin":
		return true
	default:
		return false
	}
}

func validDependencyInjectionKind(value string) bool {
	switch value {
	case "constructor", "field", "method", "provider", "binding", "entry_point", "generated_factory", "members_injector", "koin_definition":
		return true
	default:
		return false
	}
}

func validDependencyInjectionResolution(value string) bool {
	switch value {
	case "declared", "generated_confirmed", "inferred":
		return true
	default:
		return false
	}
}

func dependencyInjectionFrameworkPresent(values []DependencyInjectionFrameworkSummary, name string) bool {
	for _, value := range values {
		if value.Name == name {
			return true
		}
	}
	return false
}

const (
	dependencyInjectionMaxLineBytes     = 4 * 1024 * 1024
	dependencyInjectionMaxRecords       = 250_000
	dependencyInjectionReportClassLimit = 500
	dependencyInjectionReportEdgeLimit  = 2_000
)
