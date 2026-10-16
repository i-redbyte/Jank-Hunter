#!/usr/bin/env python3
"""Fail closed before publication: same revision, exact files and every mandatory gate."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import sys


spec = importlib.util.spec_from_file_location('stress_gate', Path(__file__).with_name('run-stability-stress.py'))
stress_gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stress_gate)

environment_spec = importlib.util.spec_from_file_location('release_environment', Path(__file__).with_name('verify-release-environment.py'))
release_environment = importlib.util.module_from_spec(environment_spec)
environment_spec.loader.exec_module(release_environment)


def verify(repository, version, revision, compatibility, stress, footprint, baseline=None, environment=None):
    errors = (['mandatory release environment proof missing'] if environment is None else
              release_environment.evidence_errors(json.loads(environment.read_text()), revision))
    artifacts = json.loads((repository / 'exact-artifacts.json').read_text())
    required = {'jankhunter-runtime', 'jankhunter-annotations', 'jankhunter-okhttp3', 'jankhunter-workmanager', 'jankhunter-jankstats', 'jankhunter-android-sdk', 'jankhunter-gradle-plugin'}
    if not artifacts or not required <= {r.get('module') for r in artifacts}:
        errors.append('mandatory exact publication modules missing')
    for item in artifacts:
        path = repository / 'io/jankhunter' / item['module'] / version / item['file']
        if hashlib.sha256(path.read_bytes()).hexdigest() != item['sha256']:
            errors.append('publication no longer matches tested bytes: ' + str(path))
    expected = {('8.0.2', '1.9.25', 37, 8), ('8.5.2', '2.0.21', 37, 11),
                ('8.10.1', '2.2.21', 28, 17), ('8.10.1', '2.1.21', 37, 17), ('8.10.1', '2.2.21', 30, 17),
                ('8.10.1', '2.2.21', 37, 17), ('8.13.2', '2.3.20', 37, 21),
                ('8.0.2', 'java', 37, 8), ('9.0.1', 'java', 37, 21)}
    observed = set()
    apk_hashes = set()
    for path in compatibility:
        report = json.loads(path.read_text())
        if report.get('pass') is not True or report.get('source_revision') != revision or report.get('source_dirty') is not False:
            errors.append('missing/dirty/wrong-revision compatibility evidence: ' + str(path))
        if report.get('artifacts') != artifacts:
            errors.append('compatibility publications differ: ' + str(path))
        observed.add((report.get('agp'), report.get('kotlin'), report.get('android_api'), report.get('java_target')))
        apk_hashes.add(tuple(report.get('apk_sha256', [])))
    if not expected <= observed:
        errors.append('mandatory consumer/ART API28/API30/API37 matrix incomplete')
    report = json.loads(stress.read_text())
    if report.get('pass') is not True or report.get('release_eligible') is not True or report.get('source_revision') != revision:
        errors.append('mandatory physical stress/performance result missing, red, or wrong revision')
    errors.extend(stress_gate.validate_measurements(report.get('measurements', []), 600, 3, report.get('weak_physical') is True and stress_gate.weak_hardware(report.get('physical'), report.get('hardware', {}))))
    if tuple(report.get('apk_sha256', [])) not in apk_hashes:
        errors.append('stress APK was not tested by compatibility gate')
    for row in report.get('measurements', []):
        if row.get('frames', {}).get('count', 0) <= 0 or row.get('input_samples', 0) <= 0:
            errors.append('FrameMetrics/input observation absent')
        if row.get('mode') != 'off' and not row.get('collection_quality_verified', False):
            errors.append('loss/queue/rotation/collector observation absent')
    heap = json.loads(footprint.read_text())
    if heap.get('source_revision') != revision or heap.get('apk_sha256') not in {v[0] for v in apk_hashes if v}:
        errors.append('registry footprint provenance differs')
    if heap.get('retained_size_state') != 'exact' or heap.get('entry_count') != 4096 or not 0 < heap.get('retained_bytes', 0) < 16 * 1024 * 1024:
        errors.append('exact ART registry retained heap criterion not proven')
    if baseline is None:
        errors.append('new-consumer/legacy SDK matrix missing')
    else:
        legacy = json.loads(baseline.read_text())
        if legacy.get('pass') is not True or legacy.get('baseline_sdk') is not True or (legacy.get('agp'), legacy.get('kotlin'), legacy.get('version'), legacy.get('android_api')) != ('8.13.2', '2.3.20', '1.0.12', 37) or legacy.get('source_revision') != revision or legacy.get('source_dirty') is not False:
            errors.append('new-consumer/legacy SDK evidence is red, missing, or from another revision')
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--environment', required=True, type=Path)
    parser.add_argument('--repository', required=True, type=Path)
    parser.add_argument('--version', required=True)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--compatibility', action='append', required=True, type=Path)
    parser.add_argument('--stress', required=True, type=Path)
    parser.add_argument('--footprint', required=True, type=Path)
    parser.add_argument('--baseline', required=True, type=Path)
    args = parser.parse_args()
    try:
        errors = verify(args.repository, args.version, args.revision, args.compatibility, args.stress, args.footprint, args.baseline, args.environment)
    except (OSError, ValueError, KeyError, IndexError) as error:
        errors = ['missing or invalid mandatory evidence: ' + str(error)]
    print(json.dumps({'pass': not errors, 'revision': args.revision, 'errors': errors}, indent=2))
    return 1 if errors else 0


if __name__ == '__main__':
    sys.exit(main())
