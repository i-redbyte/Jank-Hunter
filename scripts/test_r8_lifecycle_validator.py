"""Mutation checks for the device lifecycle evidence gate."""
import copy
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location("lifecycle_validator", Path(__file__).with_name("validate-r8-lifecycle.py"))
validator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validator)


class LifecycleValidatorTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.events_path = Path(self.directory.name) / "events.jsonl"
        self.log_path = Path(self.directory.name) / "runtime.log"
        self.events = []
        log = []
        identity = 1
        for owner in ("binding", "platform", "library", "custom", "vm", "final", "legacy.fragment", "legacy.vm", "activity", "service"):
            roles = ("viewmodel",) if owner in ("vm", "final", "legacy.vm") else ("fragment", "binding", "root")
            if owner == "legacy.fragment":
                roles = ("fragment", "root")
            if owner in ("activity", "service"):
                roles = (owner, "decor") if owner == "activity" else (owner,)
            owner_id = identity
            self.events.append({"source": "run", "dictionary": {"kind": 12, "id": identity, "value": f"probe.{owner}"}})
            identity += 1
            for role in roles:
                name = f"obfuscated.{owner}.{role}"
                log.append(f"EXPECTED owner=probe.{owner} role={role} class={name}")
                self.events.append({"source": "run", "dictionary": {"kind": 11, "id": identity, "value": name}})
                self.events.append({"source": "run", "retained": {
                    "holder_ref": {"stable": True, "id": owner_id},
                    "class_ref": {"stable": False, "id": identity},
                    "count": 1, "age_ms": 1000,
                }})
                identity += 1
        self.log_path.write_text("\n".join(log))

    def validate(self):
        self.events_path.write_text("\n".join(json.dumps(event) for event in self.events))
        return validator.validate(self.events_path, self.log_path)

    def test_complete_obfuscated_targets_pass(self):
        self.assertEqual(20, sum(self.validate().values()))

    def test_missing_registration_is_rejected(self):
        self.events.pop()
        with self.assertRaises(AssertionError):
            self.validate()

    def test_duplicate_registration_is_rejected(self):
        self.events.append(copy.deepcopy(self.events[-1]))
        with self.assertRaises(AssertionError):
            self.validate()

    def test_wrong_target_with_same_count_is_rejected(self):
        self.events[-2]["dictionary"]["value"] = "fake.Binding"
        with self.assertRaises(AssertionError):
            self.validate()

    def test_other_source_dictionary_cannot_resolve_target(self):
        self.events[-2]["source"] = "other-run"
        with self.assertRaises(AssertionError):
            self.validate()

    def test_wrong_holder_is_rejected(self):
        self.log_path.write_text(self.log_path.read_text().replace(
            "owner=probe.service role=service class=obfuscated.service.service",
            "owner=probe.service role=service class=obfuscated.service.service holder=expected.holder"))
        with self.assertRaises(AssertionError):
            self.validate()

    def test_premature_retention_is_rejected(self):
        self.events[-1]["retained"]["age_ms"] = 999
        with self.assertRaises(AssertionError):
            self.validate()


if __name__ == "__main__":
    unittest.main()
