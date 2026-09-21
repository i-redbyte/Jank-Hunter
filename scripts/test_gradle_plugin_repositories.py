#!/usr/bin/env python3
"""Regression tests for standalone Gradle settings repository policy."""

from pathlib import Path
import re
import unittest


PROJECT_ROOT = Path(__file__).resolve().parents[1]
SHARED_REPOSITORY_SETTINGS = (
    PROJECT_ROOT / "android" / "gradle" / "jankhunter-repository-settings.gradle"
)
STANDALONE_SETTINGS = (
    PROJECT_ROOT / "android" / "settings.gradle.kts",
    PROJECT_ROOT / "android" / "build-logic" / "settings.gradle.kts",
    PROJECT_ROOT / "android" / "jankhunter-gradle-plugin" / "settings.gradle.kts",
)
BUILD_LOGIC_BUILD_FILE = PROJECT_ROOT / "android" / "build-logic" / "build.gradle.kts"
CORPORATE_REPOSITORY_MARKERS = (
    "registry.vktech.team",
    "nexus.vkteam.ru",
)


class GradlePluginRepositoryPolicyTest(unittest.TestCase):
    def test_shared_repository_settings_define_public_repositories(self) -> None:
        contents = SHARED_REPOSITORY_SETTINGS.read_text(encoding="utf-8")
        self.assertIn("pluginManagement.repositories", contents)
        self.assertIn("dependencyResolutionManagement", contents)
        self.assertIn("FAIL_ON_PROJECT_REPOS", contents)
        self.assertIn("google()", contents)
        self.assertIn("mavenCentral()", contents)
        self.assertIn("gradlePluginPortal()", contents)
        for marker in CORPORATE_REPOSITORY_MARKERS:
            self.assertNotIn(marker, contents)

    def test_standalone_settings_apply_shared_repository_script(self) -> None:
        expected_apply = {
            PROJECT_ROOT / "android" / "settings.gradle.kts": (
                'apply(from = "gradle/jankhunter-repository-settings.gradle")'
            ),
            PROJECT_ROOT / "android" / "build-logic" / "settings.gradle.kts": (
                'apply(from = "../gradle/jankhunter-repository-settings.gradle")'
            ),
            PROJECT_ROOT / "android" / "jankhunter-gradle-plugin" / "settings.gradle.kts": (
                'apply(from = "../gradle/jankhunter-repository-settings.gradle")'
            ),
        }
        for settings_file, apply_line in expected_apply.items():
            with self.subTest(settings_file=settings_file):
                contents = settings_file.read_text(encoding="utf-8")
                self.assertIn(apply_line, contents)

    def test_standalone_settings_do_not_embed_corporate_mirrors(self) -> None:
        for settings_file in STANDALONE_SETTINGS:
            with self.subTest(settings_file=settings_file):
                contents = settings_file.read_text(encoding="utf-8")
                for marker in CORPORATE_REPOSITORY_MARKERS:
                    self.assertNotIn(marker, contents)

    def test_standalone_settings_do_not_redeclare_public_repositories(self) -> None:
        for settings_file in STANDALONE_SETTINGS:
            with self.subTest(settings_file=settings_file):
                contents = settings_file.read_text(encoding="utf-8")
                self.assertNotIn("mavenCentral()", contents)
                self.assertNotIn("gradlePluginPortal()", contents)

    def test_projects_cannot_override_standalone_repository_policy(self) -> None:
        contents = SHARED_REPOSITORY_SETTINGS.read_text(encoding="utf-8")
        self.assertIn("FAIL_ON_PROJECT_REPOS", contents)

        build_logic = BUILD_LOGIC_BUILD_FILE.read_text(encoding="utf-8")
        self.assertNotIn("repositories {", build_logic)


if __name__ == "__main__":
    unittest.main()
