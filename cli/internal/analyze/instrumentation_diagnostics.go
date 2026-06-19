package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type InstrumentationDiagnostics struct {
	Available            bool
	Source               string
	ClassCount           int
	MethodCount          int
	IgnoredMethodCount   int
	AnnotatedMethodCount int
	HookCount            uint64
	SkippedMethods       []InstrumentationSkippedSummary
	Hooks                []InstrumentationHookSummary
	Decisions            []InstrumentationDecisionSummary
	Annotations          []InstrumentationAnnotationSummary
	Classes              []InstrumentationClassDiagnostic
	TopClasses           []InstrumentationClassDiagnostic
	Warnings             []string
}

type InstrumentationSkippedSummary struct {
	Reason string
	Count  uint64
}

type InstrumentationHookSummary struct {
	Intent    string
	Signature string
	Bridge    string
	Method    string
	Line      int
	Count     uint64
}

type InstrumentationDecisionSummary struct {
	Kind   string
	Module string
	Family string
	Reason string
	Method string
	Line   int
	Count  uint64
}

type InstrumentationAnnotationSummary struct {
	Owner  string
	Screen string
	Flow   string
	Trace  string
	Count  uint64
}

type InstrumentationClassDiagnostic struct {
	ClassName        string
	Methods          int
	IgnoredMethods   int
	AnnotatedMethods int
	HookCount        uint64
	SkippedMethods   []InstrumentationSkippedSummary
	Hooks            []InstrumentationHookSummary
	Decisions        []InstrumentationDecisionSummary
	Annotations      []InstrumentationAnnotationSummary
}

