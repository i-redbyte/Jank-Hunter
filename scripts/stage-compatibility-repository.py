#!/usr/bin/env python3
"""Stage generated publications for isolated consumer tests, preserving exact bytes."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import sys

MODULES = ('jankhunter-runtime', 'jankhunter-annotations', 'jankhunter-okhttp3',
           'jankhunter-workmanager', 'jankhunter-android-sdk', 'jankhunter-gradle-plugin', 'jankhunter-jankstats')


def stage(android, repository, version):
    records = []
    for name in MODULES:
        build = android / name / 'build'
        publication = build / 'publications' / ('pluginMaven' if name.endswith('gradle-plugin') else 'release')
        module_path = publication / 'module.json'
        module = json.loads(module_path.read_text())
        if module['component']['version'] != version or module['component']['group'] != 'io.jankhunter':
            raise ValueError(f'wrong publication identity: {name}')
        target = repository / 'io/jankhunter' / name / version
        target.mkdir(parents=True, exist_ok=True)
        requested = {f['name']: f for v in module['variants'] for f in v.get('files', [])}
        for filename, expected in requested.items():
            candidates = list(build.rglob(filename))
            if filename.endswith('.aar'):
                candidates += list((build / 'outputs/aar').glob('*-release.aar'))
            elif filename.endswith('-sources.jar'):
                candidates += list(build.rglob('*-sources.jar'))
            matches = [p for p in set(candidates) if p.is_file()
                       and hashlib.sha256(p.read_bytes()).hexdigest() == expected['sha256']]
            if not matches:
                raise ValueError(f'{name}: required exact publication file {filename}: {len(matches)} matches')
            source = sorted(matches)[0]
            shutil.copyfile(source, target / filename)
            records.append({'module': name, 'file': filename, 'sha256': expected['sha256']})
        for source, extension in ((module_path, 'module'), (publication / 'pom-default.xml', 'pom')):
            destination = target / f'{name}-{version}.{extension}'
            shutil.copyfile(source, destination)
            records.append({'module': name, 'file': destination.name,
                            'sha256': hashlib.sha256(destination.read_bytes()).hexdigest()})
    return records


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--android', required=True, type=Path)
    parser.add_argument('--repository', required=True, type=Path)
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    try:
        records = stage(args.android.resolve(), args.repository.resolve(), args.version)
    except (OSError, ValueError, KeyError) as error:
        print(f'compatibility staging failed: {error}', file=sys.stderr)
        return 1
    (args.repository / 'exact-artifacts.json').write_text(json.dumps(records, indent=2) + '\n')
    print(f'staged {len(records)} exact publication files')
    return 0


if __name__ == '__main__':
    sys.exit(main())
