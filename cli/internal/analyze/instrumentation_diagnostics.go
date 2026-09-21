package analyze

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	instrumentationDiagnosticsMaxFileBytes = 512 << 20
	instrumentationDiagnosticsMaxLineBytes = 10 << 20
	instrumentationDiagnosticsMaxRecords   = 250_000
	instrumentationDiagnosticsMaxDetails   = 2_000_000
	instrumentationDiagnosticsMaxMethods   = 65_535
	instrumentationDiagnosticsMaxTextBytes = 65_535
)

type InstrumentationDiagnostics struct {
	sourceIdentity       artifactSourceIdentity
	Available            bool
	Source               string
	ClassCount           int
	MethodCount          int
	IgnoredMethodCount   int
	AnnotatedMethodCount int
	MethodFilterIncluded int
	MethodFilterExcluded int
	HookCount            uint64
	MethodFilterReasons  []InstrumentationSkippedSummary
	SkippedMethods       []InstrumentationSkippedSummary
	Hooks                []InstrumentationHookSummary
	Decisions            []InstrumentationDecisionSummary
	Annotations          []InstrumentationAnnotationSummary
	Classes              []InstrumentationClassDiagnostic
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
	Detail string
	Line   int
	Count  uint64
}

type InstrumentationAnnotationSummary struct {
	Owner             string
	Screen            string
	Operation         string
	OperationKind     string
	OperationBudgetMS uint64
	Count             uint64
}

type InstrumentationClassDiagnostic struct {
	Pass                 string
	Passes               []InstrumentationClassDiagnostic
	ClassName            string
	Methods              int
	IgnoredMethods       int
	AnnotatedMethods     int
	MethodFilterIncluded int
	MethodFilterExcluded int
	HookCount            uint64
	MethodFilterReasons  []InstrumentationSkippedSummary
	SkippedMethods       []InstrumentationSkippedSummary
	Hooks                []InstrumentationHookSummary
	Decisions            []InstrumentationDecisionSummary
	Annotations          []InstrumentationAnnotationSummary
}

func LoadInstrumentationDiagnostics(path string) (*InstrumentationDiagnostics, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	digest := sha256.New()
	input, err := openBoundedTextInput(
		path,
		"instrumentation diagnostics",
		instrumentationDiagnosticsMaxFileBytes,
		64*1024,
		instrumentationDiagnosticsMaxLineBytes,
		digest,
	)
	if err != nil {
		return nil, err
	}
	defer input.Close()

	builder := instrumentationDiagnosticsBuilder{
		source:              path,
		methodFilterReasons: map[string]uint64{},
		skipped:             map[string]uint64{},
		hooks:               map[instrumentationHookKey]uint64{},
		decisions:           map[instrumentationDecisionKey]uint64{},
		annotations:         map[instrumentationAnnotationKey]uint64{},
	}
	scanner := input.Scanner
	lineNumber := 0
	detailCount := 0
	seenClasses := make(map[string]uint8)
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if lineNumber > instrumentationDiagnosticsMaxRecords {
			return nil, fmt.Errorf("%s: instrumentation diagnostics exceed record limit %d", path, instrumentationDiagnosticsMaxRecords)
		}
		var record instrumentationDiagnosticsRecord
		if err := decodeStrictJSON(line, &record); err != nil {
			return nil, fmt.Errorf("parse instrumentation diagnostics line %d: %w", lineNumber, err)
		}
		if err := validateInstrumentationPass(record); err != nil {
			return nil, fmt.Errorf("parse instrumentation diagnostics line %d: %w", lineNumber, err)
		}
		if err := validateInstrumentationDiagnosticsRecord(record); err != nil {
			return nil, fmt.Errorf("parse instrumentation diagnostics line %d: %w", lineNumber, err)
		}
		bit := instrumentationPassBit(record.Pass)
		seen := seenClasses[record.ClassName]
		if seen&bit != 0 || (seen != 0 && (bit == 1 || seen&1 != 0)) {
			return nil, fmt.Errorf("parse instrumentation diagnostics line %d: duplicate class %q or conflicting pass %q", lineNumber, record.ClassName, record.Pass)
		}
		seenClasses[record.ClassName] = seen | bit
		recordDetails := len(record.MethodFilterReasons) + len(record.SkippedMethods) + len(record.Hooks) +
			len(record.Decisions) + len(record.Annotations)
		if recordDetails > instrumentationDiagnosticsMaxDetails-detailCount {
			return nil, fmt.Errorf("%s: instrumentation diagnostics exceed detail limit %d", path, instrumentationDiagnosticsMaxDetails)
		}
		detailCount += recordDetails
		builder.add(record)
	}
	if err := input.Err(); err != nil {
		return nil, err
	}
	result := builder.finish()
	result.sourceIdentity = artifactSourceIdentity{path: path, digest: hex.EncodeToString(digest.Sum(nil))}
	return result, nil
}

