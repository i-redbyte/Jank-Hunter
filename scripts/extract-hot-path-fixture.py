#!/usr/bin/env python3
"""Extract the FULL-instrumented generic callback fixture from scoped output."""
import argparse
import hashlib
from pathlib import Path
import zipfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--classes', type=Path, required=True)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
count = 0
callback_hooks = False
with zipfile.ZipFile(args.classes) as source, zipfile.ZipFile(args.output, 'w', zipfile.ZIP_DEFLATED) as target:
    for name in source.namelist():
        if name.endswith('.class') and (name.startswith('com/fixture/callback/CallbackPipeline') or
                                       name.startswith('com/fixture/compat/HookFixture')):
            data = source.read(name)
            target.writestr(name, data)
            count += 1
            if name == 'com/fixture/callback/CallbackPipeline.class':
                callback_hooks = b'io/jankhunter/runtime/JankHunterHooks' in data and b'enterMethod' in data and b'exitMethod' in data
if not count or not callback_hooks:
    raise SystemExit('FULL callback hook coverage is missing')
print(f'{count} callback/bridge classes; SHA256 {hashlib.sha256(args.output.read_bytes()).hexdigest()}')
