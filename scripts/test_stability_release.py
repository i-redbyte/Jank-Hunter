import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('release', Path(__file__).with_name('verify-stability-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseGateTest(unittest.TestCase):
    def test_missing_evidence_is_a_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(FileNotFoundError):
                release.verify(root, '1', 'sha', [], root / 'stress.json', root / 'heap.json')

    def test_wrong_sha_missing_matrix_smoke_and_unproven_heap_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'exact-artifacts.json').write_text('[]')
            compat = root / 'compat.json'
            compat.write_text(json.dumps(dict(pass_=True, source_revision='old', source_dirty=True, artifacts=[], apk_sha256=['apk','test'])))
            stress = root / 'stress.json'
            stress.write_text(json.dumps(dict(pass_=True, release_eligible=False, source_revision='old', apk_sha256=['other','test'], measurements=[])))
            heap = root / 'heap.json'
            heap.write_text(json.dumps(dict(source_revision='old', apk_sha256='other', entry_count=0, retained_bytes=0)))
            errors = release.verify(root, '1', 'new', [compat], stress, heap)
            self.assertGreaterEqual(len(errors), 5)
            self.assertIn('mandatory release environment proof missing', errors)
            environment = root / 'environment.json'
            environment.write_text('{"schema":1,"pass":true}')
            errors = release.verify(root, '1', 'new', [compat], stress, heap, environment=environment)
            self.assertTrue(any('invalid mandatory environment evidence' in error for error in errors))

    def test_api28_is_required_in_addition_to_newer_android_consumers(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'exact-artifacts.json').write_text('[]')
            stress, heap = root / 'stress.json', root / 'heap.json'
            stress.write_text('{}'); heap.write_text('{}')
            lanes = [('8.0.2', '1.9.25', 37, 8), ('8.5.2', '2.0.21', 37, 11),
                ('8.10.1', '2.1.21', 37, 17), ('8.10.1', '2.2.21', 30, 17),
                ('8.10.1', '2.2.21', 37, 17), ('8.13.2', '2.3.20', 37, 21),
                ('8.0.2', 'java', 37, 8), ('9.0.1', 'java', 37, 21),
                ('8.10.1', '2.2.21', 28, 17)]
            reports = []
            for index, (agp, kotlin, api, target) in enumerate(lanes):
                report = root / f'consumer-{index}.json'
                report.write_text(json.dumps({'pass': True, 'source_revision': 'sha', 'source_dirty': False,
                    'artifacts': [], 'agp': agp, 'kotlin': kotlin, 'android_api': api, 'java_target': target}))
                reports.append(report)
            without = release.verify(root, '1', 'sha', reports[:-1], stress, heap)
            self.assertTrue(any('matrix incomplete' in error for error in without))
            complete = release.verify(root, '1', 'sha', reports, stress, heap)
            self.assertFalse(any('matrix incomplete' in error for error in complete))


if __name__ == '__main__':
    unittest.main()
