#!/usr/bin/env python3
"""Alternating repeated FULL/callback main-thread measurements on the tested APK.

A smoke run emits evidence but is never release eligible. The release gate requires
600 seconds per mode, three repetitions and a physical device. Emulator CPU and
latency are useful regression observations and never substitute for that device.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys

spec = importlib.util.spec_from_file_location('compat', Path(__file__).with_name('run-stability-compatibility.py'))
compat = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compat)
mapping_tools = compat.load('mapping_tools', 'run-concurrency-device-validation.py')
MODES = ('off', 'normal', 'detailed', 'full', 'full_no_pipeline', 'full_callback')


def weak_hardware(physical, hardware):
    memory = hardware.get('memory_kib')
    frequency = hardware.get('cpu_max_khz')
    return physical is True and type(memory) is int and 256 * 1024 <= memory <= 4 * 1024 * 1024 and type(frequency) is int and 100_000 <= frequency <= 2_000_000


def validate_measurements(rows, seconds, repetitions, physical):
    errors = []
    if seconds < 600 or repetitions < 3 or not physical:
        errors.append('mandatory 600s x 3 physical-device observation is incomplete')
    if len(rows) != len(MODES) * repetitions:
        errors.append('missing benchmark rows')
    for repetition in range(repetitions):
        group = [r for r in rows if r.get('repetition') == repetition]
        if {r.get('mode') for r in group} != set(MODES):
            errors.append(f'missing control/treatment in repetition {repetition}')
            continue
        off = next(r for r in group if r['mode'] == 'off')
        for row in group:
            if row.get('seconds', 0) < seconds or row.get('samples', 0) < seconds * 100 or row.get('wall_ns', 0) < seconds * 900_000_000:
                errors.append('insufficient duration/samples: ' + row['mode'])
            if row.get('frames', {}).get('count', 0) == 0 or row.get('input_samples', 0) == 0:
                errors.append('frame/input observation missing: ' + row['mode'])
            quality = row.get('collection_quality', {})
            if row['mode'] != 'off':
                if not row.get('collection_quality_verified') or quality.get('post_admission_lost_events', 1) or quality.get('accepted_events') != quality.get('written_events'):
                    errors.append('invalid delivery/quality: ' + row['mode'])
                if quality.get('runtime_graph_stack_mismatches', 1) or quality.get('other_evidence_loss', 1) or quality.get('bounded_evidence_loss', 1) or quality.get('critical_runtime_hook_failures', 0):
                    errors.append('hook/attribution integrity loss: ' + row['mode'])
                if row['mode'].startswith('full') and (not quality.get('runtime_graph_enabled') or quality.get('runtime_graph_input_events', 0) < 1000):
                    errors.append('effective FULL observation missing: ' + row['mode'])
                if row['mode'].startswith('full') and not (quality.get('runtime_graph_input_events') == quality.get('runtime_graph_emitted_events') == quality.get('decoded_runtime_graph_calls')):
                    errors.append('graph delivery/decoded conservation mismatch: ' + row['mode'])
                graph_loss = quality.get('runtime_graph_producer_capacity_loss', 0)
                graph_attempts = quality.get('runtime_graph_input_events', 0) + graph_loss
                if graph_attempts > 0 and graph_loss / graph_attempts > 0.01:
                    errors.append('typical graph admission loss budget >1%: ' + row['mode'])
                attempts = quality.get('accepted_events', 0) + quality.get('pre_admission_lost_events', 0)
                if attempts == 0 or quality.get('pre_admission_lost_events', 0) / attempts > 0.01:
                    errors.append('typical pre-admission loss budget >1%: ' + row['mode'])
            if row.get('direct_hook_worst_ns', 100_000_001) > 100_000_000:
                errors.append('main hook deadline >100ms: ' + row['mode'])
            histogram = row.get('direct_hook_histogram', [])
            if len(histogram) != 2002 or any(type(v) is not int or v < 0 for v in histogram) or sum(histogram) != row.get('samples'):
                errors.append('full-window direct hook observation missing: ' + row['mode'])
            else:
                rank = (row['samples'] * 95 + 99) // 100
                cumulative = 0
                measured_p95 = None
                for index, count in enumerate(histogram):
                    cumulative += count
                    if cumulative >= rank:
                        measured_p95 = 2**63 - 1 if index == len(histogram) - 1 else (index + 1) * 500
                        break
                if measured_p95 != row.get('direct_hook_p95_ns'):
                    errors.append('reported p95 does not match full-window histogram: ' + row['mode'])
            if row.get('direct_hook_p95_ns', 100_001) > 100_000:
                errors.append('direct paired hook p95 >100us: ' + row['mode'])
            if row['mode'] in ('normal', 'detailed', 'full') and row['cpu_one_core_percent'] - off['cpu_one_core_percent'] > 5.0:
                errors.append('CPU increment >5pp: ' + row['mode'])
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--compatibility', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--adb', required=True, type=Path)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--cli', required=True, type=Path)
    parser.add_argument('--seconds', type=int, choices=range(10, 3601), default=600, metavar='10..3600')
    parser.add_argument('--repetitions', type=int, choices=range(1, 11), default=3, metavar='1..10')
    parser.add_argument('--smoke', action='store_true')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    result = {'pass': False, 'release_eligible': False, 'measurements': []}
    try:
        evidence = json.loads((args.compatibility / 'result.json').read_text())
        if evidence.get('pass') is not True:
            raise ValueError('compatibility/ART prerequisite is red')
        build = args.compatibility / 'consumer/app/build'
        apks = [build / 'outputs/apk/release/app-release.apk', build / 'outputs/apk/androidTest/release/app-release-androidTest.apk']
        hashes = [compat.digest(p) for p in apks]
        if hashes != evidence['apk_sha256']:
            raise ValueError('stress APK differs from exact compatibility APK')
        result['apk_sha256'] = hashes
        result['source_revision'] = evidence.get('source_revision')
        adb = [args.adb, '-s', args.serial]
        qemu = compat.run(adb + ['shell', 'getprop', 'ro.kernel.qemu'], args.output / 'qemu.log', 30).strip()
        result['physical'] = qemu != '1' and not args.serial.startswith('emulator-')
        memory = compat.run(adb + ['shell', 'cat', '/proc/meminfo'], args.output / 'memory.txt', 30)
        total = re.search(r'^MemTotal:\s+(\d+) kB', memory, re.M)
        frequencies = compat.run(adb + ['shell', 'cat /sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_max_freq 2>/dev/null; true'], args.output / 'frequencies.txt', 30)
        values = [int(v) for v in frequencies.split() if v.isdigit()]
        result['hardware'] = {'memory_kib': int(total.group(1)) if total else None, 'cpu_max_khz': max(values) if values else None}
        result['weak_physical'] = weak_hardware(result['physical'], result['hardware'])
        if not args.smoke and not result['weak_physical']:
            raise ValueError('weak device criterion not observed: physical, RAM<=4GiB, CPU max<=2GHz')
        if not args.smoke and (not result['physical'] or args.seconds < 600 or args.repetitions < 3):
            raise ValueError('release stress requires a physical device and full duration/repetitions')
        for index, apk in enumerate(apks):
            compat.run(adb + ['install', '-r', apk], args.output / f'install-{index}.log', 120)
        # Real saturated registry HPROF, mapping-bound to this exact shrinking APK.
        remote = '/sdcard/Android/data/com.fixture.compat/files/'
        compat.run(adb + ['shell', 'content', 'delete', '--uri', 'content://com.fixture.compat.evidence/causal-registry.hprof'], args.output / 'clear-heap.log', 30)
        heap_art = compat.run(adb + ['shell', 'am', 'instrument', '-w', '-r', '-e', 'class', 'com.fixture.compat.RegistryFootprintTest', 'com.fixture.compat.test/androidx.test.runner.AndroidJUnitRunner'], args.output / 'heap-art.log', 180)
        if not re.search(r'OK \(1 test\)', heap_art):
            raise ValueError('registry ART test did not execute')
        dump = args.output / 'causal-registry.hprof'
        compat.pull_evidence(adb, 'causal-registry.hprof', dump, args.output / 'pull-heap.log')
        mapping = (build / 'outputs/mapping/release/mapping.txt').read_text()
        registry = mapping_tools.resolve_mapped_class(mapping, 'io.jankhunter.runtime.RuntimeCausalRegistry')
        entry = mapping_tools.resolve_mapped_class(mapping, 'io.jankhunter.runtime.RuntimeCausalRegistry$Entry')
        heap_environment = os.environ.copy()
        heap_environment.update(JH_CAUSAL_REGISTRY_HEAP=str(dump), JH_CAUSAL_REGISTRY_CLASS=registry['residual_class'],
                                JH_CAUSAL_REGISTRY_ENTRY_CLASS=entry['residual_class'], JH_CAUSAL_REGISTRY_RESULT=str(args.output / 'footprint.json'))
        with (args.output / 'heap-analysis.log').open('w') as heap_log:
            check = subprocess.run(['go', 'test', './internal/analyze', '-run', '^TestCausalRegistryRetainedBudgetOnART$', '-count=1', '-v'],
                                   cwd=compat.ROOT / 'cli', env=heap_environment, stdout=heap_log, stderr=subprocess.STDOUT, timeout=180)
        if check.returncode or not (args.output / 'footprint.json').is_file():
            raise ValueError('exact ART retained registry proof failed')
        footprint = json.loads((args.output / 'footprint.json').read_text())
        footprint.update(source_revision=result['source_revision'], apk_sha256=hashes[0], mapping_sha256=compat.digest(build / 'outputs/mapping/release/mapping.txt'),
                         heap_sha256=compat.digest(dump), registry_mapping=registry, entry_mapping=entry)
        (args.output / 'footprint.json').write_text(json.dumps(footprint, indent=2) + '\n')
        for repetition in range(args.repetitions):
            order = MODES if repetition % 2 == 0 else tuple(reversed(MODES))
            for mode in order:
                directory = args.output / f'{repetition:02d}-{mode}'
                directory.mkdir()
                remote = '/sdcard/Android/data/com.fixture.compat/files/'
                for name in ('stability-benchmark.json', 'stability-benchmark.zip'):
                    compat.run(adb + ['shell', 'content', 'delete', '--uri', 'content://com.fixture.compat.evidence/' + name], directory / ('clear-' + name + '.log'), 30)
                output = compat.run(adb + ['shell', 'am', 'instrument', '-w', '-r', '-e', 'class', 'com.fixture.compat.StabilityBenchmarkTest', '-e', 'mode', mode, '-e', 'seconds', str(args.seconds), 'com.fixture.compat.test/androidx.test.runner.AndroidJUnitRunner'], directory / 'art.log', args.seconds + 60)
                if not re.search(r'OK \(1 test\)', output) or any(s in output for s in ('FAILURES!!!', 'Process crashed', 'INSTRUMENTATION_STATUS_CODE: -3')):
                    raise ValueError('benchmark skipped or failed: ' + mode)
                compat.pull_evidence(adb, 'stability-benchmark.json', directory / 'measurement.json', directory / 'pull.log')
                row = json.loads((directory / 'measurement.json').read_text())
                if row['mode'] != mode or row['seconds'] != args.seconds:
                    raise ValueError('wrong benchmark profile/duration')
                row['repetition'] = repetition
                result['measurements'].append(row)
                if mode != 'off':
                    capture = directory / 'capture.zip'
                    compat.pull_evidence(adb, 'stability-benchmark.zip', capture, directory / 'pull-zip.log')
                    compat.run([args.cli, 'inspect', capture, '--json', '--artifacts-dir', build / 'generated/jankhunter/release', '--mapping', build / 'outputs/mapping/release/mapping.txt'], directory / 'inspect.json', 120)
                    compat.run([args.cli, 'export', capture, '--out', directory / 'events.jsonl'], directory / 'export.log', 120)
                    summary = json.loads((directory / 'inspect.json').read_text())
                    if summary['MappingIdentity']['Status'] != 'verified':
                        raise ValueError('benchmark ZIP is not bound to tested APK/mapping')
                    quality = summary['CollectionQuality']
                    row['collection_quality'] = quality
                    row['collection_quality_verified'] = quality['chain_valid'] and quality['counter_invariants_valid'] and quality['quality_progression_valid']
                    row['rotation'] = summary.get('LogGrowth')
                    row['loss_distribution'] = [c for segment in summary.get('CollectionSegments', []) for c in segment.get('QualityCounters', [])]
                (args.output / 'progress.json').write_text(json.dumps(result, indent=2) + '\n')
                print(f'benchmark repetition {repetition} {mode}: observed {row["samples"]} direct pairs', flush=True)
        result['errors'] = validate_measurements(result['measurements'], args.seconds, args.repetitions, result['weak_physical'])
        result['release_eligible'] = not result['errors'] and not args.smoke
        local_errors = [e for e in result['errors'] if e != 'mandatory 600s x 3 physical-device observation is incomplete']
        result['pass'] = result['release_eligible'] or (args.smoke and not local_errors)
    except (OSError, ValueError, KeyError, subprocess.TimeoutExpired) as error:
        result['error'] = str(error)
    (args.output / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    return 0 if result['pass'] else 1


if __name__ == '__main__':
    sys.exit(main())
