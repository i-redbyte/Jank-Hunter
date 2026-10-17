#!/usr/bin/env python3
"""Paired, reproducible build measurements. Commands are JSON argv arrays, never shell text."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import random
import re
import shutil
import sys
import statistics
import subprocess
import time
import zipfile
from html.parser import HTMLParser


class ProfileRows(HTMLParser):
    def __init__(self):
        super().__init__()
        self.rows, self.row, self.cell = [], [], None

    def handle_starttag(self, tag, attrs):
        if tag == 'tr':
            self.row = []
        if tag == 'td':
            self.cell = ''

    def handle_data(self, text):
        if self.cell is not None:
            self.cell += text

    def handle_endtag(self, tag):
        if tag == 'td' and self.cell is not None:
            self.row.append(self.cell.strip())
            self.cell = None
        if tag == 'tr' and len(self.row) >= 2:
            self.rows.append(self.row)


def profile_task_times(html):
    parser = ProfileRows()
    parser.feed(html)
    tasks = {}
    for row in parser.rows:
        name = row[0]
        if not name.startswith(':') or not ('JankHunter' in name or name.endswith('ClassesWithAsm')):
            continue
        parts = re.findall(r'(\d+(?:\.\d+)?)\s*(ms|s|m|h)', row[1])
        if parts:
            tasks[name] = sum(float(number) * {'ms': .001, 's': 1, 'm': 60, 'h': 3600}[unit] for number, unit in parts)
    return tasks


def summarize(before, after, scenario):
    if not before or len(before) != len(after) or any(not math.isfinite(x) or x <= 0 for x in before + after):
        raise ValueError('Complete positive finite paired measurements are required')
    differences = [b - a for a, b in zip(before, after)]
    relative = [(b - a) / a for a, b in zip(before, after)]
    floor = 0.100 if scenario == 'no-op' else 0.150 if scenario in ('java-edit', 'kotlin-edit', 'signature-edit') else 0.0
    percent = 0.05 if scenario == 'jh-task' else 0.01 if scenario == 'no-op' else 0.02
    budget = max(percent * statistics.median(before), floor)
    excess = [b - a - max(percent * a, floor) for a, b in zip(before, after)]
    rng = random.Random(32150)
    bootstrap = sorted(statistics.median(rng.choices(differences, k=len(differences))) for _ in range(2000))
    low, high = bootstrap[49], bootstrap[1949]
    rng = random.Random(32150)
    excess_bootstrap = sorted(statistics.median(rng.choices(excess, k=len(excess))) for _ in range(2000))
    excess_low, excess_high = excess_bootstrap[49], excess_bootstrap[1949]
    status = 'INCONCLUSIVE' if len(before) < 10 else 'PASS' if excess_high <= 0 else 'FAIL' if excess_low > 0 else 'INCONCLUSIVE'
    return {'status': status, 'pairs': len(before), 'before_median_s': statistics.median(before),
            'after_median_s': statistics.median(after), 'paired_median_s': statistics.median(differences),
            'paired_relative_median': statistics.median(relative), 'paired_95_interval_s': [low, high],
            'budget_excess_95_interval_s': [excess_low, excess_high],
            'budget_s': budget, 'before_p95_s': sorted(before)[math.ceil(len(before) * .95) - 1],
            'after_p95_s': sorted(after)[math.ceil(len(after) * .95) - 1]}


def task_outcomes(output):
    result = {}
    for match in re.finditer(r'^> Task (:\S+)(?: (UP-TO-DATE|FROM-CACHE|NO-SOURCE|SKIPPED|FAILED))?\s*$', output, re.MULTILINE):
        result[match[1]] = match[2] or 'EXECUTED'
    return result


def audit_work(before, after, scenario):
    errors = []
    for index, (baseline, candidate) in enumerate(zip(before, after)):
        if scenario in ('no-op', 'cache-restore') and any(
            metric.get('decode_count', 0) or metric.get('backend_open_count', 0)
            for metric in candidate.get('metadata_metrics') or []
        ):
            errors.append(f'pair {index}: reader work on {scenario}')
        def invalidated(row):
            return {name for name, state in row['tasks'].items()
                if state in ('EXECUTED', 'FROM-CACHE') and re.search(
                    r'compile.*(?:Kotlin|JavaWithJavac)|[dD]ex|[mM]inify.*WithR8', name)}
        extra = invalidated(candidate) - invalidated(baseline)
        if extra:
            errors.append(f'pair {index}: additional compile/dex/R8 work: {sorted(extra)}')
    return {'status': 'FAIL' if errors else 'PASS', 'errors': errors}


def file_digest(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(65536), b''):
            digest.update(block)
    return digest.hexdigest()


def verify_artifacts(artifacts):
    if not artifacts:
        raise ValueError('Exact immutable A/B plugin/SDK artifacts with SHA256 are required')
    for artifact in artifacts:
        if file_digest(artifact['path']) != artifact['sha256']:
            raise ValueError(f"Artifact changed during measurement: {artifact['path']}")


def execute(command, cwd, log, timeout):
    if not isinstance(command, list) or not command or not all(isinstance(x, str) for x in command):
        raise ValueError('Command must be a nonempty string argv array')
    profiles = {p: p.stat().st_mtime_ns for p in (cwd / 'build/reports/profile').glob('*.html')}
    start = time.perf_counter()
    with log.open('w') as stream:
        completed = subprocess.run(command, cwd=cwd, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
    seconds = time.perf_counter() - start
    text = log.read_text(errors='replace')
    if completed.returncode:
        raise ValueError(f'Command failed ({completed.returncode}); see {log}')
    metrics = [json.loads(line.removeprefix('JH_METADATA_METRICS ')) for line in text.splitlines()
               if line.startswith('JH_METADATA_METRICS ')]
    changed = [p for p in (cwd / 'build/reports/profile').glob('*.html') if profiles.get(p) != p.stat().st_mtime_ns]
    task_times = profile_task_times(max(changed, key=lambda p: p.stat().st_mtime_ns).read_text()) if changed else None
    return {'seconds': seconds, 'tasks': task_outcomes(text), 'metadata_metrics': metrics or None,
            'jh_task_seconds': sum(task_times.values()) if task_times else None, 'jh_tasks': task_times,
            'log': str(log), 'argv': command, 'cwd': str(cwd)}


def measure(args):
    manifest_bytes = args.manifest.read_bytes()
    manifest = json.loads(manifest_bytes)
    artifacts = manifest['artifacts']
    verify_artifacts(artifacts)
    scenarios = manifest.get('scenarios')
    if not isinstance(scenarios, list) or not scenarios:
        raise ValueError('scenarios must be a nonempty list of named A/B objects')
    names = [scenario.get('name') for scenario in scenarios if isinstance(scenario, dict)]
    if len(names) != len(scenarios) or len(set(names)) != len(names):
        raise ValueError('Scenario names must be unique')
    if args.output.exists() and any(args.output.iterdir()):
        raise ValueError('Measurement output must be empty; existing evidence is preserved')
    args.output.mkdir(parents=True, exist_ok=True)
    result = {'schema': 1, 'manifest_sha256': hashlib.sha256(manifest_bytes).hexdigest(),
              'identity': manifest.get('identity', {}), 'scenarios': {}}
    for scenario in manifest['scenarios']:
        name = scenario['name']
        if not re.fullmatch(r'[a-z0-9-]+', name):
            raise ValueError('Scenario name must be a safe lowercase identifier')
        observations = {'A': [], 'B': []}
        target = {'status': 'NOT_RUN', 'observations': observations}
        result['scenarios'][name] = target
        try:
            for warmup in range(2):
                for label in ('A', 'B'):
                    config = scenario[label]
                    for index, command in enumerate(config.get('warmup', [])):
                        execute(command, Path(config['cwd']).resolve(), args.output / f'{name}-{label}-warmup-{warmup}-{index}.log', args.timeout)
            for pair in range(args.pairs):
                for label in (('A', 'B') if pair % 2 == 0 else ('B', 'A')):
                    verify_artifacts(artifacts)
                    config = scenario[label]
                    cwd = Path(config['cwd']).resolve()
                    for index, command in enumerate(config.get('prepare', [])):
                        execute(command, cwd, args.output / f'{name}-{pair}-{label}-prepare-{index}.log', args.timeout)
                    observations[label].append(execute(config['argv'], cwd, args.output / f'{name}-{pair}-{label}.log', args.timeout))
                    verify_artifacts(artifacts)
            target.update(summarize([x['seconds'] for x in observations['A']], [x['seconds'] for x in observations['B']], name))
            task_before = [x['jh_task_seconds'] for x in observations['A']]
            task_after = [x['jh_task_seconds'] for x in observations['B']]
            target['work_gate'] = audit_work(observations['A'], observations['B'], name)
            target['jh_task_time'] = (summarize(task_before, task_after, 'jh-task')
                if all(x is not None and x > 0 for x in task_before + task_after)
                else {'status': 'INCONCLUSIVE', 'reason': 'JH task profile absent or below timer resolution'})
        except (ValueError, OSError, subprocess.TimeoutExpired) as error:
            target.update(status='NOT_RUN', error=str(error))
        (args.output / 'results.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({name: {'wall_time': value['status'],
        'jh_task_time': value.get('jh_task_time', {}).get('status', 'NOT_RUN'),
        'work_gate': value.get('work_gate', {}).get('status', 'NOT_RUN')} for name, value in result['scenarios'].items()}))
    return 0 if all(x['status'] == 'PASS' and x.get('jh_task_time', {}).get('status') == 'PASS' and x.get('work_gate', {}).get('status') == 'PASS' for x in result['scenarios'].values()) else 1


def write_dependency(corpus_root, version):
    source = corpus_root / '.dependency-source/Dependency.java'
    source.parent.mkdir(exist_ok=True)
    source.write_text(f'package fixture.dependency; public class Dependency {{ public static final int VERSION = {version}; }}\n')
    classes = corpus_root / '.dependency-classes'
    javac = str(Path(os.environ['JAVA_HOME']) / 'bin/javac') if os.environ.get('JAVA_HOME') else 'javac'
    subprocess.run([javac, '--release', '8', '-d', str(classes), str(source)], check=True, capture_output=True)
    with zipfile.ZipFile(corpus_root / 'dependency.jar', 'w') as archive:
        archive.writestr(zipfile.ZipInfo('fixture/dependency/Dependency.class', (1980, 1, 1, 0, 0, 0)),
            (classes / 'fixture/dependency/Dependency.class').read_bytes())
    (corpus_root / '.dependency-version').write_text(str(version))


def corpus(args):
    if args.output.exists():
        raise ValueError('Corpus output must be a new directory; existing files are preserved')
    if args.classes < args.modules or args.modules < 1:
        raise ValueError('At least one class per module is required')
    args.output.mkdir(parents=True)
    quote = lambda x: json.dumps(str(x)).replace('$', '\\$')
    repositories = f'maven {{ url = uri({quote(args.repository.resolve())}) }}; google(); mavenCentral(); gradlePluginPortal()'
    modules = [f'm{i:02}' for i in range(args.modules)]
    (args.output / 'settings.gradle.kts').write_text('pluginManagement { repositories { ' + repositories + ' }\n'
        + 'resolutionStrategy { eachPlugin { if (requested.id.id.startsWith("org.jetbrains.kotlin.")) useModule("org.jetbrains.kotlin:kotlin-gradle-plugin:" + requested.version); if (requested.id.id == "io.jankhunter.android") useModule("io.jankhunter:jankhunter-gradle-plugin:" + requested.version) } } }\n'
        + 'dependencyResolutionManagement { repositories { ' + repositories + ' } }\n'
        + 'rootProject.name = "MetadataBuildCorpus"\ninclude(' + ','.join(quote(':' + x) for x in modules) + ')\n')
    (args.output / 'build.gradle.kts').write_text('plugins {\n'
        + f' id("com.android.library") version {quote(args.agp)} apply false\n'
        + f' id("org.jetbrains.kotlin.android") version {quote(args.kotlin)} apply false\n'
        + f' id("io.jankhunter.android") version {quote(args.version)} apply false\n}}\n')
    (args.output / 'gradle.properties').write_text('android.useAndroidX=true\norg.gradle.jvmargs=-Xmx4g\norg.gradle.workers.max=4\norg.gradle.parallel=true\n')
    write_dependency(args.output, 1)
    generated = 0
    for i, module in enumerate(modules):
        count = args.classes // args.modules + (i < args.classes % args.modules)
        root = args.output / module
        source = root / 'src/main/java/fixture/metadata'
        source.mkdir(parents=True)
        (root / 'src/main/AndroidManifest.xml').write_text('<manifest/>\n')
        (root / 'build.gradle.kts').write_text('plugins { id("com.android.library"); id("org.jetbrains.kotlin.android"); id("io.jankhunter.android") }\n'
            + f'android {{ namespace = "fixture.metadata.m{i}"; compileSdk = {args.compile_sdk}; defaultConfig {{ minSdk = 28 }}; compileOptions {{ sourceCompatibility = JavaVersion.VERSION_1_8; targetCompatibility = JavaVersion.VERSION_1_8 }} }}\n'
            + 'kotlin { compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_1_8) } }\n'
            + 'jankHunter { profile.set(io.jankhunter.gradle.JankHunterProfile.FULL); autoInit.set(false) }\n'
            + f'dependencies {{ implementation("io.jankhunter:jankhunter-runtime:{getattr(args, "sdk_version", None) or args.version}"); implementation(files(rootProject.file("dependency.jar"))) }}\n')
        # One source shard per 100 classes keeps corpus generation and file watching bounded.
        shard_size = min(100, max(1, count // 2))
        for start in range(0, count, shard_size):
            indexes = range(start, min(start + shard_size, count))
            if (start // shard_size) % 2 == 0:
                content = f'package fixture.metadata.m{i};\n' + ''.join(f'class C{n} {{ int value(int x) {{ return x + {n}; }} }}\n' for n in indexes)
                (source / f'Corpus{start}.java').write_text(content)
            else:
                content = f'package fixture.metadata.m{i}\n' + ''.join(f'class C{n} {{ fun value(x: Int): Int = x + {n} }}\n' for n in indexes)
                (source / f'Corpus{start}.kt').write_text(content)
        generated += count
    (args.output / '.metadata-corpus.json').write_text(json.dumps({'classes': generated, 'modules': args.modules,
        'agp': args.agp, 'kotlin': args.kotlin, 'plugin_version': args.version, 'repository': str(args.repository.resolve())}, indent=2) + '\n')
    print(json.dumps({'output': str(args.output), 'classes': generated, 'modules': args.modules}))
    return 0



def edit_source(source, scenario):
    text = source.read_text()
    if scenario == 'signature-edit':
        match = re.search(r'value(\d*)\(int x\)', text)
        if not match:
            raise ValueError(f'No signature edit target in {source}')
        version = int(match[1] or '0') + 1
        changed = text[:match.start()] + f'value{version}(int x)' + text[match.end():]
    else:
        match = re.search(r'x \+ (\d+)', text)
        if not match:
            raise ValueError(f'No body edit target in {source}')
        number = int(match[1])
        changed = text[:match.start(1)] + str(number + 1) + text[match.end(1):]
    if changed == text:
        raise ValueError(f'Edit did not change {source}')
    source.write_text(changed)


def prepare(args):
    command = [str(args.gradle), '--offline', '--console=plain', '--configuration-cache', '--build-cache']
    if args.scenario == 'clean':
        subprocess.run(command + ['clean'], cwd=args.corpus, check=True)
    elif args.scenario == 'cache-restore':
        subprocess.run(command + ['assembleDebug'], cwd=args.corpus, check=True)
        subprocess.run(command + ['clean'], cwd=args.corpus, check=True)
    elif args.scenario == 'dependency-edit':
        subprocess.run(command + ['assembleDebug'], cwd=args.corpus, check=True)
        version = int((args.corpus / '.dependency-version').read_text()) + 1
        write_dependency(args.corpus, version)
    elif args.scenario != 'no-op':
        subprocess.run(command + ['assembleDebug'], cwd=args.corpus, check=True)
        sources = args.corpus / 'm00/src/main/java/fixture/metadata'
        candidates = sorted(sources.glob('*.kt' if args.scenario == 'kotlin-edit' else '*.java'))
        if not candidates:
            raise ValueError('Mixed corpus edit source is missing')
        edit_source(candidates[0], args.scenario)
    return 0


def plan(args):
    repositories = {'A': args.baseline_repository.resolve(), 'B': args.candidate_repository.resolve()}
    versions = {'A': args.baseline_version, 'B': args.candidate_version}
    def plugin(repo, version):
        return repo / f'io/jankhunter/jankhunter-gradle-plugin/{version}/jankhunter-gradle-plugin-{version}.jar'
    if file_digest(plugin(repositories['A'], versions['A'])) == file_digest(plugin(repositories['B'], versions['B'])):
        raise ValueError('A/B require different plugin artifacts')
    if args.output.exists():
        raise ValueError('Plan output must be new; existing evidence is preserved')
    args.output.mkdir(parents=True)
    artifacts = []
    configs = {}
    snapshot_script = args.output.resolve() / 'harness.py'
    shutil.copyfile(Path(__file__).resolve(), snapshot_script)
    script = str(snapshot_script)
    artifacts.append({'path': script, 'sha256': file_digest(snapshot_script)})
    for label in ('A', 'B'):
        snapshot = args.output / f'repository-{label}'
        group = snapshot / 'io/jankhunter'
        # Both sides use exactly the baseline SDK; only the build plugin differs.
        shutil.copytree(repositories['A'] / 'io/jankhunter', group)
        if label == 'B':
            destination = group / f'jankhunter-gradle-plugin/{versions[label]}'
            if destination.exists():
                shutil.rmtree(destination)
            shutil.copytree(repositories['B'] / f'io/jankhunter/jankhunter-gradle-plugin/{versions[label]}', destination)
            annotations = repositories['B'] / f'io/jankhunter/jankhunter-annotations/{versions[label]}'
            if versions['A'] != versions['B'] and annotations.is_dir():
                shutil.copytree(annotations, group / f'jankhunter-annotations/{versions[label]}')
        for artifact in sorted(snapshot.rglob('*')):
            if artifact.is_file():
                artifacts.append({'path': str(artifact), 'sha256': file_digest(artifact)})
        root = args.output / f'corpus-{label}'
        corpus(argparse.Namespace(output=root, repository=snapshot, version=versions[label],
            sdk_version=versions['A'], classes=args.classes, modules=args.modules, agp=args.agp,
            kotlin=args.kotlin, compile_sdk=args.compile_sdk))
        command = [str(args.gradle.resolve()), 'assembleDebug', '--offline', '--console=plain',
                   '--configuration-cache', '--build-cache', '--profile']
        configs[label] = {'cwd': str(root), 'argv': command}
    scenarios = []
    for name in args.scenarios:
        scenario = {'name': name}
        for label in ('A', 'B'):
            config = dict(configs[label])
            if name == 'clean':
                config['argv'] = config['argv'] + ['--no-build-cache']
            config['warmup'] = [config['argv']]
            config['prepare'] = [[sys.executable, script, 'prepare', '--corpus', config['cwd'],
                '--scenario', name, '--gradle', str(args.gradle.resolve())]]
            scenario[label] = config
        scenarios.append(scenario)
    manifest = {'artifacts': artifacts, 'identity': {'baseline_version': versions['A'],
        'candidate_version': versions['B'], 'sdk_version': versions['A'], 'classes': args.classes,
        'modules': args.modules, 'gradle': str(args.gradle.resolve()), 'java_home': __import__('os').environ.get('JAVA_HOME')},
        'scenarios': scenarios}
    (args.output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    return 0


def main():
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog='measure requires a JSON manifest with immutable artifact SHA256 digests and paired A/B argv arrays per scenario.',
    )
    modes = parser.add_subparsers(dest='mode', required=True)
    run = modes.add_parser('measure', help='Compare current (A) and candidate (B) argv manifests; needs --pairs>=10 for PASS/FAIL')
    run.add_argument('--manifest', type=Path, required=True)
    run.add_argument('--output', type=Path, required=True)
    run.add_argument('--pairs', type=int, default=10)
    run.add_argument('--timeout', type=int, default=3600)
    generate = modes.add_parser('corpus', help='Generate a fresh mixed Android library corpus; does not execute Gradle')
    generate.add_argument('--output', type=Path, required=True)
    generate.add_argument('--repository', type=Path, required=True)
    generate.add_argument('--version', default='1.0.13')
    generate.add_argument('--agp', default='8.13.2')
    generate.add_argument('--kotlin', default='2.3.20')
    generate.add_argument('--compile-sdk', type=int, default=36)
    generate.add_argument('--classes', type=int, default=100000)
    generate.add_argument('--modules', type=int, default=20)
    planning = modes.add_parser('plan', help='Snapshot distinct A/B plugins with identical SDK and generate reproducible scenarios')
    planning.add_argument('--baseline-repository', type=Path, required=True)
    planning.add_argument('--candidate-repository', type=Path, required=True)
    planning.add_argument('--baseline-version', required=True)
    planning.add_argument('--candidate-version', required=True)
    planning.add_argument('--output', type=Path, required=True)
    planning.add_argument('--gradle', type=Path, required=True)
    planning.add_argument('--classes', type=int, default=100000)
    planning.add_argument('--modules', type=int, default=20)
    planning.add_argument('--agp', default='8.13.2')
    planning.add_argument('--kotlin', default='2.3.20')
    planning.add_argument('--compile-sdk', type=int, default=36)
    names = ('clean', 'no-op', 'java-edit', 'kotlin-edit', 'signature-edit', 'dependency-edit', 'cache-restore')
    planning.add_argument('--scenarios', nargs='+', choices=names, default=list(names))
    preparation = modes.add_parser('prepare', help='Prepare an untimed, reproducible build scenario')
    preparation.add_argument('--corpus', type=Path, required=True)
    preparation.add_argument('--gradle', type=Path, required=True)
    preparation.add_argument('--scenario', choices=names, required=True)
    args = parser.parse_args()
    if args.mode == 'measure' and (args.pairs < 1 or args.timeout < 1):
        parser.error('pairs and timeout must be positive')
    try:
        return {'measure': measure, 'corpus': corpus, 'plan': plan, 'prepare': prepare}[args.mode](args)
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        parser.exit(2, str(error) + '\n')


if __name__ == '__main__':
    raise SystemExit(main())
