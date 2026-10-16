#!/usr/bin/env python3
"""Validate the real release worker fixture as start -> terminal transitions per instance."""

import collections
import json
import sys
from pathlib import Path


def validate(path: Path) -> dict[str, list[int]]:
    names: dict[int, str] = {}
    active: dict[int, str] = {}
    terminal: set[int] = set()
    foreground: dict[int, list[int]] = {}
    outcomes: dict[str, list[int]] = collections.defaultdict(list)
    cancellation_outcome: int | None = None
    prefix = "com.example.jhsmoke.WorkerExecutionProbe$"
    with path.open() as source:
        for line in source:
            event = json.loads(line)
            dictionary = event.get("dictionary")
            if dictionary and dictionary["kind"] == 12:
                names[dictionary["id"]] = dictionary["value"]
            worker = event.get("worker")
            if not worker:
                continue
            if worker["stage"] == 4:
                assert worker["instance_id"] != 0 and worker["outcome"] == 0 and worker["duration_ms"] == 0, "Registration observation invented execution"
                continue
            name = names.get(worker["worker_ref"].get("id"), "")
            if not name.startswith(prefix):
                raise AssertionError(f"Unknown worker owner: {name!r}")
            role = name.removeprefix(prefix).removesuffix(".doWork")
            instance = worker["instance_id"]
            if worker["stage"] == 2:
                assert instance not in active and instance not in terminal, "Duplicate start"
                active[instance] = role
            elif worker["stage"] == 3:
                assert active.pop(instance, None) == role, "Terminal without matching start"
                assert instance not in terminal, "Duplicate terminal"
                terminal.add(instance)
                outcomes[role].append(worker["outcome"])
                if role == "SuspendedWorker" and worker["outcome"] in (4, 5):
                    known_reason = bool(event["flags"] & (1 << 21))
                    expected = 4 if known_reason else 5
                    assert worker["outcome"] == expected, "Cancellation outcome contradicts stop-reason precision"
                    assert worker["stop_reason"] == (1 if known_reason else 0), "Unexpected cancellation stop reason"
                    cancellation_outcome = expected
                if role == "SuspendedWorker" and worker["outcome"] != 4:
                    assert worker["duration_ms"] > 0, "Suspension lost from duration"
            elif worker["stage"] in (5, 6):
                assert active.get(instance) == role, "Foreground event outside active attempt"
                foreground.setdefault(instance, []).append(worker["stage"])
            else:
                raise AssertionError(f"Unexpected stage: {worker['stage']}")
    actual = {name: sorted(values) for name, values in outcomes.items()}
    assert not active, f"Unfinished instances: {active}"
    assert len(terminal) == 13, f"Expected 13 complete instances, got {len(terminal)}"
    assert len(foreground) == 2 and all(stages == [5, 6] for stages in foreground.values()), foreground
    assert cancellation_outcome in (4, 5), "Missing suspended cancellation"
    assert actual == {
        "AsmWorker": [1, 2, 2, 3],
        "AdapterWorker": [1, 2, 2, 3],
        "SuspendedWorker": [1, 2, 2, 3, cancellation_outcome],
    }, actual
    return actual


if __name__ == "__main__":
    print(json.dumps(validate(Path(sys.argv[1])), sort_keys=True))
