#!/usr/bin/env python3
"""Validate the device SDK's actual classfile metadata and R8 diagnostics.

This is a publication-boundary check, not a replacement for consumer dex/ART tests.
Missing/empty inputs and metadata diagnostics fail closed, including R8 exit zero.
"""
import argparse
import io
import json
from pathlib import Path
import re
import struct
import sys
import zipfile


class Reader:
    def __init__(self, data):
        self.data = data
        self.at = 0

    def take(self, size):
        end = self.at + size
        if end > len(self.data):
            raise ValueError('truncated classfile')
        value = self.data[self.at:end]
        self.at = end
        return value

    def u1(self):
        return self.take(1)[0]

    def u2(self):
        return struct.unpack('>H', self.take(2))[0]

    def u4(self):
        return struct.unpack('>I', self.take(4))[0]


def metadata_versions(data):
    r = Reader(data)
    if r.u4() != 0xCAFEBABE:
        raise ValueError('invalid classfile magic')
    r.u2()
    major = r.u2()
    cp = [None] * r.u2()
    index = 1
    while index < len(cp):
        tag = r.u1()
        if tag == 1:
            cp[index] = r.take(r.u2()).decode('utf-8', errors='replace')
        elif tag == 3:
            cp[index] = struct.unpack('>i', r.take(4))[0]
        elif tag in (4, 9, 10, 11, 12, 17, 18):
            r.take(4)
        elif tag in (5, 6):
            r.take(8)
            index += 1
        elif tag in (7, 8, 16, 19, 20):
            r.take(2)
        elif tag == 15:
            r.take(3)
        else:
            raise ValueError(f'unsupported constant pool tag {tag}')
        index += 1

    def value(a):
        tag = chr(a.u1())
        if tag in 'BCDFIJSZs':
            return cp[a.u2()]
        if tag == 'e':
            a.take(4)
        elif tag == 'c':
            a.take(2)
        elif tag == '@':
            annotation(a)
        elif tag == '[':
            return [value(a) for _ in range(a.u2())]
        else:
            raise ValueError(f'unsupported annotation tag {tag}')
        return None

    def annotation(a):
        descriptor = cp[a.u2()]
        fields = {}
        for _ in range(a.u2()):
            name = cp[a.u2()]
            fields[name] = value(a)
        return descriptor, fields

    def attributes():
        result = []
        for _ in range(r.u2()):
            name = cp[r.u2()]
            data = r.take(r.u4())
            result.append((name, data))
        return result

    r.take(6)
    r.take(2 * r.u2())
    for _ in range(2):
        for _ in range(r.u2()):
            r.take(6)
            attributes()
    versions = []
    for name, payload in attributes():
        if name not in ('RuntimeVisibleAnnotations', 'RuntimeInvisibleAnnotations'):
            continue
        a = Reader(payload)
        for _ in range(a.u2()):
            descriptor, fields = annotation(a)
            if descriptor == 'Lkotlin/Metadata;':
                mv = fields.get('mv')
                if not isinstance(mv, list) or len(mv) < 2 or not all(type(v) is int for v in mv):
                    raise ValueError('invalid Kotlin metadata version')
                versions.append(tuple(mv))
    return major, versions


def classfiles(data, label):
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        for entry in archive.infolist():
            if entry.is_dir():
                continue
            if entry.filename.endswith('.class'):
                yield f'{label}!{entry.filename}', archive.read(entry)
            elif entry.filename.endswith('.jar'):
                yield from classfiles(archive.read(entry), f'{label}!{entry.filename}')


def validate_artifact(path, maximum=(1, 9), max_bytecode=52):
    count = 0
    kotlin = 0
    errors = []
    for label, data in classfiles(Path(path).read_bytes(), str(path)):
        count += 1
        major, versions = metadata_versions(data)
        if major > max_bytecode:
            errors.append(f'{label}: classfile {major} exceeds {max_bytecode}')
        for version in versions:
            kotlin += 1
            if version[:2] > maximum:
                errors.append(f'{label}: Kotlin metadata {version} exceeds {maximum}')
    if count == 0:
        errors.append(f'{path}: no classfiles; artifact boundary not observed')
    return {'classes': count, 'kotlin_classes': kotlin, 'errors': errors}


def validate_r8_log(path):
    data = Path(path).read_text(errors='replace')
    if not data.strip():
        return ['empty R8 log; no evidence']
    # Metadata/frame diagnostics can span lines; some R8 versions still return zero.
    patterns = (
        r'unsupported (?:kotlin[ .])?metadata',
        r'malformed kotlin[ .]metadata',
        r'provided metadata[^\n]*version[^\n]*maximum supported version',
        r'(?:error|warning|failed|failure)[^\n]*kotlin metadata',
        r'kotlin metadata[^\n]*(?:error|warning|failed|failure|unsupported)',
        r'parsing[ /-]*(?:or[ /-]*)?rewriting kotlin',
        r'should never be called',
        r'invalid stack[ -]?map table',
        r'unexpected read of missing local',
        r'compilation failed with an internal error',
    )
    return ['R8 metadata/bytecode diagnostic'] if any(re.search(p, data, re.I) for p in patterns) else []


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--artifact', action='append', default=[])
    parser.add_argument('--r8-log', action='append', default=[])
    parser.add_argument('--max-metadata', default='1.9')
    args = parser.parse_args()
    if not args.artifact and not args.r8_log:
        parser.error('at least one artifact or R8 log is required')
    reports = {}
    errors = []
    maximum = tuple(int(v) for v in args.max_metadata.split('.'))
    try:
        for path in args.artifact:
            result = validate_artifact(path, maximum)
            reports[path] = result
            errors.extend(result['errors'])
        for path in args.r8_log:
            errors.extend(f'{path}: {e}' for e in validate_r8_log(path))
    except (OSError, ValueError, zipfile.BadZipFile, struct.error, IndexError) as error:
        errors.append(f'invalid or missing input: {error}')
    print(json.dumps({'artifacts': reports, 'errors': errors, 'pass': not errors}, indent=2))
    return 1 if errors else 0


if __name__ == '__main__':
    sys.exit(main())
