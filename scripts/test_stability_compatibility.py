import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location('gate', Path(__file__).with_name('run-stability-compatibility.py'))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)


class CompatibilityGateTest(unittest.TestCase):
    def test_private_reader_cannot_leak_to_apk(self):
        with tempfile.TemporaryDirectory() as directory:
            apk = Path(directory) / 'app.apk'
            for name, data in [('classes.dex', b'io/jankhunter/reader/IsolatedMetadataIndex'),
                               ('classes.dex', b'JankHunterBuildEvidenceV1_main'),
                               ('assets/jankhunter/metadata-reader/reader.jar', b'private')]:
                with zipfile.ZipFile(apk, 'w') as archive:
                    archive.writestr(name, data)
                with self.assertRaises(ValueError):
                    gate.verify_apk_reader_isolation(apk)
    def test_generated_caches_do_not_fake_dirty_source_and_real_changes_still_block(self):
        self.assertFalse(gate.source_dirty_from_status('?? jank-hunter/scripts/__pycache__/\n?? jank-hunter/android/build-logic/.kotlin/\n'))
        self.assertTrue(gate.source_dirty_from_status(' M jank-hunter/scripts/gate.py\n'))
        self.assertTrue(gate.source_dirty_from_status('?? jank-hunter/scripts/new_source.py\n'))
        self.assertTrue(gate.source_dirty_from_status(' M jank-hunter/android/build/generated.kt\n'))

    def test_exact_jh_publication_requires_no_application_logger(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            records = []
            for module in ('jankhunter-runtime', 'jankhunter-annotations', 'jankhunter-okhttp3',
                           'jankhunter-workmanager', 'jankhunter-jankstats', 'jankhunter-android-sdk', 'jankhunter-gradle-plugin'):
                name = module + '-1.jar'
                target = root / 'io/jankhunter' / module / '1' / name
                target.parent.mkdir(parents=True)
                target.write_bytes(b'exact-publication-test')
                records.append({'module': module, 'file': name, 'sha256': hashlib.sha256(target.read_bytes()).hexdigest()})
            (root / 'exact-artifacts.json').write_text(json.dumps(records))
            with patch.object(gate.boundary, 'validate_artifact', return_value={'errors': []}):
                self.assertEqual(records, gate.verify_repository(root, '1'))

    def test_exit_zero_is_not_art_success(self):
        for output in ('BUILD SUCCESSFUL', 'OK (1 test)', 'OK (2 tests)\nINSTRUMENTATION_STATUS_CODE: -3', 'OK (2 tests)\nProcess crashed'):
            with self.assertRaises(ValueError):
                gate.verify_art(output)
        gate.verify_art('OK (2 tests)\nINSTRUMENTATION_CODE: -1')
        gate.verify_art('OK (3 tests)\nINSTRUMENTATION_CODE: -1', expected_tests=3)
        with self.assertRaises(ValueError):
            gate.verify_art('OK (2 tests)', expected_tests=3)

    def test_missing_or_changed_publication_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(FileNotFoundError):
                gate.verify_repository(root, '1')
            (root / 'exact-artifacts.json').write_text('[]')
            with self.assertRaises(ValueError):
                gate.verify_repository(root, '1')

    def test_empty_zip_and_insufficient_logger_paths_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / 'capture.zip'
            counts = root / 'paths.json'
            counts.write_text('{}')
            with zipfile.ZipFile(archive, 'w') as output:
                output.writestr('empty.jhlog', b'')
            with self.assertRaises(ValueError):
                gate.verify_capture(archive, counts)
            with zipfile.ZipFile(archive, 'w') as output:
                output.writestr('sample.jhlog', b'JHLOG')
            with self.assertRaises(ValueError):
                gate.verify_capture(archive, counts)

    def test_missing_effective_full_manifest_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FileNotFoundError):
                gate.verify_full(Path(directory))


if __name__ == '__main__':
    unittest.main()
