#!/usr/bin/env python3
"""Host safety and result-contract tests; no adb/device changes."""
import importlib.util
from pathlib import Path
import unittest
import tempfile
import zipfile

spec = importlib.util.spec_from_file_location(
    "concurrency_device_validation", Path(__file__).with_name("run-concurrency-device-validation.py"),
)
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class DeviceValidationContractTest(unittest.TestCase):
    def test_only_expected_sample_apk_is_accepted(self):
        runner.validate_apk_package("package: name='io.jankhunter.sample' versionCode='1'", "io.jankhunter.sample")
        with self.assertRaises(ValueError):
            runner.validate_apk_package("package: name='com.other.application'", "io.jankhunter.sample")

    def test_adb_success_does_not_hide_instrumentation_failure_or_empty_suite(self):
        runner.validate_instrumentation("INSTRUMENTATION_RESULT: stream=\nOK (2 tests)\nINSTRUMENTATION_CODE: -1", 2)
        for output in ("FAILURES!!!\nTests run: 2", "OK (0 tests)", "INSTRUMENTATION_FAILED: bad runner", "OK (1 test)"):
            with self.assertRaises(ValueError):
                runner.validate_instrumentation(output, 2)

    def test_public_benchmark_artifacts_include_both_stable_archives(self):
        self.assertEqual(("concurrency-benchmark.json", "concurrency-benchmark-normal.zip",
                          "concurrency-benchmark-detailed.zip"), runner.artifact_names("benchmark"))

    def test_archive_measurements_match_the_pulled_zip_entries(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            phases = []
            for mode in ("normal", "detailed"):
                archive = directory / f"concurrency-benchmark-{mode}.zip"
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as output:
                    output.writestr("directory/", "")
                    output.writestr("directory/session.jhlog", b"x" * 4096)
                phases.append({"mode": mode, "log_bytes": 4096, "archive_bytes": archive.stat().st_size})
            runner.validate_archives({"phases": phases}, directory)
            phases[0]["log_bytes"] = phases[0]["archive_bytes"]
            with self.assertRaises(ValueError):
                runner.validate_archives({"phases": phases}, directory)

    def test_release_registry_uses_exact_class_mapping(self):
        text = "io.jankhunter.runtime.RuntimeCausalRegistry -> a.b:\n    int value -> c\n"
        self.assertEqual("a.b", runner.mapped_registry_class(text))
        self.assertEqual("a.c", runner.mapped_registry_entry_class(
            "io.jankhunter.runtime.RuntimeCausalRegistry$Entry -> a.c:\n"))
        with self.assertRaises(ValueError):
            runner.mapped_registry_class("io.jankhunter.runtime.OtherRegistry -> a.b:\n")

    def test_unique_residual_constructor_owner_resolves_horizontal_merge_candidate(self):
        text = (
            "androidx.lifecycle.viewmodel.internal.ViewModelImpl -> f3.b:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry.<init>(int):10:10 -> <init>\n"
            "    3:8:void io.jankhunter.runtime.RuntimeCausalRegistry.<init>(int,int):8 -> <init>\n"
            "    9:9:void other.Nested.<init>():20:20 -> <init>\n"
            "other.Type -> z.a:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry$Entry.<init>(int):10:10 -> <init>\n"
        )
        self.assertEqual("f3.b", runner.mapped_registry_class(text))
        evidence = runner.resolve_mapped_class(text, runner.REGISTRY)
        self.assertEqual("unique_residual_constructor_owner", evidence["strategy"])
        self.assertTrue(evidence["requires_heap_entry_proof"])
        self.assertEqual(["androidx.lifecycle.viewmodel.internal.ViewModelImpl"], evidence["mapping_section_owners"])

    def test_inlined_constructors_in_two_residual_owners_are_ambiguous(self):
        text = (
            "other.First -> a.b:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry.<init>(int):10:10 -> <init>\n"
            "other.Second -> a.c:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry.<init>(int):10:10 -> <init>\n"
        )
        with self.assertRaisesRegex(ValueError, "ambiguous"):
            runner.mapped_registry_class(text)
        explicit = "io.jankhunter.runtime.RuntimeCausalRegistry -> a.d:\n" + text
        self.assertEqual("a.d", runner.mapped_registry_class(explicit))
        self.assertEqual("exact_class_header", runner.resolve_mapped_class(explicit, runner.REGISTRY)["strategy"])

    def test_removed_classes_and_constructor_inlining_into_a_regular_method_are_not_owners(self):
        for text in (
            "io.jankhunter.runtime.RuntimeCausalRegistry -> R8$$REMOVED$$CLASS$$1:\n",
            "other.First -> R8$$REMOVED$$CLASS$$2:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry.<init>():10:10 -> <init>\n",
            "other.First -> a.b:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry.<init>():10:10 -> create\n",
            "other.First -> a.b:\n"
            "    1:2:void io.jankhunter.runtime.RuntimeCausalRegistry$Entry.<init>():10:10 -> <init>\n",
        ):
            with self.assertRaises(ValueError):
                runner.mapped_registry_class(text)

    def test_benchmark_requires_all_modes_and_measured_frames(self):
        data = {"seconds_per_mode": 600, "phases": [
            {"mode": mode, "tasks": 1, "frames": {"count": 60}} for mode in ("disabled", "normal", "detailed")
        ]}
        runner.validate_benchmark(data, 600)
        data["phases"][1]["frames"] = None
        with self.assertRaises(ValueError):
            runner.validate_benchmark(data, 600)


if __name__ == "__main__":
    unittest.main()
