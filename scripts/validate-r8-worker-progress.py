#!/usr/bin/env python3
"""Validate direct progress requests from both supported Worker base classes."""

import json
import sys
from pathlib import Path


def validate(path: Path, summary_path: Path) -> int:
    requests = []
    with path.open() as source:
        for line in source:
            progress = json.loads(line).get("worker_progress")
            if progress is None:
                continue
            assert progress["Version"] == 1 and progress["Stage"] == 1, progress
            assert progress["InstanceID"] != 0, progress
            assert progress["OmittedKeys"] == 0, progress
            assert progress["Fields"] == [{"Key": "step", "Type": "int", "PayloadBytes": 4}], progress
            requests.append(progress)
    assert len(requests) == 9, f"Expected 4 adapter and 5 coroutine requests, got {len(requests)}"
    summary = json.JSONDecoder().raw_decode(summary_path.read_text())[0]
    worker = summary["WorkerAnalysis"]
    attempts = worker["Attempts"]
    attributed = [attempt for attempt in attempts if attempt.get("ProgressRequests")]
    assert len(attempts) == 13 and len(attributed) == 9, (len(attempts), len(attributed))
    assert sum(len(attempt["ProgressRequests"]) for attempt in attributed) == 9
    assert worker["UnattributedProgressEvents"] == 0 and worker["DroppedProgressEvents"] == 0
    return len(requests)


if __name__ == "__main__":
    print(f"R8 worker progress PASS requests={validate(Path(sys.argv[1]), Path(sys.argv[2]))}")