type instrumentationDiagnosticsBuilder struct {
	source               string
	classes              []InstrumentationClassDiagnostic
	methodFilterReasons  map[string]uint64
	skipped              map[string]uint64
	hooks                map[instrumentationHookKey]uint64
	decisions            map[instrumentationDecisionKey]uint64
	annotations          map[instrumentationAnnotationKey]uint64
	methods              int
	ignored              int
	annotated            int
	methodFilterIncluded int
	methodFilterExcluded int
	hookCount            uint64
}

func (b *instrumentationDiagnosticsBuilder) add(record instrumentationDiagnosticsRecord) {
	class := InstrumentationClassDiagnostic{
		ClassName:            record.ClassName,
		Pass:                 record.Pass,
		Methods:              record.Methods,
		IgnoredMethods:       record.IgnoredMethods,
		AnnotatedMethods:     record.AnnotatedMethods,
		MethodFilterIncluded: record.MethodFilterIncluded,
		MethodFilterExcluded: record.MethodFilterExcluded,
		MethodFilterReasons:  skippedSummaries(record.MethodFilterReasons),
		SkippedMethods:       skippedSummaries(record.SkippedMethods),
		Hooks:                hookSummaries(record.Hooks),
		Decisions:            decisionSummaries(record.Decisions),
		Annotations:          annotationSummaries(record.Annotations),
	}
	if record.Pass != "lifecycle" {
		for _, item := range class.MethodFilterReasons {
			b.methodFilterReasons[item.Reason] = saturatingUint64Sum(b.methodFilterReasons[item.Reason], item.Count)
		}
		for _, item := range class.SkippedMethods {
			b.skipped[item.Reason] = saturatingUint64Sum(b.skipped[item.Reason], item.Count)
		}
	}
	for _, item := range class.Hooks {
		class.HookCount = saturatingUint64Sum(class.HookCount, item.Count)
		b.hookCount = saturatingUint64Sum(b.hookCount, item.Count)
		key := instrumentationHookKey{
			intent:    item.Intent,
			signature: item.Signature,
			bridge:    item.Bridge,
			method:    item.Method,
			line:      item.Line,
		}
		b.hooks[key] = saturatingUint64Sum(b.hooks[key], item.Count)
	}
	for _, item := range class.Decisions {
		key := instrumentationDecisionKey{
			kind:   item.Kind,
			module: item.Module,
			family: item.Family,
			reason: item.Reason,
			method: item.Method,
			detail: item.Detail,
			line:   item.Line,
		}
		b.decisions[key] = saturatingUint64Sum(b.decisions[key], item.Count)
	}
	if record.Pass != "lifecycle" {
		for _, item := range class.Annotations {
			key := instrumentationAnnotationKey{
				owner:             item.Owner,
				screen:            item.Screen,
				operation:         item.Operation,
				operationKind:     item.OperationKind,
				operationBudgetMS: item.OperationBudgetMS,
			}
			b.annotations[key] = saturatingUint64Sum(b.annotations[key], item.Count)
		}
		b.methods += record.Methods
		b.ignored += record.IgnoredMethods
		b.annotated += record.AnnotatedMethods
		b.methodFilterIncluded += record.MethodFilterIncluded
		b.methodFilterExcluded += record.MethodFilterExcluded
	}
	b.classes = append(b.classes, class)
}

func (b instrumentationDiagnosticsBuilder) finish() *InstrumentationDiagnostics {
	b.classes = combineInstrumentationPasses(b.classes)
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
	decisions := decisionMapSummaries(b.decisions)
	return &InstrumentationDiagnostics{
		Available:            true,
		Source:               b.source,
		ClassCount:           len(b.classes),
		MethodCount:          b.methods,
		IgnoredMethodCount:   b.ignored,
		AnnotatedMethodCount: b.annotated,
		MethodFilterIncluded: b.methodFilterIncluded,
		MethodFilterExcluded: b.methodFilterExcluded,
		HookCount:            b.hookCount,
		MethodFilterReasons:  skippedMapSummaries(b.methodFilterReasons),
		SkippedMethods:       skippedMapSummaries(b.skipped),
		Hooks:                hookMapSummaries(b.hooks),
		Decisions:            decisions,
		Annotations:          annotationMapSummaries(b.annotations),
		Classes:              b.classes,
		Warnings:             hierarchyResolutionWarnings(decisions),
	}
}

