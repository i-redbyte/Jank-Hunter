#!/usr/bin/env python3
"""Produce real javac classfiles for supported Android consumer Java targets."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--javac', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    source = Path(__file__).parent / 'fixtures/java-bytecode/JavaTarget.java'
    records = []
    for target in (8, 11, 17, 21):
        output = args.output / f'java-{target}'
        output.mkdir(parents=True, exist_ok=True)
        result = subprocess.run([str(args.javac), '--release', str(target), '-d', str(output), str(source)],
                                capture_output=True, text=True, timeout=60)
        (output / 'compiler.log').write_text(result.stdout + result.stderr)
        if result.returncode:
            raise SystemExit(f'javac target {target} failed: {result.stderr}')
        data = (output / 'fixture/javaabi/JavaTarget.class').read_bytes()
        if int.from_bytes(data[6:8], 'big') != target + 44:
            raise SystemExit(f'unexpected Java target {target}')
        records.append({'target': target, 'sha256': hashlib.sha256(data).hexdigest()})
    (args.output / 'java-compilers.json').write_text(json.dumps(records, indent=2) + '\n')
    print(json.dumps(records))


if __name__ == '__main__':
    main()