func LoadInstrumentationDiagnostics(path string) (*InstrumentationDiagnostics, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	builder := instrumentationDiagnosticsBuilder{
		source:      path,
		skipped:     map[string]uint64{},
		hooks:       map[instrumentationHookKey]uint64{},
		decisions:   map[instrumentationDecisionKey]uint64{},
		annotations: map[instrumentationAnnotationKey]uint64{},
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record instrumentationDiagnosticsRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse instrumentation diagnostics line %d: %w", lineNumber, err)
		}
		if err := validateArtifactFormat(path, "instrumentation diagnostics", record.Format, InstrumentationDiagnosticsFormat); err != nil {
			return nil, fmt.Errorf("parse instrumentation diagnostics line %d: %w", lineNumber, err)
		}
		builder.add(record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return builder.finish(), nil
}

type instrumentationDiagnosticsBuilder struct {
	source      string
	classes     []InstrumentationClassDiagnostic
	skipped     map[string]uint64
	hooks       map[instrumentationHookKey]uint64
	decisions   map[instrumentationDecisionKey]uint64
	annotations map[instrumentationAnnotationKey]uint64
	methods     int
	ignored     int
	annotated   int
	hookCount   uint64
	warnings    []string
}

func (b *instrumentationDiagnosticsBuilder) add(record instrumentationDiagnosticsRecord) {
	class := InstrumentationClassDiagnostic{
		ClassName:        record.ClassName,
		Methods:          record.Methods,
		IgnoredMethods:   record.IgnoredMethods,
		AnnotatedMethods: record.AnnotatedMethods,
		SkippedMethods:   skippedSummaries(record.SkippedMethods),
		Hooks:            hookSummaries(record.Hooks),
		Decisions:        decisionSummaries(record.Decisions),
		Annotations:      annotationSummaries(record.Annotations),
	}
	for _, item := range class.SkippedMethods {
		b.skipped[item.Reason] += item.Count
	}
	for _, item := range class.Hooks {
		class.HookCount += item.Count
		b.hookCount += item.Count
		b.hooks[instrumentationHookKey{
			intent:    item.Intent,
			signature: item.Signature,
			bridge:    item.Bridge,
			method:    item.Method,
			line:      item.Line,
		}] += item.Count
	}
	for _, item := range class.Decisions {
		b.decisions[instrumentationDecisionKey{
			kind:   item.Kind,
			module: item.Module,
			family: item.Family,
			reason: item.Reason,
			method: item.Method,
			line:   item.Line,
		}] += item.Count
	}
	for _, item := range class.Annotations {
		b.annotations[instrumentationAnnotationKey{
			owner:  item.Owner,
			screen: item.Screen,
			flow:   item.Flow,
			trace:  item.Trace,
		}] += item.Count
	}
	b.methods += record.Methods
	b.ignored += record.IgnoredMethods
	b.annotated += record.AnnotatedMethods
	b.classes = append(b.classes, class)
}

func (b instrumentationDiagnosticsBuilder) finish() *InstrumentationDiagnostics {
	sort.SliceStable(b.classes, func(i, j int) bool {
		left := b.classes[i]
		right := b.classes[j]
		if left.HookCount != right.HookCount {
			return left.HookCount > right.HookCount
		}
		if left.AnnotatedMethods != right.AnnotatedMethods {
			return left.AnnotatedMethods > right.AnnotatedMethods
		}
		if left.Methods != right.Methods {
			return left.Methods > right.Methods
		}
		return left.ClassName < right.ClassName
	})
	return &InstrumentationDiagnostics{
		Available:            true,
		Source:               b.source,
		ClassCount:           len(b.classes),
		MethodCount:          b.methods,
		IgnoredMethodCount:   b.ignored,
		AnnotatedMethodCount: b.annotated,
		HookCount:            b.hookCount,
		SkippedMethods:       skippedMapSummaries(b.skipped),
		Hooks:                hookMapSummaries(b.hooks),
		Decisions:            decisionMapSummaries(b.decisions),
		Annotations:          annotationMapSummaries(b.annotations),
		Classes:              b.classes,
		TopClasses:           limitInstrumentationClasses(b.classes, 200),
		Warnings:             b.warnings,
	}
}

type instrumentationDiagnosticsRecord struct {
	Format           int                               `json:"format"`
	ClassName        string                            `json:"class"`
	Methods          int                               `json:"methods"`
	IgnoredMethods   int                               `json:"ignoredMethods"`
	AnnotatedMethods int                               `json:"annotatedMethods"`
	SkippedMethods   []instrumentationSkippedRecord    `json:"skippedMethods"`
	Hooks            []instrumentationHookRecord       `json:"hooks"`
	Decisions        []instrumentationDecisionRecord   `json:"decisions"`
	Annotations      []instrumentationAnnotationRecord `json:"annotations"`
}

type instrumentationSkippedRecord struct {
	Reason string `json:"reason"`
	Count  uint64 `json:"count"`
}

type instrumentationHookRecord struct {
	Intent    string `json:"intent"`
	Signature string `json:"signature"`
	Bridge    string `json:"bridge"`
	Method    string `json:"method"`
	Line      int    `json:"line"`
	Count     uint64 `json:"count"`
}

type instrumentationDecisionRecord struct {
	Kind   string `json:"kind"`
	Module string `json:"module"`
	Family string `json:"family"`
	Reason string `json:"reason"`
	Method string `json:"method"`
	Line   int    `json:"line"`
	Count  uint64 `json:"count"`
}

type instrumentationAnnotationRecord struct {
	Owner  string `json:"owner"`
	Screen string `json:"screen"`
	Flow   string `json:"flow"`
	Trace  string `json:"trace"`
	Count  uint64 `json:"count"`
}

type instrumentationHookKey struct {
	intent    string
	signature string
	bridge    string
	method    string
	line      int
}

type instrumentationDecisionKey struct {
	kind   string
	module string
	family string
	reason string
	method string
	line   int
}

type instrumentationAnnotationKey struct {
	owner  string
	screen string
	flow   string
	trace  string
}

func skippedSummaries(records []instrumentationSkippedRecord) []InstrumentationSkippedSummary {
	out := make([]InstrumentationSkippedSummary, 0, len(records))
	for _, record := range records {
		out = append(out, InstrumentationSkippedSummary(record))
	}
	sortSkippedSummaries(out)
	return out
}

func hookSummaries(records []instrumentationHookRecord) []InstrumentationHookSummary {
	out := make([]InstrumentationHookSummary, 0, len(records))
	for _, record := range records {
		out = append(out, InstrumentationHookSummary(record))
	}
	sortHookSummaries(out)
	return out
}

func decisionSummaries(records []instrumentationDecisionRecord) []InstrumentationDecisionSummary {
	out := make([]InstrumentationDecisionSummary, 0, len(records))
	for _, record := range records {
		out = append(out, InstrumentationDecisionSummary(record))
	}
	sortDecisionSummaries(out)
	return out
}

func annotationSummaries(records []instrumentationAnnotationRecord) []InstrumentationAnnotationSummary {
	out := make([]InstrumentationAnnotationSummary, 0, len(records))
	for _, record := range records {
		out = append(out, InstrumentationAnnotationSummary(record))
	}
	sortAnnotationSummaries(out)
	return out
}

func skippedMapSummaries(values map[string]uint64) []InstrumentationSkippedSummary {
	out := make([]InstrumentationSkippedSummary, 0, len(values))
	for reason, count := range values {
		out = append(out, InstrumentationSkippedSummary{Reason: reason, Count: count})
	}
	sortSkippedSummaries(out)
	return out
}

func hookMapSummaries(values map[instrumentationHookKey]uint64) []InstrumentationHookSummary {
	out := make([]InstrumentationHookSummary, 0, len(values))
	for key, count := range values {
		out = append(out, InstrumentationHookSummary{
			Intent:    key.intent,
			Signature: key.signature,
			Bridge:    key.bridge,
			Method:    key.method,
			Line:      key.line,
			Count:     count,
		})
	}
	sortHookSummaries(out)
	return out
}

func decisionMapSummaries(values map[instrumentationDecisionKey]uint64) []InstrumentationDecisionSummary {
	out := make([]InstrumentationDecisionSummary, 0, len(values))
	for key, count := range values {
		out = append(out, InstrumentationDecisionSummary{
			Kind:   key.kind,
			Module: key.module,
			Family: key.family,
			Reason: key.reason,
			Method: key.method,
			Line:   key.line,
			Count:  count,
		})
	}
	sortDecisionSummaries(out)
	return out
}

func annotationMapSummaries(values map[instrumentationAnnotationKey]uint64) []InstrumentationAnnotationSummary {
	out := make([]InstrumentationAnnotationSummary, 0, len(values))
	for key, count := range values {
		out = append(out, InstrumentationAnnotationSummary{
			Owner:  key.owner,
			Screen: key.screen,
			Flow:   key.flow,
			Trace:  key.trace,
			Count:  count,
		})
	}
	sortAnnotationSummaries(out)
	return out
}

func sortSkippedSummaries(values []InstrumentationSkippedSummary) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		return values[i].Reason < values[j].Reason
	})
}

