#!/usr/bin/env python3
"""Regression tests for standalone Gradle plugin resolution on corporate CI."""

from pathlib import Path
import unittest


PROJECT_ROOT = Path(__file__).resolve().parents[1]
CORPORATE_PLUGIN_REPOSITORIES = (
    "https://registry.vktech.team/repository/"
    "maven-gradle-plugins-remote-internal-proxy/",
    "https://nexus.vkteam.ru/repository/maven-gradle-plugins-remote/",
)
CORPORATE_MAVEN_REPOSITORY = (
    "https://registry.vktech.team/repository/maven-internal-proxy/"
)
STANDALONE_SETTINGS = (
    PROJECT_ROOT / "android" / "settings.gradle.kts",
    PROJECT_ROOT / "android" / "build-logic" / "settings.gradle.kts",
    PROJECT_ROOT / "android" / "jankhunter-gradle-plugin" / "settings.gradle.kts",
)
BUILD_LOGIC_BUILD_FILE = PROJECT_ROOT / "android" / "build-logic" / "build.gradle.kts"


class GradlePluginRepositoryPolicyTest(unittest.TestCase):
    def test_standalone_builds_resolve_plugins_through_corporate_proxies(self) -> None:
        for settings_file in STANDALONE_SETTINGS:
            with self.subTest(settings_file=settings_file):
                contents = settings_file.read_text(encoding="utf-8")
                plugin_management = contents.split(
                    "dependencyResolutionManagement", maxsplit=1
                )[0]
                plugin_portal_index = plugin_management.index("gradlePluginPortal()")

                for repository in CORPORATE_PLUGIN_REPOSITORIES:
                    repository_index = plugin_management.index(repository)
                    self.assertLess(repository_index, plugin_portal_index)

    def test_standalone_dependencies_use_corporate_proxy_before_public_maven(
        self,
    ) -> None:
        for settings_file in STANDALONE_SETTINGS:
            with self.subTest(settings_file=settings_file):
                contents = settings_file.read_text(encoding="utf-8")
                dependency_resolution = contents.split(
                    "dependencyResolutionManagement", maxsplit=1
                )[1]

                proxy_index = dependency_resolution.index(
                    CORPORATE_MAVEN_REPOSITORY
                )
                public_index = dependency_resolution.index("mavenCentral()")
                self.assertLess(proxy_index, public_index)

    def test_projects_cannot_override_standalone_repository_policy(self) -> None:
        repository_mode = (
            "repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)"
        )
        for settings_file in STANDALONE_SETTINGS:
            with self.subTest(settings_file=settings_file):
                contents = settings_file.read_text(encoding="utf-8")
                dependency_resolution = contents.split(
                    "dependencyResolutionManagement", maxsplit=1
                )[1]
                self.assertIn(repository_mode, dependency_resolution)

        build_logic = BUILD_LOGIC_BUILD_FILE.read_text(encoding="utf-8")
        self.assertNotIn("repositories {", build_logic)


if __name__ == "__main__":
    unittest.main()
