#!/usr/bin/env python3
"""Focused contract tests for performance-baseline.py."""

from __future__ import annotations

import copy
import importlib.util
import io
import json
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


SCRIPT = Path(__file__).with_name("performance-baseline.py")
SPEC = importlib.util.spec_from_file_location("performance_baseline", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot load {SCRIPT}")
baseline = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(baseline)


REQUIRED_PAGES = [
    ".html",
    "-math.html",
    "-leaks.html",
    "-influence.html",
    "-diagnostics.html",
]


def complete_result() -> dict:
    return {
        "schema": baseline.CAPTURE_SCHEMA,
        "profile": "representative",
        "capture_config": {
            "profile": "representative",
            "go_benchmark_count": 3,
            "android_runtime_iterations": 100_000,
            "android_build_tools_version": "35.0.0",
        },
        "fixture": {
            "schema": baseline.FIXTURE_SCHEMA,
            "profile": "representative",
            "events": 60_000,
            "data_records": 60_000,
            "dictionary_entries": 8_000,
            "dictionary_records": 8_000,
            "control_records": 2,
            "total_records": 68_002,
            "runtime_call_events": 20_000,
            "runtime_unique_edges": 10_000,
            "flow_events": 30_000,
            "flow_tuples": 300,
            "signal_events": 10_000,
            "duration_ms": 60_000,
            "compressed_bytes": 2_000_000,
        },
        "environment": {
            "os": "Darwin",
            "os_release": "25.0.0",
            "arch": "arm64",
            "cpu": "Apple M4 Pro",
            "python": "3.13.0",
            "go": "go version go1.24 darwin/arm64",
            "java": "openjdk version 21",
        },
        "surfaces": {
            "go_benchmarks": True,
            "android_runtime": True,
            "android_artifacts": True,
            "cli": True,
            "reports": True,
            "peak_rss": True,
        },
        "quality": {
            "event_count": 60_000,
            "dictionary_entries": 8_000,
            "warnings": [],
            "collection": {
                "level": "high",
                "complete": True,
                "chain_valid": True,
                "sealed_segments": 1,
                "unsealed_segments": 0,
                "segments_with_quality": 1,
                "segments_without_quality": 0,
                "accepted_events": 60_000,
                "written_events": 60_000,
                "known_lost_events": 0,
                "dictionary_overflow": 0,
                "dictionary_truncated": 0,
                "chain_issues": [],
                "reasons": [],
            },
        },
        "measurements": {
            "go_benchmarks": {
                name: {
                    "ns_per_op": 100.0,
                    "bytes_per_op": 64.0,
                    "allocs_per_op": 2.0,
                    "samples": 3,
                }
                for name in baseline.GO_BENCHMARKS
            },
            "android_runtime": {
                name: {
                    "iterations": iterations,
                    "total_ns": iterations * 100,
                    "ns_per_op": 100.0,
                }
                for name, iterations in baseline.android_runtime_expected_iterations(
                    100_000
                ).items()
            },
            "android_artifacts": {
                name: 1_000 for name in baseline.ANDROID_ARTIFACTS
            },
            "cli": {
                name: {"wall_ms": 10.0, "peak_rss_bytes": 1_024}
                for name in baseline.CLI_COMMANDS
            },
            "reports": {
                name: {"bundle_bytes": 10_000, "pages": list(REQUIRED_PAGES)}
                for name in baseline.REPORT_GROUPS
            },
        },
    }


def acceptance_contract() -> dict:
    return {
        "schema": baseline.ACCEPTANCE_SCHEMA,
        "fixture": {
            "profile": "representative",
            "minimum_events": 1,
            "minimum_dictionary_entries": 1,
            "forbidden_warning_fragments": ["forbidden"],
        },
        "required_surfaces": list(baseline.MEASUREMENT_SURFACES),
        "required_report_suffixes": list(REQUIRED_PAGES),
        "relative_regression_limits": {
            "android_runtime_ns_per_op": 0.15,
            "android_artifact_bytes": 0.03,
            "go_ns_per_op": 0.15,
            "go_bytes_per_op": 0.10,
            "go_allocs_per_op": 0.10,
            "cli_wall_ms": 0.15,
            "cli_peak_rss_bytes": 0.10,
            "report_bundle_bytes": 0.05,
        },
        "absolute_targets": {
            "inspect_report_peak_rss_bytes": 10_000,
            "inspect_report_bundle_bytes": 100_000,
            "inspect_report_wall_ms": 100.0,
            "compare_report_wall_ms": 100.0,
        },
    }


class RepositoryAcceptanceContractTest(unittest.TestCase):
    def test_checked_in_acceptance_contract_matches_current_runner_schema(self) -> None:
        acceptance = baseline.read_json(baseline.DEFAULT_ACCEPTANCE)
        failures: list[str] = []

        baseline.validate_acceptance(acceptance, failures)

        self.assertEqual([], failures)
        self.assertEqual(
            set(baseline.MEASUREMENT_SURFACES), set(acceptance["required_surfaces"])
        )
        self.assertIn(
            "Качество сбора:",
            acceptance["fixture"]["forbidden_warning_fragments"],
        )


class PerformanceBaselineCheckTest(unittest.TestCase):
    def check(
        self,
        reference: dict,
        candidate: dict,
        acceptance: dict | None = None,
    ) -> tuple[int, str]:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            reference_path = directory / "reference.json"
            candidate_path = directory / "candidate.json"
            acceptance_path = directory / "acceptance.json"
            for path, value in (
                (reference_path, reference),
                (candidate_path, candidate),
                (
                    acceptance_path,
                    acceptance if acceptance is not None else acceptance_contract(),
                ),
            ):
                path.write_text(json.dumps(value))
            output = io.StringIO()
            with redirect_stdout(output):
                status = baseline.check_candidate(
                    reference_path, candidate_path, acceptance_path
                )
            return status, output.getvalue()

    def test_complete_candidate_passes(self) -> None:
        reference = complete_result()
        status, output = self.check(reference, copy.deepcopy(reference))

        self.assertEqual(0, status, output)
        self.assertIn("PASS:", output)

    def test_surface_parity_mismatch_fails(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["surfaces"]["go_benchmarks"] = False
        candidate["capture_config"]["go_benchmark_count"] = None
        candidate["measurements"]["go_benchmarks"] = {}

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("captured surface differs: go_benchmarks", output)

    def test_acceptance_required_surfaces_reject_focused_capture(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        for result in (reference, candidate):
            result["surfaces"]["go_benchmarks"] = False
            result["capture_config"]["go_benchmark_count"] = None
            result["measurements"]["go_benchmarks"] = {}

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn(
            "reference did not capture acceptance-required surface: go_benchmarks",
            output,
        )
        self.assertIn(
            "candidate did not capture acceptance-required surface: go_benchmarks",
            output,
        )

    def test_enabled_empty_surface_fails(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["measurements"]["go_benchmarks"] = {}

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("enabled surface go_benchmarks has no measurements", output)

    def test_decoded_counts_must_exactly_match_fixture_metadata(self) -> None:
        for target, message in (
            ("event_count", "decoded event_count differs from fixture events"),
            (
                "dictionary_entries",
                "decoded dictionary_entries differs from fixture dictionary_entries",
            ),
        ):
            with self.subTest(target=target):
                reference = complete_result()
                candidate = copy.deepcopy(reference)
                candidate["quality"][target] -= 1

                status, output = self.check(reference, candidate)

                self.assertEqual(1, status)
                self.assertIn(message, output)

    def test_fixture_record_totals_are_fail_closed(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["fixture"]["total_records"] -= 1

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("fixture total records are inconsistent", output)

    def test_fixture_composition_counts_must_be_positive(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["fixture"]["flow_tuples"] = 0

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("fixture count is missing or invalid: flow_tuples", output)

    def test_fixture_metadata_must_match_exactly(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["fixture"]["runtime_unique_edges"] += 1

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("fixture metadata differs: runtime_unique_edges", output)

    def test_fixture_schema_and_profile_are_checked(self) -> None:
        for mutate, message in (
            (
                lambda result: result["fixture"].__setitem__("schema", 1),
                "fixture schema is 1; expected 2",
            ),
            (
                lambda result: result["fixture"].__setitem__("profile", "smoke"),
                "fixture profile is 'smoke'; capture profile is 'representative'",
            ),
        ):
            with self.subTest(message=message):
                reference = complete_result()
                candidate = copy.deepcopy(reference)
                mutate(candidate)

                status, output = self.check(reference, candidate)

                self.assertEqual(1, status)
                self.assertIn(message, output)

    def test_acceptance_profile_is_checked(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        reference["profile"] = reference["fixture"]["profile"] = "smoke"
        candidate["profile"] = candidate["fixture"]["profile"] = "smoke"
        reference["capture_config"]["profile"] = "smoke"
        candidate["capture_config"]["profile"] = "smoke"

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("candidate profile is 'smoke'; expected 'representative'", output)

    def test_missing_metric_on_either_side_fails(self) -> None:
        for missing_from in ("reference", "candidate"):
            with self.subTest(missing_from=missing_from):
                reference = complete_result()
                candidate = copy.deepcopy(reference)
                target = reference if missing_from == "reference" else candidate
                del target["measurements"]["go_benchmarks"][
                    baseline.GO_BENCHMARKS[0]
                ]["ns_per_op"]

                status, output = self.check(reference, candidate)

                self.assertEqual(1, status)
                self.assertIn("ns_per_op", output)

    def test_go_allocations_are_part_of_regression_acceptance(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["measurements"]["go_benchmarks"][baseline.GO_BENCHMARKS[0]][
            "allocs_per_op"
        ] = 3.0

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("go benchmark allocs/op regressed", output)

    def test_capture_configuration_must_match(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["capture_config"]["android_build_tools_version"] = "36.0.0"

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn(
            "capture configuration differs: android_build_tools_version", output
        )

    def test_sample_count_must_match_capture_configuration(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["measurements"]["go_benchmarks"][baseline.GO_BENCHMARKS[0]][
            "samples"
        ] = 2

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("samples differ from capture configuration", output)

    def test_runtime_iterations_must_match_capture_configuration(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["measurements"]["android_runtime"][
            baseline.ANDROID_RUNTIME_BENCHMARKS[0]
        ]["iterations"] += 1

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("iterations differ from capture configuration", output)

    def test_malformed_acceptance_is_reported_without_indexing_it(self) -> None:
        acceptance = acceptance_contract()
        acceptance["fixture"]["minimum_events"] = "many"

        status, output = self.check(complete_result(), complete_result(), acceptance)

        self.assertEqual(1, status)
        self.assertIn("minimum_events is missing or invalid", output)

    def test_minimum_quality_contract_applies_to_both_results(self) -> None:
        acceptance = acceptance_contract()
        acceptance["fixture"]["minimum_events"] = 60_001

        status, output = self.check(
            complete_result(), complete_result(), acceptance
        )

        self.assertEqual(1, status)
        self.assertIn("reference decoded event count is below", output)
        self.assertIn("candidate decoded event count is below", output)

    def test_forbidden_quality_warnings_apply_to_reference(self) -> None:
        reference = complete_result()
        reference["quality"]["warnings"] = ["Качество сбора: writer lost data."]
        acceptance = acceptance_contract()
        acceptance["fixture"]["forbidden_warning_fragments"] = ["Качество сбора:"]

        status, output = self.check(reference, complete_result(), acceptance)

        self.assertEqual(1, status)
        self.assertIn("reference forbidden quality warning", output)

    def test_structured_collection_quality_must_be_pristine_on_reference(self) -> None:
        reference = complete_result()
        reference["quality"]["collection"]["known_lost_events"] = 1

        status, output = self.check(reference, complete_result())

        self.assertEqual(1, status)
        self.assertIn("reference collection quality is not pristine", output)

    def test_zero_report_bundle_fails_capture_validation(self) -> None:
        candidate = complete_result()
        candidate["measurements"]["reports"]["inspect"]["bundle_bytes"] = 0

        status, output = self.check(complete_result(), candidate)

        self.assertEqual(1, status)
        self.assertIn("reports/inspect metric not measured: bundle_bytes", output)

    def test_unbounded_measurement_is_rejected_without_overflow(self) -> None:
        reference = complete_result()
        candidate = complete_result()
        reference["measurements"]["go_benchmarks"][baseline.GO_BENCHMARKS[0]][
            "ns_per_op"
        ] = 10**400

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("metric not measured: ns_per_op", output)

    def test_non_object_capture_is_reported_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            reference_path = directory / "reference.json"
            candidate_path = directory / "candidate.json"
            acceptance_path = directory / "acceptance.json"
            reference_path.write_text("[]")
            candidate_path.write_text(json.dumps(complete_result()))
            acceptance_path.write_text(json.dumps(acceptance_contract()))
            output = io.StringIO()

            with redirect_stdout(output):
                status = baseline.check_candidate(
                    reference_path, candidate_path, acceptance_path
                )

        self.assertEqual(1, status)
        self.assertIn("JSON root must be an object", output.getvalue())

    def test_reference_and_candidate_cannot_share_an_inode(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            capture_path = directory / "capture.json"
            alias_path = directory / "alias.json"
            acceptance_path = directory / "acceptance.json"
            capture_path.write_text(json.dumps(complete_result()))
            alias_path.hardlink_to(capture_path)
            acceptance_path.write_text(json.dumps(acceptance_contract()))
            output = io.StringIO()

            with redirect_stdout(output):
                status = baseline.check_candidate(
                    capture_path, alias_path, acceptance_path
                )

        self.assertEqual(1, status)
        self.assertIn("resolve to the same file", output.getvalue())

    def test_missing_required_report_group_fails(self) -> None:
        for report_name in baseline.REPORT_GROUPS:
            with self.subTest(report_name=report_name):
                reference = complete_result()
                candidate = copy.deepcopy(reference)
                del candidate["measurements"]["reports"][report_name]

                status, output = self.check(reference, candidate)

                self.assertEqual(1, status)
                self.assertIn(f"reports measurement is missing: {report_name}", output)

    def test_missing_required_report_page_fails(self) -> None:
        reference = complete_result()
        candidate = copy.deepcopy(reference)
        candidate["measurements"]["reports"]["inspect"]["pages"].remove(
            "-diagnostics.html"
        )

        status, output = self.check(reference, candidate)

        self.assertEqual(1, status)
        self.assertIn("inspect report is missing pages: -diagnostics.html", output)

    def test_missing_absolute_target_value_fails(self) -> None:
        failures: list[str] = []

        baseline.enforce_ceiling(failures, "required value", None, 100)

        self.assertEqual(["required value not measured"], failures)


class TimedCommandTest(unittest.TestCase):
    def test_supported_time_tool_requires_parsable_rss(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            time_tool = directory / "time"
            time_tool.touch()

            def completed(command: list[str], **_: object) -> subprocess.CompletedProcess:
                output = Path(command[command.index("-o") + 1])
                output.write_text("time output without resident memory\n")
                return subprocess.CompletedProcess(command, 0)

            with (
                mock.patch.object(baseline, "TIME_TOOL", time_tool),
                mock.patch.object(baseline.platform, "system", return_value="Linux"),
                mock.patch.object(baseline.subprocess, "run", side_effect=completed),
            ):
                with self.assertRaisesRegex(RuntimeError, "parsable peak RSS"):
                    baseline.timed_command(
                        "inspect",
                        ["fake-command"],
                        cwd=directory,
                        artifact_dir=directory,
                    )

    def test_absent_time_tool_allows_unavailable_rss(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            missing_time_tool = directory / "missing-time"
            completed = subprocess.CompletedProcess(["fake-command"], 0)
            with (
                mock.patch.object(baseline, "TIME_TOOL", missing_time_tool),
                mock.patch.object(baseline.platform, "system", return_value="Linux"),
                mock.patch.object(baseline.subprocess, "run", return_value=completed),
            ):
                measurement = baseline.timed_command(
                    "inspect",
                    ["fake-command"],
                    cwd=directory,
                    artifact_dir=directory,
                )

        self.assertIsNone(measurement["peak_rss_bytes"])


class CaptureWorkspaceTest(unittest.TestCase):
    def capture_args(self, output: Path) -> SimpleNamespace:
        return SimpleNamespace(
            out=output,
            profile="smoke",
            benchmark_count=1,
            runtime_iterations=1,
            android_build_tools=None,
            skip_android=True,
            skip_go_benchmarks=True,
        )

    def test_refuses_to_delete_unowned_artifact_directory(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            output = directory / "candidate.json"
            output.write_text("stale")
            artifact_dir = directory / "candidate-artifacts"
            artifact_dir.mkdir()
            (artifact_dir / "valuable.txt").write_text("keep")

            with self.assertRaisesRegex(RuntimeError, "unowned artifact directory"):
                baseline.prepare_capture_workspace(output)

            self.assertEqual(
                "stale",
                output.read_text(),
                "preflight refusal must not remove the previous result",
            )
            self.assertEqual("keep", (artifact_dir / "valuable.txt").read_text())

    def test_refuses_symlinked_output_without_touching_its_target(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            target = directory / "important.json"
            target.write_text('{"important": true}')
            output = directory / "candidate.json"
            output.symlink_to(target)

            with self.assertRaisesRegex(RuntimeError, "symlinked capture output"):
                baseline.prepare_capture_workspace(output)

            self.assertTrue(output.is_symlink())
            self.assertEqual('{"important": true}', target.read_text())

    def test_capture_keeps_output_path_lexical_when_rejecting_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            target = directory / "important.json"
            target.write_text('{"important": true}')
            output = directory / "candidate.json"
            output.symlink_to(target)
            args = self.capture_args(output)

            with self.assertRaisesRegex(RuntimeError, "symlinked capture output"):
                baseline.capture_baseline(args)

            self.assertTrue(output.is_symlink())
            self.assertEqual('{"important": true}', target.read_text())

    def test_owned_artifact_directory_is_reset(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            output = directory / "candidate.json"
            artifact_dir = baseline.prepare_capture_workspace(output)
            (artifact_dir / "stale.txt").write_text("stale")

            reset = baseline.prepare_capture_workspace(output)

            self.assertEqual(artifact_dir, reset)
            self.assertFalse((reset / "stale.txt").exists())
            self.assertEqual(
                baseline.ARTIFACT_DIRECTORY_MARKER_CONTENT,
                (reset / baseline.ARTIFACT_DIRECTORY_MARKER).read_text(),
            )

    def test_capture_requires_json_output_extension(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "candidate.txt"

            with self.assertRaisesRegex(RuntimeError, "must use the .json extension"):
                baseline.capture_baseline(self.capture_args(output))

            self.assertFalse(output.exists())

    def test_capture_rejects_uppercase_json_suffix_to_avoid_stem_collision(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "candidate.JSON"

            with self.assertRaisesRegex(RuntimeError, "must use the .json extension"):
                baseline.capture_baseline(self.capture_args(output))

    def test_concurrent_capture_for_same_output_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "candidate.json"
            with baseline.capture_output_lock(output):
                with self.assertRaisesRegex(RuntimeError, "already running"):
                    with baseline.capture_output_lock(output):
                        self.fail("second lock must not be acquired")

    def test_unknown_lock_file_is_not_modified(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "candidate.json"
            lock = baseline.capture_lock_path(output)
            lock.write_text("belongs to another tool\n")

            with self.assertRaisesRegex(RuntimeError, "unowned capture lock"):
                with baseline.capture_output_lock(output):
                    self.fail("unknown lock must not be acquired")

            self.assertEqual("belongs to another tool\n", lock.read_text())

    def test_failed_capture_removes_stale_result(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "candidate.json"
            output.write_text('{"stale": true}')
            args = self.capture_args(output)
            with mock.patch.object(
                baseline, "run_command", side_effect=RuntimeError("fixture failed")
            ):
                with self.assertRaisesRegex(RuntimeError, "fixture failed"):
                    baseline.capture_baseline(args)

            self.assertFalse(output.exists())

    def test_atomic_json_writer_replaces_output_and_leaves_no_temp_file(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            output = directory / "candidate.json"
            output.write_text("stale")

            baseline.write_json_atomic(output, {"schema": 1})

            self.assertEqual({"schema": 1}, json.loads(output.read_text()))
            self.assertEqual([], list(directory.glob(".candidate.json.*.tmp")))


class EnvironmentMetadataTest(unittest.TestCase):
    def test_cli_only_metadata_does_not_invoke_java(self) -> None:
        commands: list[list[str]] = []

        def completed(command: list[str], **_: object) -> subprocess.CompletedProcess:
            commands.append(command)
            return subprocess.CompletedProcess(command, 0, stdout="go version go1.24\n", stderr="")

        with mock.patch.object(baseline.subprocess, "run", side_effect=completed):
            metadata = baseline.environment_metadata(include_java=False)

        self.assertEqual("not-required", metadata["java"])
        self.assertTrue(metadata["cpu"])
        self.assertFalse(any(command[0] == "java" for command in commands))

    def test_java_version_uses_java_home_executable(self) -> None:
        commands: list[list[str]] = []

        def completed(command: list[str], **_: object) -> subprocess.CompletedProcess:
            commands.append(command)
            if command[-1] == "-version":
                return subprocess.CompletedProcess(
                    command, 0, stdout="", stderr="openjdk version 21\n"
                )
            return subprocess.CompletedProcess(command, 0, stdout="version\n", stderr="")

        with (
            mock.patch.dict(baseline.os.environ, {"JAVA_HOME": "/opt/test-jdk"}),
            mock.patch.object(baseline, "cpu_identifier", return_value="Test CPU"),
            mock.patch.object(baseline.subprocess, "run", side_effect=completed),
        ):
            metadata = baseline.environment_metadata(include_java=True)

        self.assertEqual("openjdk version 21", metadata["java"])
        self.assertIn(["/opt/test-jdk/bin/java", "-version"], commands)
        self.assertEqual("Test CPU", metadata["cpu"])

    def test_missing_version_command_is_reported_as_unavailable(self) -> None:
        with mock.patch.object(
            baseline.subprocess, "run", side_effect=FileNotFoundError("missing")
        ):
            self.assertEqual("unavailable", baseline.command_version(["go", "version"]))


class StructuredQualityTest(unittest.TestCase):
    def test_extraction_normalizes_omitted_empty_lists(self) -> None:
        raw = copy.deepcopy(complete_result()["quality"]["collection"])
        del raw["chain_issues"]
        del raw["reasons"]

        extracted = baseline.extract_collection_quality({"CollectionQuality": raw})

        self.assertEqual([], extracted["chain_issues"])
        self.assertEqual([], extracted["reasons"])
        self.assertEqual(set(baseline.COLLECTION_QUALITY_FIELDS), set(extracted))


class ComparisonFixtureTest(unittest.TestCase):
    def test_copy_is_byte_identical_but_not_the_same_file(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            fixture = directory / "fixture.jhlog"
            fixture.write_bytes(b"JHLOG\r\n\x09fixture-body")

            comparison = baseline.copy_fixture_for_comparison(fixture, directory)

            self.assertEqual(fixture.read_bytes(), comparison.read_bytes())
            self.assertNotEqual(fixture.resolve(), comparison.resolve())
            self.assertFalse(fixture.samefile(comparison))


class AndroidRuntimeBenchmarkCommandTest(unittest.TestCase):
    def test_forces_benchmark_task_to_run_on_every_capture(self) -> None:
        command = baseline.android_runtime_benchmark_command(12_345)

        self.assertEqual(1, command.count("--rerun-tasks"))
        self.assertIn(":jankhunter-runtime:testDebugUnitTest", command)
        self.assertIn("-Djankhunter.benchmark=true", command)
        self.assertIn("-Djankhunter.benchmark.iterations=12345", command)
        self.assertEqual(
            "io.jankhunter.runtime.JankHunterRuntimeBenchmarkTest",
            command[command.index("--tests") + 1],
        )

    def test_passes_selected_build_tools_as_gradle_property(self) -> None:
        command = baseline.android_runtime_benchmark_command(100, "36.1.0")

        self.assertIn("-PjankHunterBuildToolsVersion=36.1.0", command)

    def test_missing_benchmark_measurements_remain_a_hard_failure(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "no parsable measurements"):
            baseline.parse_android_benchmarks(
                "> Task :jankhunter-runtime:testDebugUnitTest UP-TO-DATE\n"
            )


class BuildToolsResolutionTest(unittest.TestCase):
    def test_selects_highest_stable_installed_version(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            sdk = Path(temporary)
            for version in ("9.0.0", "35.0.1", "36.1", "37.0.0-rc1"):
                (sdk / "build-tools" / version).mkdir(parents=True)

            with mock.patch.object(
                baseline, "resolve_android_sdk_directory", return_value=sdk
            ):
                selected = baseline.resolve_android_build_tools_version(None)

        self.assertEqual("36.1", selected)

    def test_explicit_missing_version_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            sdk = Path(temporary)
            (sdk / "build-tools").mkdir()
            with mock.patch.object(
                baseline, "resolve_android_sdk_directory", return_value=sdk
            ):
                with self.assertRaisesRegex(RuntimeError, "was not found"):
                    baseline.resolve_android_build_tools_version("35.0.0")

    def test_skips_existing_sdk_without_build_tools_for_later_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            empty_sdk = directory / "empty-sdk"
            usable_sdk = directory / "usable-sdk"
            empty_sdk.mkdir()
            (usable_sdk / "build-tools" / "36.0.0").mkdir(parents=True)

            with mock.patch.object(
                baseline,
                "android_sdk_candidates",
                return_value=[("first", empty_sdk), ("second", usable_sdk)],
            ):
                sdk = baseline.resolve_android_sdk_directory()
                version = baseline.resolve_android_build_tools_version(None)

        self.assertEqual(usable_sdk.resolve(), sdk)
        self.assertEqual("36.0.0", version)

    def test_requested_build_tools_can_be_found_in_later_sdk(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            first_sdk = directory / "first-sdk"
            second_sdk = directory / "second-sdk"
            (first_sdk / "build-tools" / "35.0.0").mkdir(parents=True)
            (second_sdk / "build-tools" / "36.0.0").mkdir(parents=True)

            with mock.patch.object(
                baseline,
                "android_sdk_candidates",
                return_value=[("first", first_sdk), ("second", second_sdk)],
            ):
                sdk = baseline.resolve_android_sdk_directory(
                    required_build_tools="36.0.0"
                )

        self.assertEqual(second_sdk.resolve(), sdk)


class BenchmarkParserTest(unittest.TestCase):
    def test_go_parser_requires_every_benchmark_and_requested_samples(self) -> None:
        packages = {
            "jhlog": baseline.GO_BENCHMARKS[:2],
            "analyze": baseline.GO_BENCHMARKS[2:3],
            "report": baseline.GO_BENCHMARKS[3:],
        }
        lines: list[str] = []
        for package, benchmarks in packages.items():
            lines.append(f"pkg: example/internal/{package}")
            for benchmark in benchmarks:
                short_name = benchmark.split("/", 1)[1]
                for _ in range(2):
                    lines.append(
                        f"{short_name}-8 100 12.5 ns/op 64 B/op 2 allocs/op"
                    )

        result = baseline.parse_go_benchmarks("\n".join(lines), expected_samples=2)

        self.assertEqual(set(baseline.GO_BENCHMARKS), set(result))
        self.assertTrue(all(row["samples"] == 2 for row in result.values()))

    def test_go_parser_rejects_wrong_sample_count(self) -> None:
        lines: list[str] = []
        for benchmark in baseline.GO_BENCHMARKS:
            package, short_name = benchmark.split("/", 1)
            lines.extend(
                (
                    f"pkg: example/internal/{package}",
                    f"{short_name}-8 100 12.5 ns/op 64 B/op 2 allocs/op",
                )
            )

        with self.assertRaisesRegex(RuntimeError, "produced 1 samples; expected 2"):
            baseline.parse_go_benchmarks("\n".join(lines), expected_samples=2)

    def test_android_parser_validates_all_required_iteration_counts(self) -> None:
        requested = 100_000
        lines = [
            "JankHunter benchmark: "
            f"{name}, iterations={iterations}, total_ns={iterations * 10}, ns_per_op=10.0"
            for name, iterations in baseline.android_runtime_expected_iterations(
                requested
            ).items()
        ]

        result = baseline.parse_android_benchmarks(
            "\n".join(lines), expected_iterations=requested
        )

        self.assertEqual(set(baseline.ANDROID_RUNTIME_BENCHMARKS), set(result))

    def test_android_parser_rejects_duplicate_benchmark_name(self) -> None:
        name = baseline.ANDROID_RUNTIME_BENCHMARKS[0]
        row = (
            f"JankHunter benchmark: {name}, iterations=100, "
            "total_ns=1000, ns_per_op=10.0"
        )

        with self.assertRaisesRegex(RuntimeError, "duplicate row"):
            baseline.parse_android_benchmarks(f"{row}\n{row}")

    def test_android_parser_rejects_inconsistent_rounded_rate(self) -> None:
        name = baseline.ANDROID_RUNTIME_BENCHMARKS[0]
        row = (
            f"JankHunter benchmark: {name}, iterations=100, "
            "total_ns=1000, ns_per_op=10.2"
        )

        with self.assertRaisesRegex(RuntimeError, "inconsistent"):
            baseline.parse_android_benchmarks(row)


class AndroidArtifactSelectionTest(unittest.TestCase):
    def test_rejects_unsafe_version_before_constructing_artifact_paths(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            android = Path(temporary)
            (android / "gradle.properties").write_text(
                "jankHunterVersion=../../outside\n"
            )

            with (
                mock.patch.object(baseline, "ANDROID", android),
                self.assertRaisesRegex(RuntimeError, "missing or unsafe"),
            ):
                baseline.android_artifact_paths()

    def test_uses_exact_current_version_artifacts_not_stale_glob_matches(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            android = Path(temporary)
            (android / "gradle.properties").write_text("jankHunterVersion=1.0.0\n")
            artifacts = {
                "runtime_aar": android
                / "jankhunter-runtime/build/outputs/aar/jankhunter-runtime-release.aar",
                "annotations_jar": android
                / "jankhunter-annotations/build/libs/jankhunter-annotations-1.0.0.jar",
                "okhttp_aar": android
                / "jankhunter-okhttp3/build/outputs/aar/jankhunter-okhttp3-release.aar",
                "gradle_plugin_jar": android
                / "jankhunter-gradle-plugin/build/libs/jankhunter-gradle-plugin-1.0.0.jar",
                "sample_debug_apk": android
                / "sample-app/build/outputs/apk/debug/sample-app-debug.apk",
            }
            for index, artifact in enumerate(artifacts.values(), start=1):
                artifact.parent.mkdir(parents=True, exist_ok=True)
                artifact.write_bytes(b"x" * index)
            stale = android / "jankhunter-annotations/build/libs/jankhunter-annotations-9.9.9.jar"
            stale.write_bytes(b"stale artifact must be ignored")

            with mock.patch.object(baseline, "ANDROID", android):
                sizes = baseline.android_artifact_sizes()

        self.assertEqual(
            {name: index for index, name in enumerate(artifacts, start=1)}, sizes
        )

    def test_expected_artifacts_are_unlinked_before_gradle_build(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            android = Path(temporary)
            (android / "gradle.properties").write_text("jankHunterVersion=1.0.0\n")
            with mock.patch.object(baseline, "ANDROID", android):
                expected = baseline.android_artifact_paths()
                for artifact in expected.values():
                    artifact.parent.mkdir(parents=True, exist_ok=True)
                    artifact.write_bytes(b"stale")
                unrelated = android / "sample-app/build/outputs/apk/debug/keep.txt"
                unrelated.write_text("keep")

                baseline.remove_expected_android_artifacts()

            self.assertTrue(all(not artifact.exists() for artifact in expected.values()))
            self.assertEqual("keep", unrelated.read_text())


if __name__ == "__main__":
    unittest.main()
