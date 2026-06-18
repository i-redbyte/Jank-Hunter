package analyze

import (
	"sort"
	"strings"
)

const maxLightweightIndexEntriesPerKey = 256

type LightweightGraphIndexes struct {
	OwnerCalls       map[string][]RuntimeCallStats
	MethodHooks      map[string][]InstrumentationHookLocation
	ClassAnnotations map[string][]InstrumentationAnnotationSummary
	ScreenProblems   map[string][]ProblemWindowStats
	TraceEvents      map[string][]TraceEventRef
}

type InstrumentationHookLocation struct {
	ClassName string
	Method    string
	Intent    string
	Signature string
	Bridge    string
	Line      int
	Count     uint64
}

type TraceEventRef struct {
	Screen string
	Flow   string
	Step   string
	Owner  string
	Kind   string
	Count  uint64
}

func BuildLightweightGraphIndexes(summary Summary, diagnostics *InstrumentationDiagnostics) LightweightGraphIndexes {
	indexes := LightweightGraphIndexes{
		OwnerCalls:       map[string][]RuntimeCallStats{},
		MethodHooks:      map[string][]InstrumentationHookLocation{},
		ClassAnnotations: map[string][]InstrumentationAnnotationSummary{},
		ScreenProblems:   map[string][]ProblemWindowStats{},
		TraceEvents:      map[string][]TraceEventRef{},
	}

	for _, call := range summary.RuntimeCalls {
		addRuntimeCallIndex(indexes.OwnerCalls, call.Caller, call)
		if call.Callee != call.Caller {
			addRuntimeCallIndex(indexes.OwnerCalls, call.Callee, call)
		}
		addTraceEventIndex(indexes.TraceEvents, traceEventKey(call.Screen, call.Flow, call.Step), TraceEventRef{
			Screen: call.Screen,
			Flow:   call.Flow,
			Step:   call.Step,
			Owner:  firstNonEmpty(call.Callee, call.Caller),
			Kind:   "runtime_call",
			Count:  call.Count,
		})
	}

	for _, problem := range summary.ProblemWindows {
		addProblemIndex(indexes.ScreenProblems, problem.Screen, problem)
		addTraceEventIndex(indexes.TraceEvents, traceEventKey(problem.Screen, problem.Flow, problem.Step), TraceEventRef{
			Screen: problem.Screen,
			Flow:   problem.Flow,
			Step:   problem.Step,
			Owner:  problem.Owner,
			Kind:   "problem:" + problem.Kind,
			Count:  problem.Count,
		})
	}

	for _, spam := range summary.LogSpam {
		addTraceEventIndex(indexes.TraceEvents, traceEventKey(spam.Screen, spam.Flow, spam.Step), TraceEventRef{
			Screen: spam.Screen,
			Flow:   spam.Flow,
			Step:   spam.Step,
			Owner:  spam.Owner,
			Kind:   "log_spam",
			Count:  spam.Count,
		})
	}

	for _, class := range instrumentationDiagnosticClasses(diagnostics) {
		for _, hook := range class.Hooks {
			key := methodNodeKey(class.ClassName, hook.Method)
			addHookIndex(indexes.MethodHooks, key, InstrumentationHookLocation{
				ClassName: class.ClassName,
				Method:    hook.Method,
				Intent:    hook.Intent,
				Signature: hook.Signature,
				Bridge:    hook.Bridge,
				Line:      hook.Line,
				Count:     hook.Count,
			})
		}
		for _, annotation := range class.Annotations {
			addAnnotationIndex(indexes.ClassAnnotations, class.ClassName, annotation)
		}
	}

	sortLightweightIndexes(indexes)
	return indexes
}

func instrumentationDiagnosticClasses(diagnostics *InstrumentationDiagnostics) []InstrumentationClassDiagnostic {
	if diagnostics == nil {
		return nil
	}
	if len(diagnostics.Classes) > 0 {
		return diagnostics.Classes
	}
	return diagnostics.TopClasses
}

func addRuntimeCallIndex(target map[string][]RuntimeCallStats, key string, value RuntimeCallStats) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if len(target[key]) >= maxLightweightIndexEntriesPerKey {
		return
	}
	target[key] = append(target[key], value)
}

func addProblemIndex(target map[string][]ProblemWindowStats, key string, value ProblemWindowStats) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if len(target[key]) >= maxLightweightIndexEntriesPerKey {
		return
	}
	target[key] = append(target[key], value)
}

func addHookIndex(target map[string][]InstrumentationHookLocation, key string, value InstrumentationHookLocation) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if len(target[key]) >= maxLightweightIndexEntriesPerKey {
		return
	}
	target[key] = append(target[key], value)
}

func addAnnotationIndex(
	target map[string][]InstrumentationAnnotationSummary,
	key string,
	value InstrumentationAnnotationSummary,
) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if len(target[key]) >= maxLightweightIndexEntriesPerKey {
		return
	}
	target[key] = append(target[key], value)
}

func addTraceEventIndex(target map[string][]TraceEventRef, key string, value TraceEventRef) {
	key = strings.TrimSpace(key)
	if key == "" || value.Count == 0 {
		return
	}
	if len(target[key]) >= maxLightweightIndexEntriesPerKey {
		return
	}
	target[key] = append(target[key], value)
}

func traceEventKey(screen string, flow string, step string) string {
	parts := []string{}
	for _, value := range []string{screen, flow, step} {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " / ")
}

func sortLightweightIndexes(indexes LightweightGraphIndexes) {
	for key := range indexes.OwnerCalls {
		sort.SliceStable(indexes.OwnerCalls[key], func(i, j int) bool {
			left := indexes.OwnerCalls[key][i]
			right := indexes.OwnerCalls[key][j]
			if left.TotalMS != right.TotalMS {
				return left.TotalMS > right.TotalMS
			}
			if left.Count != right.Count {
				return left.Count > right.Count
			}
			return left.Callee < right.Callee
		})
	}
	for key := range indexes.MethodHooks {
		sort.SliceStable(indexes.MethodHooks[key], func(i, j int) bool {
			left := indexes.MethodHooks[key][i]
			right := indexes.MethodHooks[key][j]
			if left.Count != right.Count {
				return left.Count > right.Count
			}
			if left.Intent != right.Intent {
				return left.Intent < right.Intent
			}
			if left.Signature != right.Signature {
				return left.Signature < right.Signature
			}
			return left.Line < right.Line
		})
	}
	for key := range indexes.ClassAnnotations {
		sortAnnotationSummaries(indexes.ClassAnnotations[key])
	}
	for key := range indexes.ScreenProblems {
		sort.SliceStable(indexes.ScreenProblems[key], func(i, j int) bool {
			left := indexes.ScreenProblems[key][i]
			right := indexes.ScreenProblems[key][j]
			if left.Count != right.Count {
				return left.Count > right.Count
			}
			if left.MaxMS != right.MaxMS {
				return left.MaxMS > right.MaxMS
			}
			return left.Kind < right.Kind
		})
	}
	for key := range indexes.TraceEvents {
		sort.SliceStable(indexes.TraceEvents[key], func(i, j int) bool {
			left := indexes.TraceEvents[key][i]
			right := indexes.TraceEvents[key][j]
			if left.Count != right.Count {
				return left.Count > right.Count
			}
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			return left.Owner < right.Owner
		})
	}
}
