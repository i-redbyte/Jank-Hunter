#!/usr/bin/env python3
"""Run the real debug/release concurrency acceptance matrix on one Android device.

APKs must already be built. This script never uninstalls, clears app data, or builds.
Each benchmark repetition runs disabled/normal/detailed for --seconds each, with
FrameMetrics enabled. Default: 600 seconds per mode and three repetitions per build.
Stable benchmark ZIPs are exported by the sample application after measurement.
No root access, run-as, private-file extraction, or global settings changes are used.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import shutil
import subprocess
import time
import zipfile

SAMPLE = "io.jankhunter.sample"
REGISTRY = "io.jankhunter.runtime.RuntimeCausalRegistry"
TESTS = {
    "functional": (f"{SAMPLE}.ConcurrencyDiagnosticsLogTest", 2),
    "footprint": (f"{SAMPLE}.ConcurrencyRegistryFootprintTest", 1),
    "benchmark": (f"{SAMPLE}.ConcurrencyBoundaryBenchmarkTest", 1),
}


def validate_apk_package(badging: str, expected: str) -> None:
    match = re.search(r"^package: name='([^']+)'", badging, re.MULTILINE)
    if not match or match.group(1) != expected:
        raise ValueError(f"expected sample APK package {expected!r}, got {match.group(1) if match else 'unknown'!r}")


def validate_instrumentation(output: str, expected_tests: int) -> None:
    match = re.search(r"\bOK \((\d+) tests?\)", output)
    if any(marker in output for marker in ("FAILURES!!!", "INSTRUMENTATION_FAILED", "Process crashed")):
        raise ValueError("Android instrumentation failed; inspect the saved instrumentation log")
    if not match or int(match.group(1)) != expected_tests:
        raise ValueError(f"expected successful execution of exactly {expected_tests} instrumentation tests")


def artifact_names(stage: str) -> tuple[str, ...]:
    return {
        "functional": ("concurrency-diagnostics-normal.zip", "concurrency-diagnostics-detailed.zip"),
        "footprint": ("causal-registry.hprof",),
        "benchmark": ("concurrency-benchmark.json", "concurrency-benchmark-normal.zip", "concurrency-benchmark-detailed.zip"),
    }[stage]


def resolve_mapped_class(mapping: str, original: str) -> dict:
    """Find a residual class candidate; constructor fallback also requires the HPROF entry proof."""
    explicit = set(re.findall(rf"^{re.escape(original)} -> ([^:]+):$", mapping, re.MULTILINE))
    explicit = {name for name in explicit if not name.startswith("R8$$REMOVED")}
    if len(explicit) > 1:
        raise ValueError(f"ambiguous explicit residual classes for {original}: {sorted(explicit)}")
    if explicit:
        return {
            "original_class": original, "residual_class": next(iter(explicit)),
            "strategy": "exact_class_header", "mapping_section_owners": [original],
            "requires_heap_entry_proof": False,
        }
    header = re.compile(r"^([^\s#][^:]*) -> ([^:]+):$")
    constructor = re.compile(
        rf"^\s+(?:\d+:\d+:)?void {re.escape(original)}\.<init>\([^)]*\)"
        r"(?::\d+(?::\d+)?)?\s+->\s+<init>\s*$"
    )
    owners: dict[str, set[str]] = {}
    section = None
    for line in mapping.splitlines():
        class_header = header.fullmatch(line)
        if class_header:
            section = class_header.groups()
        elif section and constructor.fullmatch(line):
            source_owner, residual_owner = section
            if not residual_owner.startswith("R8$$REMOVED"):
                owners.setdefault(residual_owner, set()).add(source_owner)
    if len(owners) != 1:
        qualifier = "ambiguous" if owners else "missing"
        raise ValueError(f"{qualifier} residual constructor owner for {original}: {sorted(owners)}")
    residual, sources = next(iter(owners.items()))
    return {
        "original_class": original, "residual_class": residual,
        "strategy": "unique_residual_constructor_owner", "mapping_section_owners": sorted(sources),
        "requires_heap_entry_proof": True,
        "limitation": "mapping suggests a horizontal merge or constructor inlining; exact dominated weak Entry objects must validate the candidate in ART",
    }


def mapped_class(mapping: str, original: str) -> str:
    return resolve_mapped_class(mapping, original)["residual_class"]


def mapped_registry_class(mapping: str) -> str:
    return mapped_class(mapping, REGISTRY)


def mapped_registry_entry_class(mapping: str) -> str:
    return mapped_class(mapping, REGISTRY + "$Entry")


def validate_archives(data: dict, directory: Path) -> None:
    for phase in data["phases"]:
        if phase["mode"] == "disabled":
            continue
        archive = directory / f"concurrency-benchmark-{phase['mode']}.zip"
        with zipfile.ZipFile(archive) as source:
            log_bytes = sum(entry.file_size for entry in source.infolist() if not entry.is_dir())
        if log_bytes <= 0 or phase.get("log_bytes") != log_bytes:
            raise ValueError(f"reported log_bytes do not match captured ZIP entries: {archive}")
        if phase.get("archive_bytes") != archive.stat().st_size:
            raise ValueError(f"reported archive_bytes do not match captured ZIP file: {archive}")


def validate_benchmark(data: dict, seconds: int) -> None:
    if data.get("seconds_per_mode") != seconds:
        raise ValueError("benchmark duration does not match the requested run")
    phases = data.get("phases", [])
    if len(phases) != 3 or {phase.get("mode") for phase in phases} != {"disabled", "normal", "detailed"}:
        raise ValueError("benchmark must include disabled, normal and detailed exactly once")
    for phase in phases:
        if phase.get("tasks", 0) <= 0 or not isinstance(phase.get("frames"), dict) or phase["frames"].get("count", 0) <= 0:
            raise ValueError(f"missing measured workload or FrameMetrics in {phase.get('mode')}")


def checked(command: list[str], timeout: int = 60) -> str:
    result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=timeout)
    return result.stdout.strip()


def run_logged(command: list[str], destination: Path, timeout: int) -> str:
    print(f"Running {destination.name}", flush=True)
    started = time.monotonic()
    with destination.open("w", encoding="utf-8") as output:
        process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT, text=True)
        try:
            while True:
                remaining = timeout - (time.monotonic() - started)
                if remaining <= 0:
                    raise TimeoutError(f"command exceeded {timeout} seconds: {destination}")
                try:
                    code = process.wait(timeout=min(30, remaining))
                    break
                except subprocess.TimeoutExpired:
                    print(f"  {destination.name}: {int(time.monotonic() - started)} seconds elapsed", flush=True)
            if code:
                raise RuntimeError(f"command exited {code}: {destination}")
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
    return destination.read_text(encoding="utf-8")


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--adb", default="adb")
    parser.add_argument("--aapt2", required=True, help="Android SDK build-tools aapt2 executable; verifies sample-only APKs before installation")
    parser.add_argument("--serial", required=True)
    parser.add_argument("--out", type=Path, required=True, help="new output directory; existing directories are rejected")
    for variant in ("debug", "release"):
        parser.add_argument(f"--{variant}-apk", type=Path, required=True)
        parser.add_argument(f"--{variant}-test-apk", type=Path, required=True)
    parser.add_argument("--release-mapping", type=Path, required=True)
    parser.add_argument("--seconds", type=int, default=600, choices=range(5, 601), metavar="5..600")
    parser.add_argument("--repetitions", type=int, default=3, choices=range(1, 11), metavar="1..10")
    return parser.parse_args()


def main() -> None:
    args = parse_arguments()
    adb = [args.adb, "-s", args.serial]
    variants = (
        ("debug", SAMPLE, args.debug_apk, args.debug_test_apk),
        ("release", SAMPLE + ".release", args.release_apk, args.release_test_apk),
    )
    # Validate all inputs before installation.
    for _, package, apk, test_apk in variants:
        for artifact, expected in ((apk, package), (test_apk, package + ".test")):
            validate_apk_package(checked([args.aapt2, "dump", "badging", str(artifact.resolve())]), expected)
    mapping = args.release_mapping.read_text(encoding="utf-8")
    registry_mapping = resolve_mapped_class(mapping, REGISTRY)
    entry_mapping = resolve_mapped_class(mapping, REGISTRY + "$Entry")
    release_class = registry_mapping["residual_class"]
    release_entry_class = entry_mapping["residual_class"]
    checked(adb + ["get-state"])
    properties = {key: checked(adb + ["shell", "getprop", key]) for key in (
        "ro.kernel.qemu", "ro.boot.qemu", "ro.build.fingerprint", "ro.build.version.sdk", "ro.product.cpu.abi",
    )}
    output = args.out.resolve()
    output.mkdir(parents=True, exist_ok=False)
    metadata = {
        "started_at_utc": datetime.now(timezone.utc).isoformat(), "serial": args.serial,
        "device": properties, "seconds_per_mode": args.seconds, "repetitions_per_build": args.repetitions,
        "release_registry_class": release_class, "release_registry_entry_class": release_entry_class,
        "release_registry_mapping": registry_mapping, "release_registry_entry_mapping": entry_mapping,
        "status": "running", "runs": [],
        "scope": "sample app only; benchmarks measure CPU/task and throughput as well as process CPU; target budgets require interpretation",
    }
    metadata_path = output / "run-metadata.json"
    active_package = None
    try:
        for variant, package, apk, test_apk in variants:
            active_package = package
            directory = output / variant
            directory.mkdir()
            if variant == "release":
                shutil.copy2(args.release_mapping, directory / "mapping.txt")
                (directory / "registry-mapping-provenance.json").write_text(
                    json.dumps({"registry": registry_mapping, "entry": entry_mapping}, indent=2), encoding="utf-8")
            (directory / "registry-class.txt").write_text(release_class if variant == "release" else REGISTRY, encoding="utf-8")
            (directory / "registry-entry-class.txt").write_text(
                release_entry_class if variant == "release" else REGISTRY + "$Entry", encoding="utf-8")
            run_logged(adb + ["install", "-r", str(apk.resolve())], directory / "install-app.log", 180)
            run_logged(adb + ["install", "-r", "-t", str(test_apk.resolve())], directory / "install-tests.log", 180)
            available = checked(adb + ["shell", "pm", "list", "instrumentation"])
            runners = re.findall(rf"^instrumentation:({re.escape(package + '.test')}/\S+) \(target={re.escape(package)}\)$", available, re.MULTILINE)
            if len(runners) != 1:
                raise ValueError(f"expected exactly one test runner targeting {package}: {available}")
            component = runners[0]
            stages = [("functional", 1), ("footprint", 1)] + [("benchmark", iteration) for iteration in range(1, args.repetitions + 1)]
            for stage, iteration in stages:
                run_directory = directory / (f"benchmark-{iteration:02d}" if stage == "benchmark" else stage)
                run_directory.mkdir()
                test_class, count = TESTS[stage]
                command = adb + ["shell", "am", "instrument", "-w", "-r", "-e", "class", test_class]
                if stage == "benchmark":
                    checked(adb + ["shell", "input", "keyevent", "KEYCODE_WAKEUP"])
                    checked(adb + ["shell", "wm", "dismiss-keyguard"])
                    command += ["-e", "benchmark_seconds", str(args.seconds), "-e", "benchmark_frames", "true"]
                command.append(component)
                result = run_logged(command, run_directory / "instrumentation.log", max(300, args.seconds * 3 + 300) if stage == "benchmark" else 300)
                validate_instrumentation(result, count)
                names = artifact_names(stage)
                for name in names:
                    run_logged(adb + ["pull", f"/sdcard/Android/data/{package}/files/{name}", str(run_directory / name)], run_directory / f"pull-{name}.log", 180)
                    if (run_directory / name).stat().st_size == 0:
                        raise ValueError(f"empty device artifact: {name}")
                if stage == "benchmark":
                    benchmark = json.loads((run_directory / "concurrency-benchmark.json").read_text(encoding="utf-8"))
                    validate_benchmark(benchmark, args.seconds)
                    validate_archives(benchmark, run_directory)
                metadata["runs"].append({"variant": variant, "stage": stage, "iteration": iteration, "status": "passed"})
                metadata_path.write_text(json.dumps(metadata, indent=2), encoding="utf-8")
        metadata["status"] = "passed"
    except BaseException as failure:
        metadata["status"] = "failed"
        metadata["failure"] = str(failure)
        if active_package:
            for package in (active_package + ".test", active_package):
                subprocess.run(adb + ["shell", "am", "force-stop", package], capture_output=True, timeout=30)
        raise
    finally:
        metadata["finished_at_utc"] = datetime.now(timezone.utc).isoformat()
        metadata_path.write_text(json.dumps(metadata, indent=2), encoding="utf-8")
    print(f"Device matrix passed; artifacts: {output}", flush=True)


if __name__ == "__main__":
    main()