type instrumentationDiagnosticsRecord struct {
	Pass                 string                            `json:"pass"`
	Format               int                               `json:"format"`
	ClassName            string                            `json:"class"`
	Methods              int                               `json:"methods"`
	IgnoredMethods       int                               `json:"ignoredMethods"`
	AnnotatedMethods     int                               `json:"annotatedMethods"`
	MethodFilterIncluded int                               `json:"methodFilterIncluded"`
	MethodFilterExcluded int                               `json:"methodFilterExcluded"`
	MethodFilterReasons  []instrumentationSkippedRecord    `json:"methodFilterReasons"`
	SkippedMethods       []instrumentationSkippedRecord    `json:"skippedMethods"`
	Hooks                []instrumentationHookRecord       `json:"hooks"`
	Decisions            []instrumentationDecisionRecord   `json:"decisions"`
	Annotations          []instrumentationAnnotationRecord `json:"annotations"`
}

func validateInstrumentationDiagnosticsRecord(record instrumentationDiagnosticsRecord) error {
	if strings.TrimSpace(record.ClassName) == "" || len(record.ClassName) > instrumentationDiagnosticsMaxTextBytes {
		return fmt.Errorf("class must contain 1..%d bytes", instrumentationDiagnosticsMaxTextBytes)
	}
	if record.Methods < 0 || record.Methods > instrumentationDiagnosticsMaxMethods {
		return fmt.Errorf("methods must be between 0 and %d", instrumentationDiagnosticsMaxMethods)
	}
	if record.IgnoredMethods < 0 || record.IgnoredMethods > record.Methods {
		return fmt.Errorf("ignoredMethods must be between 0 and methods")
	}
	if record.AnnotatedMethods < 0 || record.AnnotatedMethods > record.Methods {
		return fmt.Errorf("annotatedMethods must be between 0 and methods")
	}
	if record.MethodFilterIncluded < 0 || record.MethodFilterExcluded < 0 {
		return fmt.Errorf("method filter counters must not be negative")
	}
	if record.MethodFilterIncluded > record.Methods-record.MethodFilterExcluded {
		return fmt.Errorf("method filter counters must not exceed methods")
	}
	if err := validateInstrumentationSkippedRecords("methodFilterReasons", record.MethodFilterReasons, record.Methods, false); err != nil {
		return err
	}
	if err := validateInstrumentationSkippedRecords("skippedMethods", record.SkippedMethods, record.Methods, true); err != nil {
		return err
	}
	for index, hook := range record.Hooks {
		if err := validateInstrumentationText(fmt.Sprintf("hooks[%d].intent", index), hook.Intent, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("hooks[%d].signature", index), hook.Signature, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("hooks[%d].bridge", index), hook.Bridge, false); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("hooks[%d].method", index), hook.Method, true); err != nil {
			return err
		}
		if hook.Line < 0 {
			return fmt.Errorf("hooks[%d].line must not be negative", index)
		}
		if hook.Count == 0 {
			return fmt.Errorf("hooks[%d].count must be positive", index)
		}
	}
	for index, decision := range record.Decisions {
		if err := validateInstrumentationText(fmt.Sprintf("decisions[%d].kind", index), decision.Kind, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("decisions[%d].module", index), decision.Module, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("decisions[%d].family", index), decision.Family, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("decisions[%d].reason", index), decision.Reason, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("decisions[%d].method", index), decision.Method, true); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("decisions[%d].detail", index), decision.Detail, false); err != nil {
			return err
		}
		if decision.Line < 0 {
			return fmt.Errorf("decisions[%d].line must not be negative", index)
		}
		if decision.Count == 0 {
			return fmt.Errorf("decisions[%d].count must be positive", index)
		}
	}
	var annotationCount uint64
	for index, annotation := range record.Annotations {
		if err := validateInstrumentationText(fmt.Sprintf("annotations[%d].owner", index), annotation.Owner, false); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("annotations[%d].screen", index), annotation.Screen, false); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("annotations[%d].operation", index), annotation.Operation, false); err != nil {
			return err
		}
		if err := validateInstrumentationText(fmt.Sprintf("annotations[%d].operationKind", index), annotation.OperationKind, false); err != nil {
			return err
		}
		if annotation.Count == 0 {
			return fmt.Errorf("annotations[%d].count must be positive", index)
		}
		annotationCount = saturatingUint64Sum(annotationCount, annotation.Count)
	}
	if annotationCount != uint64(record.AnnotatedMethods) {
		return fmt.Errorf("annotation counts must equal annotatedMethods")
	}
	return nil
}

