#!/usr/bin/env python3
"""Verify the real minified run hook through Android -> jhlog -> Go export."""
import json
import sys

snapshots = []
points = []
with open(sys.argv[1], encoding="utf-8") as stream:
    for line in stream:
        event = json.loads(line)
        if event.get("thread_lifecycle"):
            points.append((event["thread_lifecycle"], event.get("attribution", {}), event.get("producer", {})))
        if event.get("thread_snapshot"):
            snapshots.append(event["thread_snapshot"])
assert snapshots, "No thread snapshots captured"
linked = []
owned = []
owned_sources = set()
known_owners = {
    "Jank Hunter maintenance", "Jank Hunter event consumer", "Jank Hunter writer",
    "Jank Hunter call graph", "Jank Hunter metric drain", "Jank Hunter main watchdog",
}
for snapshot in snapshots:
    assert snapshot["version"] == 3, snapshot
    assert snapshot["dropped_java_identities"] == 0, snapshot
    for row in snapshot["threads"]:
        java = row.get("java")
        if java and java.get("owned_by_jank_hunter") is True:
            owned.append(java)
            assert java["source"] in known_owners, java
            owned_sources.add(java["source"])
        if java and java["name"] == "jh-identity-r8-java":
            assert int(java["id"]) > 0 and row["tid"] > 0 and row["start_ticks"] > 0
            assert java["source"].endswith("ThreadIdentityProbe.run"), java
            assert java["daemon"] is True
            assert int(java["first_observed_elapsed_nanos"]) <= snapshot["elapsed_ms"] * 1_000_000
            assert java["owned_by_jank_hunter"] is False, java
            linked.append(java)
assert linked, "R8 run hook did not establish a live Java/kernel identity"
assert owned, "The executing Jank Hunter maintenance thread was not identified"
assert {"Jank Hunter maintenance", "Jank Hunter event consumer"}.issubset(owned_sources), owned_sources
assert any(java["state"] == "TIMED_WAITING" for java in linked), linked
print(f"R8 thread identity PASS snapshots={len(snapshots)} linked_observations={len(linked)} owned_observations={len(owned)} roles={sorted(owned_sources)}")

short = [(point, attr, producer) for point, attr, producer in points if point["name"] == "jh-short-lived-probe"]
assert {point["stage"] for point, _, _ in short} == {1, 2, 3}, short
assert len({point["java_id"] for point, _, _ in short}) == 1, short
for point, attribution, producer in short:
    if point["stage"] == 1:
        assert point["target_tid"] > 0 and point["target_tid"] == producer["thread_id"], point
        assert point["source"].endswith("ShortThreadRun.run"), point
    else:
        assert point.get("target_tid", 0) == 0, point
        assert attribution.get("operation_id", 0) > 0, attribution
assert not any(point["name"] == "jh-failed-start" for point, _, _ in points), "Failed start was recorded as returned"
short_in_scans = sum(1 for snapshot in snapshots for row in snapshot["threads"] if row.get("java", {}).get("name") == "jh-short-lived-probe")
print(f"R8 thread points PASS events={len(points)} short_points={len(short)} short_in_snapshots={short_in_scans}")
