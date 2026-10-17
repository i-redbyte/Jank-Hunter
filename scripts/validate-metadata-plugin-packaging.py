#!/usr/bin/env python3
"""Validate private reader resources and the actual published plugin dependency graph."""
import argparse
import hashlib
import json
import sys
import zipfile
import xml.etree.ElementTree as ET
from pathlib import Path


FORBIDDEN = {'kotlin-metadata-jvm', 'kotlin-compiler-embeddable', 'kotlin-reflect', 'metadata-reader'}


def dependency(group, name, version):
    if name in FORBIDDEN or (group == 'org.jetbrains.kotlin' and name.startswith('kotlin-stdlib') and not version.startswith('1.9.')):
        raise ValueError(f'Private reader dependency leaked into published graph: {group}:{name}:{version}')


def validate_dependencies(module):
    for variant in module['variants']:
        for item in variant.get('dependencies', []):
            version = item.get('version', {})
            dependency(item['group'], item['module'], version.get('requires', version.get('strictly', '')))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('plugin_jar', type=Path)
    parser.add_argument('--module', type=Path, required=True, help='Exact published Gradle module metadata')
    parser.add_argument('--pom', type=Path, required=True, help='Exact published Maven POM')
    args = parser.parse_args()
    prefix = 'META-INF/jankhunter/metadata-reader/'
    with zipfile.ZipFile(args.plugin_jar) as archive:
        names = set(archive.namelist())
        jars = {name for name in names if name.startswith(prefix) and name.endswith('.jar')}
        if not jars:
            raise ValueError('Private reader JAR resources are missing')
        if any(name.startswith('kotlin/') and name.endswith('.class') for name in names):
            raise ValueError('Kotlin implementation classes leaked into plugin JAR root')
        properties = dict(line.split('=', 1) for line in archive.read(prefix + 'capabilities.properties').decode().splitlines()
                          if line and not line.startswith('#') and '=' in line)
        declared = {prefix + key[4:] for key in properties if key.startswith('jar.')}
        if jars != declared:
            raise ValueError('Capability descriptor does not match bundled reader JARs')
        for name in jars:
            if hashlib.sha256(archive.read(name)).hexdigest() != properties['jar.' + name.removeprefix(prefix)]:
                raise ValueError('Private reader digest mismatch')
    validate_dependencies(json.loads(args.module.read_text()))
    ns = {'m': 'http://maven.apache.org/POM/4.0.0'}
    for item in ET.parse(args.pom).findall('./m:dependencies/m:dependency', ns):
        dependency(item.findtext('m:groupId', '', ns), item.findtext('m:artifactId', '', ns), item.findtext('m:version', '', ns))
    print(json.dumps({'status': 'PASS', 'plugin_jar': str(args.plugin_jar), 'private_jars': len(jars),
                      'module': str(args.module), 'pom': str(args.pom)}))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, zipfile.BadZipFile) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
