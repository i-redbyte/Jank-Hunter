#!/usr/bin/env python3
"""Validate public Builder hints survive minification and remain tied to request UUIDs."""

import json
import sys
from pathlib import Path


def validate(path: Path) -> int:
    captured = {}
    with path.open() as source:
        for line in source:
            submission = json.loads(line).get("work_submission")
            if not submission or submission.get("UniqueName") != "r8-unique-intent":
                continue
            key = (submission["UUIDMost"], submission["UUIDLeast"])
            captured[key] = submission
    assert len(captured) == 2, captured
    values = list(captured.values())
    assert all(item["Version"] == 3 for item in values), values
    assert sum(item["BackoffPolicy"] == 1 and int(item["RequestedBackoffMS"]) == 15000
               and item["QuotaPolicy"] == 0 for item in values) == 1, values
    assert sum(item["QuotaPolicy"] == 2 and item["BackoffPolicy"] == 0
               for item in values) == 1, values
    return len(captured)


if __name__ == "__main__":
    print(f"R8 work builder PASS requests={validate(Path(sys.argv[1]))}")
