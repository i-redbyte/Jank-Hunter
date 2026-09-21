#!/usr/bin/env python3
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("stall_validator", Path(__file__).with_name("validate-r8-stall.py"))
validator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validator)


class StallValidatorTest(unittest.TestCase):
    def fixture(self):
        return [
            {"source": "log", "dictionary": {"id": 1, "kind": 5, "value": "\tat a.b(File.java:1)\n\tat a.c(File.java:2)"}},
            {"source": "log", "stall": {"incident_id": 1, "state": 1, "stack_ref": {"id": 1}, "duration_ms": 110}},
            {"source": "log", "stall": {"incident_id": 1, "state": 2, "stack_ref": {"id": 1}, "duration_ms": 1200}},
        ]

    def check(self, rows):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "events.jsonl"
            path.write_text("\n".join(map(json.dumps, rows)))
            return validator.validate(path)

    def test_complete(self):
        self.assertEqual(self.check(self.fixture())["owner"], "unknown")

    def test_mutations(self):
        for mutation in (
            lambda rows: rows.pop(),
            lambda rows: rows.append(rows[-1]),
            lambda rows: rows[-1]["stall"].update(incident_id=2),
            lambda rows: rows[0]["dictionary"].update(value="single.frame(File.java:1)"),
            lambda rows: rows[-1].update(attribution={"owner": {"id": 1}}),
            lambda rows: rows[-1].update(attribution={"owner": {"id": 999}}),
            lambda rows: rows[-1]["stall"].update(duration_ms=10),
        ):
            with self.subTest(mutation=mutation):
                rows = self.fixture()
                mutation(rows)
                with self.assertRaises(AssertionError):
                    self.check(rows)


if __name__ == "__main__":
    unittest.main()
