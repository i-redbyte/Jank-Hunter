import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("capture", Path(__file__).with_name("validate-concurrency-capture.py"))
capture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(capture)


def fixture():
    def event(kind, phase, identity, **fields):
        return {"type": 39, "causal_event": dict(entity_kind=kind, phase=phase, entity_id=str(identity),
                presence=4 | (2048 | 8 if kind == 5 and phase == 1 else 0), source_origin=1, source_method="app.Scenario#run()V", **fields)}
    events = [event(1, 1, 1), event(2, 1, 2), event(3, 1, 3), event(4, 1, 10),
              event(4, 6, 11), event(6, 2, 20), event(6, 7, 21)]
    attempts = []
    for offset, outcome in enumerate(("retry", "success")):
        identity = 30 + offset
        events.append(event(5, 1, identity, worker_instance_id=str(identity), run_attempt=offset,
                            generation=0, app_caller_method="app.Scenario#enqueueRetryWorker()V"))
        events.append(event(5, 6, identity))
        events.append(event(5, 3 if offset == 0 else 5, identity))
        attempts.append(dict(Scope="rps", UUID="worker-uuid", InstanceID=str(identity), RunAttempt=offset,
                             Generation=0, Outcome=outcome, Coverage="start_and_finish_observed",
                             StartMS=offset*100+1, EndMS=offset*100+50))
    summary = {
        "CollectionSegments": [dict(RunID="r", ProcessInstanceID="p", SessionID="s", Status="closed_clean",
                                    Sealed=True, EndReason="normal", QualitySequence=1, QualityCounters=[])],
        "CollectionQuality": dict(accepted_events=100, written_events=100, counter_invariants_valid=True,
                                  quality_progression_valid=True, chain_valid=True),
        "CausalAnalysis": dict(Available=True, Events=len(events), Complete=True, Metrics={
            "CoroutineStarts":1, "CoroutineObservedMachines":1, "ResourcesAcquired":1, "ResourcesObservedUse":1}),
        "WorkerAnalysis": dict(Attempts=attempts),
    }
    return summary, events


class CaptureGateTest(unittest.TestCase):
    def test_complete_and_honest_partial_captures_pass(self):
        summary, events = fixture()
        self.assertEqual(capture.validate_capture(summary, events, "detailed")["causal_events"], len(events))
        summary["CausalAnalysis"].update(TransportLoss=1, Complete=False)
        capture.validate_capture(summary, events, "detailed")

    def test_normal_does_not_require_detailed_resources_or_worker(self):
        summary, events = fixture()
        events = [event for event in events if event["causal_event"]["entity_kind"] not in (5, 6)]
        summary["CausalAnalysis"]["Events"] = len(events)
        summary.pop("WorkerAnalysis")
        capture.validate_capture(summary, events, "normal")

    def test_empty_and_mismatched_captures_fail(self):
        summary, events = fixture()
        for bad in ([], events[:-1]):
            with self.assertRaises(ValueError): capture.validate_capture(summary, bad, "detailed")

    def test_false_complete_and_writer_io_error_fail(self):
        for mutate in (
            lambda s: s["CausalAnalysis"].update(TransportLoss=1),
            lambda s: s["CollectionSegments"][0].update(Sealed=False, Status="open_clean"),
            lambda s: s["CollectionQuality"].update(critical_runtime_hook_failures=1),
            lambda s: s["CollectionQuality"].update(written_events=99),
            lambda s: s["CollectionSegments"][0].update(EndReason="io_error"),
            lambda s: s["CollectionSegments"][0].update(QualityCounters=[{"Name":"writer_io_error_total", "Value":1}]),
        ):
            summary, events = fixture()
            mutate(summary)
            with self.assertRaises(ValueError): capture.validate_capture(summary, events, "detailed")

    def test_wrong_resource_and_coroutine_semantics_fail(self):
        for kind, phase, replacement in ((6, 7, 2), (4, 6, 1)):
            summary, events = fixture()
            for event in events:
                causal = event["causal_event"]
                if causal["entity_kind"] == kind and causal["phase"] == phase: causal["phase"] = replacement
            with self.assertRaises(ValueError): capture.validate_capture(summary, events, "detailed")

    def test_worker_cross_scope_or_missing_exact_binding_fail(self):
        for mutate in (
            lambda s,e: s["WorkerAnalysis"]["Attempts"][1].update(Scope="other"),
            lambda s,e: s["WorkerAnalysis"]["Attempts"][1].update(InstanceID="999"),
            lambda s,e: s["WorkerAnalysis"]["Attempts"][1].update(Outcome="retry"),
            lambda s,e: s["WorkerAnalysis"]["Attempts"][1].update(IdentityAmbiguous=True),
        ):
            summary, events = fixture()
            mutate(summary,events)
            with self.assertRaises(ValueError): capture.validate_capture(summary, events, "detailed")

if __name__ == "__main__":
    unittest.main()
