#!/usr/bin/env python3
"""Check catalog and current WorkManager registry evidence from the release fixture."""
import json
import sys
import uuid
from pathlib import Path


def validate(path: Path) -> dict:
    with path.open() as source:
        events = [json.loads(line) for line in source]
    catalogs = [event["worker_catalog"] for event in events if event.get("worker_catalog")]
    assert catalogs, "Missing catalog observation"
    catalog = catalogs[-1]
    assert catalog["status"] == "available", catalog
    assert "com.example.jhsmoke.WorkerExecutionProbe$NeverRunWorker" in catalog["classes"], catalog
    packets = [event["work_registry"] for event in events if event.get("work_registry")]
    assert packets, "Missing registry observations"
    jobs = [row for packet in packets for row in (packet.get("Jobs") or [])]
    assert any(row["State"] == 4 for row in jobs), "Missing dependency-blocked job"
    assert any(row["InitialDelayMS"] >= 3600000 for row in jobs), "Missing delayed job"
    assert all(row["EarliestEligibleUnixMS"] is None for row in jobs if row["State"] != 0)
    identities = [event["worker_identity"] for event in events if event.get("worker_identity")]
    assert identities, "Missing explicit UUID bindings"
    bound = {item["InstanceID"] for item in identities}
    started = [event["worker"] for event in events if event.get("worker", {}).get("stage") == 2]
    assert started and all(item["instance_id"] in bound for item in started), "Missing binding for observed start"
    failures = [event["worker_failure"] for event in events if event.get("worker_failure")]
    assert len(failures) == 4, failures  # Three throws plus CoroutineWorker cancellation.
    assert sum(item["ExceptionClass"] == "java.lang.IllegalStateException" and
               "WorkerExecutionProbe" in item["Stack"] for item in failures) == 3, failures
    assert all("Expected worker exception" not in item["Stack"] and
               len(item["Stack"].encode()) <= 1024 for item in failures), failures
    segments = [event["coroutine_segment"] for event in events if event.get("coroutine_segment")]
    sessions = [event["session"] for event in events if event.get("session")]
    assert sessions and any(row["collector_flags"] & (1 << 12) for row in sessions), "Thread collector scope missing"
    assert any(event.get("io", {}).get("source_ref", {}).get("id", 0) > 0 and
               event.get("producer", {}).get("thread_id", 0) > 0
               for event in events), "Missing file completion with producer TID"
    assert segments and all(row["Version"] == 2 and row["EntryTID"] > 0 and
                            row["ExitTID"] > 0 for row in segments), "Missing native coroutine thread endpoints"
    by_token = {}
    for segment in segments:
        by_token.setdefault((segment["Generation"], segment["Token"]), []).append(segment)
    assert any(any(row["State"] == 0 for row in rows) and
               any(row["State"] == 1 for row in rows) and
               len({row["JavaThreadID"] for row in rows}) > 1 and
               len({row["EntryTID"] for row in rows}) > 1
               for rows in by_token.values()), "Missing one coroutine observed on two Linux threads"
    submissions = [event for event in events if event.get("work_submission")]
    assert len(submissions) >= 20, "Missing submission observations"
    unique = [event for event in submissions if event["work_submission"]["UniqueName"] == "r8-unique-intent"]
    assert len(unique) == 2 and all(event["work_submission"]["Policy"] == 1 for event in unique), unique
    assert all(event.get("attribution", {}).get("operation_id", 0) > 0 for event in unique), "Missing submission caller context"
    assert any(event["work_submission"]["Policy"] == 5 and event["work_submission"]["Periodic"] for event in submissions)
    callers = [event["work_submission"] for event in submissions
               if event["work_submission"].get("CallerSource") not in (None, "", "unknown")]
    assert len(callers) == 10, "Direct enqueue overloads missing or counted twice"
    assert all(row["Version"] in (2, 3) and
               row["CallerSource"].startswith("com.example.jhsmoke.WorkerExecutionProbe") for row in callers), callers
    edges = [event["work_chain_edge"] for event in events if event.get("work_chain_edge")]
    assert edges, "Missing observed WorkContinuation.then dependency"
    registered = {row["UUID"] for row in jobs}
    for edge in edges:
        parent = str(uuid.UUID(int=(edge["ParentMost"] << 64) | edge["ParentLeast"]))
        child = str(uuid.UUID(int=(edge["ChildMost"] << 64) | edge["ChildLeast"]))
        assert parent != child and parent in registered and child in registered, edge
        assert edge["CallerSource"].startswith("com.example.jhsmoke.WorkerExecutionProbe"), edge
    roots = [event["work_chain_root"] for event in events if event.get("work_chain_root")]
    assert len(roots) == 3 and len(edges) == 3, (roots, edges)
    root = next(row for row in roots if row["UniqueName"] == "registry-probe")
    assert root["UniqueName"] == "registry-probe" and root["Policy"] == 2, root
    assert root["CallerSource"].startswith("com.example.jhsmoke.WorkerExecutionProbe"), root
    root_uuid = str(uuid.UUID(int=(root["UUIDMost"] << 64) | root["UUIDLeast"]))
    assert root_uuid in registered, root_uuid
    return {"submission_records": len(submissions), "chain_roots": len(roots), "chain_edges": len(edges), "catalogs": len(catalogs), "registry_packets": len(packets), "observed_rows": len(jobs), "identity_records": len(identities)}


if __name__ == "__main__":
    print(json.dumps(validate(Path(sys.argv[1]))))
