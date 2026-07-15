#!/usr/bin/env python3
"""Capture and compare the local Jank Hunter performance contract."""

from __future__ import annotations

import argparse
import fcntl
import json
import math
import os
import platform
import re
import shutil
import stat
import statistics
import subprocess
import sys
import tempfile
import time
from contextlib import contextmanager
from pathlib import Path
from typing import Any, Iterator


ROOT = Path(__file__).resolve().parents[1]
CLI = ROOT / "cli"
ANDROID = ROOT / "android"
DEFAULT_ACCEPTANCE = ROOT / "benchmarks" / "acceptance.json"
DIAGNOSTICS = ROOT / "benchmarks" / "fixtures" / "instrumentation-diagnostics.jsonl"
TIME_TOOL = Path("/usr/bin/time")
CAPTURE_SCHEMA = 4
ACCEPTANCE_SCHEMA = 3
FIXTURE_SCHEMA = 2
ARTIFACT_DIRECTORY_MARKER = ".jankhunter-performance-artifacts-v1"
ARTIFACT_DIRECTORY_MARKER_CONTENT = "owned by scripts/performance-baseline.py\n"
CAPTURE_LOCK_MAGIC = b"JANK_HUNTER_PERFORMANCE_LOCK_V1\n"
BUILD_TOOLS_VERSION_PATTERN = re.compile(r"^[0-9]+(?:\.[0-9]+){1,2}$")
ARTIFACT_VERSION_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._+\-]*$")
MAX_CONTRACT_INTEGER = (1 << 63) - 1
MAX_CONTRACT_FLOAT = float(MAX_CONTRACT_INTEGER)
MAX_BENCHMARK_COUNT = 1_000
MAX_RUNTIME_ITERATIONS = 1_000_000_000
MEASUREMENT_SURFACES = (
    "go_benchmarks",
    "android_runtime",
    "android_artifacts",
    "cli",
    "reports",
)
CAPTURE_SURFACES = (*MEASUREMENT_SURFACES, "peak_rss")
CLI_COMMANDS = ("size", "inspect_json", "inspect_report", "compare_report")
REPORT_GROUPS = ("inspect", "compare")
ANDROID_ARTIFACTS = (
    "runtime_aar",
    "annotations_jar",
    "okhttp_aar",
    "gradle_plugin_jar",
    "sample_debug_apk",
)
GO_BENCHMARKS = (
    "jhlog/BenchmarkStreamFileRepresentative",
    "jhlog/BenchmarkProfileFileRepresentative",
    "analyze/BenchmarkInspectRepresentative",
    "report/BenchmarkWriteInspectRepresentative",
)
ANDROID_RUNTIME_BENCHMARKS = (
    "flow start/step/end",
    "log spam counter",
    "runnable wrapper creation",
    "runnable wrapper execution",
    "coroutine propagation wrapper",
    "ASM method hook no-writer guard",
    "metric aggregation counter/gauge",
    "binary log writer counter/gauge",
)
CAPTURE_CONFIG_KEYS = (
    "profile",
    "go_benchmark_count",
    "android_runtime_iterations",
    "android_build_tools_version",
)
ACCEPTANCE_RELATIVE_LIMITS = (
    "android_runtime_ns_per_op",
    "android_artifact_bytes",
    "go_ns_per_op",
    "go_bytes_per_op",
    "go_allocs_per_op",
    "cli_wall_ms",
    "cli_peak_rss_bytes",
    "report_bundle_bytes",
)
ACCEPTANCE_ABSOLUTE_TARGETS = (
    "inspect_report_peak_rss_bytes",
    "inspect_report_bundle_bytes",
    "inspect_report_wall_ms",
    "compare_report_wall_ms",
)
COLLECTION_QUALITY_FIELDS = (
    "level",
    "complete",
    "chain_valid",
    "sealed_segments",
    "unsealed_segments",
    "segments_with_quality",
    "segments_without_quality",
    "accepted_events",
    "written_events",
    "known_lost_events",
    "dictionary_overflow",
    "dictionary_truncated",
    "chain_issues",
    "reasons",
)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    capture = subparsers.add_parser("capture", help="capture a local baseline or candidate")
    capture.add_argument("--out", type=Path, required=True)
    capture.add_argument("--profile", choices=("smoke", "representative"), default="representative")
    capture.add_argument("--benchmark-count", type=benchmark_count, default=3)
    capture.add_argument(
        "--runtime-iterations", type=runtime_iterations, default=100_000
    )
    capture.add_argument(
        "--android-build-tools",
        "--android-build-tools-version",
        dest="android_build_tools",
        metavar="VERSION",
        help="Android Build Tools version; defaults to the highest installed stable version",
    )
    capture.add_argument("--skip-android", action="store_true")
    capture.add_argument("--skip-go-benchmarks", action="store_true")

    check = subparsers.add_parser("check", help="compare a candidate with a reference and product ceilings")
    check.add_argument("--reference", type=Path, required=True)
    check.add_argument("--candidate", type=Path, required=True)
    check.add_argument("--acceptance", type=Path, default=DEFAULT_ACCEPTANCE)

    args = parser.parse_args()
    if args.command == "capture":
        capture_baseline(args)
        return 0
    return check_candidate(args.reference, args.candidate, args.acceptance)


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed < 1:
        raise argparse.ArgumentTypeError("must be at least 1")
    return parsed


def benchmark_count(value: str) -> int:
    return bounded_positive_int(value, MAX_BENCHMARK_COUNT)


def runtime_iterations(value: str) -> int:
    return bounded_positive_int(value, MAX_RUNTIME_ITERATIONS)


def bounded_positive_int(value: str, maximum: int) -> int:
    parsed = positive_int(value)
    if parsed > maximum:
        raise argparse.ArgumentTypeError(f"must be at most {maximum}")
    return parsed


def prepare_capture_workspace(output: Path) -> Path:
    """Remove stale public output and reset only an artifact directory owned by this runner."""
    output.parent.mkdir(parents=True, exist_ok=True)
    if output.is_symlink():
        raise RuntimeError(f"refusing to replace symlinked capture output: {output}")
    if output.exists() and not output.is_file():
        raise RuntimeError(f"capture output is not a regular file: {output}")

    artifact_dir = output.parent / f"{output.stem}-artifacts"
    if artifact_dir.is_symlink():
        raise RuntimeError(f"refusing to replace symlinked artifact directory: {artifact_dir}")
    if artifact_dir.exists():
        marker = artifact_dir / ARTIFACT_DIRECTORY_MARKER
        if (
            not artifact_dir.is_dir()
            or marker.is_symlink()
            or not marker.is_file()
            or marker.read_text() != ARTIFACT_DIRECTORY_MARKER_CONTENT
        ):
            raise RuntimeError(
                f"refusing to delete unowned artifact directory: {artifact_dir}; "
                f"remove it manually if it is safe"
            )
    output.unlink(missing_ok=True)
    if artifact_dir.exists():
        shutil.rmtree(artifact_dir)
    artifact_dir.mkdir()
    (artifact_dir / ARTIFACT_DIRECTORY_MARKER).write_text(
        ARTIFACT_DIRECTORY_MARKER_CONTENT
    )
    return artifact_dir


def capture_baseline(args: argparse.Namespace) -> None:
    output = Path(os.path.abspath(args.out.expanduser()))
    validate_capture_output_path(output)
    with capture_output_lock(output):
        capture_baseline_to_output(args, output)


def validate_capture_output_path(output: Path) -> None:
    if output.suffix != ".json":
        raise RuntimeError(f"capture output must use the .json extension: {output}")
    output.parent.mkdir(parents=True, exist_ok=True)
    if output.is_symlink():
        raise RuntimeError(f"refusing to replace symlinked capture output: {output}")


