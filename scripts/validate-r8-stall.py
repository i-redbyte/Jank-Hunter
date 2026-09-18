#!/usr/bin/env python3
"""Check raw stall evidence before any deobfuscation; observed frames must not invent an owner."""
import json
import sys
from pathlib import Path


def validate(path: Path) -> dict:
    dictionary = {}
    stalls = []
    for line in path.read_text().splitlines():
        event = json.loads(line)
        source = event["source"]
        if entry := event.get("dictionary"):
            dictionary[source, entry["kind"] == 12, entry["id"]] = entry["value"]
        if not (stall := event.get("stall")):
            continue
        def resolve(reference):
            return dictionary.get((source, reference.get("stable", False), reference.get("id", 0)), "")
        owner_ref = event.get("attribution", {}).get("owner", {})
        if owner_ref.get("id", 0):
            assert (source, owner_ref.get("stable", False), owner_ref["id"]) in dictionary, "Unresolved owner cannot be treated as missing"
        owner = resolve(owner_ref)
        assert owner in ("", "unknown"), f"Observed frame became an owner: {owner}"
        stack = resolve(stall["stack_ref"])
        frames = [line for line in stack.splitlines() if line.strip().startswith("at ")]
        assert 2 <= len(frames) <= 32, f"Caller chain missing or oversized: {stack}"
        assert len(stack.encode("utf-8")) <= 4096, "Stack payload exceeded its byte limit"
        stalls.append(stall)
    assert len(stalls) == 2, stalls
    assert [item["state"] for item in stalls] == [1, 2], stalls
    assert stalls[0]["incident_id"] == stalls[1]["incident_id"] != 0
    assert 100 <= stalls[0]["duration_ms"] < stalls[1]["duration_ms"]
    assert stalls[1]["duration_ms"] >= 1000
    return {"states": [1, 2], "duration_ms": stalls[1]["duration_ms"], "owner": "unknown", "frames": len(frames)}


if __name__ == "__main__":
    print(json.dumps(validate(Path(sys.argv[1]))))
    if len(sys.argv) > 2:
        summary, _ = json.JSONDecoder().raw_decode(Path(sys.argv[2]).read_text())
        assert summary["MappingIdentity"]["Status"] == "verified"
        owners = [item for item in summary["Owners"] if item["Kind"] == "main_thread_stall"]
        assert len(owners) == 1
        assert owners[0]["Owner"] == "unknown"
        for expected in ("kotlin.SynchronizedLazyImpl.getValue", "androidx.lifecycle.LiveData", "StallExecutionProbe"):
            assert expected in owners[0]["StackHint"], f"Missing restored frame: {expected}"
        assert owners[0]["StackRetrace"]["Raw"]
        print("Verified official Retrace preserves Kotlin, AndroidX and application caller chain")
