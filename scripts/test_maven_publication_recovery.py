#!/usr/bin/env python3
"""Exercise immutable publication recovery without contacting Nexus."""

import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

ROOT = Path(__file__).resolve().parents[2]
PUBLISH = ROOT / 'scripts/jank-hunter-publish'
MONOREPO = (PUBLISH / 'reconcile-integrations.py').is_file()
reconcile = None
if MONOREPO:
    spec = importlib.util.spec_from_file_location('reconcile', PUBLISH / 'reconcile-integrations.py')
    reconcile = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(reconcile)


class FakeRepository:
    def __init__(self):
        self.files = {}
        self.uploads = []
        self.fail_after_upload = False
        self.fail_read = False

    def request(self, relative, data=None):
        if data is None:
            if self.fail_read:
                raise RuntimeError('HTTP 401')
            return self.files.get(relative)
        if relative in self.files:
            raise AssertionError('Attempted to overwrite an immutable file')
        self.files[relative] = data
        self.uploads.append(relative)
        if self.fail_after_upload:
            self.fail_after_upload = False
            raise RuntimeError('Connection lost after upload')


@unittest.skipUnless(MONOREPO, 'Publication scripts belong to shared-android-libs')
class RecoveryTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.staging = Path(self.temporary.name)
        self.expected = {}
        for module in reconcile.MODULES:
            directory = self.staging / 'io/jankhunter' / module / '1.0.13'
            directory.mkdir(parents=True)
            for suffix in ('.aar', '-sources.jar', '.pom', '.module'):
                path = directory / (module + '-1.0.13' + suffix)
                content = path.name.encode()
                path.write_bytes(content)
                self.expected[path.relative_to(self.staging).as_posix()] = content
            (directory / (module + '-1.0.13.pom.sha1')).write_text('checksum')
            (directory.parent / 'maven-metadata.xml').write_text('shared metadata')
        self.repository = FakeRepository()

    def run_recovery(self):
        reconcile.reconcile(self.staging, '1.0.13', self.repository)

    def test_missing_release_is_uploaded_and_repeat_uploads_nothing(self):
        self.run_recovery()
        self.assertEqual(self.expected, self.repository.files)
        self.assertEqual(8, len(self.repository.uploads))
        self.repository.uploads.clear()
        self.run_recovery()
        self.assertEqual([], self.repository.uploads)

    def test_partial_module_and_complete_module_are_resumed(self):
        for relative, content in list(self.expected.items())[:5]:
            self.repository.files[relative] = content
        self.run_recovery()
        self.assertEqual(self.expected, self.repository.files)
        self.assertEqual(3, len(self.repository.uploads))

    def test_interrupted_upload_can_be_resumed_without_overwrite(self):
        self.repository.fail_after_upload = True
        with self.assertRaisesRegex(RuntimeError, 'Connection lost'):
            self.run_recovery()
        self.assertEqual(1, len(self.repository.files))
        self.run_recovery()
        self.assertEqual(self.expected, self.repository.files)
        self.assertEqual(8, len(self.repository.uploads))

    def test_conflict_in_second_module_prevents_all_uploads(self):
        relative = next(key for key in self.expected if 'jankstats' in key)
        self.repository.files[relative] = b'other release'
        with self.assertRaisesRegex(RuntimeError, 'Existing file differs'):
            self.run_recovery()
        self.assertEqual([], self.repository.uploads)

    def test_auth_failure_prevents_all_uploads(self):
        self.repository.fail_read = True
        with self.assertRaisesRegex(RuntimeError, '401'):
            self.run_recovery()
        self.assertEqual([], self.repository.uploads)

    def test_missing_staged_file_prevents_all_uploads(self):
        (self.staging / next(iter(self.expected))).unlink()
        with self.assertRaisesRegex(ValueError, 'Missing staged'):
            self.run_recovery()
        self.assertEqual([], self.repository.uploads)

    def test_corrupt_upload_fails_verification(self):
        original = self.repository.request
        def corrupt(relative, data=None):
            result = original(relative, data)
            if data is not None:
                self.repository.files[relative] = b'corrupt'
            return result
        self.repository.request = corrupt
        with self.assertRaisesRegex(RuntimeError, 'verification failed'):
            self.run_recovery()

    def test_only_404_is_treated_as_missing(self):
        repository = reconcile.Repository('https://example.invalid/repository', 'user', 'secret')
        for status in (401, 403, 404, 500):
            with self.subTest(status=status), patch.object(repository.opener, 'open', side_effect=HTTPError('url', status, 'error', {}, None)):
                if status == 404:
                    self.assertIsNone(repository.request('artifact.pom'))
                else:
                    with self.assertRaisesRegex(RuntimeError, str(status)):
                        repository.request('artifact.pom')


