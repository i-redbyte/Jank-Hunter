#!/usr/bin/env python3
"""Compile real historical/current Kotlin fixture bytes using explicit cached compiler inputs."""
import argparse
import json
from pathlib import Path
import subprocess
import sys


def jar(cache, group, artifact, version):
    candidates = [p for p in (cache / group / artifact / version).rglob('*.jar')
                  if not p.name.endswith('-sources.jar') and not p.name.endswith('-javadoc.jar')]
    if len(candidates) != 1:
        raise ValueError(f'required exact {group}:{artifact}:{version} binary missing/ambiguous')
    return candidates[0]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--java', default='java')
    parser.add_argument('--compiler', action='append', choices=('1.3.72', '1.4.32', '1.5.31', '1.6.21', '1.7.22', '1.8.22', '1.9.23', '2.0.21', '2.1.21', '2.2.21', '2.3.20'))
    parser.add_argument('--android-bootclasspath', type=Path,
                        help='JVM8 Android stubs for historical dependency fixtures on a modern build JDK')
    args = parser.parse_args()
    fixture_root = Path(__file__).resolve().parent / 'fixtures/kotlin-metadata'
    results = []
    try:
        def sources(compiler):
            files = sorted(fixture_root.glob('*.kt'))
            if compiler in ('1.3.72', '1.4.32'):
                files = [path for path in files if path.name != 'MetadataValue.kt']
            if not files:
                raise ValueError('no kotlin metadata fixture sources found')
            return files
        matrix = {'1.3.72': '1.3.72', '1.4.32': '1.4.32', '1.5.31': '1.5.31', '1.6.21': '1.6.21', '1.7.22': '1.7.22', '1.8.22': '1.8.22',
                  '1.9.23': '1.9.10', '2.0.21': '2.0.21', '2.1.21': '2.1.21', '2.2.21': '2.2.21', '2.3.20': '2.3.20'}
        for compiler in args.compiler or matrix:
            stdlib = matrix[compiler]
            host = [jar(args.cache, 'org.jetbrains.kotlin', 'kotlin-compiler-embeddable', compiler),
                    jar(args.cache, 'org.jetbrains.kotlin', 'kotlin-stdlib', stdlib),
                    jar(args.cache, 'org.jetbrains', 'annotations', '13.0'),
                    jar(args.cache, 'org.jetbrains.intellij.deps', 'trove4j', '1.0.20200330')]
            historical = compiler in ('1.3.72', '1.4.32', '1.5.31', '1.6.21', '1.7.22', '1.8.22')
            if historical:
                host.append(jar(args.cache, 'org.jetbrains.kotlin', 'kotlin-reflect', compiler))
                host.append(jar(args.cache, 'org.jetbrains.kotlin', 'kotlin-script-runtime', compiler))
            elif compiler != '1.9.23':
                host.append(jar(args.cache, 'org.jetbrains.kotlinx', 'kotlinx-coroutines-core-jvm', '1.9.0'))
                host.append(jar(args.cache, 'org.jetbrains.kotlin', 'kotlin-reflect', '1.6.10' if compiler in ('2.0.21', '2.1.21') else '2.2.21'))
            target = args.output / compiler
            target.mkdir(parents=True, exist_ok=True)
            target_classpath = [host[1]]
            legacy_flags = []
            if historical and args.android_bootclasspath:
                if not args.android_bootclasspath.is_file():
                    raise ValueError('Android bootclasspath does not exist')
                target_classpath.append(args.android_bootclasspath)
                legacy_flags.append('-no-jdk')
            command = [args.java, '--add-opens=java.base/java.util=ALL-UNNAMED', '-cp', ':'.join(map(str, host)), 'org.jetbrains.kotlin.cli.jvm.K2JVMCompiler',
                       '-no-stdlib', '-no-reflect', '-jvm-target', '1.8' if historical else '17',
                       '-classpath', ':'.join(map(str, target_classpath)), *legacy_flags,
                       '-d', str(target), *[str(path) for path in sources(compiler)]]
            result = subprocess.run(command, capture_output=True, text=True, timeout=120)
            (target / 'compiler.log').write_text(result.stdout + result.stderr)
            if result.returncode:
                raise ValueError(f'compiler {compiler} failed: {result.stderr[-1500:]}')
            expected = ['fixture/metadata/MetadataData.class', 'fixture/metadata/MetadataInline.class',
                        'fixture/metadata/MetadataSuspend.class', 'fixture/metadata/MetadataMultifile.class']
            if compiler not in ('1.3.72', '1.4.32'):
                expected.append('fixture/metadata/MetadataValue.class')
            missing = [name for name in expected if not (target / name).is_file()]
            if missing:
                raise ValueError(f'compiler {compiler} did not produce fixture classes: {missing}')
            import hashlib
            results.append({'compiler': compiler, 'stdlib': stdlib, 'output': str(target),
                            'compiler_sha256': hashlib.sha256(host[0].read_bytes()).hexdigest(),
                            'class_sha256': hashlib.sha256((target / 'fixture/metadata/MetadataData.class').read_bytes()).hexdigest()})
        (args.output / 'fixture-compilers.json').write_text(json.dumps(results, indent=2) + '\n')
    except (OSError, ValueError, subprocess.TimeoutExpired) as error:
        print(error, file=sys.stderr)
        return 1
    print(json.dumps(results))
    return 0


if __name__ == '__main__':
    sys.exit(main())
