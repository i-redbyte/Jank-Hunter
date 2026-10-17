#!/usr/bin/env python3
"""Validate confirmed foreground entry and attempt boundary in a minified consumer."""

import json
import sys
from pathlib import Path


def validate(path: Path, summary_path: Path) -> int:
    stages: dict[int, list[int]] = {}
    with path.open() as source:
        for line in source:
            worker = json.loads(line).get("worker")
            if not worker or worker["stage"] not in (5, 6):
                continue
            assert worker["outcome"] == 0 and worker["duration_ms"] == 0, worker
            stages.setdefault(worker["instance_id"], []).append(worker["stage"])
    assert len(stages) == 2, f"Expected foreground for adapter and coroutine success, got {stages}"
    assert all(values == [5, 6] for values in stages.values()), stages
    summary = json.JSONDecoder().raw_decode(summary_path.read_text())[0]
    analysis = summary["WorkerAnalysis"]
    intervals = [attempt for attempt in analysis["Attempts"] if attempt.get("ForegroundEnteredMS") is not None]
    assert len(intervals) == 2 and analysis["UnattributedForegroundEvents"] == 0, analysis
    assert all(attempt["ForegroundBoundaryMS"] >= attempt["ForegroundEnteredMS"] for attempt in intervals), intervals
    return len(intervals)


if __name__ == "__main__":
    print(f"R8 worker foreground PASS attempts={validate(Path(sys.argv[1]), Path(sys.argv[2]))}")
