import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('stress', Path(__file__).with_name('run-stability-stress.py'))
stress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stress)


class StressGateTest(unittest.TestCase):
    def rows(self):
        return [dict(mode=mode, repetition=repeat, seconds=600, samples=600_000,
                     wall_ns=600_000_000_000, direct_hook_p95_ns=20_000,
                     direct_hook_worst_ns=20_000, direct_hook_histogram=[0]*39+[600_000]+[0]*1962, cpu_one_core_percent=3.0, frames=dict(count=1000), input_samples=1000, collection_quality_verified=True,
                     collection_quality=dict(chain_valid=True, counter_invariants_valid=True, quality_progression_valid=True,
                         post_admission_lost_events=0, accepted_events=1000, written_events=1000,
                         runtime_graph_stack_mismatches=0, other_evidence_loss=0, bounded_evidence_loss=0, runtime_graph_enabled=True,
                         runtime_graph_input_events=1000, runtime_graph_emitted_events=1000, decoded_runtime_graph_calls=1000, pre_admission_lost_events=0)) for repeat in range(3) for mode in stress.MODES]

    def test_weak_device_cannot_be_claimed_from_missing_or_stub_properties(self):
        self.assertFalse(stress.weak_hardware(True, {}))
        self.assertFalse(stress.weak_hardware(True, dict(memory_kib=3_000_000, cpu_max_khz=2)))
        self.assertFalse(stress.weak_hardware(False, dict(memory_kib=3_000_000, cpu_max_khz=1_800_000)))
        self.assertTrue(stress.weak_hardware(True, dict(memory_kib=3_000_000, cpu_max_khz=1_800_000)))

    def test_emulator_smoke_never_satisfies_release_gate(self):
        self.assertTrue(stress.validate_measurements(self.rows(), 600, 3, False))
        self.assertTrue(stress.validate_measurements(self.rows(), 10, 1, True))

    def test_missing_rows_and_latency_or_cpu_regression_are_red(self):
        rows = self.rows()
        self.assertFalse(stress.validate_measurements(rows, 600, 3, True))
        self.assertTrue(stress.validate_measurements(rows[:-1], 600, 3, True))
        rows[3]['direct_hook_p95_ns'] = 100_001
        self.assertTrue(stress.validate_measurements(rows, 600, 3, True))
        rows[3]['direct_hook_p95_ns'] = 20_000
        rows[3]['cpu_one_core_percent'] = 8.1
        self.assertTrue(stress.validate_measurements(rows, 600, 3, True))


if __name__ == '__main__':
    unittest.main()
