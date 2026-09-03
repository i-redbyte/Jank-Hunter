package main

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func printSummary(summary analyze.Summary) {
	fmt.Printf(
		"logs: %d data_events: %d data_records: %d total_records: %d dictionary_records: %d control_records: %d duration: %dms\n",
		summary.LogCount,
		summary.EventCount,
		summary.DataRecordCount,
		summary.TotalRecordCount,
		summary.DictionaryRecords,
		summary.ControlRecords,
		summary.DurationMS,
	)
	quality := summary.CollectionQuality
	fmt.Println(httpSummaryLine(summary))
	if summary.UIAvgFPS > 0 {
		fmt.Printf("ui: frames=%d janky=%d rate=%.2f%% avg_fps=%.1f min_fps=%.1f\n", summary.UIFrames, summary.UIJank, summary.UIJankPct, summary.UIAvgFPS, summary.UIMinFPS)
	} else {
		fmt.Printf("ui: frames=%d janky=%d rate=%.2f%% fps=not_measured(%s)\n", summary.UIFrames, summary.UIJank, summary.UIJankPct, summary.UIFPSStatus)
	}
	if len(summary.AppVersions) > 0 {
		fmt.Printf("app_versions: %s\n", namedValues(summary.AppVersions))
	}
	if len(summary.SDKs) > 0 {
		fmt.Printf("sdks: %s\n", namedValues(summary.SDKs))
	}
	if len(summary.Devices) > 0 {
		fmt.Printf("devices: %s\n", namedValues(summary.Devices))
	}
	if len(summary.Cohorts) > 0 {
		fmt.Printf("cohorts: %s\n", namedValues(summary.Cohorts))
	}
	if len(summary.Network) > 0 {
		fmt.Printf("network: %s\n", namedValues(summary.Network))
	}
	if len(summary.JankStats) > 0 {
		fmt.Printf("jankstats: %s\n", namedValues(summary.JankStats))
	}
	fmt.Printf("stalls: count=%d max=%dms\n", summary.StallCount, summary.StallMaxMS)
	if len(summary.Processes) > 0 {
		fmt.Printf("processes: %s\n", namedValues(summary.Processes))
	}
	fmt.Printf("context: samples=%d battery_min=%d%% avail_mem_min=%dKB low_mem=%d rx_max=%d tx_max=%d\n", summary.ContextCount, summary.BatteryMinPct, summary.AvailMemoryMinKB, summary.LowMemoryCount, summary.TrafficRxMax, summary.TrafficTxMax)
	fmt.Printf("memory: max_pss=%dKB retained=%d\n", summary.MemoryMaxKB, summary.Retained)
	inputs := summary.AnalysisInputs
	fmt.Printf(
		"analysis_inputs: status=%s complete=%t runtime=%t class_graph=%t asm_diagnostics=%t heap=%t artifact_identity=%t auto_discovered=%t artifacts=%q missing=%s\n",
		inputs.Status,
		inputs.Complete,
		inputs.RuntimeEvidence,
		inputs.ClassGraph,
		inputs.InstrumentationDiagnostics,
		inputs.HeapEvidence,
		inputs.ArtifactIdentityVerified,
		inputs.ArtifactsAutoDiscovered,
		inputs.ArtifactDirectory,
		strings.Join(inputs.Missing, ","),
	)
	if len(summary.RetainedClasses) > 0 {
		fmt.Printf("retained_classes: %s\n", namedValues(summary.RetainedClasses))
	}
	if len(summary.Owners) > 0 {
		fmt.Printf("top_owners: %s\n", ownerValues(summary.Owners, 5))
	}
	fmt.Printf(
		"collection_quality: level=%s complete=%t chain_valid=%t process_scope=%s all_processes=%t process_roster=%d/%d roster_complete=%t roster_declared=%t run_cohorts=%d cohort_consistent=%t counters_valid=%t quality_progression=%t sealed=%d unsealed=%d accepted=%d written=%d committed_chunks=%d/%d runtime_graph=%d/%d/%d known_lost=%d hook_failures=%d dictionary_overflow=%d dictionary_truncated=%d\n",
		quality.Level,
		quality.Complete,
		quality.ChainValid,
		quality.ProcessScope,
		quality.AllProcessesConfigured,
		quality.ObservedProcessCount,
		quality.ExpectedProcessCount,
		quality.ProcessRosterComplete,
		quality.ProcessRosterDeclarationComplete,
		quality.RunCohortCount,
		quality.RunCohortConsistent,
		quality.CounterInvariantsValid,
		quality.QualityProgressionValid,
		quality.SealedSegments,
		quality.UnsealedSegments,
		quality.AcceptedEvents,
		quality.WrittenEvents,
		quality.ReportedCommittedChunks,
		quality.DecodedCommittedChunks,
		quality.DecodedRuntimeGraphCalls,
		quality.RuntimeGraphEmittedEvents,
		quality.RuntimeGraphInputEvents,
		quality.KnownLostEvents,
		quality.RuntimeHookFailures,
		quality.DictionaryOverflow,
		quality.DictionaryTruncated,
	)
	if len(quality.Notices) > 0 || len(summary.Warnings) > 0 {
		fmt.Println("collection_quality_details:")
		for _, notice := range quality.Notices {
			fmt.Printf("info: Качество сбора: %s.\n", notice)
		}
		for _, warning := range summary.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
	}
	for _, detail := range quality.RuntimeHookFailureDetails {
		fmt.Printf("hook_failure: reason=%s count=%d impact=%s explanation=%s\n", detail.Reason, detail.Count, detail.Impact, detail.Explanation)
	}
}

func httpSummaryLine(summary analyze.Summary) string {
	transportFailures := 0
	http4xx := 0
	http5xx := 0
	if network := summary.NetworkAnalysis; network != nil {
		transportFailures = network.TransportFailures
		http4xx = network.HTTP4xx
		http5xx = network.HTTP5xx
	}
	return fmt.Sprintf(
		"http: count=%d operational_failed=%d transport_failed=%d http_4xx=%d http_5xx=%d p95=%dms",
		summary.HTTPCount,
		summary.HTTPFailed,
		transportFailures,
		http4xx,
		http5xx,
		summary.HTTPP95MS,
	)
}

func namedValues(values []analyze.NamedValue) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%s=%d", value.Name, value.Value))
	}
	return strings.Join(parts, ", ")
}

func ownerValues(values []analyze.OwnerStats, limit int) string {
	if len(values) < limit {
		limit = len(values)
	}
	parts := make([]string, 0, limit)
	for _, value := range values[:limit] {
		parts = append(parts, fmt.Sprintf("%s=%d max=%dms", value.Owner, value.Count, value.MaxMS))
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
