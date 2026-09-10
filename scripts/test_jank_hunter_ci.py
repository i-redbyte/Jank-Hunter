#!/usr/bin/env python3

import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest import mock


PROJECT_ROOT = Path(__file__).resolve().parents[1]
REPOSITORY_ROOT = PROJECT_ROOT.parent
SCRIPT_PATH = PROJECT_ROOT / "scripts" / "jank_hunter_ci.py"


def load_ci_module():
    spec = importlib.util.spec_from_file_location("jank_hunter_ci", SCRIPT_PATH)
    if spec is None or spec.loader is None:
        raise AssertionError(f"Cannot load {SCRIPT_PATH}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class JankHunterCiTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.ci = load_ci_module()

    def test_only_jank_hunter_changes_use_standalone_build(self) -> None:
        self.assertTrue(
            self.ci.is_jank_hunter_only_change(
                (
                    "jank-hunter/android/build.gradle.kts",
                    "jank-hunter/cli/main.go",
                )
            )
        )

    def test_mixed_change_falls_back_to_monorepo_build(self) -> None:
        self.assertFalse(
            self.ci.is_jank_hunter_only_change(
                (
                    "jank-hunter/android/build.gradle.kts",
                    "shared-cloud-logger/build.gradle.kts",
                )
            )
        )

    def test_jank_hunter_ci_control_files_are_allowed_with_jank_hunter(self) -> None:
        self.assertTrue(
            self.ci.is_jank_hunter_only_change(
                (
                    "jank-hunter/scripts/jank_hunter_ci.py",
                    "CODEOWNERS",
                    "GitlabConfigs/custom_jobs/build_scripts.yml",
                    "GitlabConfigs/custom_jobs/static_analyse.yml",
                    "GitlabConfigs/custom_jobs/tests_scripts.yml",
                    "scripts/jank-hunter-publish/publish-android.sh",
                )
            )
        )

    def test_ci_control_file_without_jank_hunter_change_uses_monorepo(self) -> None:
        self.assertFalse(
            self.ci.is_jank_hunter_only_change(
                ("GitlabConfigs/custom_jobs/build_scripts.yml",)
            )
        )

    def test_empty_change_set_falls_back_to_monorepo_build(self) -> None:
        self.assertFalse(self.ci.is_jank_hunter_only_change(()))

    def test_diff_failure_falls_back_to_monorepo_build(self) -> None:
        failed = subprocess.CompletedProcess(
            args=("git", "diff"),
            returncode=128,
            stdout="",
            stderr="missing base",
        )
        with mock.patch.object(self.ci.subprocess, "run", return_value=failed):
            self.assertFalse(
                self.ci.is_jank_hunter_only_merge_request(
                    {
                        "CI_MERGE_REQUEST_DIFF_BASE_SHA": "base",
                        "CI_COMMIT_SHA": "head",
                    }
                )
            )

    def test_missing_merge_request_metadata_falls_back_to_monorepo(self) -> None:
        self.assertFalse(self.ci.is_jank_hunter_only_merge_request({}))

    def test_standalone_plans_never_reference_monorepo_gradle(self) -> None:
        for check in self.ci.Check:
            with self.subTest(check=check):
                plan = self.ci.command_plan(check)
                self.assertTrue(plan)
                for command in plan:
                    self.assertEqual(
                        PROJECT_ROOT / "android" / "gradlew",
                        Path(command[0]),
                    )
                    self.assertNotIn(str(REPOSITORY_ROOT / "gradlew"), command)

    def test_ci_jobs_keep_original_fallbacks_for_other_projects(self) -> None:
        build = (
            REPOSITORY_ROOT / "GitlabConfigs/custom_jobs/build_scripts.yml"
        ).read_text(encoding="utf-8")
        static = (
            REPOSITORY_ROOT / "GitlabConfigs/custom_jobs/static_analyse.yml"
        ).read_text(encoding="utf-8")
        tests = (
            REPOSITORY_ROOT / "GitlabConfigs/custom_jobs/tests_scripts.yml"
        ).read_text(encoding="utf-8")

        self.assertIn("jank_hunter_ci.py scope", build)
        self.assertIn("jank_hunter_ci.py run assemble", build)
        self.assertIn("./gradlew assembleRelease -PCI=true", build)

        self.assertIn("jank_hunter_ci.py scope", static)
        self.assertIn("jank_hunter_ci.py run static-analysis", static)
        self.assertIn("./gradlew detektAll -PCI=true", static)

        self.assertIn("jank_hunter_ci.py scope", tests)
        self.assertIn("jank_hunter_ci.py run unit-tests", tests)
        self.assertIn("./gradlew testReleaseUnitTest -PCI=true", tests)
        self.assertIn("./gradlew testAndroidHostTest -PCI=true", tests)
        self.assertIn("./gradlew koverLogRelease -PCI=true", tests)
        self.assertIn("./gradlew koverVerifyRelease -PCI=true", tests)

    def test_changed_ci_files_have_jank_hunter_owner(self) -> None:
        codeowners = (REPOSITORY_ROOT / "CODEOWNERS").read_text(encoding="utf-8")

        self.assertIn("[Jank Hunter] @il.sokolov", codeowners)
        for path in (
            "/GitlabConfigs/custom_jobs/build_scripts.yml",
            "/GitlabConfigs/custom_jobs/static_analyse.yml",
            "/GitlabConfigs/custom_jobs/tests_scripts.yml",
            "/GitlabConfigs/job_configs/deployment_jobs.json",
            "/GitlabConfigs/job_configs/web_build_jobs.json",
        ):
            with self.subTest(path=path):
                self.assertIn(path, codeowners)

    def test_cli_artifact_job_is_available_for_merge_request_and_web_pipelines(self) -> None:
        build_scripts = (
            REPOSITORY_ROOT / "GitlabConfigs/custom_jobs/build_scripts.yml"
        ).read_text(encoding="utf-8")
        self.assertIn(".buildJankHunterCli:", build_scripts)
        self.assertIn(
            ":jankhunter-cli:packageDarwinArm64Cli",
            build_scripts,
        )
        self.assertIn("sha256sum ./*.tar.gz > checksums.txt", build_scripts)

        for relative_path in (
            "GitlabConfigs/job_configs/deployment_jobs.json",
            "GitlabConfigs/job_configs/web_build_jobs.json",
        ):
            with self.subTest(path=relative_path):
                config = json.loads(
                    (REPOSITORY_ROOT / relative_path).read_text(encoding="utf-8")
                )
                job = config["buildJankHunterCli"]
                self.assertEqual("manual", job["when"])
                self.assertEqual(
                    "!reference [.buildJankHunterCli, script]",
                    job["script"],
                )
                self.assertEqual([], job["before_script"])
                self.assertEqual("30 days", job["artifacts"]["expire_in"])
                self.assertEqual(
                    [
                        "jank-hunter/android/jankhunter-cli/"
                        "build/distributions/"
                    ],
                    job["artifacts"]["paths"],
                )

    def test_android_studio_plugin_artifact_job_is_available_for_merge_request_and_web_pipelines(
        self,
    ) -> None:
        build_scripts = (
            REPOSITORY_ROOT / "GitlabConfigs/custom_jobs/build_scripts.yml"
        ).read_text(encoding="utf-8")
        self.assertIn(".buildJankHunterAndroidStudioPlugin:", build_scripts)
        self.assertIn(
            "jank-hunter/plugin-as/gradlew -p jank-hunter/plugin-as buildPlugin",
            build_scripts,
        )
        self.assertIn("sha256sum ./*.zip > checksums.txt", build_scripts)

        for relative_path in (
            "GitlabConfigs/job_configs/deployment_jobs.json",
            "GitlabConfigs/job_configs/web_build_jobs.json",
        ):
            with self.subTest(path=relative_path):
                config = json.loads(
                    (REPOSITORY_ROOT / relative_path).read_text(encoding="utf-8")
                )
                job = config["buildJankHunterAndroidStudioPlugin"]
                self.assertEqual("manual", job["when"])
                self.assertEqual(["build-app-mac"], job["tags"])
                self.assertEqual(
                    "!reference [.buildJankHunterAndroidStudioPlugin, script]",
                    job["script"],
                )
                self.assertEqual([], job["before_script"])
                self.assertEqual("30 days", job["artifacts"]["expire_in"])
                self.assertEqual(
                    ["jank-hunter/plugin-as/build/distributions/"],
                    job["artifacts"]["paths"],
                )


if __name__ == "__main__":
    unittest.main()
