import hashlib
import importlib.util
import json
import os
import subprocess
import textwrap
from pathlib import Path
import tempfile
import unittest
import zipfile

FILE = Path(__file__).with_name('verify-release-environment.py')


class ReleaseEnvironmentTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('release_environment', FILE)
        cls.env = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.env)

    def test_gradle_version_must_be_exact_not_substring_or_banner(self):
        self.env.validate_gradle_version('Welcome\nGradle 8.0.2\nKotlin: 1.8', '8.0.2')
        for value in ['Gradle 8.0.20', 'Welcome to Gradle 8.0.2!', 'Gradle 8.13', 'Gradle 8.0.2\nGradle 8.13']:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.env.validate_gradle_version(value, '8.0.2')

    def test_distribution_verifies_archive_and_all_extracted_files_before_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            home = root / 'gradle-8.0.2'
            (home / 'bin').mkdir(parents=True)
            (home / 'lib').mkdir()
            (home / 'bin/gradle').write_bytes(b'launcher')
            jar = home / 'lib/gradle-launcher.jar'
            jar.write_bytes(b'original executable code')
            archive = root / 'distribution.zip'
            with zipfile.ZipFile(archive, 'w') as output:
                for path in home.rglob('*'):
                    if path.is_file(): output.write(path, str(path.relative_to(root)))
            sha = self.env.digest(archive)
            self.env.verify_distribution(home / 'bin/gradle', archive, '8.0.2', sha)
            jar.write_bytes(b'tampered executable code')
            with self.assertRaises(ValueError):
                self.env.verify_distribution(home / 'bin/gradle', archive, '8.0.2', sha)
            jar.write_bytes(b'original executable code')
            (home / 'lib/extra.jar').write_bytes(b'extra')
            with self.assertRaises(ValueError):
                self.env.verify_distribution(home / 'bin/gradle', archive, '8.0.2', sha)
            with self.assertRaises(ValueError):
                self.env.verify_distribution(home / 'bin/gradle', archive, '8.0.2', '0'*64)

    def test_distribution_rejects_path_traversal(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / 'bad.zip'
            with zipfile.ZipFile(archive, 'w') as output:
                output.writestr('gradle-8.0.2/../escape', b'x')
            with self.assertRaises(ValueError):
                self.env.verify_distribution(root / 'gradle-8.0.2/bin/gradle', archive, '8.0.2', self.env.digest(archive))

    def test_cache_rejects_mutation_unlisted_files_and_escape(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            item = root / 'module.jar'
            item.write_bytes(b'original')
            records = {'module.jar': self.env.digest(item)}
            self.env.verify_cache(root, records)
            item.write_bytes(b'changed')
            with self.assertRaises(ValueError): self.env.verify_cache(root, records)
            item.write_bytes(b'original')
            (root / 'unlisted.jar').write_bytes(b'x')
            with self.assertRaises(ValueError): self.env.verify_cache(root, records)
            with self.assertRaises(ValueError): self.env.verify_cache(root, {'../escape': '0'*64})
            with self.assertRaises(ValueError): self.env.verify_cache(root, {})

    def test_wrong_device_api_abi_emulator_or_unknown_hardware_cannot_pass(self):
        facts = dict(api=28, abi='arm64-v8a', fingerprint='vendor/device/build', serial='physical123',
                     state='device', kernel_qemu='', boot_qemu='', memory_kib=3_000_000, cpu_max_khz=1_800_000,
                     cpu_present_count=4, cpu_frequency_count=4)
        pin = dict(abi='arm64-v8a', fingerprint='vendor/device/build')
        self.env.validate_device(facts, pin, 28, weak=True)
        for change in [dict(api=37), dict(abi='x86_64'), dict(kernel_qemu='1'), dict(boot_qemu='1'),
                       dict(serial='emulator-5554'), dict(state='offline'), dict(cpu_max_khz=None),
                       dict(memory_kib=8_000_000), dict(cpu_max_khz=3_000_000), dict(fingerprint='other'),
                       dict(cpu_frequency_count=3), dict(cpu_present_count=0), dict(serial=''),
                       dict(serial='-s other'), dict(cpu_frequency_count=4.0)]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.env.validate_device(dict(facts, **change), pin, 28, weak=True)

    def test_sdk_must_match_api_revision_and_pinned_jar(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            platform = root / 'platforms/android-35'
            platform.mkdir(parents=True)
            (platform / 'source.properties').write_text('AndroidVersion.ApiLevel=35\nPkg.Revision=2\n')
            jar = platform / 'android.jar'; jar.write_bytes(b'expected platform')
            pin = dict(revision='2', android_jar_sha256=self.env.digest(jar))
            self.env.verify_sdk_platform(root, 35, pin)
            jar.write_bytes(b'other platform')
            with self.assertRaises(ValueError): self.env.verify_sdk_platform(root, 35, pin)
            (platform / 'source.properties').write_text('AndroidVersion.ApiLevel=34\nPkg.Revision=2\n')
            with self.assertRaises(ValueError): self.env.verify_sdk_platform(root, 35, pin)

    def test_provisioning_manifest_must_match_independent_sha(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'manifest.json'; path.write_text('{"schema":1}')
            self.assertEqual(1, self.env.load_pinned_manifest(path, self.env.digest(path))['schema'])
            for pin in ['', 'unknown', '0'*64]:
                with self.assertRaises(ValueError): self.env.load_pinned_manifest(path, pin)

    def test_release_evidence_rejects_missing_or_fabricated_empty_pass(self):
        for value in [{}, dict(schema=1, pass_=True), {'schema': 1, 'pass': True, 'source_revision': 'sha'}]:
            self.assertTrue(self.env.evidence_errors(value, 'sha'))

    def test_legacy_manifest_cannot_relabel_new_sdk_as_1_0_12(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            records = []
            for module in ('jankhunter-runtime', 'jankhunter-gradle-plugin'):
                path = root / 'io/jankhunter' / module / '1.0.12' / (module + '-1.0.12.pom')
                path.parent.mkdir(parents=True)
                path.write_text(f'<project><groupId>io.jankhunter</groupId><artifactId>{module}</artifactId><version>1.0.12</version></project>')
                records.append(dict(module=module, file=path.name))
                records.append(dict(module=module, file=module + '-1.0.12-sources.jar'))
            self.env.verify_legacy_coordinates(root, records)
            path.write_text(path.read_text().replace('1.0.12', '1.0.13'))
            with self.assertRaises(ValueError): self.env.verify_legacy_coordinates(root, records)

    def test_complete_environment_evidence_is_accepted_and_mutations_rejected(self):
        locks = json.loads(FILE.with_name('release-gradle-distributions.json').read_text())
        proof = {'schema': 1, 'pass': True, 'source_revision': 'sha', 'source_dirty': False,
                 'input_manifest_sha256': 'a'*64, 'cache': {'files': 10},
                 'legacy': {'version': '1.0.12', 'manifest_sha256': 'b'*64, 'files': 10},
                 'metadata_baseline': {'version': '1.0.11', 'files': 10},
                 'gradle': {}, 'devices': {}, 'sdk': {}}
        for role, versions in self.env.ROLES.items():
            version = sorted(versions)[0]
            proof['gradle'][role] = dict(version=version, archive_sha256=locks[version]['sha256'])
        for role, api in self.env.DEVICE_APIS.items():
            proof['devices'][role] = dict(api=api or 28, state='device', serial='physical123', abi='arm64-v8a',
                fingerprint='vendor/build', memory_kib=3_000_000, cpu_max_khz=1_800_000, kernel_qemu='', boot_qemu='',
                cpu_present_count=4, cpu_frequency_count=4)
        for api in (33, 34, 35): proof['sdk'][str(api)] = dict(api=api, revision='2', android_jar_sha256='c'*64)
        self.assertEqual([], self.env.evidence_errors(proof, 'sha'))
        self.assertTrue(self.env.evidence_errors(proof, 'other'))
        proof['devices']['JH_API28_SERIAL']['api'] = 37
        self.assertTrue(self.env.evidence_errors(proof, 'sha'))

    def test_release_version_is_validated_without_shell_evaluation(self):
        workflow = (FILE.parents[1] / '.github/workflows/release.yml').read_text()
        block = workflow.split('      - name: Resolve release metadata', 1)[1].split('        run: |\n', 1)[1].split('\n      - uses:', 1)[0]
        script = textwrap.dedent(block)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            payload = "$(touch '" + str(root / 'unexpected') + "')"
            for value, accepted in [('1.0.13', True), ('1.0.13-rc.1+build.2', True), (payload, False), ('../bad', False), ('', False)]:
                environment = dict(os.environ, GITHUB_EVENT_NAME='workflow_dispatch', RELEASE_INPUT_VERSION=value,
                                   GITHUB_OUTPUT=str(root / 'output'), GITHUB_REF_NAME='unused')
                rendered = script.replace('${{ github.event_name }}', 'workflow_dispatch').replace('${{ inputs.version }}', value)
                result = subprocess.run(['bash', '-c', rendered], env=environment, capture_output=True)
                self.assertEqual(accepted, result.returncode == 0, value)
                self.assertFalse((root / 'unexpected').exists())

    def test_workflow_runs_preflight_before_any_release_gate_and_archives_proof(self):
        workflow = (FILE.parents[1] / '.github/workflows/release.yml').read_text()
        self.assertLess(workflow.index('scripts/verify-release-environment.py'), workflow.index('name: Build macOS CLI archives'))
        self.assertIn('--environment "$RUNNER_TEMP/release-environment-$GITHUB_SHA.json"', workflow)
        self.assertIn('release-environment-${{ github.sha }}.json', workflow)


if __name__ == '__main__': unittest.main()
