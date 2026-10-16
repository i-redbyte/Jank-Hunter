import importlib.util
from pathlib import Path
import unittest
import tempfile
import argparse
import json
import sys
from unittest.mock import patch


def harness():
    path = Path(__file__).with_name('metadata-build-benchmark.py')
    spec = importlib.util.spec_from_file_location('metadata_build_benchmark', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class MetadataBuildBenchmarkTest(unittest.TestCase):
    def test_profile_reports_actual_jh_and_asm_tasks_without_counting_other_work(self):
        times = harness().profile_task_times('<table>'
            '<tr><td>:app:transformDebugClassesWithAsm</td><td>1.250s</td></tr>'
            '<tr><td>:app:mergeDebugJankHunterInstrumentationArtifacts</td><td>250ms</td></tr>'
            '<tr><td>:app:compileKotlin</td><td>1m2.5s</td></tr></table>')
        self.assertEqual(1.5, sum(times.values()))
        self.assertEqual({}, harness().profile_task_times('missing profile'))
    def test_cli_measures_list_manifest_and_returns_inconclusive_for_two_pairs(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifact = root / 'plugin.jar'
            artifact.write_bytes(b'fixed artifact')
            config = {'cwd': directory, 'argv': [sys.executable, '-c', 'print("fixture")']}
            manifest = root / 'manifest.json'
            manifest.write_text(json.dumps({'artifacts': [{'path': str(artifact), 'sha256': module.file_digest(artifact)}],
                'scenarios': [{'name': 'clean', 'A': config, 'B': config}]}))
            self.assertEqual(1, module.measure(argparse.Namespace(manifest=manifest, output=root / 'result', pairs=2, timeout=10)))
            self.assertEqual('INCONCLUSIVE', json.loads((root / 'result/results.json').read_text())['scenarios']['clean']['status'])

    def test_manifest_shape_is_validated_before_executing_commands(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifact = root / 'plugin.jar'
            artifact.write_bytes(b'fixed artifact')
            manifest = root / 'manifest.json'
            manifest.write_text(json.dumps({'artifacts': [{'path': str(artifact), 'sha256': module.file_digest(artifact)}],
                'scenarios': {'clean': {}}}))
            with self.assertRaisesRegex(ValueError, 'scenarios must be a nonempty list'):
                module.measure(argparse.Namespace(manifest=manifest, output=root / 'result', pairs=2, timeout=10))

    def test_plan_rejects_same_plugin_for_both_sides(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / 'repo'
            plugin = repo / 'io/jankhunter/jankhunter-gradle-plugin/1.0.13/jankhunter-gradle-plugin-1.0.13.jar'
            plugin.parent.mkdir(parents=True)
            plugin.write_bytes(b'same plugin')
            with self.assertRaisesRegex(ValueError, 'different plugin'):
                module.plan(argparse.Namespace(baseline_repository=repo, candidate_repository=repo,
                    baseline_version='1.0.13', candidate_version='1.0.13', output=root / 'output'))

    def test_body_edits_change_each_trial_and_preserve_other_classes(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / 'Corpus0.java'
            source.write_text('package fixture;\nclass C0 { int value(int x) { return x + 0; } }\nclass C1 {}\n')
            before = source.read_text()
            module.edit_source(source, 'java-edit')
            edited = source.read_text()
            self.assertNotEqual(before, edited)
            self.assertIn('class C1 {}', edited)
            module.edit_source(source, 'java-edit')
            self.assertNotEqual(before, source.read_text())
            self.assertNotEqual(edited, source.read_text())
    def test_dependency_edits_change_the_actual_jar_on_each_trial(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            module.write_dependency(root, 1)
            before = (root / 'dependency.jar').read_bytes()
            args = argparse.Namespace(corpus=root, gradle='gradle', scenario='dependency-edit')
            real_run = module.subprocess.run
            def run(command, **kwargs):
                return None if command[0] == 'gradle' else real_run(command, **kwargs)
            with patch.object(module.subprocess, 'run', side_effect=run):
                module.prepare(args)
                edited = (root / 'dependency.jar').read_bytes()
                self.assertNotEqual(before, edited)
                module.prepare(args)
                self.assertNotEqual(before, (root / 'dependency.jar').read_bytes())
                self.assertNotEqual(edited, (root / 'dependency.jar').read_bytes())
                self.assertEqual('3', (root / '.dependency-version').read_text())

    def test_signature_edits_never_return_to_an_old_signature(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'Corpus.java'
            source.write_text('class C0 { int value(int x) { return x + 0; } } class C1 {}')
            module.edit_source(source, 'signature-edit')
            self.assertIn('value1(int x)', source.read_text())
            module.edit_source(source, 'signature-edit')
            self.assertIn('value2(int x)', source.read_text())
            self.assertIn('class C1 {}', source.read_text())

    def test_pairing_rejects_missing_or_failed_measurements(self):
        module = harness()
        with self.assertRaises(ValueError):
            module.summarize([1.0], [], 'clean')
        with self.assertRaises(ValueError):
            module.summarize([0.0], [1.0], 'clean')

    def test_real_regression_is_not_hidden_by_noise(self):
        module = harness()
        result = module.summarize([10.0] * 10, [11.0] * 10, 'clean')
        self.assertEqual('FAIL', result['status'])

    def test_equivalent_builds_pass_and_missing_trials_are_inconclusive(self):
        module = harness()
        self.assertEqual('PASS', module.summarize([10.0] * 10, [10.0] * 10, 'clean')['status'])
        self.assertEqual('INCONCLUSIVE', module.summarize([10.0], [10.0], 'clean')['status'])

    def test_noop_budget_has_an_absolute_floor(self):
        module = harness()
        self.assertEqual('PASS', module.summarize([1.0] * 10, [1.05] * 10, 'no-op')['status'])
        self.assertEqual('FAIL', module.summarize([1.0] * 10, [1.2] * 10, 'no-op')['status'])

    def test_jh_task_budget_is_five_percent_without_wall_time_floor(self):
        module = harness()
        self.assertEqual('PASS', module.summarize([1.0] * 10, [1.04] * 10, 'jh-task')['status'])
        self.assertEqual('FAIL', module.summarize([1.0] * 10, [1.06] * 10, 'jh-task')['status'])

    def test_noop_parser_work_and_extra_compile_invalidation_cannot_pass(self):
        module = harness()
        baseline = {'tasks': {':app:compileDebugKotlin': 'UP-TO-DATE'}, 'metadata_metrics': None}
        candidate = {'tasks': {':app:compileDebugKotlin': 'EXECUTED'}, 'metadata_metrics': [{'decode_count': 1}]}
        result = module.audit_work([baseline], [candidate], 'no-op')
        self.assertEqual('FAIL', result['status'])
        self.assertEqual(2, len(result['errors']))
        self.assertEqual('PASS', module.audit_work([baseline], [baseline], 'cache-restore')['status'])

    def test_task_outcomes_distinguish_cache_hits_from_execution(self):
        module = harness()
        self.assertEqual({':app:transform': 'FROM-CACHE', ':app:compile': 'UP-TO-DATE', ':app:package': 'EXECUTED'},
                         module.task_outcomes('> Task :app:transform FROM-CACHE\n> Task :app:compile UP-TO-DATE\n> Task :app:package\n'))

    def test_measure_rejects_mismatched_pair_lengths(self):
        module = harness()
        with self.assertRaises(ValueError):
            module.summarize([1.0, 2.0], [1.0], 'clean')

    def test_artifacts_cannot_change_during_a_paired_series(self):
        module = harness()
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory) / 'plugin.jar'
            artifact.write_bytes(b'baseline')
            identity = [{'path': str(artifact), 'sha256': module.file_digest(artifact)}]
            module.verify_artifacts(identity)
            artifact.write_bytes(b'candidate')
            with self.assertRaises(ValueError):
                module.verify_artifacts(identity)
        with self.assertRaises(ValueError):
            module.verify_artifacts([])


if __name__ == '__main__':
    unittest.main()
