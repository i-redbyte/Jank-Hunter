#!/usr/bin/env python3
"""Validate ConcurrencyDiagnosticsLogTest captures using matching inspect JSON and export JSONL.

Run once per normal/detailed archive (one run/process/session). This is a controlled
sample acceptance gate, not a general declaration that a production log is complete.
"""
import argparse
from collections import defaultdict
import json
from pathlib import Path
import sys

MAX_EVENTS = 100_000


def require(condition, message):
    if not condition:
        raise ValueError(message)


def validate_capture(summary, events, mode):
    require(mode in ("normal", "detailed"), "mode must be normal or detailed")
    causal = summary.get("CausalAnalysis") or {}
    require(causal.get("Available") and causal.get("Events", 0) > 0, "capture contains no causal evidence")
    segments = summary.get("CollectionSegments") or []
    require(segments, "capture has no segment quality evidence")
    scopes = {row["RunID"] + row["ProcessInstanceID"] + row["SessionID"] for row in segments}
    require(len(scopes) == 1, "validate one run/process/session per capture; export has no scope header")
    scope = next(iter(scopes))
    for segment in segments:
        require(segment.get("Status") in ("closed_clean", "open_clean") and not segment.get("TailBytes"),
                "capture has damaged or truncated segments")
        require(segment.get("QualitySequence", 0) > 0, "segment lacks quality snapshot")
        require(segment.get("EndReason") != "io_error", "writer ended with io_error")
        for counter in segment.get("QualityCounters") or []:
            if counter["Name"] in ("writer_io_error_total", "event_lost_after_io_total", "failed_chunk_total"):
                require(counter["Value"] == 0, "writer quality failure: " + counter["Name"])
    quality = summary.get("CollectionQuality") or {}
    for field in ("chain_valid", "counter_invariants_valid", "quality_progression_valid"):
        require(quality.get(field) is True, "collection integrity is unproven: " + field)
    require(quality.get("accepted_events", 0) > 0 and quality["accepted_events"] == quality.get("written_events"),
            "accepted/written mismatch or empty recording")
    require(quality.get("critical_runtime_hook_failures", 0) == 0, "runtime hook failures in controlled scenario")
    losses = sum(causal.get(name, 0) for name in ("TransportLoss", "RegistryLoss", "StaleEpoch", "BindingMiss", "DroppedCLI"))
    incomplete = losses or any(not row.get("Sealed") or row.get("Status") != "closed_clean" for row in segments)
    require(not incomplete or causal.get("Complete") is False, "partial causal capture falsely claims completeness")

    kinds = defaultdict(int)
    by_entity = defaultdict(list)
    project_sources = 0
    count = 0
    for record in events:
        event = record.get("causal_event")
        if event is None:
            continue
        require(record.get("type") == 39, "causal payload has wrong record type")
        count += 1
        require(count <= MAX_EVENTS, "capture exceeds acceptance evidence limit")
        kind, identity = event["entity_kind"], str(event["entity_id"])
        require(identity != "0", "zero causal identity")
        kinds[kind] += 1
        by_entity[kind, identity].append(event)
        if event.get("source_origin") == 1:
            require(event.get("presence", 0) & 4 and event.get("source_method"), "PROJECT provenance lacks source")
            project_sources += 1
    require(count == causal["Events"], "export and inspect causal populations differ or are empty")
    require(project_sources > 0, "no proven PROJECT source")
    require(all(kinds[kind] > 0 for kind in (1, 2, 3, 4)), "Thread/Executor/Handler/coroutine sample boundary missing")

    def identities(kind, phase):
        return {identity for (entity_kind, identity), rows in by_entity.items()
                if entity_kind == kind and any(row["phase"] == phase for row in rows)}

    created = identities(4, 1)
    observed_only = identities(4, 6) - created
    metrics = causal.get("Metrics") or {}
    require(created and observed_only, "verified coroutine creation and first-observed machine are not distinct")
    require(0 < metrics.get("CoroutineStarts", 0) <= len(created), "coroutine creation metric exceeds proof or is absent")
    require(0 < metrics.get("CoroutineObservedMachines", 0) <= len(observed_only), "observed machine metric exceeds proof or is absent")
    if mode == "detailed":
        acquired, used = identities(6, 2), identities(6, 7)
        require(acquired and used and not acquired.intersection(used), "acquire and existing-resource use are conflated or missing")
        require(0 < metrics.get("ResourcesAcquired", 0) <= len(acquired), "acquisition metric exceeds proof or is absent")
        require(0 < metrics.get("ResourcesObservedUse", 0) <= len(used), "use metric exceeds proof or is absent")
        bindings = {}
        for (kind, identity), rows in by_entity.items():
            if kind != 5:
                continue
            for row in rows:
                if row["phase"] == 1 and row.get("presence", 0) & (1 << 11):
                    key = (str(row.get("worker_instance_id")), row.get("run_attempt", 0), row.get("generation", 0))
                    require(key not in bindings, "ambiguous causal Worker binding")
                    bindings[key] = rows
        jobs = defaultdict(list)
        for attempt in (summary.get("WorkerAnalysis") or {}).get("Attempts") or []:
            if attempt.get("Scope") != scope or not attempt.get("UUID") or attempt.get("Ambiguous") or attempt.get("IdentityAmbiguous"):
                continue
            if attempt.get("Coverage") != "start_and_finish_observed" or attempt.get("Outcome") not in ("retry", "success"):
                continue
            key = (str(attempt.get("InstanceID")), attempt.get("RunAttempt", 0), attempt.get("Generation", 0))
            rows = bindings.get(key, [])
            phases = {row["phase"] for row in rows}
            required_phase = 3 if attempt["Outcome"] == "retry" else 5
            implementations = {row.get("source_method") for row in rows if row["phase"] == 6 and row.get("source_origin") == 1}
            callers = {row.get("app_caller_method") for row in rows if row.get("presence", 0) & 8 and row.get("app_caller_method")}
            if required_phase in phases and implementations and callers and implementations != callers:
                jobs[attempt["UUID"]].append(attempt)
        retry_success = False
        for attempts in jobs.values():
            # Sort boundaries once; do not compare every pair of attempts in a large job.
            boundaries = [(attempt["EndMS"] if attempt["Outcome"] == "retry" else attempt["StartMS"],
                           0 if attempt["Outcome"] == "retry" else 1, attempt["RunAttempt"])
                          for attempt in attempts if attempt.get("EndMS") is not None and attempt.get("StartMS") is not None]
            earliest_retry_attempt = None
            for _, outcome, attempt in sorted(boundaries):
                if outcome == 0:
                    earliest_retry_attempt = attempt if earliest_retry_attempt is None else min(earliest_retry_attempt, attempt)
                elif earliest_retry_attempt is not None and earliest_retry_attempt < attempt:
                    retry_success = True
                    break
            if retry_success:
                break
        require(retry_success, "no same-scope exact Worker retry-to-success chain with separate enqueue/implementation sources")
    return {"mode": mode, "causal_events": count, "kinds": dict(sorted(kinds.items())),
            "created_coroutines": len(created), "observed_only_coroutines": len(observed_only),
            "causal_complete": causal.get("Complete"), "causal_losses": losses}


def read_events(path):
    with path.open(encoding="utf-8") as source:
        for index, line in enumerate(source, 1):
            require(index <= 2_000_000 and len(line) <= 1_048_576, "export exceeds bounded acceptance input")
            if line.strip():
                yield json.loads(line)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("summary", type=Path, help="stdout from jankhunter inspect ARCHIVE --json --out report.html")
    parser.add_argument("events", type=Path, help="JSONL from jankhunter export the same ARCHIVE --out events.jsonl")
    parser.add_argument("--mode", choices=("normal", "detailed"), required=True)
    args = parser.parse_args()
    try:
        require(args.summary.stat().st_size <= 256 << 20, "inspect JSON exceeds bounded acceptance input")
        summary = json.loads(args.summary.read_text(encoding="utf-8"))
        result = validate_capture(summary, read_events(args.events), args.mode)
        print(json.dumps(result, ensure_ascii=False, indent=2))
    except (ValueError, KeyError, TypeError, OSError) as failure:
        print("capture validation failed: " + str(failure), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
