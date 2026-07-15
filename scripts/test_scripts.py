#!/usr/bin/env python3
"""Local regression tests for repository maintenance scripts."""

from __future__ import annotations

import os
import json
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("integrate-android-project.sh").resolve()
ANDROID_E2E_SCRIPT = Path(__file__).with_name("android-e2e.sh").resolve()
GRADLE_SMOKE_SCRIPT = Path(__file__).with_name("gradle-plugin-smoke.sh").resolve()
GROUP = "io.jankhunter"
VERSION = "1.0.0"


class AndroidIntegrationScriptTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.jankhunter = self.root / "jankhunter"
        android = self.jankhunter / "android"
        android.mkdir(parents=True)
        (android / "gradle.properties").write_text(
            f"jankHunterGroup={GROUP}\njankHunterVersion={VERSION}\n",
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def create_project(self, dsl: str, *, existing_integration: bool = True) -> Path:
        project = self.root / f"project-{dsl}"
        app = project / "app"
        app.mkdir(parents=True)
        suffix = ".kts" if dsl == "kts" else ""
        (project / f"settings.gradle{suffix}").write_text(
            self.settings_text(existing_integration), encoding="utf-8"
        )
        (app / f"build.gradle{suffix}").write_text(
            self.build_text(dsl, existing_integration), encoding="utf-8"
        )
        gradlew = project / "gradlew"
        gradlew.write_text(
            "#!/usr/bin/env sh\nprintf 'called\\n' > \"$PWD/gradle-invoked\"\n",
            encoding="utf-8",
        )
        gradlew.chmod(gradlew.stat().st_mode | stat.S_IXUSR)
        return project

    @staticmethod
    def settings_text(existing_integration: bool) -> str:
        legacy_plugin = ""
        legacy_dependency = ""
        if existing_integration:
            legacy_plugin = """    // Jank Hunter plugin repository
    repositories {
        maven { url = uri("legacy/jankhunter-maven") }
    }

"""
            legacy_dependency = """    // Jank Hunter dependency repository
    repositories {
        maven { url = uri("legacy/jankhunter-maven") }
    }

"""
        return f"""pluginManagement {{
{legacy_plugin}    repositories {{
        google()
        gradlePluginPortal()
    }}
}}

dependencyResolutionManagement {{
{legacy_dependency}    repositories {{
        google()
        mavenCentral()
    }}
}}

rootProject.name = "IntegrationFixture"
include(":app")
"""

    @staticmethod
    def build_text(dsl: str, existing_integration: bool) -> str:
        if dsl == "kts":
            jh_plugin = (
                '    id("io.jankhunter.android") version "0.9.0"\n'
                if existing_integration
                else ""
            )
            manual_dependencies = """dependencies {
    implementation("io.jankhunter:jankhunter-runtime:manual-version")
    customImplementation("io.jankhunter:jankhunter-okhttp3:manual-version")
    implementation("com.example:user-owned:1")
}
"""
            legacy_helper = """// Jank Hunter optional OkHttp/WebSocket helper
dependencies {
    debugImplementation("io.jankhunter:jankhunter-okhttp3:0.9.0")
    implementation("com.example:inside-legacy-block:1")
}
"""
        else:
            jh_plugin = (
                "    id 'io.jankhunter.android' version '0.9.0'\n"
                if existing_integration
                else ""
            )
            manual_dependencies = """dependencies {
    implementation 'io.jankhunter:jankhunter-runtime:manual-version'
    customImplementation 'io.jankhunter:jankhunter-okhttp3:manual-version'
    implementation 'com.example:user-owned:1'
}
"""
            legacy_helper = """// Jank Hunter optional OkHttp/WebSocket helper
dependencies {
    debugImplementation 'io.jankhunter:jankhunter-okhttp3:0.9.0'
    implementation 'com.example:inside-legacy-block:1'
}
"""

        manual_dsl = """jankHunter {
    verboseLogs = true
    retainedHeapDump {
        privacyApproved = true
    }
}
"""
        if not existing_integration:
            manual_dsl = ""
            legacy_helper = ""
        return f"""plugins {{
    id("com.android.application")
{jh_plugin}}}

android {{
    namespace = "com.example.fixture"
}}

{manual_dependencies}
{manual_dsl}
{legacy_helper}"""

    def create_artifacts(self, project: Path, maven_dir: str, cli_dir: str) -> None:
        repository = project / maven_dir
        group_path = Path(*GROUP.split("."))
        artifacts = (
            repository
            / group_path
            / "jankhunter-android-sdk"
            / VERSION
            / f"jankhunter-android-sdk-{VERSION}.aar",
            repository
            / group_path
            / "jankhunter-runtime"
            / VERSION
            / f"jankhunter-runtime-{VERSION}.aar",
            repository
            / group_path
            / "jankhunter-annotations"
            / VERSION
            / f"jankhunter-annotations-{VERSION}.jar",
            repository
            / group_path
            / "jankhunter-gradle-plugin"
            / VERSION
            / f"jankhunter-gradle-plugin-{VERSION}.jar",
            repository
            / group_path
            / "jankhunter-okhttp3"
            / VERSION
            / f"jankhunter-okhttp3-{VERSION}.aar",
            repository
            / "io/jankhunter/android/io.jankhunter.android.gradle.plugin"
            / VERSION
            / f"io.jankhunter.android.gradle.plugin-{VERSION}.pom",
        )
        for artifact in artifacts:
            artifact.parent.mkdir(parents=True, exist_ok=True)
            artifact.touch()

        cli = project / cli_dir / "jankhunter"
        cli.parent.mkdir(parents=True, exist_ok=True)
        cli.write_text("#!/usr/bin/env sh\nexit 0\n", encoding="utf-8")
        cli.chmod(cli.stat().st_mode | stat.S_IXUSR)

    def run_script(
        self, project: Path, *arguments: str, expected_status: int = 0
    ) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment.pop("ANDROID_HOME", None)
        environment.pop("ANDROID_SDK_ROOT", None)
        environment["HOME"] = str(self.root / "empty-home")
        completed = subprocess.run(
            [
                str(SCRIPT),
                "--target",
                str(project),
                "--jankhunter",
                str(self.jankhunter),
                *arguments,
            ],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )
        self.assertEqual(
            expected_status,
            completed.returncode,
            f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}",
        )
        return completed

    def integration_arguments(self, maven_dir: str, cli_dir: str) -> tuple[str, ...]:
        return (
            "--module",
            ":app",
            "--maven-dir",
            maven_dir,
            "--cli-dir",
            cli_dir,
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            "--no-gitignore",
        )

    def assert_reintegration(self, dsl: str) -> None:
        project = self.create_project(dsl)
        old_maven = "local/old-maven"
        new_maven = "local/new-maven"
        cli_dir = "local/bin"
        self.create_artifacts(project, old_maven, cli_dir)
        self.create_artifacts(project, new_maven, cli_dir)
        common_old = self.integration_arguments(old_maven, cli_dir)
        common_new = self.integration_arguments(new_maven, cli_dir)

        self.run_script(
            project,
            *common_old,
            "--build-type",
            "qa",
            "--build-type",
            "staging",
            "--include-package",
            "com.example.first",
            "--exclude-package",
            "com.example.generated",
            "--runtime-call-graph",
            "--okhttp",
            "--websockets",
            "--analyze-di",
            "--asm-progress-log",
            "--max-session-log-size-mib",
            "12",
        )

        suffix = ".kts" if dsl == "kts" else ""
        build_file = project / "app" / f"build.gradle{suffix}"
        settings_file = project / f"settings.gradle{suffix}"
        first_build = build_file.read_text(encoding="utf-8")
        first_settings = settings_file.read_text(encoding="utf-8")
        self.assertIn("io.jankhunter.android", first_build)
        self.assertNotIn("0.9.0", first_build)
        self.assertIn(f'jankhunter-android-sdk:{VERSION}', first_build)
        self.assertIn('enabledBuildTypes.set(setOf("qa", "staging"))' if dsl == "kts" else 'enabledBuildTypes.set(["qa", "staging"])', first_build)
        self.assertEqual(1, first_build.count("managed configuration - BEGIN"))
        self.assertEqual(1, first_build.count("optional helper dependencies - BEGIN"))
        self.assertEqual(2, first_settings.count(old_maven))

        self.run_script(
            project,
            *common_new,
            "--build-type",
            "qa",
            "--build-type",
            "staging",
            "--include-package",
            "com.example.updated",
            "--no-runtime-call-graph",
            "--no-okhttp",
            "--no-websockets",
            "--no-analyze-di",
            "--no-session-log-size-limit",
        )
        second_build = build_file.read_text(encoding="utf-8")
        second_settings = settings_file.read_text(encoding="utf-8")

        self.assertIn("verboseLogs = true", second_build)
        self.assertIn("privacyApproved = true", second_build)
        self.assertIn("jankhunter-runtime:manual-version", second_build)
        self.assertIn("jankhunter-okhttp3:manual-version", second_build)
        self.assertIn("com.example:user-owned:1", second_build)
        self.assertIn("com.example:inside-legacy-block:1", second_build)
        self.assertNotIn("jankhunter-okhttp3:0.9.0", second_build)
        self.assertEqual(1, second_build.count("optional helper dependencies - BEGIN"))
        self.assertIn(f"jankhunter-android-sdk:{VERSION}", second_build)
        self.assertNotIn(f"jankhunter-okhttp3:{VERSION}", second_build)
        self.assertIn("dependencyInjectionAnalysis = io.jankhunter.gradle.JankHunterFeatureMode.DISABLED", second_build)
        self.assertIn("sessionLogSizeLimitEnabled = false", second_build)
        self.assertIn("runtimeCallGraph = false", second_build)
        self.assertIn("okhttp = false", second_build)
        self.assertIn("webSockets = false", second_build)
        self.assertIn("com.example.updated", second_build)
        self.assertNotIn("com.example.first", second_build)
        self.assertEqual(1, second_build.count("managed configuration - BEGIN"))
        self.assertEqual(1, second_build.count("managed configuration - END"))
        self.assertEqual(1, second_build.count("io.jankhunter.android"))
        self.assertIn(VERSION, second_build)

        self.assertEqual(2, second_settings.count(new_maven))
        self.assertNotIn(old_maven, second_settings)
        self.assertEqual(1, second_settings.count("managed plugin repository - BEGIN"))
        self.assertEqual(1, second_settings.count("managed dependency repository - BEGIN"))
        self.assertIn("gradlePluginPortal()", second_settings)
        self.assertIn("mavenCentral()", second_settings)

        self.run_script(
            project,
            *common_new,
            "--build-type",
            "qa",
            "--build-type",
            "staging",
            "--include-package",
            "com.example.updated",
            "--no-runtime-call-graph",
            "--no-okhttp",
            "--no-websockets",
            "--no-analyze-di",
            "--no-session-log-size-limit",
        )
        self.assertEqual(second_build, build_file.read_text(encoding="utf-8"))
        self.assertEqual(second_settings, settings_file.read_text(encoding="utf-8"))

    def test_reintegrates_existing_kotlin_dsl_project(self) -> None:
        self.assert_reintegration("kts")

    def test_reintegrates_existing_groovy_dsl_project(self) -> None:
        self.assert_reintegration("groovy")

    def test_reintegrates_groovy_project_with_slashy_literals_idempotently(self) -> None:
        project = self.create_project("groovy", existing_integration=True)
        build = project / "app/build.gradle"
        manual_literals = r"""jankHunter {
    def slashyPattern = /a{2}/
    def dollarSlashyPattern = $/a{2}/$
    def escapedSlashyPattern = /a\/b{2}/
    def escapedDollarSlashyPattern = $/a$/b{2}/$
    def ratio = 6 / 2
"""
        build.write_text(
            build.read_text(encoding="utf-8").replace(
                "jankHunter {\n", manual_literals, 1
            ),
            encoding="utf-8",
        )
        self.create_artifacts(project, "repo/maven", "repo/bin")
        arguments = (
            *self.integration_arguments("repo/maven", "repo/bin"),
            "--runtime-call-graph",
        )

        self.run_script(project, *arguments)
        first = build.read_text(encoding="utf-8")

        self.assertIn("def slashyPattern = /a{2}/", first)
        self.assertIn("def dollarSlashyPattern = $/a{2}/$", first)
        self.assertIn(r"def escapedSlashyPattern = /a\/b{2}/", first)
        self.assertIn("def escapedDollarSlashyPattern = $/a$/b{2}/$", first)
        self.assertIn("def ratio = 6 / 2", first)
        self.assertEqual(1, first.count("managed configuration - BEGIN"))

        self.run_script(project, *arguments)

        self.assertEqual(first, build.read_text(encoding="utf-8"))

    def test_ambiguous_command_style_slashy_fails_before_writes(self) -> None:
        project = self.create_project("groovy", existing_integration=True)
        build = project / "app/build.gradle"
        settings = project / "settings.gradle"
        build.write_text(
            build.read_text(encoding="utf-8").replace(
                "jankHunter {\n",
                "jankHunter {\n    println /a{2}/\n",
                1,
            ),
            encoding="utf-8",
        )
        before_build = build.read_bytes()
        before_settings = settings.read_bytes()

        completed = self.run_script(
            project,
            *self.integration_arguments("repo/maven", "repo/bin"),
            "--dry-run",
            expected_status=1,
        )

        self.assertIn("ambiguous Groovy slashy string", completed.stderr)
        self.assertEqual(before_build, build.read_bytes())
        self.assertEqual(before_settings, settings.read_bytes())
        self.assertFalse((project / ".jankhunter-backups").exists())

    def test_dry_run_verify_does_not_write_or_run_gradle(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        settings = project / "settings.gradle.kts"
        build = project / "app/build.gradle.kts"
        before = {
            settings: settings.read_bytes(),
            build: build.read_bytes(),
            project / "gradlew": (project / "gradlew").read_bytes(),
        }

        completed = self.run_script(
            project,
            "--module",
            ":app",
            "--dry-run",
            "--verify",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            "--no-gitignore",
        )

        self.assertIn("target Gradle verification is skipped", completed.stdout)
        self.assertFalse((project / "gradle-invoked").exists())
        self.assertFalse((project / ".jankhunter-backups").exists())
        for path, content in before.items():
            self.assertEqual(content, path.read_bytes())

    def test_output_directories_cannot_escape_through_symlinks(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        outside = self.root / "outside"
        outside.mkdir()
        (project / "escape").symlink_to(outside, target_is_directory=True)

        for option, safe_other in (
            ("--maven-dir", ("--cli-dir", "safe/bin")),
            ("--cli-dir", ("--maven-dir", "safe/maven")),
        ):
            with self.subTest(option=option):
                completed = self.run_script(
                    project,
                    "--module",
                    ":app",
                    option,
                    "escape/output",
                    *safe_other,
                    "--dry-run",
                    "--skip-publish",
                    "--skip-cli-build",
                    "--skip-local-properties",
                    "--no-gitignore",
                    expected_status=1,
                )
                self.assertIn("resolves outside the target project", completed.stderr)

    def test_one_line_plugins_block_is_patched_idempotently(self) -> None:
        for dsl in ("kts", "groovy"):
            with self.subTest(dsl=dsl):
                project = self.create_project(dsl, existing_integration=False)
                suffix = ".kts" if dsl == "kts" else ""
                build = project / "app" / f"build.gradle{suffix}"
                build.write_text(
                    'plugins { id("com.android.application") }\n'
                    'android { namespace = "com.example.fixture" }\n',
                    encoding="utf-8",
                )
                self.create_artifacts(project, "repo/maven", "repo/bin")
                arguments = self.integration_arguments("repo/maven", "repo/bin")

                self.run_script(project, *arguments)
                first = build.read_text(encoding="utf-8")
                self.assertIn(
                    'id("io.jankhunter.android") version "1.0.0"'
                    if dsl == "kts"
                    else "id 'io.jankhunter.android' version '1.0.0'",
                    first,
                )
                self.assertNotIn('version "1.0.0" id(', first)
                self.assertNotIn("version '1.0.0' id(", first)

                self.run_script(project, *arguments)
                self.assertEqual(first, build.read_text(encoding="utf-8"))

    def test_updates_settings_and_root_central_plugin_versions(self) -> None:
        for dsl in ("kts", "groovy"):
            with self.subTest(dsl=dsl):
                project = self.create_project(dsl, existing_integration=False)
                suffix = ".kts" if dsl == "kts" else ""
                settings = project / f"settings.gradle{suffix}"
                settings.write_text(
                    '// pluginManagement { documentation only\n'
                    'pluginManagement {\n'
                    '    plugins { id("io.jankhunter.android") version "0.9.0" }\n'
                    '    repositories { google(); gradlePluginPortal() }\n'
                    '}\n'
                    'dependencyResolutionManagement { repositories { google(); mavenCentral() } }\n'
                    'include(":app")\n',
                    encoding="utf-8",
                )
                root_build = project / f"build.gradle{suffix}"
                root_build.write_text(
                    'plugins { id("io.jankhunter.android") version "0.9.0" apply false }\n',
                    encoding="utf-8",
                )
                build = project / "app" / f"build.gradle{suffix}"
                build.write_text(
                    'plugins {\n'
                    '    id("com.android.application")\n'
                    '    id("io.jankhunter.android")\n'
                    '}\n'
                    'android { namespace = "com.example.fixture" }\n',
                    encoding="utf-8",
                )
                self.create_artifacts(project, "repo/maven", "repo/bin")

                self.run_script(
                    project, *self.integration_arguments("repo/maven", "repo/bin")
                )

                self.assertNotIn("0.9.0", settings.read_text(encoding="utf-8"))
                self.assertNotIn("0.9.0", root_build.read_text(encoding="utf-8"))
                self.assertIn(VERSION, settings.read_text(encoding="utf-8"))
                self.assertIn(VERSION, root_build.read_text(encoding="utf-8"))
                self.assertNotIn(
                    f'id("io.jankhunter.android") version "{VERSION}"',
                    build.read_text(encoding="utf-8"),
                )

    def test_comments_and_strings_are_not_gradle_blocks_or_plugin_declarations(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        settings = project / "settings.gradle.kts"
        settings.write_text(
            '// pluginManagement { documentation only\n'
            'pluginManagement { repositories { google(); gradlePluginPortal() } }\n'
            '// dependencyResolutionManagement { documentation only\n'
            'dependencyResolutionManagement { repositories { google(); mavenCentral() } }\n'
            'include(":app")\n',
            encoding="utf-8",
        )
        build = project / "app/build.gradle.kts"
        build.write_text(
            'plugins { id("com.android.application") }\n'
            '// io.jankhunter.android is mentioned in documentation\n'
            'val pluginDocumentation = "io.jankhunter.android"\n'
            'android { namespace = "com.example.fixture" }\n',
            encoding="utf-8",
        )
        self.create_artifacts(project, "repo/maven", "repo/bin")

        self.run_script(project, *self.integration_arguments("repo/maven", "repo/bin"))

        settings_text = settings.read_text(encoding="utf-8")
        build_text = build.read_text(encoding="utf-8")
        actual_plugin_lines = [
            line
            for line in build_text.splitlines()
            if line.lstrip().startswith('id("io.jankhunter.android")')
        ]
        self.assertEqual(1, len(actual_plugin_lines))
        actual_plugin_management = settings_text.index("pluginManagement {")
        managed_repository = settings_text.index(
            "Jank Hunter integration managed plugin repository - BEGIN"
        )
        self.assertLess(actual_plugin_management, managed_repository)

    def assert_version_catalog_reintegration(self, mode: str) -> None:
        project = self.create_project("kts", existing_integration=False)
        build = project / "app/build.gradle.kts"
        build.write_text(
            'plugins {\n    alias(libs.plugins.jankhunter)\n}\n'
            'android { namespace = "com.example.fixture" }\n',
            encoding="utf-8",
        )
        catalog = project / "gradle/libs.versions.toml"
        catalog.parent.mkdir()
        if mode == "inline":
            catalog.write_text(
                '[plugins]\n'
                'jankhunter = { id = "io.jankhunter.android", version = "0.9.0" }\n',
                encoding="utf-8",
            )
        else:
            catalog.write_text(
                '[versions]\njankhunter = "0.9.0"\n\n'
                '[plugins]\n'
                'jankhunter = { id = "io.jankhunter.android", version.ref = "jankhunter" }\n',
                encoding="utf-8",
            )
        self.create_artifacts(project, "repo/maven", "repo/bin")
        arguments = self.integration_arguments("repo/maven", "repo/bin")

        self.run_script(project, *arguments)
        first_build = build.read_text(encoding="utf-8")
        first_catalog = catalog.read_text(encoding="utf-8")
        self.assertIn(f'"{VERSION}"', first_catalog)
        self.assertNotIn("0.9.0", first_catalog)
        self.assertIn("alias(libs.plugins.jankhunter)", first_build)
        self.assertNotIn('id("io.jankhunter.android")', first_build)

        self.run_script(project, *arguments)
        self.assertEqual(first_build, build.read_text(encoding="utf-8"))
        self.assertEqual(first_catalog, catalog.read_text(encoding="utf-8"))

    def test_updates_inline_version_catalog_plugin_version(self) -> None:
        self.assert_version_catalog_reintegration("inline")

    def test_updates_version_ref_catalog_plugin_version(self) -> None:
        self.assert_version_catalog_reintegration("ref")

    def test_shared_version_catalog_ref_fails_before_writes(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        build = project / "app/build.gradle.kts"
        build.write_text(
            'plugins { alias(libs.plugins.jankhunter) }\n'
            'android { namespace = "com.example.fixture" }\n',
            encoding="utf-8",
        )
        catalog = project / "gradle/libs.versions.toml"
        catalog.parent.mkdir()
        catalog.write_text(
            '[versions]\nshared = "0.9.0"\n\n'
            '[plugins]\n'
            'jankhunter = { id = "io.jankhunter.android", version.ref = "shared" }\n'
            'another = { id = "com.example.another", version.ref = "shared" }\n',
            encoding="utf-8",
        )
        before = {
            build: build.read_bytes(),
            catalog: catalog.read_bytes(),
            project / "settings.gradle.kts": (
                project / "settings.gradle.kts"
            ).read_bytes(),
        }

        completed = self.run_script(
            project,
            "--module",
            ":app",
            "--dry-run",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            "--no-gitignore",
            expected_status=1,
        )

        self.assertIn("is shared", completed.stderr)
        self.assertFalse((project / ".jankhunter-backups").exists())
        for path, content in before.items():
            self.assertEqual(content, path.read_bytes())

    def test_unknown_custom_plugin_alias_fails_before_writes(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        build = project / "app/build.gradle.kts"
        build.write_text(
            'plugins { alias(company.plugins.jankhunter) }\n'
            'android { namespace = "com.example.fixture" }\n',
            encoding="utf-8",
        )
        before = build.read_bytes()

        completed = self.run_script(
            project,
            "--module",
            ":app",
            "--dry-run",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            "--no-gitignore",
            expected_status=1,
        )

        self.assertIn("custom plugin alias", completed.stderr)
        self.assertEqual(before, build.read_bytes())
        self.assertFalse((project / ".jankhunter-backups").exists())

    def test_legacy_helper_migration_ignores_braces_in_comments(self) -> None:
        for dsl in ("kts", "groovy"):
            with self.subTest(dsl=dsl):
                project = self.create_project(dsl, existing_integration=False)
                suffix = ".kts" if dsl == "kts" else ""
                build = project / "app" / f"build.gradle{suffix}"
                dependency = (
                    '    implementation("com.example:keep:1") // } documentation\n'
                    '    implementation("com.example:also-keep:1")\n'
                    '    debugImplementation("io.jankhunter:jankhunter-okhttp3:0.9.0")\n'
                    if dsl == "kts"
                    else "    implementation 'com.example:keep:1' // } documentation\n"
                    "    implementation 'com.example:also-keep:1'\n"
                    "    debugImplementation 'io.jankhunter:jankhunter-okhttp3:0.9.0'\n"
                )
                build.write_text(
                    'plugins { id("com.android.application") }\n'
                    '// Jank Hunter optional OkHttp/WebSocket helper\n'
                    f'dependencies {{\n{dependency}}}\n'
                    'android { namespace = "com.example.fixture" }\n',
                    encoding="utf-8",
                )
                self.create_artifacts(project, "repo/maven", "repo/bin")

                self.run_script(
                    project,
                    *self.integration_arguments("repo/maven", "repo/bin"),
                    "--no-okhttp",
                    "--no-websockets",
                )

                result = build.read_text(encoding="utf-8")
                self.assertIn("com.example:keep:1", result)
                self.assertIn("com.example:also-keep:1", result)
                self.assertNotIn("jankhunter-okhttp3:0.9.0", result)
                dependencies_start = result.index("dependencies {")
                dependencies_end = result.index("\n}", dependencies_start)
                self.assertLess(
                    result.index("com.example:also-keep:1"), dependencies_end
                )

    def test_reversed_managed_markers_fail_before_writes(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        build = project / "app/build.gradle.kts"
        build.write_text(
            build.read_text(encoding="utf-8")
            + "\n// Jank Hunter integration managed configuration - END\n"
            + "val userCode = 1\n"
            + "// Jank Hunter integration managed configuration - BEGIN\n",
            encoding="utf-8",
        )
        settings = project / "settings.gradle.kts"
        before_build = build.read_bytes()
        before_settings = settings.read_bytes()

        completed = self.run_script(
            project,
            "--module",
            ":app",
            "--dry-run",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            "--no-gitignore",
            expected_status=1,
        )

        self.assertIn("BEGIN-before-END", completed.stderr)
        self.assertEqual(before_build, build.read_bytes())
        self.assertEqual(before_settings, settings.read_bytes())
        self.assertFalse((project / ".jankhunter-backups").exists())

    def test_all_modules_are_preflighted_before_first_write(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        feature = project / "feature"
        feature.mkdir()
        feature_build = feature / "build.gradle.kts"
        feature_build.write_text(
            'plugins { id("com.android.library") }\n'
            '// Jank Hunter integration managed configuration - END\n'
            '// Jank Hunter integration managed configuration - BEGIN\n',
            encoding="utf-8",
        )
        settings = project / "settings.gradle.kts"
        app_build = project / "app/build.gradle.kts"
        before_settings = settings.read_bytes()
        before_app = app_build.read_bytes()

        self.run_script(
            project,
            "--module",
            ":app",
            "--module",
            ":feature",
            "--dry-run",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            "--no-gitignore",
            expected_status=1,
        )

        self.assertEqual(before_settings, settings.read_bytes())
        self.assertEqual(before_app, app_build.read_bytes())
        self.assertFalse((project / ".jankhunter-backups").exists())

    def test_backup_and_target_file_symlinks_cannot_escape(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        outside_backups = self.root / "outside-backups"
        outside_backups.mkdir()
        (project / ".jankhunter-backups").symlink_to(
            outside_backups, target_is_directory=True
        )

        completed = self.run_script(
            project,
            "--module",
            ":app",
            "--dry-run",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            expected_status=1,
        )
        self.assertIn("resolves outside", completed.stderr)
        self.assertEqual([], list(outside_backups.iterdir()))

        (project / ".jankhunter-backups").unlink()
        outside_gitignore = self.root / "outside-gitignore"
        outside_gitignore.write_text("KEEP\n", encoding="utf-8")
        (project / ".gitignore").symlink_to(outside_gitignore)
        completed = self.run_script(
            project,
            "--module",
            ":app",
            "--dry-run",
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
            expected_status=1,
        )
        self.assertIn("symlink resolves outside", completed.stderr)
        self.assertEqual("KEEP\n", outside_gitignore.read_text(encoding="utf-8"))

    def test_settings_build_and_local_properties_external_symlinks_are_rejected(self) -> None:
        for target_name in ("settings", "build", "local.properties"):
            with self.subTest(target=target_name):
                project = self.create_project("kts", existing_integration=False)
                unique_project = self.root / f"symlink-{target_name.replace('.', '-')}"
                project.rename(unique_project)
                project = unique_project
                if target_name == "settings":
                    target = project / "settings.gradle.kts"
                elif target_name == "build":
                    target = project / "app/build.gradle.kts"
                else:
                    target = project / "local.properties"
                outside = self.root / f"outside-{target_name.replace('.', '-')}"
                if target.exists():
                    outside.write_bytes(target.read_bytes())
                    target.unlink()
                else:
                    outside.write_text("sdk.dir=/tmp/sdk\n", encoding="utf-8")
                target.symlink_to(outside)

                completed = self.run_script(
                    project,
                    "--module",
                    ":app",
                    "--dry-run",
                    "--skip-publish",
                    "--skip-cli-build",
                    "--skip-local-properties",
                    "--no-gitignore",
                    expected_status=1,
                )

                self.assertIn("symlink resolves outside", completed.stderr)
                self.assertFalse((project / ".jankhunter-backups").exists())

    def test_safe_temp_files_and_custom_gitignore_paths(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        maven_dir = "custom/maven"
        cli_dir = "custom/bin"
        self.create_artifacts(project, maven_dir, cli_dir)
        outside = self.root / "outside-sensitive"
        outside.write_text("KEEP ME\n", encoding="utf-8")
        (project / ".gitignore.tmp").symlink_to(outside)
        arguments = (
            "--module",
            ":app",
            "--maven-dir",
            maven_dir,
            "--cli-dir",
            cli_dir,
            "--skip-publish",
            "--skip-cli-build",
            "--skip-local-properties",
        )

        self.run_script(project, *arguments)
        gitignore = project / ".gitignore"
        first = gitignore.read_bytes()
        self.assertEqual("KEEP ME\n", outside.read_text(encoding="utf-8"))
        self.assertIn("/custom/maven/", first.decode())
        self.assertIn("/custom/bin/", first.decode())

        self.run_script(project, *arguments)
        self.assertEqual(first, gitignore.read_bytes())
        self.assertEqual("KEEP ME\n", outside.read_text(encoding="utf-8"))

    def test_local_properties_temp_symlink_is_not_followed(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        self.create_artifacts(project, "repo/maven", "repo/bin")
        sdk = self.root / "android-sdk"
        sdk.mkdir()
        outside = self.root / "outside-local-properties"
        outside.write_text("KEEP SDK SENTINEL\n", encoding="utf-8")
        (project / "local.properties.tmp").symlink_to(outside)

        self.run_script(
            project,
            "--module",
            ":app",
            "--maven-dir",
            "repo/maven",
            "--cli-dir",
            "repo/bin",
            "--android-sdk",
            str(sdk),
            "--skip-publish",
            "--skip-cli-build",
            "--no-gitignore",
        )

        self.assertEqual(
            "KEEP SDK SENTINEL\n", outside.read_text(encoding="utf-8")
        )
        self.assertEqual(
            f"sdk.dir={sdk}\n",
            (project / "local.properties").read_text(encoding="utf-8"),
        )

    def test_verify_failure_rolls_back_all_target_files(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        self.create_artifacts(project, "repo/maven", "repo/bin")
        gradlew = project / "gradlew"
        gradlew.write_text("#!/usr/bin/env sh\nexit 23\n", encoding="utf-8")
        gradlew.chmod(gradlew.stat().st_mode | stat.S_IXUSR)
        settings = project / "settings.gradle.kts"
        build = project / "app/build.gradle.kts"
        before_settings = settings.read_bytes()
        before_build = build.read_bytes()

        self.run_script(
            project,
            *self.integration_arguments("repo/maven", "repo/bin"),
            "--verify",
            expected_status=23,
        )

        self.assertEqual(before_settings, settings.read_bytes())
        self.assertEqual(before_build, build.read_bytes())
        self.assertTrue((project / ".jankhunter-backups").is_dir())

    def test_default_overlay_preserves_manual_dsl_and_legacy_helper(self) -> None:
        for dsl in ("kts", "groovy"):
            with self.subTest(dsl=dsl):
                project = self.create_project(dsl, existing_integration=True)
                suffix = ".kts" if dsl == "kts" else ""
                build = project / "app" / f"build.gradle{suffix}"
                current = build.read_text(encoding="utf-8")
                current = current.replace(
                    "    retainedHeapDump {",
                    "    instrument {\n"
                    "        okhttp = true\n"
                    "        runtimeCallGraph = true\n"
                    "    }\n"
                    "    retainedHeapDump {",
                )
                build.write_text(current, encoding="utf-8")
                self.create_artifacts(project, "repo/maven", "repo/bin")
                arguments = self.integration_arguments("repo/maven", "repo/bin")

                self.run_script(project, *arguments)
                first = build.read_text(encoding="utf-8")
                self.assertIn("okhttp = true", first)
                self.assertIn("runtimeCallGraph = true", first)
                self.assertNotIn("managed configuration - BEGIN", first)
                self.assertIn("optional helper dependencies - BEGIN", first)
                self.assertIn(f"jankhunter-android-sdk:{VERSION}", first)
                self.assertIn("com.example:inside-legacy-block:1", first)

                self.run_script(project, *arguments)
                self.assertEqual(first, build.read_text(encoding="utf-8"))

    def test_unspecified_options_preserve_existing_managed_overlay(self) -> None:
        project = self.create_project("kts", existing_integration=False)
        build = project / "app/build.gradle.kts"
        self.create_artifacts(project, "repo/maven", "repo/bin")
        arguments = self.integration_arguments("repo/maven", "repo/bin")

        self.run_script(
            project,
            *arguments,
            "--build-type",
            "qa",
            "--runtime-call-graph",
            "--okhttp",
            "--analyze-di",
            "--max-session-log-size-mib",
            "12",
        )
        configured = build.read_bytes()

        self.run_script(project, *arguments)

        self.assertEqual(configured, build.read_bytes())
        result = configured.decode()
        self.assertIn('enabledBuildTypes.set(setOf("qa"))', result)
        self.assertIn("runtimeCallGraph = true", result)
        self.assertIn("okhttp = true", result)
        self.assertIn("maxSessionLogSizeMiB = 12", result)
        self.assertIn(f'implementation("{GROUP}:jankhunter-android-sdk:{VERSION}")', result)


class AndroidE2EScriptTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.sdk = self.root / "sdk"
        build_tools = self.sdk / "build-tools/35.0.0"
        build_tools.mkdir(parents=True)
        self.write_executable(
            build_tools / "aapt",
            """#!/bin/sh
case "${3:-}" in
  *androidTest*) package_id="$FAKE_TEST_APK_PACKAGE" ;;
  *) package_id="$FAKE_APP_APK_PACKAGE" ;;
esac
printf "package: name='%s' versionCode='1'\n" "$package_id"
""",
        )
        self.bin = self.root / "bin"
        self.bin.mkdir()

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @staticmethod
    def write_executable(path: Path, text: str) -> None:
        path.write_text(text, encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR)

    def environment(self, adb: Path) -> dict[str, str]:
        environment = os.environ.copy()
        environment.update(
            {
                "ADB": str(adb),
                "ANDROID_HOME": str(self.sdk),
                "ANDROID_BUILD_TOOLS_VERSION": "35.0.0",
                "HOME": str(self.root / "home"),
                "PYTHON": sys.executable,
            }
        )
        environment.pop("ANDROID_SDK_ROOT", None)
        return environment

    def run_actual_script(
        self, adb: Path, *arguments: str
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [str(ANDROID_E2E_SCRIPT), *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=self.environment(adb),
        )

    def test_adb_failure_and_empty_bash3_array_have_actionable_errors(self) -> None:
        failing_adb = self.bin / "adb-fails"
        self.write_executable(failing_adb, "#!/bin/sh\nprintf 'transport failed\\n' >&2\nexit 42\n")
        failed = self.run_actual_script(failing_adb, "--serial", "emulator-5554")
        self.assertNotEqual(0, failed.returncode)
        self.assertIn("adb devices failed: transport failed", failed.stderr)
        self.assertNotIn("unbound variable", failed.stderr)

        empty_adb = self.bin / "adb-empty"
        self.write_executable(empty_adb, "#!/bin/sh\nprintf 'List of devices attached\\n'\n")
        empty = self.run_actual_script(empty_adb, "--serial", "emulator-5554")
        self.assertNotEqual(0, empty.returncode)
        self.assertIn("device is not online or was not found", empty.stderr)
        self.assertNotIn("unbound variable", empty.stderr)

    def test_nonempty_unowned_output_directory_is_not_cleaned(self) -> None:
        adb = self.bin / "adb"
        self.write_executable(
            adb,
            "#!/bin/sh\nprintf 'List of devices attached\\nemulator-5554\\tdevice\\n'\n",
        )
        output = self.root / "not-owned"
        output.mkdir()
        sentinel = output / "keep-me.txt"
        sentinel.write_text("important", encoding="utf-8")

        completed = self.run_actual_script(adb, "--out-dir", str(output))

        self.assertNotEqual(0, completed.returncode)
        self.assertIn("non-empty unowned output directory", completed.stderr)
        self.assertEqual("important", sentinel.read_text(encoding="utf-8"))

    def complete_summary(self, *warnings: str) -> dict:
        return {
            "LogCount": 1,
            "EventCount": 12,
            "DataRecordCount": 12,
            "Dictionary": 8,
            "CollectionQuality": {
                "complete": True,
                "chain_valid": True,
                "unsealed_segments": 0,
                "known_lost_events": 0,
                "dictionary_overflow": 0,
                "dictionary_truncated": 0,
                "accepted_events": 12,
                "written_events": 12,
            },
            "Counters": [
                {"Name": "sample.e2e.retained.watch.count", "Value": 1},
                {"Name": "sample.e2e.background.count", "Value": 1},
            ],
            "Gauges": [
                {"Name": "sample.e2e.background.duration_ms", "Value": 80}
            ],
            "Screens": [{"Screen": "SampleEndToEnd"}],
            "Owners": [{"Owner": "sample.e2e.synthetic_stall"}],
            "Warnings": list(warnings),
        }

    def create_full_fixture(self) -> tuple[Path, Path, dict[str, str]]:
        fixture = self.root / "fixture"
        scripts = fixture / "scripts"
        android = fixture / "android"
        cli = fixture / "cli"
        scripts.mkdir(parents=True)
        android.mkdir()
        cli.mkdir()
        copied_script = scripts / "android-e2e.sh"
        shutil.copy2(ANDROID_E2E_SCRIPT, copied_script)

        gradle_args = self.root / "gradle-args.txt"
        self.write_executable(
            android / "gradlew",
            "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$FAKE_GRADLE_ARGS\"\n",
        )
        app_apk = android / "sample-app/build/outputs/apk/debug/sample-app-debug.apk"
        test_apk = android / "sample-app/build/outputs/apk/androidTest/debug/sample-app-debug-androidTest.apk"
        app_apk.parent.mkdir(parents=True)
        test_apk.parent.mkdir(parents=True)
        app_apk.write_bytes(b"app-apk")
        test_apk.write_bytes(b"test-apk")
        device_logs = self.root / "device-logs"
        device_logs.mkdir()
        (device_logs / "jh-session-log.2026-07-14.0.jhlog").write_bytes(b"fixture")

        adb = self.bin / "adb-full"
        adb_args = self.root / "adb-args.txt"
        self.write_executable(
            adb,
            """#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_ADB_ARGS"
if [ "${1:-}" = devices ]; then
  printf 'List of devices attached\\nemulator-5554\\tdevice\\n'
  exit 0
fi
if [ "${1:-}" = -s ]; then
  shift 2
fi
case "${1:-}" in
  install)
    exit 0
    ;;
  uninstall)
    if [ "${2:-}" = "${FAKE_UNINSTALL_FAILURE_PACKAGE:-}" ]; then
      printf 'uninstall failed\n' >&2
      exit 1
    fi
    printf 'Success\n'
    exit 0
    ;;
  shell)
    if [ "${2:-}" = pm ] && [ "${3:-}" = list ] && [ "${4:-}" = packages ]; then
      if [ "${5:-}" = "${FAKE_EXISTING_PACKAGE:-}" ]; then
        printf 'package:%s\n' "${5:-}"
      fi
      exit 0
    fi
    printf '%s\n' "$FAKE_INSTRUMENTATION_OUTPUT"
    exit "${FAKE_INSTRUMENTATION_EXIT_CODE:-0}"
    ;;
  exec-out)
    exec tar -C "$FAKE_DEVICE_LOGS" -cf - .
    ;;
esac
printf 'unexpected adb command: %s\n' "$*" >&2
exit 2
""",
        )
        go_args = self.root / "go-args.txt"
        self.write_executable(
            self.bin / "go",
            """#!/bin/sh
printf '%s\\n' "$*" > "$FAKE_GO_ARGS"
out=''
while [ "$#" -gt 0 ]; do
  if [ "$1" = --out ]; then
    shift
    out="$1"
  fi
  shift
done
printf '<html>report</html>\\n' > "$out"
printf '%s\\n' "$FAKE_INSPECT_JSON"
""",
        )
        environment = self.environment(adb)
        environment.update(
            {
                "PATH": f"{self.bin}{os.pathsep}{environment['PATH']}",
                "FAKE_DEVICE_LOGS": str(device_logs),
                "FAKE_ADB_ARGS": str(adb_args),
                "FAKE_GO_ARGS": str(go_args),
                "FAKE_GRADLE_ARGS": str(gradle_args),
                "FAKE_APP_APK_PACKAGE": "io.jankhunter.sample",
                "FAKE_TEST_APK_PACKAGE": "io.jankhunter.sample.test",
                "FAKE_EXISTING_PACKAGE": "",
                "FAKE_INSTRUMENTATION_OUTPUT": "OK (1 test)",
                "FAKE_INSTRUMENTATION_EXIT_CODE": "0",
                "FAKE_UNINSTALL_FAILURE_PACKAGE": "",
            }
        )
        return copied_script, go_args, environment

    def test_runtime_quality_gate_allows_only_missing_optional_asm_warning(self) -> None:
        script, _, environment = self.create_full_fixture()
        output = self.root / "e2e-output"
        environment["FAKE_INSPECT_JSON"] = json.dumps(
            self.complete_summary(
                "Качество сбора: ASM-диагностика не передана; нельзя подтвердить hooks."
            ),
            ensure_ascii=False,
        )

        passed = subprocess.run(
            [str(script), "--out-dir", str(output)],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )
        self.assertEqual(0, passed.returncode, passed.stderr)
        self.assertEqual(
            "jankhunter-android-e2e:v1\n",
            (output / ".jankhunter-android-e2e-owned").read_text(encoding="utf-8"),
        )

        environment["FAKE_INSPECT_JSON"] = json.dumps(
            self.complete_summary(
                "Качество сбора: runtime-граф вызовов отбросил ребра: 1."
            ),
            ensure_ascii=False,
        )
        rejected = subprocess.run(
            [str(script), "--out-dir", str(output)],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )
        self.assertNotEqual(0, rejected.returncode)
        self.assertIn("forbidden collection-quality warning", rejected.stderr)

    def test_runtime_quality_gate_requires_markers_in_real_screen_and_owner_rows(self) -> None:
        script, _, environment = self.create_full_fixture()
        summary = self.complete_summary()
        summary["Screens"] = []
        summary["Owners"] = []
        summary["Warnings"] = [
            "SampleEndToEnd",
            "sample.e2e.synthetic_stall",
        ]
        summary["Unrelated"] = {
            "Screen": "SampleEndToEnd",
            "Owner": "sample.e2e.synthetic_stall",
        }
        environment["FAKE_INSPECT_JSON"] = json.dumps(summary, ensure_ascii=False)

        completed = subprocess.run(
            [str(script), "--out-dir", str(self.root / "missing-real-markers")],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertNotEqual(0, completed.returncode)
        self.assertIn("expected screen context is missing", completed.stderr)
        self.assertIn("expected owner is missing", completed.stderr)

    def test_runtime_quality_gate_accepts_screen_context_from_short_flow(self) -> None:
        script, _, environment = self.create_full_fixture()
        summary = self.complete_summary()
        summary["Screens"] = []
        summary["Flows"] = [{"Screen": "SampleEndToEnd"}]
        environment["FAKE_INSPECT_JSON"] = json.dumps(summary, ensure_ascii=False)

        completed = subprocess.run(
            [str(script), "--out-dir", str(self.root / "flow-screen-context")],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertEqual(0, completed.returncode, completed.stderr)

    def test_diagnostics_option_is_forwarded_and_build_tools_property_is_set(self) -> None:
        script, go_args, environment = self.create_full_fixture()
        environment["FAKE_INSPECT_JSON"] = json.dumps(self.complete_summary())
        diagnostics = self.root / "instrumentation-diagnostics.jsonl"
        diagnostics.write_text('{"format":1}\n', encoding="utf-8")
        output = self.root / "e2e-with-diagnostics"

        completed = subprocess.run(
            [
                str(script),
                "--out-dir",
                str(output),
                "--instrumentation-diagnostics",
                str(diagnostics),
            ],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertEqual(0, completed.returncode, completed.stderr)
        self.assertIn("--instrumentation-diagnostics", go_args.read_text(encoding="utf-8"))
        gradle_args = (self.root / "gradle-args.txt").read_text(encoding="utf-8")
        self.assertIn("-PjankHunterBuildToolsVersion=35.0.0", gradle_args)
        self.assertIn("--console=plain", gradle_args)
        self.assertIn(":sample-app:assembleDebug", gradle_args)
        self.assertIn(":sample-app:assembleDebugAndroidTest", gradle_args)
        self.assertNotIn("connectedDebugAndroidTest", gradle_args)

        adb_calls = (self.root / "adb-args.txt").read_text(encoding="utf-8").splitlines()
        install_app = next(
            index
            for index, call in enumerate(adb_calls)
            if "install -r -t" in call and "androidTest" not in call
        )
        install_test = next(
            index
            for index, call in enumerate(adb_calls)
            if "install -r -t" in call and "androidTest" in call
        )
        instrumentation = next(
            index for index, call in enumerate(adb_calls) if "shell am instrument" in call
        )
        copy_logs = next(
            index for index, call in enumerate(adb_calls) if "exec-out run-as" in call
        )
        uninstall_test = next(
            index
            for index, call in enumerate(adb_calls)
            if "uninstall io.jankhunter.sample.test" in call
        )
        uninstall_app = next(
            index
            for index, call in enumerate(adb_calls)
            if call.endswith("uninstall io.jankhunter.sample")
        )
        self.assertLess(install_app, install_test)
        self.assertLess(install_test, instrumentation)
        self.assertLess(instrumentation, copy_logs)
        self.assertLess(copy_logs, uninstall_test)
        self.assertLess(uninstall_test, uninstall_app)

    def test_instrumentation_failure_is_rejected_before_private_logs_are_copied(self) -> None:
        script, _, environment = self.create_full_fixture()
        environment["FAKE_INSPECT_JSON"] = json.dumps(self.complete_summary())
        environment["FAKE_INSTRUMENTATION_OUTPUT"] = (
            "FAILURES!!!\nTests run: 1, Failures: 1"
        )

        completed = subprocess.run(
            [str(script), "--out-dir", str(self.root / "failed-instrumentation")],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertNotEqual(0, completed.returncode)
        self.assertIn(
            "did not report exactly one successful E2E test",
            completed.stderr,
        )
        adb_calls = (self.root / "adb-args.txt").read_text(encoding="utf-8").splitlines()
        self.assertFalse(any("exec-out run-as" in call for call in adb_calls))
        self.assertTrue(
            any("uninstall io.jankhunter.sample.test" in call for call in adb_calls)
        )
        self.assertTrue(
            any(call.endswith("uninstall io.jankhunter.sample") for call in adb_calls)
        )

    def test_apk_package_mismatch_fails_before_device_packages_are_changed(self) -> None:
        script, _, environment = self.create_full_fixture()
        environment["FAKE_INSPECT_JSON"] = json.dumps(self.complete_summary())
        environment["FAKE_TEST_APK_PACKAGE"] = "com.example.unexpected.test"

        completed = subprocess.run(
            [str(script), "--out-dir", str(self.root / "package-mismatch")],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertNotEqual(0, completed.returncode)
        self.assertIn("instrumentation APK package is", completed.stderr)
        adb_calls = (self.root / "adb-args.txt").read_text(encoding="utf-8").splitlines()
        self.assertFalse(any(" install " in call for call in adb_calls))
        self.assertFalse(any(" uninstall " in call for call in adb_calls))

    def test_preinstalled_sample_package_is_never_overwritten_or_removed(self) -> None:
        script, _, environment = self.create_full_fixture()
        environment["FAKE_INSPECT_JSON"] = json.dumps(self.complete_summary())
        environment["FAKE_EXISTING_PACKAGE"] = "io.jankhunter.sample"

        completed = subprocess.run(
            [str(script), "--out-dir", str(self.root / "preinstalled-package")],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertNotEqual(0, completed.returncode)
        self.assertIn("refusing to overwrite already installed package", completed.stderr)
        adb_calls = (self.root / "adb-args.txt").read_text(encoding="utf-8").splitlines()
        self.assertFalse(any(" install " in call for call in adb_calls))
        self.assertFalse(any(" uninstall " in call for call in adb_calls))

    def test_uninstall_failure_makes_successful_instrumentation_run_fail_closed(self) -> None:
        script, _, environment = self.create_full_fixture()
        environment["FAKE_INSPECT_JSON"] = json.dumps(self.complete_summary())
        environment["FAKE_UNINSTALL_FAILURE_PACKAGE"] = "io.jankhunter.sample.test"

        completed = subprocess.run(
            [str(script), "--out-dir", str(self.root / "cleanup-failure")],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

        self.assertNotEqual(0, completed.returncode)
        self.assertIn("could not uninstall io.jankhunter.sample.test", completed.stderr)
        self.assertIn("warning: could not uninstall", completed.stderr)
        adb_calls = (self.root / "adb-args.txt").read_text(encoding="utf-8").splitlines()
        test_uninstalls = [
            call for call in adb_calls if "uninstall io.jankhunter.sample.test" in call
        ]
        self.assertGreaterEqual(len(test_uninstalls), 2)
        self.assertTrue(
            any(call.endswith("uninstall io.jankhunter.sample") for call in adb_calls)
        )


class GradlePluginSmokeScriptValidationTest(unittest.TestCase):
    def run_smoke(self, *arguments: str, **updates: str) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment.update(updates)
        return subprocess.run(
            [str(GRADLE_SMOKE_SCRIPT), *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )

    def test_rejects_invalid_boolean_environment_before_build(self) -> None:
        completed = self.run_smoke(SMOKE_CONFIGURATION_CACHE="true")
        self.assertNotEqual(0, completed.returncode)
        self.assertIn("SMOKE_CONFIGURATION_CACHE must be 0 or 1", completed.stderr)

    def test_help_rejects_trailing_arguments(self) -> None:
        completed = self.run_smoke("--help", "unexpected")
        self.assertNotEqual(0, completed.returncode)
        self.assertIn("unknown or unexpected arguments", completed.stderr)


if __name__ == "__main__":
    unittest.main()
