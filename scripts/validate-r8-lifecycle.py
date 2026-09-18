#!/usr/bin/env python3
"""Check automatic lifecycle registration using retained events, not attempted-watch counters."""

import collections
import json
import re
import sys
from pathlib import Path


def validate(path: Path, runtime_log: Path) -> dict[str, int]:
    expected_roles = {
        "probe.binding": {"fragment", "binding", "root"},
        "probe.platform": {"fragment", "binding", "root"},
        "probe.library": {"fragment", "binding", "root"},
        "probe.custom": {"fragment", "binding", "root"},
        "probe.activity": {"activity", "decor"},
        "probe.service": {"service"},
        "probe.vm": {"viewmodel"},
        "probe.final": {"viewmodel"},
        "probe.legacy.fragment": {"fragment", "root"},
        "probe.legacy.vm": {"viewmodel"},
    }
    holders: dict[tuple[str, str], str] = {}
    roles: dict[str, set[str]] = collections.defaultdict(set)
    expected_classes: dict[str, set[str]] = collections.defaultdict(set)
    with runtime_log.open() as log:
        for line in log:
            match = re.search(r"EXPECTED owner=(\S+) role=(\S+) class=(\S+)(?: holder=(\S+))?", line)
            if not match:
                continue
            owner, role, name, holder = match.groups()
            holder = holder or owner
            assert (holder, name) not in holders, f"Duplicate fixture holder/class: {holder}/{name}"
            holders[holder, name] = owner
            assert role not in roles[owner], f"Duplicate fixture expectation: {owner}/{role}"
            roles[owner].add(role)
            expected_classes[owner].add(name)
    assert dict(roles) == expected_roles, f"Incomplete fixture expectations: {dict(roles)}"
    assert all(len(expected_classes[owner]) == len(values) for owner, values in roles.items())
    expected_holders = {holder for holder, _ in holders}
    dictionaries: dict[tuple[str, bool, int], str] = {}
    counts: dict[str, int] = collections.Counter()
    classes: dict[str, set[str]] = collections.defaultdict(set)
    with path.open() as source:
        for line in source:
            event = json.loads(line)
            scope = event["source"]
            dictionary = event.get("dictionary")
            if dictionary:
                dictionaries[scope, dictionary["kind"] == 12, dictionary["id"]] = dictionary["value"]
            retained = event.get("retained")
            if not retained:
                continue
            reference = retained.get("holder_ref", {})
            owner = dictionaries.get((scope, reference.get("stable", False), reference.get("id", 0)), "")
            if not owner.startswith("probe.") and owner not in expected_holders:
                continue
            assert retained["age_ms"] >= 1000, retained
            assert retained["count"] > 0, retained
            class_ref = retained.get("class_ref", {})
            runtime_class = dictionaries.get((scope, class_ref.get("stable", False), class_ref.get("id", 0)), "")
            assert (owner, runtime_class) in holders, f"Unexpected holder/target: {owner}/{runtime_class}"
            owner = holders[owner, runtime_class]
            assert runtime_class in expected_classes.get(owner, set()), f"Unexpected target for {owner}: {runtime_class}"
            assert runtime_class not in classes[owner], f"Repeated retained class for {owner}: {runtime_class}"
            assert retained["count"] == 1, f"Repeated registration for {owner}: {retained}"
            classes[owner].add(runtime_class)
            counts[owner] += retained["count"]
    expected = {owner: len(values) for owner, values in expected_roles.items()}
    assert counts == expected, f"Automatic lifecycle targets: expected {expected}, got {dict(counts)}"
    assert dict(classes) == expected_classes, classes
    return dict(counts)


if __name__ == "__main__":
    print(json.dumps(validate(Path(sys.argv[1]), Path(sys.argv[2])), sort_keys=True))