@unittest.skipUnless(MONOREPO, 'Publication jobs belong to shared-android-libs')
class JobTest(unittest.TestCase):
    def test_ci_jobs_are_distinct_serialized_non_interruptible_and_required(self):
        configs = [json.loads((ROOT / 'GitlabConfigs/job_configs' / name).read_text())
                   for name in ('deployment_jobs.json', 'web_build_jobs.json')]
        jobs = [config[name] for config, name in zip(configs, ('publishJankHunterIntegrations', 'publishJankHunterIntegrationsWeb'))]
        self.assertNotIn('publishJankHunterIntegrations', configs[1])
        for config, job in zip(configs, jobs):
            self.assertFalse(job['interruptible'])
            self.assertFalse(job['allow_failure'])
            self.assertEqual('manual', job['when'])
            self.assertEqual(config['publishJankHunterAndroid']['resource_group'], job['resource_group'])
            self.assertEqual('!reference [.publishJankHunterIntegrations, script]', job['script'])

    def test_shell_accepts_maven_only_or_nexus_only_and_rejects_missing_values(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            script = root / 'scripts/jank-hunter-publish/publish-android.sh'
            script.parent.mkdir(parents=True)
            shutil.copy2(PUBLISH / 'publish-android.sh', script)
            # Capture remote configuration after Gradle has been scoped to staging.
            helper = script.parent / 'reconcile-integrations.py'
            helper.write_text('import os, pathlib\npathlib.Path(os.environ["CAPTURE_REMOTE"]).write_text(os.environ["MAVEN_REPOSITORY_URL"])\n')
            android = root / 'jank-hunter/android'
            android.mkdir(parents=True)
            (android / 'gradle.properties').write_text('jankHunterVersion=1.0.13\n')
            (root / 'sdk/build-tools/35.0.0').mkdir(parents=True)
            wrapper = android / 'gradlew'
            wrapper.write_text('#!/bin/bash\nprintf "%s\\n" "$MAVEN_REPOSITORY_URL" "$@" > "$CAPTURE_GRADLE"\n')
            wrapper.chmod(0o755)
            env = {key: value for key, value in os.environ.items() if not key.startswith(('MAVEN_REPOSITORY_', 'NEXUS_'))}
            env.update(ANDROID_HOME=str(root / 'sdk'), ANDROID_BUILD_TOOLS_VERSION='35.0.0', CAPTURE_REMOTE=str(root / 'remote'), CAPTURE_GRADLE=str(root / 'gradle'))
            for prefix in ('MAVEN_REPOSITORY', 'NEXUS'):
                credentials = {f'{prefix}_{key}': value for key, value in [('URL', 'https://example.invalid/maven'), ('USERNAME', 'user'), ('PASSWORD', 'secret')]}
                if prefix == 'NEXUS':
                    credentials['NEXUS_REPOSITORY_URL'] = credentials.pop('NEXUS_URL')
                result = subprocess.run(['bash', str(script), '--integrations-only'], env={**env, **credentials}, capture_output=True, text=True)
                self.assertEqual(0, result.returncode, result.stderr)
                args = (root / 'gradle').read_text().splitlines()
                self.assertTrue(args[0].startswith('file:'))
                self.assertEqual([':jankhunter-workmanager:publishReleasePublicationToRemoteReleaseRepository', ':jankhunter-jankstats:publishReleasePublicationToRemoteReleaseRepository'], [arg for arg in args if arg.startswith(':')])
                self.assertEqual('https://example.invalid/maven', (root / 'remote').read_text())
                self.assertFalse(Path(args[0][5:]).parent.exists())
            result = subprocess.run(['bash', str(script), '--integrations-only'], env=env, capture_output=True, text=True)
            self.assertNotEqual(0, result.returncode)
            self.assertIn('MAVEN_REPOSITORY_URL', result.stderr)


if __name__ == '__main__':
    unittest.main()