func sortHookSummaries(values []InstrumentationHookSummary) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		if values[i].Intent != values[j].Intent {
			return values[i].Intent < values[j].Intent
		}
		if values[i].Signature != values[j].Signature {
			return values[i].Signature < values[j].Signature
		}
		if values[i].Bridge != values[j].Bridge {
			return values[i].Bridge < values[j].Bridge
		}
		if values[i].Method != values[j].Method {
			return values[i].Method < values[j].Method
		}
		return values[i].Line < values[j].Line
	})
}

func sortDecisionSummaries(values []InstrumentationDecisionSummary) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		if values[i].Module != values[j].Module {
			return values[i].Module < values[j].Module
		}
		if values[i].Family != values[j].Family {
			return values[i].Family < values[j].Family
		}
		if values[i].Reason != values[j].Reason {
			return values[i].Reason < values[j].Reason
		}
		if values[i].Method != values[j].Method {
			return values[i].Method < values[j].Method
		}
		return values[i].Line < values[j].Line
	})
}

func sortAnnotationSummaries(values []InstrumentationAnnotationSummary) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		if values[i].Owner != values[j].Owner {
			return values[i].Owner < values[j].Owner
		}
		if values[i].Screen != values[j].Screen {
			return values[i].Screen < values[j].Screen
		}
		if values[i].Flow != values[j].Flow {
			return values[i].Flow < values[j].Flow
		}
		return values[i].Trace < values[j].Trace
	})
}

func limitInstrumentationClasses(values []InstrumentationClassDiagnostic, limit int) []InstrumentationClassDiagnostic {
	if len(values) <= limit {
		return values
	}
	out := make([]InstrumentationClassDiagnostic, limit)
	copy(out, values[:limit])
	return out
}
