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
    outcomes: dict[str, list[int]] = collections.defaultdict(list)
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
            name = names.get(worker["worker_ref"]["id"], "")
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
                if role == "SuspendedWorker" and worker["outcome"] != 4:
                    assert worker["duration_ms"] > 0, "Suspension lost from duration"
            else:
                raise AssertionError(f"Unexpected stage: {worker['stage']}")
    actual = {name: sorted(values) for name, values in outcomes.items()}
    assert not active, f"Unfinished instances: {active}"
    assert len(terminal) == 11, f"Expected 11 complete instances, got {len(terminal)}"
    assert actual == {
        "AsmWorker": [1, 2, 3],
        "AdapterWorker": [1, 2, 3],
        "SuspendedWorker": [1, 2, 2, 3, 4],
    }, actual
    return actual


if __name__ == "__main__":
    print(json.dumps(validate(Path(sys.argv[1])), sort_keys=True))
