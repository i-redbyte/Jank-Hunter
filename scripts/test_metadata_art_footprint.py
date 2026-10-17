import importlib.util
from pathlib import Path
import unittest


def module():
    path = Path(__file__).with_name('run-metadata-art-footprint.py')
    spec = importlib.util.spec_from_file_location('metadata_art_footprint', path)
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


class MetadataArtFootprintTest(unittest.TestCase):
    def test_parse_pss_kb(self):
        text = 'TOTAL PSS:   12345\n'
        self.assertEqual(12345, module().parse_pss_kb(text))


if __name__ == '__main__':
    unittest.main()
