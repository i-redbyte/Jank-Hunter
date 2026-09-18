#!/usr/bin/env python3
"""Legacy compatibility means callable ABI and explicit partial coverage, not target parity."""
import json
import sys
from pathlib import Path


def validate(path: Path, summary_path: Path) -> int:
    dictionaries = {}
    partial = 0
    for line in path.read_text().splitlines():
        event = json.loads(line)
        scope = event["source"]
        dictionary = event.get("dictionary")
        if dictionary:
            dictionaries[scope, dictionary["kind"] == 12, dictionary["id"]] = dictionary["value"]
        metric = event.get("metric")
        if metric and event["type"] == 9:
            ref = metric.get("metric_ref", {})
            name = dictionaries.get((scope, ref.get("stable", False), ref.get("id", 0)), "")
            if name == "jankhunter.lifecycle.coverage.legacy_partial.count":
                partial += metric["value"]
    assert partial > 0, "Legacy callbacks did not declare partial lifecycle/binding coverage"
    summary = json.loads(summary_path.read_text())
    warnings = " ".join(summary.get("Warnings", []))
    assert "lifecycle/binding-покрытие частичное" in warnings, "CLI hid partial legacy coverage"
    assert "обновите Gradle plugin" in warnings
    return partial


if __name__ == "__main__":
    print(json.dumps({"legacy_partial_calls": validate(Path(sys.argv[1]), Path(sys.argv[2]))}))
