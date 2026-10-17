#!/usr/bin/env python3
"""Build exact packaged JH in an isolated shrinking consumer and require real ART evidence.

No mavenLocal, composite build, dependency override, warning suppression, device skip,
or implicit toolchain upgrade. Supply the old/new matrix toolchain explicitly.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parent.parent

def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

boundary = load('boundary', 'validate-kotlin-boundary.py')
manifests = load('manifests', 'validate-build-manifests.py')


def source_dirty_from_status(status):
    for line in status.splitlines():
        parts = Path(line[3:].strip('"')).parts
        if line[:2] == '??' and any(part in ('__pycache__', '.kotlin', '.gradle', 'build') for part in parts):
            continue
        if line.strip():
            return True
    return False


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def verify_repository(repository, version, maximum=(1, 9), max_bytecode=52):
    records = json.loads((repository / 'exact-artifacts.json').read_text())
    required = {'jankhunter-runtime', 'jankhunter-annotations', 'jankhunter-okhttp3',
                'jankhunter-workmanager', 'jankhunter-jankstats', 'jankhunter-android-sdk', 'jankhunter-gradle-plugin'}
    if version == '1.0.12':
        required.discard('jankhunter-jankstats')
    if not required <= {r['module'] for r in records}:
        raise ValueError('missing exact publication modules')
    devices = []
    for record in records:
        path = repository / 'io/jankhunter' / record['module'] / version / record['file']
        if digest(path) != record['sha256']:
            raise ValueError(f'publication bytes changed: {path}')
        if path.suffix in ('.aar', '.jar') and not path.name.endswith('-sources.jar') and record['module'] not in ('jankhunter-gradle-plugin', 'jankhunter-android-sdk'):
            result = boundary.validate_artifact(path, maximum, max_bytecode)
            if result['errors']:
                raise ValueError(str(result['errors']))
            devices.append(record)
    if len(devices) < 4:
        raise ValueError('device publication boundary not observed')
    return records


def verify_full(build, baseline=False):
    manifests.validate(build, 'release', 'apk')
    generated = build / 'generated/jankhunter/release'
    metadata = json.loads((generated / 'artifact-metadata.json').read_text())
    if not baseline and (not metadata['hooks'].get('threadLifecycle') or not metadata['hooks'].get('concurrencyDiagnostics')):
        raise ValueError('required causal/thread instrumentation scope missing')
    if not metadata['hooks']['runtimeCallGraph'] or metadata['variant'] != 'release':
        raise ValueError('wrong variant or missing FULL graph')
    if not baseline:
        if metadata.get('metadataCompatibilityMode') not in ('COMPATIBILITY', 'STRICT'):
            raise ValueError('artifact metadata missing metadataCompatibilityMode')
        if not metadata.get('metadataReaderIdentity'):
            raise ValueError('artifact metadata missing metadataReaderIdentity')
    if 'com.fixture.callback' not in metadata['includePackages'] or metadata['excludePackages']:
        raise ValueError('generic callback scope excluded or absent')
    compatibility_json = {}
    if not baseline:
        compatibility = generated / 'metadata-compatibility.json'
        if not compatibility.is_file():
            raise ValueError('metadata-compatibility summary missing')
        compatibility_json = json.loads(compatibility.read_text())
        if compatibility_json.get('schema') != 1:
            raise ValueError('metadata-compatibility schema mismatch')
    records = [json.loads(line) for line in (generated / 'instrumentation-diagnostics.jsonl').read_text().splitlines()]
    logger = [r for r in records if r['class'].startswith('com.fixture.callback.CallbackPipeline')]
    if not any(r['class'] == 'com.fixture.callback.CallbackPipeline' and r['methodFilterIncluded'] > 0 for r in logger):
        raise ValueError('no observed callback method coverage')
    classes = build / 'intermediates/classes/release/ALL/instrumentReleaseJankHunterTransport/classes.jar'
    with zipfile.ZipFile(classes) as archive:
        data = archive.read('com/fixture/callback/CallbackPipeline.class')
    if not all(value in data for value in (b'io/jankhunter/runtime/JankHunterHooks', b'enterMethod', b'exitMethod')):
        raise ValueError('FULL callback hook invocation missing from packaged input')
    return {'fixture_classes': len(logger), 'fixture_methods': sum(r['methodFilterIncluded'] for r in logger),
            'classes_sha256': digest(classes), 'metadata': metadata,
            'metadata_compatibility_observations': compatibility_json.get('observations', 0)}


def verify_art(output, expected_tests=2):
    success = re.search(r'\bOK \((\d+) tests?\)', output)
    if not success or int(success.group(1)) != expected_tests or any(s in output for s in ('FAILURES!!!', 'INSTRUMENTATION_FAILED', 'Process crashed', 'INSTRUMENTATION_STATUS_CODE: -3')):
        raise ValueError('mandatory ART tests failed, skipped, or were not executed')


def verify_apk_reader_isolation(apk):
    with zipfile.ZipFile(apk) as archive:
        for name in archive.namelist():
            if 'jankhunter/metadata-reader/' in name:
                raise ValueError('Private metadata reader resources leaked into APK')
            if name.endswith('.dex'):
                data = archive.read(name)
                if b'io/jankhunter/reader/' in data or b'kotlin/metadata/jvm/' in data or b'JankHunterBuildEvidenceV1_' in data:
                    raise ValueError('Private metadata reader classes leaked into DEX')


def verify_capture(path, counts):
    with zipfile.ZipFile(path) as archive:
        logs = [e for e in archive.infolist() if e.filename.endswith('.jhlog') and e.file_size > 0]
        if not logs or any(archive.read(e)[:4] != b'JHLO' for e in logs):
            raise ValueError('missing or invalid JHLOG capture')
    observed = json.loads(counts.read_text())
    if observed.get('pre_init') != 64 or observed.get('ready') != 1000 or observed.get('masked_inside', 0) < 64 or observed.get('masked_outside', 0) < 1000 or observed.get('failures') != 0:
        raise ValueError('insufficient callback PRE_INIT/DRAINING/READY observation')
    return observed


def run(command, path, timeout=900):
    with path.open('w') as output:
        result = subprocess.run([str(v) for v in command], stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
    if result.returncode:
        raise ValueError(f'command failed ({result.returncode}): inspect {path}')
    return path.read_text(errors='replace')


def pull_evidence(adb, name, destination, log):
    # API30 may deny shell traversal of Android/data despite successful app export.
    # The fixture exposes only these exact test files to shell UID through a read-only port.
    allowed = {'compat-hot-path.zip', 'hot-path-counts.json', 'stability-benchmark.json', 'stability-benchmark.zip', 'causal-registry.hprof'}
    if name not in allowed:
        raise ValueError('unexpected test evidence filename')
    with destination.open('wb') as output, log.open('wb') as errors:
        result = subprocess.run([str(v) for v in adb] + ['exec-out', 'content', 'read', '--uri', 'content://com.fixture.compat.evidence/' + name],
                                stdout=output, stderr=errors, timeout=60)
    if result.returncode or not destination.is_file() or destination.stat().st_size == 0:
        raise ValueError('test evidence transfer failed: ' + name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('repository', 'output', 'gradle', 'adb', 'cli'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--legacy-metadata-directory', type=Path, help='Real Kotlin1.3 compiler output containing fixture/metadata classes')
    parser.add_argument('--java-only', action='store_true', help='consumer without Kotlin Gradle plugin or Kotlin application sources')
    parser.add_argument('--baseline-sdk', action='store_true', help='separate new-consumer/old1.0.12 ABI lane; never release-artifact approval')
    parser.add_argument('--dsl', choices=('groovy', 'kotlin'), default='groovy')
    parser.add_argument('--serial', required=True)
    parser.add_argument('--agp', choices=('8.0.2', '8.5.2', '8.10.1', '8.13.2', '9.0.1'), default='8.10.1')
    parser.add_argument('--kotlin', choices=('1.9.25', '2.0.21', '2.1.21', '2.2.21', '2.3.20'), default='2.2.21')
    parser.add_argument('--compile-sdk', type=int, choices=(33, 34, 35), default=35)
    parser.add_argument('--java-target', type=int, choices=(8, 11, 17, 21), default=17)
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    report = {'pass': False, 'agp': args.agp, 'kotlin': 'java' if args.java_only else args.kotlin, 'version': args.version, 'dsl': args.dsl, 'baseline_sdk': args.baseline_sdk, 'java_target': args.java_target, 'compile_sdk': args.compile_sdk, 'java_only': args.java_only}
    try:
        if args.java_only and args.agp not in ('8.0.2', '9.0.1'):
            raise ValueError('unsupported Java-only matrix row')
        if not args.java_only and (args.agp, args.kotlin) not in (('8.0.2', '1.9.25'), ('8.5.2', '2.0.21'), ('8.10.1', '2.1.21'), ('8.10.1', '2.2.21'), ('8.13.2', '2.3.20')):
            raise ValueError('unsupported matrix row')
        report['source_revision'] = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
        changed = subprocess.check_output(['git', '-C', str(ROOT), 'status', '--porcelain', '--', 'android', 'cli', 'scripts', 'wire', '.github', 'README.md'], text=True)
        report['source_dirty'] = source_dirty_from_status(changed)
        if args.baseline_sdk and (args.version != '1.0.12' or args.agp != '8.13.2' or args.kotlin != '2.3.20'):
            raise ValueError('baseline SDK lane requires1.0.12 and explicit new supported consumer')
        report['artifacts'] = verify_repository(args.repository.resolve(), args.version, (2, 3) if args.baseline_sdk else (1, 9), 61 if args.baseline_sdk else 52)
        fixture = output / 'consumer'
        if fixture.exists():
            raise ValueError('output consumer already exists; use a fresh evidence directory')
        shutil.copytree(ROOT / 'scripts/fixtures/metadata-consumer', fixture, ignore=shutil.ignore_patterns('build', '.gradle', 'local.properties'))
        text = (fixture / 'build.gradle').read_text().replace('gradle:8.10.1', 'gradle:' + args.agp).replace('kotlin-gradle-plugin:2.2.21', 'kotlin-gradle-plugin:' + args.kotlin)
        (fixture / 'build.gradle').write_text(text)
        app = fixture / 'app/build.gradle'
        if args.dsl == 'kotlin':
            app.unlink()
            app = fixture / 'app/build.gradle.kts'
            app.write_text((fixture / 'app/build.gradle.kts.template').read_text())
        app.write_text(app.read_text().replace('kotlin-stdlib:2.2.21', 'kotlin-stdlib:' + args.kotlin).replace('kotlin-reflect:2.2.21', 'kotlin-reflect:' + args.kotlin))
        java_version = '1_8' if args.java_target == 8 else str(args.java_target)
        app_text = app.read_text().replace('VERSION_17', 'VERSION_' + java_version).replace('JVM_17', 'JVM_' + java_version)
        app_text = app_text.replace('compileSdk 35', 'compileSdk ' + str(args.compile_sdk)).replace('compileSdk = 35', 'compileSdk = ' + str(args.compile_sdk))
        if args.kotlin.startswith('1.'):
            if args.dsl == 'kotlin':
                app_text = re.sub(r'configure<org.jetbrains.kotlin.gradle.dsl.KotlinAndroidProjectExtension> \{.*?\n\}',
                    'tasks.withType<org.jetbrains.kotlin.gradle.tasks.KotlinCompile>().configureEach { kotlinOptions.jvmTarget = "' + ('1.8' if args.java_target == 8 else str(args.java_target)) + '" }', app_text, flags=re.S)
            else:
                app_text = re.sub(r'kotlin \{ compilerOptions .*?\n',
                    "tasks.withType(org.jetbrains.kotlin.gradle.tasks.KotlinCompile).configureEach { kotlinOptions.jvmTarget = '" + ('1.8' if args.java_target == 8 else str(args.java_target)) + "' }\n", app_text)
        if args.java_only:
            app_text = re.sub(r'configure<org.jetbrains.kotlin.gradle.dsl.KotlinAndroidProjectExtension> \{.*?\n\}', '', app_text, flags=re.S)
            app_text = '\n'.join(line for line in app_text.splitlines() if 'org.jetbrains.kotlin.android' not in line) + '\n'
            app_text = re.sub(r'^kotlin \{.*?\n|^tasks.withType.*KotlinCompile.*\n|^.*(?:kotlin-stdlib|kotlin-reflect).*\n', '', app_text, flags=re.M)
            root_build = fixture / 'build.gradle'
            root_build.write_text('\n'.join(line for line in root_build.read_text().splitlines() if 'kotlin-gradle-plugin' not in line) + '\n')
            shutil.rmtree(fixture / 'app/src/main/kotlin')
            destination = fixture / 'app/src/main/java/com/fixture/compat'
            for source in (ROOT / 'scripts/fixtures/java-consumer').glob('*.java'):
                shutil.copyfile(source, destination / source.name)
            (destination / 'FixtureApplication.java').write_text('package com.fixture.compat; public final class FixtureApplication extends android.app.Application {}\n')
            rules = fixture / 'app/compat.pro'
            rules.write_text(rules.read_text() + '\n-keep class com.fixture.compat.CompatibilityProbe { *; }\n')
            tests = fixture / 'app/src/androidTest/java/com/fixture/compat/CompatibilityTest.java'
            tests.write_text(tests.read_text().replace('Contracts() {', 'Contracts() throws Exception {').replace('ReadyPaths() {', 'ReadyPaths() throws Exception {'))
        if args.agp == '8.0.2':
            # Test runner must honor the consumer's compileSdk, just like production dependencies.
            app_text = app_text.replace('androidx.test:runner:1.6.2', 'androidx.test:runner:1.5.2').replace('androidx.test.ext:junit:1.2.1', 'androidx.test.ext:junit:1.1.5')
        if args.legacy_metadata_directory:
            legacy = args.legacy_metadata_directory.resolve() / 'fixture/metadata'
            expected = legacy / 'MetadataData.class'
            if not expected.is_file():
                raise ValueError('Real historical MetadataData.class is required')
            jar = fixture / 'app/libs/historical-metadata.jar'
            jar.parent.mkdir(parents=True, exist_ok=True)
            with zipfile.ZipFile(jar, 'w') as archive:
                for source in sorted(legacy.glob('*.class')):
                    archive.writestr('fixture/metadata/' + source.name, source.read_bytes())
            report['historical_dependency_sha256'] = digest(jar)
            app_text += ('\ndependencies { add("implementation", files("libs/historical-metadata.jar")) }\n'
                         if args.dsl == 'kotlin' else '\ndependencies { implementation files("libs/historical-metadata.jar") }\n')
            rules = fixture / 'app/compat.pro'
            rules.write_text(rules.read_text() + '\n-keep class fixture.metadata.MetadataData { *; }\n-keepattributes RuntimeVisibleAnnotations\n')
            tests = fixture / 'app/src/androidTest/java/com/fixture/compat/CompatibilityTest.java'
            text = tests.read_text()
            position = text.rfind('}')
            text = text[:position] + """
    @Test public void historicalDependencyPreservesReflectionAndBehavior() throws Exception {
        Class<?> type = Class.forName("fixture.metadata.MetadataData");
        Object instance = type.getConstructor(int.class).newInstance(41);
        assertEquals(42, ((Number) type.getMethod("declared", int.class).invoke(instance, 41)).intValue());
        org.junit.Assert.assertNotNull(type.getAnnotation(kotlin.Metadata.class));
    }
