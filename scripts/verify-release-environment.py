#!/usr/bin/env python3
"""Fail closed on misprovisioned release toolchains before executing consumer Gradle."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
import zipfile

ROOT = Path(__file__).resolve().parents[1]
ROLES = {'JH_GRADLE_MIN': {'8.0.2'}, 'JH_GRADLE_OLD': {'8.11.1'},
         'JH_GRADLE_NEW': {'8.13'}, 'JH_GRADLE_AGP9': {'9.1.0', '9.4.1'}}
DEVICE_APIS = {'JH_API28_SERIAL': 28, 'JH_API30_SERIAL': 30, 'JH_API37_SERIAL': 37,
               'JH_WEAK_PHYSICAL_SERIAL': None}


def digest(path):
    with Path(path).open('rb') as stream:
        return stream_digest(stream)


def stream_digest(stream):
    result = hashlib.sha256()
    for block in iter(lambda: stream.read(65536), b''): result.update(block)
    return result.hexdigest()


def checked_sha(value):
    if not isinstance(value, str) or not re.fullmatch('[0-9a-f]{64}', value):
        raise ValueError('a pinned lowercase SHA256 is required')
    return value


def load_pinned_manifest(path, sha):
    if Path(path).stat().st_size > 32 * 1024 * 1024: raise ValueError('provisioning manifest too large')
    if digest(path) != checked_sha(sha): raise ValueError('provisioning manifest SHA256 differs')
    value = json.loads(Path(path).read_text())
    if not isinstance(value, dict) or value.get('schema') != 1: raise ValueError('unsupported provisioning manifest schema')
    return value


def safe_relative(name):
    path = PurePosixPath(name)
    if not name or path.is_absolute() or '..' in path.parts or '\\' in name or str(path) != name:
        raise ValueError('invalid manifest/archive relative path: ' + name)
    return path


def verify_distribution(executable, archive, version, sha):
    if digest(archive) != checked_sha(sha): raise ValueError('Gradle archive SHA256 differs: ' + version)
    executable = Path(executable).absolute()
    home = executable.parent.parent
    if executable.name != 'gradle' or executable.parent.name != 'bin' or executable.is_symlink():
        raise ValueError('expected extracted distribution/bin/gradle, not a wrapper or symlink')
    expected = {}
    prefix = 'gradle-' + version + '/'
    with zipfile.ZipFile(archive) as source:
        for entry in source.infolist():
            safe_relative(entry.filename.rstrip('/'))
            if not entry.filename.startswith(prefix): raise ValueError('unexpected Gradle archive root')
            if entry.is_dir(): continue
            if (entry.external_attr >> 16) & 0o170000 == 0o120000: raise ValueError('Gradle archive symlink')
            relative = entry.filename[len(prefix):]
            if relative in expected: raise ValueError('duplicate Gradle archive entry')
            with source.open(entry) as stream: expected[relative] = stream_digest(stream)
    if 'bin/gradle' not in expected or not any(p.startswith('lib/') for p in expected):
        raise ValueError('Gradle distribution is incomplete')
    verify_cache(home, expected)
    return executable.resolve()


def verify_cache(root, records):
    root = Path(root)
    if not root.is_dir() or not isinstance(records, dict) or not records: raise ValueError('complete cache inventory required')
    actual = set()
    for path in root.rglob('*'):
        if path.is_symlink(): raise ValueError('symlink in pinned inputs: ' + str(path))
        if path.is_file(): actual.add(path.relative_to(root).as_posix())
    if actual != set(records): raise ValueError('pinned input inventory differs: ' + str(root))
    for name, expected in records.items():
        path = root.joinpath(*safe_relative(name).parts)
        if digest(path) != checked_sha(expected): raise ValueError('pinned file changed: ' + str(path))
    return len(records)


def validate_gradle_version(output, expected):
    if re.findall(r'^Gradle ([0-9]+(?:\.[0-9]+){1,2})\s*$', output, re.M) != [expected]:
        raise ValueError('actual Gradle version differs from required ' + expected)


def properties(path):
    result = {}
    for line in Path(path).read_text().splitlines():
        if '=' in line and not line.lstrip().startswith('#'):
            key, value = line.split('=', 1); result[key.strip()] = value.strip()
    return result


def verify_sdk_platform(sdk, api, pin):
    platform = Path(sdk) / f'platforms/android-{api}'
    props = properties(platform / 'source.properties')
    revision = pin.get('revision')
    if not revision or props.get('AndroidVersion.ApiLevel') != str(api) or props.get('Pkg.Revision') != revision:
        raise ValueError('SDK platform API/revision differs: ' + str(api))
    sha = digest(platform / 'android.jar')
    if sha != checked_sha(pin.get('android_jar_sha256')): raise ValueError('SDK android.jar differs: ' + str(api))
    return dict(api=api, revision=revision, android_jar_sha256=sha)


def validate_device(facts, pin, api, weak=False):
    serial = facts.get('serial')
    if not isinstance(serial, str) or not re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.:-]*', serial):
        raise ValueError('invalid or empty ADB serial')
    observed = facts.get('api')
    if facts.get('state') != 'device' or type(observed) is not int or (observed != api if api is not None else observed < 28):
        raise ValueError('device unavailable or incorrect Android API')
    if pin.get('abi') not in {'arm64-v8a', 'armeabi-v7a', 'x86', 'x86_64'} or facts.get('abi') != pin['abi']:
        raise ValueError('device ABI differs')
    if not pin.get('fingerprint') or facts.get('fingerprint') != pin['fingerprint']:
        raise ValueError('device fingerprint differs from provisioned lane')
    if weak:
        physical = facts.get('kernel_qemu') != '1' and facts.get('boot_qemu') != '1' and not facts.get('serial', '').startswith('emulator-')
        memory, cpu = facts.get('memory_kib'), facts.get('cpu_max_khz')
        complete_cpu = (type(facts.get('cpu_present_count')) is int and facts['cpu_present_count'] > 0 and
                        type(facts.get('cpu_frequency_count')) is int and facts['cpu_frequency_count'] == facts['cpu_present_count'])
        if not physical or not complete_cpu or type(memory) is not int or not 256 * 1024 <= memory <= 4 * 1024 * 1024 or type(cpu) is not int or not 100_000 <= cpu <= 2_000_000:
            raise ValueError('weak physical device criterion not observed')


def run(command, environment=None, cwd=None):
    return subprocess.run([str(p) for p in command], env=environment, cwd=cwd, check=True,
                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=90).stdout.strip()


def inspect_device(adb, serial):
    if not re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.:-]*', serial): raise ValueError('invalid or empty ADB serial')
    prefix = [adb, '-s', serial]
    def prop(key): return run(prefix + ['shell', 'getprop', key])
    facts = dict(serial=serial, state=run(prefix + ['get-state']), api=int(prop('ro.build.version.sdk')),
                 abi=prop('ro.product.cpu.abi'), fingerprint=prop('ro.build.fingerprint'),
                 kernel_qemu=prop('ro.kernel.qemu'), boot_qemu=prop('ro.boot.qemu'))
    memory = run(prefix + ['shell', 'cat', '/proc/meminfo'])
    total = re.search(r'^MemTotal:\s+(\d+) kB', memory, re.M)
    # A fixed shell command is required for glob expansion; no input is interpolated into it.
    present = run(prefix + ['shell', 'cat /sys/devices/system/cpu/present 2>/dev/null; true'])
    cpus = set()
    for group in present.split(','):
        if not re.fullmatch(r'\d+(?:-\d+)?', group): continue
        bounds = [int(n) for n in group.split('-')]
        first, last = bounds[0], bounds[-1]
        if not 0 <= first <= last <= 4095: raise ValueError('invalid CPU inventory')
        cpus.update(range(first, last + 1))
    values = run(prefix + ['shell', 'for c in /sys/devices/system/cpu/cpu[0-9]*; do printf "%s " "${c##*cpu}"; cat "$c/cpufreq/cpuinfo_max_freq" 2>/dev/null || echo unknown; done'])
    frequencies = {}
    for line in values.splitlines():
        fields = line.split()
        if len(fields) == 2 and all(v.isdigit() for v in fields): frequencies[int(fields[0])] = int(fields[1])
    complete = bool(cpus) and set(frequencies) == cpus
    facts.update(memory_kib=int(total[1]) if total else None, cpu_max_khz=max(frequencies.values()) if complete else None,
                 cpu_present_count=len(cpus), cpu_frequency_count=len(frequencies))
    return facts


def load_compatibility():
    spec = importlib.util.spec_from_file_location('release_compatibility', ROOT / 'scripts/run-stability-compatibility.py')
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module


def verify_legacy_coordinates(repository, records):
    seen = set()
    for record in records:
        module, name = record['module'], record['file']
        allowed = {module + '-1.0.12' + suffix for suffix in ('.aar', '.jar', '.pom', '.module', '-sources.jar')}
        if not re.fullmatch(r'jankhunter-[a-z0-9-]+', module) or name not in allowed:
            raise ValueError('legacy artifact coordinates differ')
        path = repository / 'io/jankhunter' / module / '1.0.12' / name
        if path.suffix == '.pom':
            try: root = ET.parse(path).getroot()
            except ET.ParseError as error: raise ValueError('invalid legacy POM') from error
            coordinates = tuple(root.findtext('{*}' + key) for key in ('groupId', 'artifactId', 'version'))
            if coordinates != ('io.jankhunter', module, '1.0.12'): raise ValueError('legacy POM coordinates differ')
            seen.add(module)
        elif path.suffix == '.module':
            component = json.loads(path.read_text())['component']
            if tuple(component.get(key) for key in ('group', 'module', 'version')) != ('io.jankhunter', module, '1.0.12'):
                raise ValueError('legacy Gradle module coordinates differ')
    if not {'jankhunter-runtime', 'jankhunter-gradle-plugin'} <= seen: raise ValueError('legacy publication POMs missing')


def preflight(environment, revision):
    pin = checked_sha(environment['JH_RELEASE_INPUTS_SHA256'])
    profile = load_pinned_manifest(Path(environment['JH_RELEASE_INPUTS_MANIFEST']), pin)
    report = dict(schema=1, source_revision=revision, source_dirty=False, input_manifest_sha256=pin,
                  gradle={}, devices={}, sdk={})
    actual_revision = run(['git', 'rev-parse', 'HEAD'], cwd=ROOT)
    compat = load_compatibility()
    if actual_revision != revision or compat.source_dirty_from_status(run(['git', 'status', '--porcelain'], cwd=ROOT)):
        raise ValueError('release checkout is dirty or revision differs')
    # No variable-selected Gradle executable is invoked until every distribution is verified.
    locks = json.loads((ROOT / 'scripts/release-gradle-distributions.json').read_text())
    executables = {}
    for role, versions in ROLES.items():
        item = profile['gradle'][role]; version = item['version']
        if version not in versions: raise ValueError('unapproved version for ' + role)
        if not Path(environment[role]).is_absolute() or not Path(item['archive']).is_absolute():
            raise ValueError('absolute distribution/archive paths required')
        sha = locks[version]['sha256']
        executables[role] = verify_distribution(Path(environment[role]), Path(item['archive']), version, sha)
        report['gradle'][role] = dict(version=version, archive_sha256=sha)
    cache = profile['dependency_cache']
    cache_root = Path(environment.get('GRADLE_USER_HOME', str(Path.home() / '.gradle'))) / 'caches/modules-2/files-2.1'
    if not cache_root.is_absolute(): raise ValueError('GRADLE_USER_HOME must be absolute')
    if Path(cache['root']).resolve() != cache_root.resolve(): raise ValueError('pinned cache is not the active Gradle cache')
    report['cache'] = dict(files=verify_cache(cache_root, cache['files']), root=str(cache_root))
    legacy = Path(environment['JH_LEGACY_REPOSITORY'])
    candidate_group = Path.home() / '.m2/repository/io/jankhunter'
    if (legacy / 'io/jankhunter').resolve().is_relative_to(candidate_group.resolve()):
        raise ValueError('legacy baseline would be deleted by candidate publication')
    sha = digest(legacy / 'exact-artifacts.json')
    if sha != checked_sha(profile['legacy_manifest_sha256']): raise ValueError('legacy repository provenance differs')
    records = compat.verify_repository(legacy, '1.0.12')
    verify_legacy_coordinates(legacy, records)
    report['legacy'] = dict(version='1.0.12', manifest_sha256=sha, files=len(records))
    baseline = profile['metadata_baseline']
    if environment['JH_METADATA_BASELINE_VERSION'] != baseline['version'] or not baseline['version']:
        raise ValueError('metadata baseline version differs from provisioning pin')
    baseline_root = Path(environment['JH_METADATA_BASELINE_REPOSITORY']) / 'io/jankhunter'
    if baseline_root.resolve().is_relative_to(candidate_group.resolve()):
        raise ValueError('metadata baseline would be deleted by candidate publication')
    report['metadata_baseline'] = dict(version=baseline['version'], files=verify_cache(baseline_root, baseline['files']))
    sdk = Path(environment['ANDROID_HOME'])
    if environment.get('ANDROID_SDK_ROOT') and Path(environment['ANDROID_SDK_ROOT']).resolve() != sdk.resolve():
        raise ValueError('ANDROID_HOME and ANDROID_SDK_ROOT differ')
    for api in (33, 34, 35): report['sdk'][str(api)] = verify_sdk_platform(sdk, api, profile['sdk_platforms'][str(api)])
    for role, api in DEVICE_APIS.items():
        facts = inspect_device(sdk / 'platform-tools/adb', environment[role])
        validate_device(facts, profile['devices'][role], api, weak=role == 'JH_WEAK_PHYSICAL_SERIAL')
        report['devices'][role] = facts
    with tempfile.TemporaryDirectory(prefix='jh-gradle-preflight-') as work:
        for role, executable in executables.items():
            execution_environment = dict(environment)
            if role == 'JH_GRADLE_MIN': execution_environment['JAVA_HOME'] = environment['JH_JAVA17_HOME']
            text = run([executable, '--offline', '--version'], execution_environment, work)
            validate_gradle_version(text, report['gradle'][role]['version'])
    report['pass'] = True
    return report


def evidence_errors(report, revision):
    try:
        if report.get('schema') != 1 or report.get('pass') is not True or report.get('source_revision') != revision or report.get('source_dirty') is not False:
            raise ValueError('missing or wrong-revision environment proof')
        checked_sha(report['input_manifest_sha256'])
        locks = json.loads((ROOT / 'scripts/release-gradle-distributions.json').read_text())
        for role, versions in ROLES.items():
            item = report['gradle'][role]
            if item['version'] not in versions or item['archive_sha256'] != locks[item['version']]['sha256']:
                raise ValueError('unapproved Gradle proof')
        for role, api in DEVICE_APIS.items():
            facts = report['devices'][role]
            validate_device(facts, facts, api, weak=role == 'JH_WEAK_PHYSICAL_SERIAL')
        for api in (33, 34, 35):
            item = report['sdk'][str(api)]
            if item['api'] != api or not item['revision']: raise ValueError('SDK proof missing')
            checked_sha(item['android_jar_sha256'])
        if type(report['cache']['files']) is not int or report['cache']['files'] <= 0 or report['legacy']['version'] != '1.0.12' or type(report['legacy']['files']) is not int or report['legacy']['files'] <= 0:
            raise ValueError('dependency cache or legacy proof missing')
        checked_sha(report['legacy']['manifest_sha256'])
        baseline = report['metadata_baseline']
        if not baseline['version'] or type(baseline['files']) is not int or baseline['files'] <= 0:
            raise ValueError('metadata baseline proof missing')
    except (KeyError, TypeError, ValueError) as error:
        return ['invalid mandatory environment evidence: ' + str(error)]
    return []


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    report = dict(schema=1, source_revision=args.revision, **{'pass': False})
    try:
        report = preflight(os.environ, args.revision)
        errors = evidence_errors(report, args.revision)
        if errors: raise ValueError('; '.join(errors))
    except (OSError, ValueError, KeyError, TypeError, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        report['pass'] = False; report['errors'] = [str(error)]
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'pass': report['pass'], 'evidence': str(args.output), 'errors': report.get('errors', [])}))
    return 0 if report['pass'] else 1


if __name__ == '__main__': sys.exit(main())
