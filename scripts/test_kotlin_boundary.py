import importlib.util
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('boundary', Path(__file__).with_name('validate-kotlin-boundary.py'))
boundary = importlib.util.module_from_spec(spec)
spec.loader.exec_module(boundary)


class BoundaryTests(unittest.TestCase):
    def test_r8_metadata_warning_is_failure_even_when_process_succeeded(self):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / 'r8.log'
            for warning in (
                'Warning: Unsupported metadata version 2.3, maximum supported version is 2.2\nBUILD SUCCESSFUL',
                'An error occurred when parsing/rewriting Kotlin metadata\nBUILD SUCCESSFUL',
                'Should never be called',
                'Warning: Invalid stack map table at instruction index 17: lload 2, error: Unexpected read of missing local at index 2.\nBUILD SUCCESSFUL',
                'Info: Class io.jankhunter.Test has malformed kotlin.Metadata: Provided Metadata instance has version 2.3.0, while maximum supported version is 2.2.0.\nBUILD SUCCESSFUL',
            ):
                log.write_text(warning)
                self.assertTrue(boundary.validate_r8_log(log))
            log.write_text('R8 8.10.24\nCompilation completed')
            self.assertFalse(boundary.validate_r8_log(log))
            log.write_text('')
            self.assertTrue(boundary.validate_r8_log(log))

    def test_empty_or_corrupt_artifact_is_never_success(self):
        with tempfile.TemporaryDirectory() as directory:
            jar = Path(directory) / 'empty.jar'
            with zipfile.ZipFile(jar, 'w') as archive:
                archive.writestr('META-INF/MANIFEST.MF', 'Manifest-Version: 1.0\n')
            self.assertTrue(boundary.validate_artifact(jar)['errors'])
            with self.assertRaises(ValueError):
                boundary.metadata_versions(b'\xca\xfe\xba\xbe')
            with self.assertRaises(ValueError):
                boundary.metadata_versions(b'not a classfile')


if __name__ == '__main__':
    unittest.main()
