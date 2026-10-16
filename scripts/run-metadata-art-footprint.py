#!/usr/bin/env python3
"""Capture ART PSS after installing a metadata consumer APK; requires adb and a running device."""
import argparse
import json
import re
import subprocess
import sys


def run(command, timeout=120):
    completed = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
    if completed.returncode:
        raise ValueError(f'command failed ({completed.returncode}): {" ".join(command)}\n{completed.stderr}')
    return completed.stdout


def parse_pss_kb(text):
    match = re.search(r'TOTAL(?:\s+PSS)?[:\s]+(\d+)', text)
    if not match:
        raise ValueError('TOTAL PSS line missing from dumpsys meminfo output')
    return int(match.group(1))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--adb', required=True)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--package', default='com.fixture.compat')
    parser.add_argument('--output', type=argparse.FileType('w'), default='-')
    args = parser.parse_args()
    adb = [args.adb, '-s', args.serial]
    run(adb + ['shell', 'am', 'force-stop', args.package])
    run(adb + ['shell', 'monkey', '-p', args.package, '-c', 'android.intent.category.LAUNCHER', '1'])
    meminfo = run(adb + ['shell', 'dumpsys', 'meminfo', args.package])
    report = {
        'schema': 1,
        'package': args.package,
        'pss_kb': parse_pss_kb(meminfo),
        'note': 'single-sample footprint; compare against baseline in release evidence',
    }
    json.dump(report, args.output, indent=2)
    args.output.write('\n')
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, subprocess.TimeoutExpired) as error:
        print(error, file=sys.stderr)
        raise SystemExit(2)
