import importlib.util
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location('metadata_packaging', Path(__file__).with_name('validate-metadata-plugin-packaging.py'))
packaging = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packaging)


class MetadataPackagingTest(unittest.TestCase):
    def test_runtime_variant_cannot_leak_the_private_reader(self):
        with self.assertRaisesRegex(ValueError, 'Private reader'):
            packaging.validate_dependencies({'variants': [{'name': 'runtimeElements', 'dependencies': [
                {'group': 'org.jetbrains.kotlin', 'module': 'kotlin-metadata-jvm', 'version': {'requires': '2.3.20'}},
            ]}]})

    def test_normal_host_stdlib_is_allowed_but_reader_stdlib_is_not(self):
        def module(version):
            return {'variants': [{'name': 'apiElements', 'dependencies': [
                {'group': 'org.jetbrains.kotlin', 'module': 'kotlin-stdlib', 'version': {'requires': version}},
            ]}]}
        packaging.validate_dependencies(module('1.9.25'))
        with self.assertRaises(ValueError):
            packaging.validate_dependencies(module('2.3.20'))


if __name__ == '__main__':
    unittest.main()
