#!/usr/bin/env python3
"""Select and run standalone Jank Hunter checks for Jank Hunter-only MRs."""

import argparse
from enum import Enum
import os
from pathlib import Path
import shlex
import subprocess
import sys
from typing import Mapping, Sequence


PROJECT_ROOT = Path(__file__).resolve().parents[1]
REPOSITORY_ROOT = PROJECT_ROOT.parent
ANDROID_ROOT = PROJECT_ROOT / "android"
ANDROID_GRADLEW = ANDROID_ROOT / "gradlew"
JANK_HUNTER_PREFIX = "jank-hunter/"
JANK_HUNTER_CI_CONTROL_PATHS = frozenset(
    {
        "CODEOWNERS",
        "GitlabConfigs/custom_jobs/build_scripts.yml",
        "GitlabConfigs/custom_jobs/static_analyse.yml",
        "GitlabConfigs/custom_jobs/tests_scripts.yml",
    }
)


class Check(Enum):
    STATIC_ANALYSIS = "static-analysis"
    ASSEMBLE = "assemble"
    UNIT_TESTS = "unit-tests"


def is_jank_hunter_only_change(changed_paths: Sequence[str]) -> bool:
    normalized = tuple(path.removeprefix("./") for path in changed_paths if path)
    has_jank_hunter_change = any(
        path.startswith(JANK_HUNTER_PREFIX) for path in normalized
    )
    return has_jank_hunter_change and all(
        path.startswith(JANK_HUNTER_PREFIX)
        or path in JANK_HUNTER_CI_CONTROL_PATHS
        for path in normalized
    )


def is_jank_hunter_only_merge_request(environment: Mapping[str, str]) -> bool:
    base_sha = environment.get("CI_MERGE_REQUEST_DIFF_BASE_SHA", "").strip()
    head_sha = environment.get("CI_COMMIT_SHA", "").strip()
    if not base_sha or not head_sha:
        print(
            "[jank-hunter-ci] MR diff metadata is unavailable; using monorepo CI",
            file=sys.stderr,
        )
        return False

    completed = subprocess.run(
        (
            "git",
            "diff",
            "--name-only",
            "--no-renames",
            "--diff-filter=ACDMRTUXB",
            f"{base_sha}...{head_sha}",
        ),
        cwd=REPOSITORY_ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    if completed.returncode != 0:
        diagnostic = completed.stderr.strip() or "git diff failed"
        print(
            f"[jank-hunter-ci] {diagnostic}; using monorepo CI",
            file=sys.stderr,
        )
        return False

    changed_paths = tuple(completed.stdout.splitlines())
    isolated = is_jank_hunter_only_change(changed_paths)
    scope = "standalone Jank Hunter" if isolated else "monorepo"
    print(f"[jank-hunter-ci] selected {scope} CI for {len(changed_paths)} paths")
    return isolated


def command_plan(check: Check) -> tuple[tuple[str, ...], ...]:
    prefix = (
        str(ANDROID_GRADLEW),
        "-p",
        str(ANDROID_ROOT),
    )
    common = ("--no-daemon", "--stacktrace")
    if check is Check.STATIC_ANALYSIS:
        return (prefix + ("detekt",) + common,)
    if check is Check.ASSEMBLE:
        return (prefix + ("assembleRelease",) + common,)
    if check is Check.UNIT_TESTS:
        return (
            prefix
            + (
                "test",
                ":jankhunter-gradle-plugin:test",
            )
            + common,
        )
    raise AssertionError(f"Unsupported check: {check}")


def run(check: Check) -> int:
    for command in command_plan(check):
        print(f"[jank-hunter-ci] $ {shlex.join(command)}", flush=True)
        completed = subprocess.run(command, cwd=REPOSITORY_ROOT, check=False)
        if completed.returncode != 0:
            return completed.returncode
    return 0


def parse_args(arguments: Sequence[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="action", required=True)
    subparsers.add_parser("scope")
    run_parser = subparsers.add_parser("run")
    run_parser.add_argument("check", choices=tuple(check.value for check in Check))
    return parser.parse_args(arguments)


def main(arguments: Sequence[str]) -> int:
    args = parse_args(arguments)
    if args.action == "scope":
        return 0 if is_jank_hunter_only_merge_request(os.environ) else 1
    return run(Check(args.check))


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