func validateInstrumentationSkippedRecords(
	field string,
	records []instrumentationSkippedRecord,
	methodCount int,
	requireBoundedTotal bool,
) error {
	var total uint64
	for index, item := range records {
		if err := validateInstrumentationText(fmt.Sprintf("%s[%d].reason", field, index), item.Reason, true); err != nil {
			return err
		}
		if item.Count == 0 || item.Count > uint64(methodCount) {
			return fmt.Errorf("%s[%d].count must be between 1 and methods", field, index)
		}
		total = saturatingUint64Sum(total, item.Count)
	}
	if requireBoundedTotal && total > uint64(methodCount) {
		return fmt.Errorf("%s counts must not exceed methods", field)
	}
	return nil
}

func validateInstrumentationText(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > instrumentationDiagnosticsMaxTextBytes {
		return fmt.Errorf("%s exceeds %d bytes", field, instrumentationDiagnosticsMaxTextBytes)
	}
	return nil
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
	Detail string `json:"detail"`
	Line   int    `json:"line"`
	Count  uint64 `json:"count"`
}

type instrumentationAnnotationRecord struct {
	Owner             string `json:"owner"`
	Screen            string `json:"screen"`
	Operation         string `json:"operation"`
	OperationKind     string `json:"operationKind"`
	OperationBudgetMS uint64 `json:"operationBudgetMs"`
	Count             uint64 `json:"count"`
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
	detail string
	line   int
}

type instrumentationAnnotationKey struct {
	owner             string
	screen            string
	operation         string
	operationKind     string
	operationBudgetMS uint64
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
			Detail: key.detail,
			Line:   key.line,
			Count:  count,
		})
	}
	sortDecisionSummaries(out)
	return out
}

func hierarchyResolutionWarnings(decisions []InstrumentationDecisionSummary) []string {
	const maxWarnings = 20
	warnings := make([]string, 0, min(len(decisions), maxWarnings+1))
	omitted := 0
	for _, decision := range decisions {
		if decision.Kind != "warning" || decision.Module != "class_hierarchy" {
			continue
		}
		if len(warnings) >= maxWarnings {
			omitted++
			continue
		}
		if decision.Reason == "metadata_failures_omitted" {
			warnings = append(warnings, fmt.Sprintf(
				"Иерархия %s разрешена частично: дополнительно скрыто %d ошибок чтения метаданных. Подробности ограничены, чтобы не раздувать отчёт.",
				decision.Method,
				decision.Count,
			))
			continue
		}
		detail := strings.TrimSpace(decision.Detail)
		if detail == "" {
			detail = "причина не передана"
		}
		warnings = append(warnings, fmt.Sprintf(
			"Иерархия %s разрешена частично: AGP не смог прочитать метаданные класса (%s). Возможны пропуски распознавания Service, IBinder, AIDL, BroadcastReceiver и БД; проверьте classpath и целостность байткода.",
			decision.Method,
			detail,
		))
	}
	if omitted > 0 {
		warnings = append(warnings, fmt.Sprintf("Ещё %d предупреждений об иерархии скрыто; полный список сохранён в таблице решений.", omitted))
	}
	return warnings
}

func annotationMapSummaries(values map[instrumentationAnnotationKey]uint64) []InstrumentationAnnotationSummary {
	out := make([]InstrumentationAnnotationSummary, 0, len(values))
	for key, count := range values {
		out = append(out, InstrumentationAnnotationSummary{
			Owner:             key.owner,
			Screen:            key.screen,
			Operation:         key.operation,
			OperationKind:     key.operationKind,
			OperationBudgetMS: key.operationBudgetMS,
			Count:             count,
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
		if values[i].Detail != values[j].Detail {
			return values[i].Detail < values[j].Detail
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
		if values[i].Operation != values[j].Operation {
			return values[i].Operation < values[j].Operation
		}
		if values[i].OperationKind != values[j].OperationKind {
			return values[i].OperationKind < values[j].OperationKind
		}
		return values[i].OperationBudgetMS < values[j].OperationBudgetMS
	})
}