@contextmanager
def capture_output_lock(output: Path) -> Iterator[None]:
    lock_path = capture_lock_path(output)
    no_follow = os.O_NOFOLLOW if hasattr(os, "O_NOFOLLOW") else 0
    descriptor: int | None = None
    created = False
    try:
        try:
            descriptor = os.open(
                lock_path,
                os.O_RDWR | os.O_CREAT | os.O_EXCL | no_follow,
                0o600,
            )
            created = True
            os.write(descriptor, CAPTURE_LOCK_MAGIC)
            os.fsync(descriptor)
        except FileExistsError:
            before = os.lstat(lock_path)
            if not stat.S_ISREG(before.st_mode):
                raise RuntimeError(f"capture lock is not a regular file: {lock_path}")
            descriptor = os.open(lock_path, os.O_RDWR | os.O_NONBLOCK | no_follow)
            after = os.fstat(descriptor)
            if (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
                raise RuntimeError(f"capture lock changed during validation: {lock_path}")

        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError(f"capture is already running for output: {output}") from error
        if not created:
            os.lseek(descriptor, 0, os.SEEK_SET)
            existing = os.read(descriptor, 4_096)
            if not is_owned_capture_lock(existing):
                raise RuntimeError(
                    f"refusing to modify unowned capture lock: {lock_path}"
                )
        os.ftruncate(descriptor, 0)
        os.lseek(descriptor, 0, os.SEEK_SET)
        os.write(descriptor, CAPTURE_LOCK_MAGIC + f"pid={os.getpid()}\n".encode())
        os.fsync(descriptor)
        yield
    except OSError as error:
        raise RuntimeError(f"cannot use capture lock {lock_path}: {error}") from error
    finally:
        if descriptor is not None:
            try:
                fcntl.flock(descriptor, fcntl.LOCK_UN)
            finally:
                os.close(descriptor)


def capture_lock_path(output: Path) -> Path:
    return output.with_name(f".jankhunter-performance-{output.name}.lock")


def is_owned_capture_lock(content: bytes) -> bool:
    if content == CAPTURE_LOCK_MAGIC:
        return True
    if not content.startswith(CAPTURE_LOCK_MAGIC):
        return False
    owner = content[len(CAPTURE_LOCK_MAGIC) :]
    return re.fullmatch(rb"pid=[0-9]+\n", owner) is not None


def capture_baseline_to_output(args: argparse.Namespace, output: Path) -> None:
    artifact_dir = prepare_capture_workspace(output)
    build_tools_version: str | None = None
    if not args.skip_android:
        build_tools_version = resolve_android_build_tools_version(args.android_build_tools)

    fixture = artifact_dir / "fixture.jhlog"
    fixture_metadata = artifact_dir / "fixture.json"
    run_command(
        "generate_fixture",
        [
            "go", "run", "./cmd/jhfixture",
            "--profile", args.profile,
            "--out", str(fixture),
            "--metadata", str(fixture_metadata),
        ],
        cwd=CLI,
        artifact_dir=artifact_dir,
    )

    result: dict[str, Any] = {
        "schema": CAPTURE_SCHEMA,
        "profile": args.profile,
        "capture_config": {
            "profile": args.profile,
            "go_benchmark_count": None if args.skip_go_benchmarks else args.benchmark_count,
            "android_runtime_iterations": None if args.skip_android else args.runtime_iterations,
            "android_build_tools_version": build_tools_version,
        },
        "environment": environment_metadata(include_java=not args.skip_android),
        "surfaces": {
            "go_benchmarks": not args.skip_go_benchmarks,
            "android_runtime": not args.skip_android,
            "android_artifacts": not args.skip_android,
            "cli": True,
            "reports": True,
            "peak_rss": peak_rss_capture_available(),
        },
        "fixture": read_json(fixture_metadata),
        "measurements": {
            "go_benchmarks": {},
            "android_runtime": {},
            "android_artifacts": {},
            "cli": {},
            "reports": {},
        },
    }

    if not args.skip_go_benchmarks:
        go_output = artifact_dir / "go-benchmarks.txt"
        run_command(
            "go_benchmarks",
            [
                "go", "test", "-run", "^$", "-bench", "Representative",
                "-benchmem", "-count", str(args.benchmark_count),
                "./internal/jhlog", "./internal/analyze", "./internal/report",
            ],
            cwd=CLI,
            artifact_dir=artifact_dir,
            stdout_path=go_output,
        )
        result["measurements"]["go_benchmarks"] = parse_go_benchmarks(
            go_output.read_text(), expected_samples=args.benchmark_count
        )

    if not args.skip_android:
        android_output = artifact_dir / "android-runtime.txt"
        run_command(
            "android_runtime",
            android_runtime_benchmark_command(args.runtime_iterations, build_tools_version),
            cwd=ANDROID,
            artifact_dir=artifact_dir,
            stdout_path=android_output,
        )
        result["measurements"]["android_runtime"] = parse_android_benchmarks(
            android_output.read_text(), expected_iterations=args.runtime_iterations
        )

        remove_expected_android_artifacts()
        run_command(
            "android_artifacts",
            [
                "./gradlew",
                ":jankhunter-runtime:assembleRelease",
                ":jankhunter-annotations:jar",
                ":jankhunter-okhttp3:assembleRelease",
                ":jankhunter-gradle-plugin:jar",
                ":sample-app:assembleDebug",
                f"-PjankHunterBuildToolsVersion={build_tools_version}",
                "--no-daemon", "--console=plain",
            ],
            cwd=ANDROID,
            artifact_dir=artifact_dir,
        )
        result["measurements"]["android_artifacts"] = android_artifact_sizes()

    binary = artifact_dir / "jankhunter"
    run_command(
        "build_cli",
        ["go", "build", "-trimpath", "-o", str(binary), "./cmd/jankhunter"],
        cwd=CLI,
        artifact_dir=artifact_dir,
    )

    size_json = artifact_dir / "size.json"
    size_measurement = timed_command(
        "size",
        [str(binary), "size", str(fixture), "--json"],
        cwd=CLI,
        artifact_dir=artifact_dir,
        stdout_path=size_json,
    )
    inspect_json = artifact_dir / "inspect.json"
    inspect_json_measurement = timed_command(
        "inspect_json",
        [
            str(binary), "inspect", str(fixture), "--json",
            "--instrumentation-diagnostics", str(DIAGNOSTICS),
        ],
        cwd=CLI,
        artifact_dir=artifact_dir,
        stdout_path=inspect_json,
    )

    reports = artifact_dir / "reports"
    reports.mkdir()
    inspect_report = reports / "inspect.html"
    inspect_report_measurement = timed_command(
        "inspect_report",
        [
            str(binary), "inspect", str(fixture),
            "--instrumentation-diagnostics", str(DIAGNOSTICS),
            "--out", str(inspect_report),
        ],
        cwd=CLI,
        artifact_dir=artifact_dir,
    )
    compare_report = reports / "compare.html"
    comparison_fixture = copy_fixture_for_comparison(fixture, artifact_dir)
    compare_report_measurement = timed_command(
        "compare_report",
        [
            str(binary), "compare",
            "--baseline", str(fixture), "--candidate", str(comparison_fixture),
            "--instrumentation-diagnostics", str(DIAGNOSTICS),
            "--out", str(compare_report),
        ],
        cwd=CLI,
        artifact_dir=artifact_dir,
    )

    summary = read_json(inspect_json)
    result["quality"] = {
        "event_count": summary.get("EventCount", summary.get("event_count", 0)),
        "dictionary_entries": summary.get("Dictionary", summary.get("dictionary", 0)),
        "warnings": summary.get("Warnings", summary.get("warnings", [])) or [],
        "collection": extract_collection_quality(summary),
    }
    result["measurements"]["cli"] = {
        "size": size_measurement,
        "inspect_json": inspect_json_measurement,
        "inspect_report": inspect_report_measurement,
        "compare_report": compare_report_measurement,
    }
    result["measurements"]["reports"] = {
        "inspect": report_measurement(reports, "inspect"),
        "compare": report_measurement(reports, "compare"),
    }
    capture_failures: list[str] = []
    validate_capture(result, "capture", capture_failures)
    if capture_failures:
        raise RuntimeError(f"invalid performance capture: {'; '.join(capture_failures)}")
    write_json_atomic(output, result)
    print(f"captured {args.profile} baseline: {args.out}")


def extract_collection_quality(summary: dict[str, Any]) -> dict[str, Any]:
    raw = summary.get("CollectionQuality", summary.get("collection_quality"))
    if not isinstance(raw, dict):
        return {}
    return {
        "level": raw.get("level"),
        "complete": raw.get("complete"),
        "chain_valid": raw.get("chain_valid"),
        "sealed_segments": raw.get("sealed_segments"),
        "unsealed_segments": raw.get("unsealed_segments"),
        "segments_with_quality": raw.get("segments_with_quality"),
        "segments_without_quality": raw.get("segments_without_quality"),
        "accepted_events": raw.get("accepted_events"),
        "written_events": raw.get("written_events"),
        "known_lost_events": raw.get("known_lost_events"),
        "dictionary_overflow": raw.get("dictionary_overflow"),
        "dictionary_truncated": raw.get("dictionary_truncated"),
        "chain_issues": raw.get("chain_issues", []),
        "reasons": raw.get("reasons", []),
    }


def copy_fixture_for_comparison(fixture: Path, artifact_dir: Path) -> Path:
    comparison_fixture = artifact_dir / "comparison-fixture.jhlog"
    if fixture.resolve() == comparison_fixture.resolve():
        raise RuntimeError("comparison fixture path aliases the source fixture")
    shutil.copyfile(fixture, comparison_fixture)
    if fixture.samefile(comparison_fixture):
        raise RuntimeError("comparison fixture aliases the source fixture")
    return comparison_fixture


def android_runtime_benchmark_command(
    iterations: int, build_tools_version: str | None = None
) -> list[str]:
    build_tools_argument = (
        [f"-PjankHunterBuildToolsVersion={build_tools_version}"]
        if build_tools_version
        else []
    )
    return [
        "./gradlew",
        ":jankhunter-runtime:testDebugUnitTest",
        "--rerun-tasks",
        "-Djankhunter.benchmark=true",
        f"-Djankhunter.benchmark.iterations={iterations}",
        *build_tools_argument,
        "--tests",
        "io.jankhunter.runtime.JankHunterRuntimeBenchmarkTest",
        "--no-daemon",
        "--console=plain",
    ]


def resolve_android_build_tools_version(requested: str | None) -> str:
    if requested is not None and not BUILD_TOOLS_VERSION_PATTERN.fullmatch(requested):
        raise RuntimeError(
            f"invalid Android Build Tools version {requested!r}; expected N.N or N.N.N"
        )
    sdk_dir = resolve_android_sdk_directory(required_build_tools=requested)
    build_tools_dir = sdk_dir / "build-tools"
    if requested is not None:
        if requested not in installed_build_tools_versions(sdk_dir):
            raise RuntimeError(
                f"Android Build Tools {requested} was not found in {build_tools_dir}"
            )
        return requested

    versions = installed_build_tools_versions(sdk_dir)
    return versions[-1]


def build_tools_version_key(version: str) -> tuple[int, int, int, int, str]:
    components = tuple(int(component) for component in version.split("."))
    normalized = (*components, *(0 for _ in range(3 - len(components))))[:3]
    return (*normalized, len(components), version)


def installed_build_tools_versions(sdk_dir: Path) -> list[str]:
    build_tools_dir = sdk_dir / "build-tools"
    if not build_tools_dir.is_dir():
        return []
    return sorted(
        (
            child.name
            for child in build_tools_dir.iterdir()
            if child.is_dir() and BUILD_TOOLS_VERSION_PATTERN.fullmatch(child.name)
        ),
        key=build_tools_version_key,
    )


def android_sdk_candidates() -> list[tuple[str, Path]]:
    candidates: list[tuple[str, Path]] = []
    for variable in ("ANDROID_HOME", "ANDROID_SDK_ROOT"):
        value = os.environ.get(variable)
        if value:
            candidates.append((variable, Path(value).expanduser()))

    local_sdk = read_gradle_property(ANDROID / "local.properties", "sdk.dir")
    if local_sdk:
        local_sdk_path = Path(local_sdk).expanduser()
        if not local_sdk_path.is_absolute():
            local_sdk_path = ANDROID / local_sdk_path
        candidates.append(("android/local.properties sdk.dir", local_sdk_path))

    home = Path.home()
    if platform.system() == "Darwin":
        candidates.append(("macOS default", home / "Library" / "Android" / "sdk"))
    else:
        candidates.append(("Linux default", home / "Android" / "Sdk"))
    return candidates


def resolve_android_sdk_directory(*, required_build_tools: str | None = None) -> Path:
    candidates = android_sdk_candidates()
    for _, candidate in candidates:
        if not candidate.is_dir():
            continue
        versions = installed_build_tools_versions(candidate)
        if required_build_tools is None and versions:
            return candidate.resolve()
        if required_build_tools is not None and required_build_tools in versions:
            return candidate.resolve()
    checked = ", ".join(f"{source}={path}" for source, path in candidates)
    requirement = (
        f"Android Build Tools {required_build_tools}"
        if required_build_tools is not None
        else "stable Android Build Tools"
    )
    raise RuntimeError(
        f"Android SDK containing {requirement} was not found; "
        "set ANDROID_HOME/ANDROID_SDK_ROOT or sdk.dir"
        + (f" (checked {checked})" if checked else "")
    )


def read_gradle_property(path: Path, key: str) -> str | None:
    if not path.is_file():
        return None
    for raw_line in path.read_text(errors="replace").splitlines():
        line = raw_line.strip()
        if not line or line.startswith(("#", "!")):
            continue
        match = re.match(r"([^:=\s]+)\s*[:=]\s*(.*)$", line)
        if match and match.group(1) == key:
            return match.group(2).replace(r"\:", ":").replace(r"\\", "\\")
    return None


def run_command(
    label: str,
    command: list[str],
    *,
    cwd: Path,
    artifact_dir: Path,
    stdout_path: Path | None = None,
) -> None:
    stdout_path = stdout_path or artifact_dir / f"{label}.stdout.txt"
    stderr_path = artifact_dir / f"{label}.stderr.txt"
    with stdout_path.open("w") as stdout, stderr_path.open("w") as stderr:
        completed = subprocess.run(command, cwd=cwd, stdout=stdout, stderr=stderr, text=True, check=False)
    if completed.returncode != 0:
        raise RuntimeError(f"{label} failed with exit code {completed.returncode}; see {stderr_path.name}")


def timed_command(
    label: str,
    command: list[str],
    *,
    cwd: Path,
    artifact_dir: Path,
    stdout_path: Path | None = None,
) -> dict[str, Any]:
    stdout_path = stdout_path or artifact_dir / f"{label}.stdout.txt"
    stderr_path = artifact_dir / f"{label}.stderr.txt"
    time_path = artifact_dir / f"{label}.time.txt"
    wrapped, rss_expected = timed_command_line(command, time_path)
    started = time.perf_counter_ns()
    with stdout_path.open("w") as stdout, stderr_path.open("w") as stderr:
        completed = subprocess.run(wrapped, cwd=cwd, stdout=stdout, stderr=stderr, text=True, check=False)
    wall_ms = (time.perf_counter_ns() - started) / 1_000_000
    if completed.returncode != 0:
        raise RuntimeError(f"{label} failed with exit code {completed.returncode}; see {stderr_path.name}")
    peak_rss = parse_peak_rss(time_path)
    if rss_expected and peak_rss is None:
        raise RuntimeError(
            f"{label} completed but {TIME_TOOL} did not produce parsable peak RSS; "
            f"see {time_path.name}"
        )
    return {
        "wall_ms": round(wall_ms, 3),
        "peak_rss_bytes": peak_rss,
    }


def peak_rss_capture_available() -> bool:
    return TIME_TOOL.exists() and platform.system() in ("Darwin", "Linux")


def timed_command_line(command: list[str], output: Path) -> tuple[list[str], bool]:
    if not peak_rss_capture_available():
        return command, False
    if platform.system() == "Darwin":
        return [str(TIME_TOOL), "-l", "-o", str(output), *command], True
    return [str(TIME_TOOL), "-v", "-o", str(output), *command], True


def parse_peak_rss(path: Path) -> int | None:
    if not path.exists():
        return None
    text = path.read_text(errors="replace")
    mac = re.search(r"(?m)^\s*(\d+)\s+maximum resident set size\s*$", text)
    if mac:
        return int(mac.group(1))
    linux = re.search(r"Maximum resident set size \(kbytes\):\s*(\d+)", text)
    if linux:
        return int(linux.group(1)) * 1024
    return None


def parse_go_benchmarks(
    text: str, *, expected_samples: int | None = None
) -> dict[str, Any]:
    package = "unknown"
    samples: dict[str, dict[str, list[float]]] = {}
    for line in text.splitlines():
        if line.startswith("pkg: "):
            package = line.removeprefix("pkg: ").rsplit("/", 1)[-1]
            continue
        fields = line.split()
        if not fields or not fields[0].startswith("Benchmark"):
            continue
        name = f"{package}/{fields[0].rsplit('-', 1)[0]}"
        row = samples.setdefault(name, {"ns_per_op": [], "bytes_per_op": [], "allocs_per_op": []})
        for index, unit in enumerate(fields):
            if index == 0:
                continue
            if unit == "ns/op":
                metric = "ns_per_op"
            elif unit == "B/op":
                metric = "bytes_per_op"
            elif unit == "allocs/op":
                metric = "allocs_per_op"
            else:
                continue
            value = float(fields[index - 1])
            if not is_nonnegative_number(value):
                raise RuntimeError(
                    f"Go benchmark {name} contains an invalid {metric} measurement"
                )
            row[metric].append(value)
    if not samples:
        raise RuntimeError("Go benchmark output contained no parsable measurements")
    missing_benchmarks = sorted(set(GO_BENCHMARKS) - samples.keys())
    unexpected_benchmarks = sorted(samples.keys() - set(GO_BENCHMARKS))
    if missing_benchmarks or unexpected_benchmarks:
        details: list[str] = []
        if missing_benchmarks:
            details.append("missing " + ", ".join(missing_benchmarks))
        if unexpected_benchmarks:
            details.append("unexpected " + ", ".join(unexpected_benchmarks))
        raise RuntimeError("Go benchmark set differs from the contract: " + "; ".join(details))
    result: dict[str, Any] = {}
    for name, row in sorted(samples.items()):
        sample_counts = {metric: len(values) for metric, values in row.items()}
        if any(count == 0 for count in sample_counts.values()):
            missing = ", ".join(
                metric for metric, count in sample_counts.items() if count == 0
            )
            raise RuntimeError(f"Go benchmark {name} omitted required metrics: {missing}")
        if len(set(sample_counts.values())) != 1:
            raise RuntimeError(f"Go benchmark {name} has inconsistent metric sample counts")
        sample_count = next(iter(sample_counts.values()))
        if expected_samples is not None and sample_count != expected_samples:
            raise RuntimeError(
                f"Go benchmark {name} produced {sample_count} samples; "
                f"expected {expected_samples}"
            )
        result[name] = {
            metric: round(statistics.median(values), 3)
            for metric, values in row.items()
        }
        result[name]["samples"] = sample_count
    return result


def parse_android_benchmarks(
    text: str, *, expected_iterations: int | None = None
) -> dict[str, Any]:
    pattern = re.compile(
        r"JankHunter benchmark: (?P<name>.*?), iterations=(?P<iterations>\d+), "
        r"total_ns=(?P<total>\d+), ns_per_op=(?P<per_op>[0-9.]+)"
    )
    result: dict[str, Any] = {}
    for match in pattern.finditer(text):
        name = match.group("name")
        if name in result:
            raise RuntimeError(f"Android benchmark output contains duplicate row: {name}")
        iterations = int(match.group("iterations"))
        total_ns = int(match.group("total"))
        ns_per_op = float(match.group("per_op"))
        if not is_positive_int(iterations) or not is_positive_int(total_ns):
            raise RuntimeError(f"Android benchmark {name} has invalid counters")
        if not is_nonnegative_number(ns_per_op):
            raise RuntimeError(f"Android benchmark {name} has invalid ns_per_op")
        if not android_rate_is_consistent(total_ns, iterations, ns_per_op):
            raise RuntimeError(
                f"Android benchmark {name} has inconsistent total_ns/iterations/ns_per_op"
            )
        result[name] = {
            "iterations": iterations,
            "total_ns": total_ns,
            "ns_per_op": ns_per_op,
        }
    if not result:
        raise RuntimeError("Android benchmark output contained no parsable measurements")
    missing_benchmarks = sorted(set(ANDROID_RUNTIME_BENCHMARKS) - result.keys())
    unexpected_benchmarks = sorted(result.keys() - set(ANDROID_RUNTIME_BENCHMARKS))
    if missing_benchmarks or unexpected_benchmarks:
        details: list[str] = []
        if missing_benchmarks:
            details.append("missing " + ", ".join(missing_benchmarks))
        if unexpected_benchmarks:
            details.append("unexpected " + ", ".join(unexpected_benchmarks))
        raise RuntimeError(
            "Android benchmark set differs from the contract: " + "; ".join(details)
        )
    if expected_iterations is not None:
        expected_counts = android_runtime_expected_iterations(expected_iterations)
        mismatched = sorted(
            name for name, row in result.items() if row["iterations"] != expected_counts[name]
        )
        if mismatched:
            raise RuntimeError(
                "Android benchmark iterations differ from capture configuration for: "
                + ", ".join(mismatched)
            )
    return result


def android_rate_is_consistent(
    total_ns: int, iterations: int, ns_per_op: float | int
) -> bool:
    if (
        not is_positive_int(total_ns)
        or not is_positive_int(iterations)
        or not is_nonnegative_number(ns_per_op)
    ):
        return False
    calculated = total_ns / iterations
    return abs(calculated - ns_per_op) <= 0.050_001


def android_runtime_expected_iterations(requested: int) -> dict[str, int]:
    return {
        "flow start/step/end": requested,
        "log spam counter": max(requested, 2_000_000),
        "runnable wrapper creation": max(requested, 2_000_000),
        "runnable wrapper execution": max(requested, 2_000_000),
        "coroutine propagation wrapper": max(requested, 2_000_000),
        "ASM method hook no-writer guard": max(requested, 1_000_000) * 4,
        "metric aggregation counter/gauge": max(requested, 500_000) * 2,
        "binary log writer counter/gauge": requested * 2,
    }


def android_artifact_paths() -> dict[str, Path]:
    version = read_gradle_property(ANDROID / "gradle.properties", "jankHunterVersion")
    if not version or not ARTIFACT_VERSION_PATTERN.fullmatch(version):
        raise RuntimeError(
            "jankHunterVersion is missing or unsafe in android/gradle.properties"
        )
    return {
        "runtime_aar": ANDROID
        / "jankhunter-runtime/build/outputs/aar/jankhunter-runtime-release.aar",
        "annotations_jar": ANDROID
        / f"jankhunter-annotations/build/libs/jankhunter-annotations-{version}.jar",
        "okhttp_aar": ANDROID
        / "jankhunter-okhttp3/build/outputs/aar/jankhunter-okhttp3-release.aar",
        "gradle_plugin_jar": ANDROID
        / f"jankhunter-gradle-plugin/build/libs/jankhunter-gradle-plugin-{version}.jar",
        "sample_debug_apk": ANDROID
        / "sample-app/build/outputs/apk/debug/sample-app-debug.apk",
    }


def remove_expected_android_artifacts() -> None:
    for artifact in android_artifact_paths().values():
        validate_android_artifact_path(artifact)
        if artifact.is_symlink():
            raise RuntimeError(f"refusing to unlink symlinked Android artifact: {artifact}")
        if artifact.exists() and not artifact.is_file():
            raise RuntimeError(f"expected Android artifact path is not a file: {artifact}")
        artifact.unlink(missing_ok=True)


def android_artifact_sizes() -> dict[str, int]:
    artifacts = android_artifact_paths()
    sizes: dict[str, int] = {}
    for name, artifact in artifacts.items():
        validate_android_artifact_path(artifact)
        if artifact.is_symlink() or not artifact.is_file():
            raise RuntimeError(f"Android artifact was not produced: {name} ({artifact})")
        sizes[name] = artifact.stat().st_size
    return sizes


def validate_android_artifact_path(artifact: Path) -> None:
    if not artifact.parent.resolve().is_relative_to(ANDROID.resolve()):
        raise RuntimeError(
            f"refusing to use Android artifact outside the project: {artifact}"
        )


def report_measurement(directory: Path, prefix: str) -> dict[str, Any]:
    pages = sorted(directory.glob(f"{prefix}*.html"))
    suffixes = [page.name.removeprefix(prefix) for page in pages]
    return {
        "bundle_bytes": sum(page.stat().st_size for page in pages),
        "pages": suffixes,
    }


def environment_metadata(*, include_java: bool = True) -> dict[str, str]:
    return {
        "os": platform.system(),
        "os_release": platform.release(),
        "arch": platform.machine(),
        "cpu": cpu_identifier(),
        "python": platform.python_version(),
        "go": command_version(["go", "version"]),
        "java": (
            command_version([str(java_executable()), "-version"], stderr=True)
            if include_java
            else "not-required"
        ),
    }


def java_executable() -> Path | str:
    java_home = os.environ.get("JAVA_HOME")
    return Path(java_home).expanduser() / "bin" / "java" if java_home else "java"


def cpu_identifier() -> str:
    system = platform.system()
    if system == "Darwin":
        for key in ("machdep.cpu.brand_string", "hw.model"):
            value = command_version(["sysctl", "-n", key])
            if value != "unavailable":
                return value
    elif system == "Linux":
        cpuinfo = Path("/proc/cpuinfo")
        if cpuinfo.is_file():
            for line in cpuinfo.read_text(errors="replace").splitlines():
                if line.lower().startswith(("model name", "hardware")) and ":" in line:
                    value = line.split(":", 1)[1].strip()
                    if value:
                        return value
    return platform.processor() or platform.machine() or "unavailable"


def command_version(command: list[str], *, stderr: bool = False) -> str:
    try:
        completed = subprocess.run(command, capture_output=True, text=True, check=False)
    except OSError:
        return "unavailable"
    value = completed.stderr if stderr else completed.stdout
    return value.strip().splitlines()[0] if value.strip() else "unavailable"


def validate_acceptance(acceptance: dict[str, Any], failures: list[str]) -> None:
    expected_top_level = {
        "schema",
        "fixture",
        "required_surfaces",
        "required_report_suffixes",
        "relative_regression_limits",
        "absolute_targets",
    }
    if acceptance.get("schema") != ACCEPTANCE_SCHEMA:
        failures.append(
            f"acceptance schema is {acceptance.get('schema')!r}; "
            f"expected {ACCEPTANCE_SCHEMA}"
        )
    validate_exact_keys(acceptance, expected_top_level, "acceptance", failures)

    fixture = acceptance.get("fixture")
    fixture_keys = {
        "profile",
        "minimum_events",
        "minimum_dictionary_entries",
        "forbidden_warning_fragments",
    }
    if not isinstance(fixture, dict):
        failures.append("acceptance fixture contract is missing or invalid")
    else:
        validate_exact_keys(fixture, fixture_keys, "acceptance fixture", failures)
        if fixture.get("profile") not in ("smoke", "representative"):
            failures.append("acceptance fixture profile is missing or invalid")
        for key in ("minimum_events", "minimum_dictionary_entries"):
            if not is_nonnegative_int(fixture.get(key)):
                failures.append(f"acceptance fixture {key} is missing or invalid")
        warnings = fixture.get("forbidden_warning_fragments")
        if (
            not isinstance(warnings, list)
            or not warnings
            or any(not isinstance(value, str) or not value for value in warnings)
        ):
            failures.append(
                "acceptance forbidden_warning_fragments must be a non-empty string list"
            )

    pages = acceptance.get("required_report_suffixes")
    if (
        not isinstance(pages, list)
        or not pages
        or any(not isinstance(page, str) or not page for page in pages)
        or len(set(pages)) != len(pages)
    ):
        failures.append("acceptance required_report_suffixes must be a unique string list")

    required_surfaces = acceptance.get("required_surfaces")
    if (
        not isinstance(required_surfaces, list)
        or not required_surfaces
        or any(
            not isinstance(surface, str) or surface not in MEASUREMENT_SURFACES
            for surface in required_surfaces
        )
        or len(set(required_surfaces)) != len(required_surfaces)
    ):
        failures.append(
            "acceptance required_surfaces must be a unique list of known measurement surfaces"
        )

    validate_numeric_contract(
        acceptance.get("relative_regression_limits"),
        ACCEPTANCE_RELATIVE_LIMITS,
        "acceptance relative regression limits",
        failures,
        allow_zero=True,
        maximum=1.0,
    )
    validate_numeric_contract(
        acceptance.get("absolute_targets"),
        ACCEPTANCE_ABSOLUTE_TARGETS,
        "acceptance absolute targets",
        failures,
        allow_zero=False,
        maximum=MAX_CONTRACT_FLOAT,
    )


def validate_numeric_contract(
    raw: Any,
    required: tuple[str, ...],
    label: str,
    failures: list[str],
    *,
    allow_zero: bool,
    maximum: float,
) -> None:
    if not isinstance(raw, dict):
        failures.append(f"{label} object is missing or invalid")
        return
    validate_exact_keys(raw, set(required), label, failures)
    for key in required:
        value = raw.get(key)
        if (
            not is_number(value)
            or value < 0
            or value > maximum
            or (not allow_zero and value == 0)
        ):
            failures.append(f"{label} value is missing or invalid: {key}")


def validate_exact_keys(
    value: dict[str, Any], expected: set[str], label: str, failures: list[str]
) -> None:
    actual = set(value)
    missing = sorted(expected - actual)
    unexpected = sorted(actual - expected)
    if missing:
        failures.append(f"{label} is missing fields: {', '.join(missing)}")
    if unexpected:
        failures.append(f"{label} has unknown fields: {', '.join(unexpected)}")


def check_candidate(reference_path: Path, candidate_path: Path, acceptance_path: Path) -> int:
    try:
        if reference_path.samefile(candidate_path):
            print("FAIL: reference and candidate resolve to the same file")
            return 1
    except OSError:
        pass
    try:
        reference = read_json(reference_path)
        candidate = read_json(candidate_path)
        acceptance = read_json(acceptance_path)
    except (OSError, RuntimeError, ValueError) as error:
        print(f"FAIL: cannot read performance contract: {error}")
        return 1
    failures: list[str] = []

    validate_acceptance(acceptance, failures)
    if failures:
        return report_check_failures(failures)

    reference_surfaces = validate_capture(reference, "reference", failures)
    candidate_surfaces = validate_capture(candidate, "candidate", failures)
    if failures:
        return report_check_failures(failures)

    for surface in acceptance["required_surfaces"]:
        for label, captured in (
            ("reference", reference_surfaces),
            ("candidate", candidate_surfaces),
        ):
            if captured.get(surface) is not True:
                failures.append(
                    f"{label} did not capture acceptance-required surface: {surface}"
                )

    if reference_surfaces != candidate_surfaces:
        for surface in CAPTURE_SURFACES:
            if reference_surfaces.get(surface) != candidate_surfaces.get(surface):
                failures.append(
                    f"captured surface differs: {surface} "
                    f"(reference={reference_surfaces.get(surface)!r}, "
                    f"candidate={candidate_surfaces.get(surface)!r})"
                )

    reference_config = reference["capture_config"]
    candidate_config = candidate["capture_config"]
    for key in CAPTURE_CONFIG_KEYS:
        if reference_config[key] != candidate_config[key]:
            failures.append(
                f"capture configuration differs: {key} "
                f"(reference={reference_config[key]!r}, candidate={candidate_config[key]!r})"
            )

    if reference.get("profile") != candidate.get("profile"):
        failures.append("reference and candidate profiles differ")
    for key in ("os", "os_release", "arch", "cpu", "python", "go", "java"):
        if reference.get("environment", {}).get(key) != candidate.get("environment", {}).get(
            key
        ):
            failures.append(f"environment differs: {key}")

    reference_fixture = reference["fixture"]
    candidate_fixture = candidate["fixture"]
    for key in sorted(reference_fixture.keys() | candidate_fixture.keys()):
        if reference_fixture.get(key) != candidate_fixture.get(key):
            failures.append(
                f"fixture metadata differs: {key} "
                f"(reference={reference_fixture.get(key)!r}, "
                f"candidate={candidate_fixture.get(key)!r})"
            )

    fixture_contract = acceptance["fixture"]
    validate_result_against_acceptance(
        reference, "reference", fixture_contract, failures
    )
    validate_result_against_acceptance(
        candidate, "candidate", fixture_contract, failures
    )

    required_pages = set(acceptance["required_report_suffixes"])
    validate_report_pages(reference, "reference", reference_surfaces, required_pages, failures)
    validate_report_pages(candidate, "candidate", candidate_surfaces, required_pages, failures)

    limits = acceptance["relative_regression_limits"]
    compare_metric_maps(
        failures, "go benchmark ns/op",
        benchmark_metric_map(reference, "go_benchmarks", "ns_per_op"),
        benchmark_metric_map(candidate, "go_benchmarks", "ns_per_op"),
        limits["go_ns_per_op"],
    )
    compare_metric_maps(
        failures, "go benchmark B/op",
        benchmark_metric_map(reference, "go_benchmarks", "bytes_per_op"),
        benchmark_metric_map(candidate, "go_benchmarks", "bytes_per_op"),
        limits["go_bytes_per_op"],
    )
    compare_metric_maps(
        failures, "go benchmark allocs/op",
        benchmark_metric_map(reference, "go_benchmarks", "allocs_per_op"),
        benchmark_metric_map(candidate, "go_benchmarks", "allocs_per_op"),
        limits["go_allocs_per_op"],
    )
    compare_metric_maps(
        failures, "Android runtime ns/op",
        benchmark_metric_map(reference, "android_runtime", "ns_per_op"),
        benchmark_metric_map(candidate, "android_runtime", "ns_per_op"),
        limits["android_runtime_ns_per_op"],
    )
    compare_metric_maps(
        failures, "Android artifact bytes",
        measurement_rows(reference, "android_artifacts"),
        measurement_rows(candidate, "android_artifacts"),
        limits["android_artifact_bytes"],
    )

    reference_cli = measurement_rows(reference, "cli")
    candidate_cli = measurement_rows(candidate, "cli")
    compare_metric_maps(
        failures, "CLI wall time",
        nested_metric_map(reference_cli, "wall_ms"),
        nested_metric_map(candidate_cli, "wall_ms"),
        limits["cli_wall_ms"],
    )
    compare_metric_maps(
        failures, "CLI peak RSS",
        nested_metric_map(reference_cli, "peak_rss_bytes"),
        nested_metric_map(candidate_cli, "peak_rss_bytes"),
        limits["cli_peak_rss_bytes"],
    )
    compare_metric_maps(
        failures, "report bundle bytes",
        nested_metric_map(measurement_rows(reference, "reports"), "bundle_bytes"),
        nested_metric_map(measurement_rows(candidate, "reports"), "bundle_bytes"),
        limits["report_bundle_bytes"],
    )

    targets = acceptance["absolute_targets"]
    if candidate_surfaces.get("peak_rss"):
        enforce_ceiling(
            failures,
            "inspect report RSS",
            row_value(candidate_cli, "inspect_report", "peak_rss_bytes"),
            targets["inspect_report_peak_rss_bytes"],
        )
    enforce_ceiling(
        failures,
        "inspect report bundle",
        row_value(measurement_rows(candidate, "reports"), "inspect", "bundle_bytes"),
        targets["inspect_report_bundle_bytes"],
    )
    enforce_ceiling(
        failures,
        "inspect report wall time",
        row_value(candidate_cli, "inspect_report", "wall_ms"),
        targets["inspect_report_wall_ms"],
    )
    enforce_ceiling(
        failures,
        "compare report wall time",
        row_value(candidate_cli, "compare_report", "wall_ms"),
        targets["compare_report_wall_ms"],
    )

    if failures:
        return report_check_failures(failures)
    print("PASS: performance and quality acceptance contract")
    return 0


def validate_result_against_acceptance(
    result: dict[str, Any],
    label: str,
    fixture_contract: dict[str, Any],
    failures: list[str],
) -> None:
    expected_profile = fixture_contract["profile"]
    if result.get("profile") != expected_profile:
        failures.append(
            f"{label} profile is {result.get('profile')!r}; expected {expected_profile!r}"
        )
    quality = result["quality"]
    if quality["event_count"] < fixture_contract["minimum_events"]:
        failures.append(
            f"{label} decoded event count is below the acceptance fixture contract"
        )
    if quality["dictionary_entries"] < fixture_contract["minimum_dictionary_entries"]:
        failures.append(
            f"{label} decoded dictionary is below the acceptance fixture contract"
        )
    for warning in quality["warnings"]:
        lowered = warning.casefold()
        for fragment in fixture_contract["forbidden_warning_fragments"]:
            if fragment.casefold() in lowered:
                failures.append(f"{label} forbidden quality warning: {warning}")

    collection = quality["collection"]
    expected_events = result["fixture"]["events"]
    pristine_requirements = (
        (collection["level"] == "high", "level is not high"),
        (collection["complete"] is True, "capture is not complete"),
        (collection["chain_valid"] is True, "segment chain is invalid"),
        (collection["sealed_segments"] > 0, "no sealed segments were observed"),
        (collection["unsealed_segments"] == 0, "unsealed segments were observed"),
        (
            collection["segments_with_quality"] > 0,
            "no segment contains quality metadata",
        ),
        (
            collection["segments_without_quality"] == 0,
            "segments without quality metadata were observed",
        ),
        (
            collection["sealed_segments"] + collection["unsealed_segments"]
            == collection["segments_with_quality"]
            + collection["segments_without_quality"],
            "segment accounting is inconsistent",
        ),
        (
            collection["accepted_events"] == expected_events,
            "accepted event count differs from fixture metadata",
        ),
        (
            collection["written_events"] == expected_events,
            "written event count differs from fixture metadata",
        ),
        (collection["known_lost_events"] == 0, "known event loss was recorded"),
        (collection["dictionary_overflow"] == 0, "dictionary overflow was recorded"),
        (
            collection["dictionary_truncated"] == 0,
            "dictionary truncation was recorded",
        ),
        (collection["chain_issues"] == [], "segment chain issues were recorded"),
        (collection["reasons"] == [], "collection degradation reasons were recorded"),
    )
    for passed, reason in pristine_requirements:
        if not passed:
            failures.append(f"{label} collection quality is not pristine: {reason}")


def report_check_failures(failures: list[str]) -> int:
    for failure in failures:
        print(f"FAIL: {failure}")
    return 1


def benchmark_metric_map(result: dict[str, Any], surface: str, metric: str) -> dict[str, float]:
    rows = measurement_rows(result, surface)
    return {
        name: row[metric]
        for name, row in rows.items()
        if isinstance(row, dict) and is_number(row.get(metric))
    }


def nested_metric_map(rows: dict[str, Any], metric: str) -> dict[str, float]:
    return {
        name: row[metric]
        for name, row in rows.items()
        if isinstance(row, dict) and is_number(row.get(metric))
    }


def compare_metric_maps(
    failures: list[str],
    label: str,
    reference: dict[str, float],
    candidate: dict[str, float],
    tolerance: float,
) -> None:
    if not is_nonnegative_number(tolerance) or tolerance > 1.0:
        failures.append(f"{label} tolerance is invalid: {tolerance!r}")
        return
    for name in sorted(reference.keys() - candidate.keys()):
        failures.append(f"{label} not measured for candidate: {name}")
    for name in sorted(candidate.keys() - reference.keys()):
        failures.append(f"{label} has no reference measurement: {name}")
    for name in sorted(reference.keys() & candidate.keys()):
        if not is_nonnegative_number(reference[name]) or not is_nonnegative_number(
            candidate[name]
        ):
            failures.append(f"{label} contains an invalid measurement: {name}")
            continue
        allowed = reference[name] * (1.0 + tolerance)
        if candidate[name] > allowed:
            failures.append(f"{label} regressed for {name}: {candidate[name]:.3f} > {allowed:.3f}")


def enforce_ceiling(
    failures: list[str],
    label: str,
    value: float | int | None,
    ceiling: float | int,
) -> None:
    if not is_number(ceiling) or ceiling <= 0:
        failures.append(f"{label} target is invalid")
    elif value is None:
        failures.append(f"{label} not measured")
    elif not is_nonnegative_number(value):
        failures.append(f"{label} measurement is invalid")
    elif value > ceiling:
        failures.append(f"{label} exceeds target: {value} > {ceiling}")


def validate_capture(result: dict[str, Any], label: str, failures: list[str]) -> dict[str, bool]:
    validate_exact_keys(
        result,
        {
            "schema",
            "profile",
            "capture_config",
            "environment",
            "surfaces",
            "fixture",
            "quality",
            "measurements",
        },
        f"{label} capture",
        failures,
    )
    if result.get("schema") != CAPTURE_SCHEMA:
        failures.append(
            f"{label} capture schema is {result.get('schema')!r}; expected {CAPTURE_SCHEMA}"
        )

    profile = result.get("profile")
    if profile not in ("smoke", "representative"):
        failures.append(f"{label} capture profile is missing or invalid")

    raw_surfaces = result.get("surfaces")
    if not isinstance(raw_surfaces, dict):
        failures.append(f"{label} captured-surface metadata is missing")
        surfaces: dict[str, bool] = {}
    else:
        surfaces = {
            name: value
            for name, value in raw_surfaces.items()
            if name in CAPTURE_SURFACES and isinstance(value, bool)
        }
        for name in CAPTURE_SURFACES:
            if not isinstance(raw_surfaces.get(name), bool):
                failures.append(f"{label} captured surface {name} is missing or not boolean")
        validate_exact_keys(
            raw_surfaces, set(CAPTURE_SURFACES), f"{label} captured surfaces", failures
        )

    if surfaces.get("android_runtime") != surfaces.get("android_artifacts"):
        failures.append(f"{label} Android runtime and artifact surfaces must be captured together")
    for always_enabled in ("cli", "reports"):
        if surfaces.get(always_enabled) is not True:
            failures.append(f"{label} captured surface {always_enabled} must be enabled")

    raw_config = result.get("capture_config")
    if not isinstance(raw_config, dict):
        failures.append(f"{label} capture configuration is missing")
        capture_config: dict[str, Any] = {}
    else:
        capture_config = raw_config
        validate_exact_keys(
            capture_config,
            set(CAPTURE_CONFIG_KEYS),
            f"{label} capture configuration",
            failures,
        )
        if capture_config.get("profile") != profile:
            failures.append(f"{label} capture configuration profile differs from capture profile")
        validate_optional_capture_count(
            capture_config,
            "go_benchmark_count",
            surfaces.get("go_benchmarks") is True,
            label,
            failures,
            maximum=MAX_BENCHMARK_COUNT,
        )
        validate_optional_capture_count(
            capture_config,
            "android_runtime_iterations",
            surfaces.get("android_runtime") is True,
            label,
            failures,
            maximum=MAX_RUNTIME_ITERATIONS,
        )
        build_tools_version = capture_config.get("android_build_tools_version")
        if surfaces.get("android_runtime"):
            if (
                not isinstance(build_tools_version, str)
                or not BUILD_TOOLS_VERSION_PATTERN.fullmatch(build_tools_version)
            ):
                failures.append(
                    f"{label} Android Build Tools capture configuration is missing or invalid"
                )
        elif build_tools_version is not None:
            failures.append(
                f"{label} Android Build Tools capture configuration must be null when Android is skipped"
            )

    environment = result.get("environment")
    environment_keys = {"os", "os_release", "arch", "cpu", "python", "go", "java"}
    if not isinstance(environment, dict):
        failures.append(f"{label} environment metadata is missing")
    else:
        validate_exact_keys(environment, environment_keys, f"{label} environment", failures)
        for key in environment_keys:
            if not isinstance(environment.get(key), str) or not environment.get(key):
                failures.append(f"{label} environment value is missing or invalid: {key}")

    validate_fixture_quality_contract(result, label, failures)
    quality = result.get("quality")
    if isinstance(quality, dict):
        validate_exact_keys(
            quality,
            {"event_count", "dictionary_entries", "warnings", "collection"},
            f"{label} quality metadata",
            failures,
        )
        warnings = quality.get("warnings")
        if not isinstance(warnings, list) or any(
            not isinstance(warning, str) for warning in warnings
        ):
            failures.append(f"{label} decoded quality warnings are missing or invalid")
        validate_collection_quality(quality.get("collection"), label, failures)

    measurements = result.get("measurements")
    if not isinstance(measurements, dict):
        failures.append(f"{label} measurements object is missing")
        measurements = {}
    else:
        validate_exact_keys(
            measurements,
            set(MEASUREMENT_SURFACES),
            f"{label} measurements",
            failures,
        )

    for surface in MEASUREMENT_SURFACES:
        rows = measurements.get(surface)
        if surfaces.get(surface) and (not isinstance(rows, dict) or not rows):
            failures.append(f"{label} enabled surface {surface} has no measurements")
        elif surfaces.get(surface) is False and rows != {}:
            failures.append(f"{label} disabled surface {surface} contains measurements")

    validate_measurement_rows(result, label, surfaces, capture_config, failures)
    return surfaces


def validate_optional_capture_count(
    capture_config: dict[str, Any],
    key: str,
    enabled: bool,
    label: str,
    failures: list[str],
    *,
    maximum: int,
) -> None:
    value = capture_config.get(key)
    if enabled and (not is_positive_int(value) or value > maximum):
        failures.append(f"{label} capture configuration {key} is missing or invalid")
    elif not enabled and value is not None:
        failures.append(f"{label} capture configuration {key} must be null when skipped")


def validate_collection_quality(raw: Any, label: str, failures: list[str]) -> None:
    if not isinstance(raw, dict):
        failures.append(f"{label} structured collection quality is missing")
        return
    validate_exact_keys(
        raw,
        set(COLLECTION_QUALITY_FIELDS),
        f"{label} structured collection quality",
        failures,
    )
    if raw.get("level") not in ("low", "medium", "high"):
        failures.append(f"{label} collection quality level is missing or invalid")
    for field in ("complete", "chain_valid"):
        if not isinstance(raw.get(field), bool):
            failures.append(f"{label} collection quality {field} is missing or invalid")
    for field in (
        "sealed_segments",
        "unsealed_segments",
        "segments_with_quality",
        "segments_without_quality",
        "accepted_events",
        "written_events",
        "known_lost_events",
        "dictionary_overflow",
        "dictionary_truncated",
    ):
        if not is_nonnegative_int(raw.get(field)):
            failures.append(f"{label} collection quality {field} is missing or invalid")
    for field in ("chain_issues", "reasons"):
        values = raw.get(field)
        if not isinstance(values, list) or any(
            not isinstance(value, str) or not value for value in values
        ):
            failures.append(f"{label} collection quality {field} is missing or invalid")


def validate_fixture_quality_contract(
    result: dict[str, Any], label: str, failures: list[str]
) -> None:
    fixture = result.get("fixture")
    if not isinstance(fixture, dict):
        failures.append(f"{label} fixture metadata is missing")
        return
    fixture_fields = {
        "schema",
        "profile",
        "events",
        "data_records",
        "dictionary_entries",
        "dictionary_records",
        "control_records",
        "total_records",
        "runtime_call_events",
        "runtime_unique_edges",
        "flow_events",
        "flow_tuples",
        "signal_events",
        "duration_ms",
        "compressed_bytes",
    }
    validate_exact_keys(fixture, fixture_fields, f"{label} fixture metadata", failures)
    if fixture.get("schema") != FIXTURE_SCHEMA:
        failures.append(
            f"{label} fixture schema is {fixture.get('schema')!r}; expected {FIXTURE_SCHEMA}"
        )

    fixture_profile = fixture.get("profile")
    capture_profile = result.get("profile")
    if not isinstance(fixture_profile, str) or not fixture_profile:
        failures.append(f"{label} fixture profile is missing")
    elif fixture_profile != capture_profile:
        failures.append(
            f"{label} fixture profile is {fixture_profile!r}; capture profile is {capture_profile!r}"
        )

    count_names = (
        "events",
        "data_records",
        "dictionary_entries",
        "dictionary_records",
        "control_records",
        "total_records",
        "runtime_call_events",
        "runtime_unique_edges",
        "flow_events",
        "flow_tuples",
        "signal_events",
        "duration_ms",
        "compressed_bytes",
    )
    counts: dict[str, int] = {}
    for name in count_names:
        value = fixture.get(name)
        if not is_positive_int(value):
            failures.append(f"{label} fixture count is missing or invalid: {name}")
        else:
            counts[name] = value

    core_count_names = (
        "events",
        "data_records",
        "dictionary_entries",
        "dictionary_records",
        "control_records",
        "total_records",
    )
    if all(name in counts for name in core_count_names):
        if counts["events"] != counts["data_records"]:
            failures.append(
                f"{label} fixture semantic events differ from data records: "
                f"{counts['events']} != {counts['data_records']}"
            )
        if counts["dictionary_entries"] != counts["dictionary_records"]:
            failures.append(
                f"{label} fixture dictionary entries differ from dictionary records: "
                f"{counts['dictionary_entries']} != {counts['dictionary_records']}"
            )
        expected_total = (
            counts["data_records"]
            + counts["dictionary_records"]
            + counts["control_records"]
        )
        if counts["total_records"] != expected_total:
            failures.append(
                f"{label} fixture total records are inconsistent: "
                f"{counts['total_records']} != {expected_total}"
            )

    quality = result.get("quality")
    if not isinstance(quality, dict):
        failures.append(f"{label} decoded quality metadata is missing")
        return
    for quality_name, fixture_name in (
        ("event_count", "events"),
        ("dictionary_entries", "dictionary_entries"),
    ):
        decoded = quality.get(quality_name)
        if not is_nonnegative_int(decoded):
            failures.append(f"{label} decoded quality count is missing or invalid: {quality_name}")
            continue
        expected = counts.get(fixture_name)
        if expected is not None and decoded != expected:
            failures.append(
                f"{label} decoded {quality_name} differs from fixture {fixture_name}: "
                f"{decoded} != {expected}"
            )


def validate_measurement_rows(
    result: dict[str, Any],
    label: str,
    surfaces: dict[str, bool],
    capture_config: dict[str, Any],
    failures: list[str],
) -> None:
    if surfaces.get("go_benchmarks"):
        go_benchmarks = measurement_rows(result, "go_benchmarks")
        validate_named_rows(go_benchmarks, label, "go_benchmarks", GO_BENCHMARKS, failures)
        validate_row_metrics(
            go_benchmarks,
            label,
            "go_benchmarks",
            ("ns_per_op", "bytes_per_op", "allocs_per_op"),
            failures,
            GO_BENCHMARKS,
        )
        expected_samples = capture_config.get("go_benchmark_count")
        for name in GO_BENCHMARKS:
            row = go_benchmarks.get(name)
            if not isinstance(row, dict):
                continue
            validate_exact_keys(
                row,
                {"ns_per_op", "bytes_per_op", "allocs_per_op", "samples"},
                f"{label} go_benchmarks/{name}",
                failures,
            )
            samples = row.get("samples")
            if not is_positive_int(samples):
                failures.append(
                    f"{label} go_benchmarks/{name} metric not measured: samples"
                )
            elif is_positive_int(expected_samples) and samples != expected_samples:
                failures.append(
                    f"{label} go_benchmarks/{name} samples differ from capture configuration: "
                    f"{samples} != {expected_samples}"
                )
    if surfaces.get("android_runtime"):
        android_runtime = measurement_rows(result, "android_runtime")
        validate_named_rows(
            android_runtime,
            label,
            "android_runtime",
            ANDROID_RUNTIME_BENCHMARKS,
            failures,
        )
        validate_row_metrics(
            android_runtime,
            label,
            "android_runtime",
            ("ns_per_op",),
            failures,
            ANDROID_RUNTIME_BENCHMARKS,
        )
        requested_iterations = capture_config.get("android_runtime_iterations")
        expected_iterations = (
            android_runtime_expected_iterations(requested_iterations)
            if is_positive_int(requested_iterations)
            else {}
        )
        for name in ANDROID_RUNTIME_BENCHMARKS:
            row = android_runtime.get(name)
            if not isinstance(row, dict):
                continue
            validate_exact_keys(
                row,
                {"iterations", "total_ns", "ns_per_op"},
                f"{label} android_runtime/{name}",
                failures,
            )
            for metric in ("iterations", "total_ns"):
                if not is_positive_int(row.get(metric)):
                    failures.append(
                        f"{label} android_runtime/{name} metric not measured: {metric}"
                    )
            if (
                is_positive_int(row.get("iterations"))
                and is_positive_int(row.get("total_ns"))
                and is_nonnegative_number(row.get("ns_per_op"))
                and not android_rate_is_consistent(
                    row["total_ns"], row["iterations"], row["ns_per_op"]
                )
            ):
                failures.append(
                    f"{label} android_runtime/{name} has inconsistent "
                    "total_ns/iterations/ns_per_op"
                )
            expected = expected_iterations.get(name)
            if expected is not None and row.get("iterations") != expected:
                failures.append(
                    f"{label} android_runtime/{name} iterations differ from capture "
                    f"configuration: {row.get('iterations')} != {expected}"
                )
    if surfaces.get("android_artifacts"):
        validate_scalar_metrics(
            measurement_rows(result, "android_artifacts"),
            label,
            "android_artifacts",
            ANDROID_ARTIFACTS,
            failures,
        )
    if surfaces.get("cli"):
        cli = measurement_rows(result, "cli")
        validate_named_rows(cli, label, "cli", CLI_COMMANDS, failures)
        validate_row_metrics(cli, label, "cli", ("wall_ms",), failures, CLI_COMMANDS)
        for name in CLI_COMMANDS:
            row = cli.get(name)
            if not isinstance(row, dict):
                continue
            validate_exact_keys(
                row,
                {"wall_ms", "peak_rss_bytes"},
                f"{label} cli/{name}",
                failures,
            )
            peak_rss = row.get("peak_rss_bytes")
            if surfaces.get("peak_rss"):
                if not is_positive_int(peak_rss):
                    failures.append(f"{label} cli/{name} metric not measured: peak_rss_bytes")
            elif peak_rss is not None:
                failures.append(
                    f"{label} cli/{name} peak RSS must be null when capture is unavailable"
                )
    if surfaces.get("reports"):
        reports = measurement_rows(result, "reports")
        validate_named_rows(reports, label, "reports", REPORT_GROUPS, failures)
        for name in REPORT_GROUPS:
            row = reports.get(name)
            if not isinstance(row, dict):
                continue
            validate_exact_keys(
                row,
                {"bundle_bytes", "pages"},
                f"{label} reports/{name}",
                failures,
            )
            if not is_positive_int(row.get("bundle_bytes")):
                failures.append(
                    f"{label} reports/{name} metric not measured: bundle_bytes"
                )
            pages = row.get("pages")
            if (
                not isinstance(pages, list)
                or not pages
                or any(not isinstance(page, str) or not page for page in pages)
            ):
                failures.append(f"{label} reports/{name} pages are missing or invalid")


def validate_named_rows(
    rows: dict[str, Any],
    label: str,
    surface: str,
    required: tuple[str, ...],
    failures: list[str],
) -> None:
    validate_exact_keys(rows, set(required), f"{label} {surface} measurements", failures)
    for name in required:
        if not isinstance(rows.get(name), dict):
            failures.append(f"{label} {surface} measurement is missing: {name}")


def validate_row_metrics(
    rows: dict[str, Any],
    label: str,
    surface: str,
    metrics: tuple[str, ...],
    failures: list[str],
    required_rows: tuple[str, ...] | None = None,
) -> None:
    names = required_rows or tuple(rows)
    for name in names:
        row = rows.get(name)
        if not isinstance(row, dict):
            failures.append(f"{label} {surface}/{name} measurement row is invalid")
            continue
        for metric in metrics:
            if not is_nonnegative_number(row.get(metric)):
                failures.append(f"{label} {surface}/{name} metric not measured: {metric}")


def validate_scalar_metrics(
    rows: dict[str, Any],
    label: str,
    surface: str,
    metrics: tuple[str, ...],
    failures: list[str],
) -> None:
    validate_exact_keys(rows, set(metrics), f"{label} {surface} measurements", failures)
    for metric in metrics:
        if not is_positive_int(rows.get(metric)):
            failures.append(f"{label} {surface} metric not measured: {metric}")


def validate_report_pages(
    result: dict[str, Any],
    label: str,
    surfaces: dict[str, bool],
    required_pages: set[str],
    failures: list[str],
) -> None:
    if not surfaces.get("reports"):
        return
    reports = measurement_rows(result, "reports")
    for report_name in REPORT_GROUPS:
        report_data = reports.get(report_name)
        if not isinstance(report_data, dict):
            continue
        pages = report_data.get("pages")
        if not isinstance(pages, list) or any(not isinstance(page, str) for page in pages):
            failures.append(f"{label} {report_name} report pages were not measured")
            continue
        missing = required_pages - set(pages)
        if missing:
            failures.append(
                f"{label} {report_name} report is missing pages: {', '.join(sorted(missing))}"
            )


def measurement_rows(result: dict[str, Any], surface: str) -> dict[str, Any]:
    measurements = result.get("measurements")
    if not isinstance(measurements, dict):
        return {}
    rows = measurements.get(surface)
    return rows if isinstance(rows, dict) else {}


def row_value(rows: dict[str, Any], row_name: str, metric: str) -> float | int | None:
    row = rows.get(row_name)
    if not isinstance(row, dict):
        return None
    value = row.get(metric)
    return value if is_number(value) else None


def is_number(value: Any) -> bool:
    if isinstance(value, bool):
        return False
    if isinstance(value, int):
        return -MAX_CONTRACT_INTEGER <= value <= MAX_CONTRACT_INTEGER
    return (
        isinstance(value, float)
        and math.isfinite(value)
        and -MAX_CONTRACT_FLOAT <= value <= MAX_CONTRACT_FLOAT
    )


def is_nonnegative_number(value: Any) -> bool:
    return is_number(value) and value >= 0


def is_nonnegative_int(value: Any) -> bool:
    return (
        isinstance(value, int)
        and not isinstance(value, bool)
        and 0 <= value <= MAX_CONTRACT_INTEGER
    )


def is_positive_int(value: Any) -> bool:
    return (
        isinstance(value, int)
        and not isinstance(value, bool)
        and 0 < value <= MAX_CONTRACT_INTEGER
    )


def read_json(path: Path) -> dict[str, Any]:
    with path.open() as source:
        value = json.load(source)
    if not isinstance(value, dict):
        raise RuntimeError(f"JSON root must be an object: {path}")
    return value


def write_json_atomic(path: Path, value: dict[str, Any]) -> None:
    descriptor, temporary_name = tempfile.mkstemp(
        dir=path.parent, prefix=f".{path.name}.", suffix=".tmp"
    )
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w") as target:
            json.dump(value, target, ensure_ascii=False, indent=2, sort_keys=True)
            target.write("\n")
            target.flush()
            os.fsync(target.fileno())
        os.replace(temporary, path)
    except BaseException:
        try:
            os.close(descriptor)
        except OSError:
            pass
        temporary.unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, RuntimeError, ValueError) as error:
        print(f"performance-baseline: {error}", file=sys.stderr)
        raise SystemExit(2)
