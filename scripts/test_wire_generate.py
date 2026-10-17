#!/usr/bin/env python3

from contextlib import redirect_stderr
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest import mock


PROJECT_ROOT = Path(__file__).resolve().parents[1]
GENERATOR_PATH = PROJECT_ROOT / "wire" / "generate.py"


def load_generator_module():
    spec = importlib.util.spec_from_file_location("wire_generate", GENERATOR_PATH)
    if spec is None or spec.loader is None:
        raise AssertionError(f"Cannot load {GENERATOR_PATH}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class WireGenerateTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.generator = load_generator_module()

    def test_check_accepts_gofmt_alignment_without_running_gofmt(self) -> None:
        source = "const (\n\tshort = 1\n\tlongerName = 2\n)"
        formatted = "const (\n\tshort      = 1\n\tlongerName = 2\n)\n"

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "wire_generated.go"
            path.write_text(formatted)

            with mock.patch.object(self.generator, "ROOT", Path(directory)):
                with mock.patch.object(self.generator.subprocess, "run") as run:
                    self.assertTrue(self.generator.update_go(path, source, check=True))
                    run.assert_not_called()

    def test_check_rejects_changed_generated_value_without_gofmt(self) -> None:
        source = "const FormatMinor = 3"

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "wire_generated.go"
            path.write_text("const FormatMinor = 2\n")

            diagnostic = io.StringIO()
            with mock.patch.object(self.generator, "ROOT", Path(directory)):
                with redirect_stderr(diagnostic):
                    self.assertFalse(self.generator.update_go(path, source, check=True))
            self.assertIn("stale generated file: wire_generated.go", diagnostic.getvalue())


if __name__ == "__main__":
    unittest.main()