""" + text[position:]
            tests.write_text(text)
        app.write_text(app_text)

        if args.baseline_sdk or args.java_only:
            # Older SDK ABI does not offer the new concurrency benchmark APIs.
            for name in ('StabilityBenchmark.kt', 'RegistryFootprint.kt', 'BenchmarkActivity.kt'):
                (fixture / 'app/src/main/kotlin/com/fixture/compat' / name).unlink(missing_ok=True)
            for name in ('StabilityBenchmarkTest.java', 'RegistryFootprintTest.java'):
                (fixture / 'app/src/androidTest/java/com/fixture/compat' / name).unlink()
            manifest = fixture / 'app/src/main/AndroidManifest.xml'
            manifest.write_text(manifest.read_text().replace('<activity android:name=".BenchmarkActivity" android:exported="false" />', ''))
        build_log = run([args.gradle, '-p', fixture, '-PjhRepository=' + str(args.repository.resolve()), '-PjhVersion=' + args.version, '-PfixtureCompileSdk=' + str(args.compile_sdk), ':app:assembleRelease', ':app:assembleReleaseAndroidTest', ':app:dependencies', '--configuration', 'releaseRuntimeClasspath', '--offline', '--console=plain'], output / 'build.log')
        errors = boundary.validate_r8_log(output / 'build.log')
        if errors:
            raise ValueError(str(errors))
        for coordinate in ('io.jankhunter:jankhunter-runtime:' + args.version,):
            if coordinate not in build_log:
                raise ValueError('resolved dependency missing: ' + coordinate)
        build = fixture / 'app/build'
        report['coverage'] = verify_full(build, args.baseline_sdk)
        apks = [build / 'outputs/apk/release/app-release.apk', build / 'outputs/apk/androidTest/release/app-release-androidTest.apk']
        verify_apk_reader_isolation(apks[0])
        report['apk_sha256'] = [digest(p) for p in apks]
        adb = [args.adb, '-s', args.serial]
        report['device'] = {}
        for key in ('ro.kernel.qemu', 'ro.boot.qemu', 'ro.build.version.sdk', 'ro.product.cpu.abi', 'ro.product.model', 'ro.build.fingerprint'):
            report['device'][key] = run(adb + ['shell', 'getprop', key], output / (key + '.txt'), 30).strip()
        report['android_api'] = int(run(adb + ['shell', 'getprop', 'ro.build.version.sdk'], output / 'api.txt', 30).strip())
        for index, apk in enumerate(apks):
            run(adb + ['install', '-r', apk], output / f'install-{index}.log', 120)
        # Remove only fixture-produced evidence so stale captures cannot satisfy the gate.
        remote = '/sdcard/Android/data/com.fixture.compat/files/'
        for name in ('compat-hot-path.zip', 'hot-path-counts.json'):
            run(adb + ['shell', 'content', 'delete', '--uri', 'content://com.fixture.compat.evidence/' + name], output / ('clear-' + name + '.log'), 30)
        art = run(adb + ['shell', 'am', 'instrument', '-w', '-r', '-e', 'class', 'com.fixture.compat.CompatibilityTest', 'com.fixture.compat.test/androidx.test.runner.AndroidJUnitRunner'], output / 'art.log', 120)
        verify_art(art, 3 if args.legacy_metadata_directory else 2)
        for name in ('compat-hot-path.zip', 'hot-path-counts.json'):
            pull_evidence(adb, name, output / name, output / ('pull-' + name + '.log'))
        report['paths'] = verify_capture(output / 'compat-hot-path.zip', output / 'hot-path-counts.json')
        run([args.cli, 'inspect', output / 'compat-hot-path.zip', '--json', '--artifacts-dir', build / 'generated/jankhunter/release', '--mapping', build / 'outputs/mapping/release/mapping.txt'], output / 'inspect.json', 120)
        summary = json.loads((output / 'inspect.json').read_text())
        if summary['MappingIdentity']['Status'] != 'verified' or summary['MappingIdentity']['VerifiedLogs'] <= 0:
            raise ValueError('archive identity differs from exact APK/mapping')
        run([args.cli, 'export', output / 'compat-hot-path.zip', '--out', output / 'events.jsonl'], output / 'export.log', 120)
        if not (output / 'events.jsonl').is_file() or (output / 'events.jsonl').stat().st_size == 0:
            raise ValueError('empty event export')
        report['pass'] = True
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.TimeoutExpired) as error:
        report['error'] = str(error)
    (output / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({key: value for key, value in report.items() if key != 'device'}, indent=2))
    return 0 if report['pass'] else 1


if __name__ == '__main__':
    sys.exit(main())
